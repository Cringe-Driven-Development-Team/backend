package grpcdelivery_test

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	identityv1 "github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/gen/cellestial/identity/v1"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/app/identity"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/observability"
	authredis "github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/user/adapters/redis"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/user/adapters/security"
	grpcdelivery "github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/user/delivery/grpc"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/user/usecase"
	goredis "github.com/redis/go-redis/v9"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// safeLogBuffer позволяет читать итоговые логи параллельно с gRPC server.
type safeLogBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

// verifyFunc задаёт только прикладной исход; RPC и TLS в тестах остаются настоящими.
type verifyFunc func(context.Context, string) (usecase.ValidatedSession, error)

// Verify реализует порт прикладной проверки сессии.
func (f verifyFunc) Verify(ctx context.Context, token string) (usecase.ValidatedSession, error) {
	return f(ctx, token)
}

// TestSessionRPCOutcomes закрепляет validation, recovery и correlation в ошибочных ответах.
func TestSessionRPCOutcomes(t *testing.T) {
	for _, tc := range []struct {
		name   string
		token  string
		verify verifyFunc
		want   codes.Code
	}{
		{name: "empty token", want: codes.InvalidArgument},
		{name: "oversized token", token: strings.Repeat("s", 8193), want: codes.InvalidArgument},
		{name: "dependency failure", token: "private-token", verify: func(context.Context, string) (usecase.ValidatedSession, error) {
			return usecase.ValidatedSession{}, errors.New("private-storage-error")
		}, want: codes.Unavailable},
		{name: "dependency deadline", token: "private-token", verify: func(context.Context, string) (usecase.ValidatedSession, error) {
			return usecase.ValidatedSession{}, fmt.Errorf("private-storage-error: %w", context.DeadlineExceeded)
		}, want: codes.Unavailable},
		{name: "dependency cancellation", token: "private-token", verify: func(context.Context, string) (usecase.ValidatedSession, error) {
			return usecase.ValidatedSession{}, fmt.Errorf("private-storage-error: %w", context.Canceled)
		}, want: codes.Unavailable},
		{name: "panic", token: "private-token", verify: func(context.Context, string) (usecase.ValidatedSession, error) { panic("private-panic-payload") }, want: codes.Internal},
	} {
		t.Run(tc.name, func(t *testing.T) {
			logs := &safeLogBuffer{}
			verifier := tc.verify
			if verifier == nil {
				verifier = func(context.Context, string) (usecase.ValidatedSession, error) {
					t.Error("invalid input reached usecase")
					return usecase.ValidatedSession{}, nil
				}
			}
			client, _ := sessionClient(t, verifier, newPKI(t), logs)
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			ctx = metadata.NewOutgoingContext(ctx, metadata.Pairs("x-request-id", "error-correlation"))
			var header metadata.MD
			_, err := client.ValidateSession(ctx, &identityv1.ValidateSessionRequest{AccessToken: tc.token}, grpc.Header(&header))
			if status.Code(err) != tc.want {
				t.Fatalf("status=%v; want %v", err, tc.want)
			}
			if got := header.Get("x-request-id"); len(got) != 1 || got[0] != "error-correlation" {
				t.Fatalf("missing error correlation: %v", got)
			}
			text := logs.String()
			if strings.Count(text, `"msg":"rpc.request"`) != 1 || !strings.Contains(text, `"status":"`+tc.want.String()+`"`) {
				t.Fatalf("wrong final log: %s", text)
			}
			for _, secret := range []string{"private-token", "private-storage-error", "private-panic-payload"} {
				if strings.Contains(text, secret) || strings.Contains(err.Error(), secret) {
					t.Fatalf("secret leaked: %s", secret)
				}
			}
		})
	}
}

// TestSessionRPCContext проверяет передачу correlation и отмены до прикладного обработчика.
func TestSessionRPCContext(t *testing.T) {
	observed := make(chan error, 1)
	verifier := verifyFunc(func(ctx context.Context, _ string) (usecase.ValidatedSession, error) {
		if observability.RequestID(ctx) != "cancel-correlation" {
			observed <- errors.New("request context lost")
			return usecase.ValidatedSession{}, context.Canceled
		}
		<-ctx.Done()
		observed <- ctx.Err()
		return usecase.ValidatedSession{}, ctx.Err()
	})
	client, _ := sessionClient(t, verifier, newPKI(t), &safeLogBuffer{})
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	ctx = metadata.NewOutgoingContext(ctx, metadata.Pairs("x-request-id", "cancel-correlation"))
	_, err := client.ValidateSession(ctx, &identityv1.ValidateSessionRequest{AccessToken: "private-token"})
	if status.Code(err) != codes.DeadlineExceeded {
		t.Fatalf("status=%v; want deadline", err)
	}
	select {
	case err := <-observed:
		if !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("cancel did not reach usecase")
	}
}

// Write сохраняет одну часть структурированного лога.
func (b *safeLogBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.Write(p)
}

// String возвращает согласованный снимок записей.
func (b *safeLogBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.String()
}

// testPKI выпускает только одноразовые сертификаты для transport-тестов.
type testPKI struct {
	ca   *x509.Certificate
	key  *ecdsa.PrivateKey
	pool *x509.CertPool
}

// newPKI создаёт отдельный доверенный CA каждого теста.
func newPKI(t *testing.T) *testPKI {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	ca := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "test-only CA"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	der, err := x509.CreateCertificate(rand.Reader, ca, ca, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(parsed)
	return &testPKI{ca: parsed, key: key, pool: pool}
}

// certificate выдаёт leaf с точным DNS SAN и назначением client/server.
func (p *testPKI) certificate(t *testing.T, name string, usage ...x509.ExtKeyUsage) tls.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		t.Fatal(err)
	}
	leaf := &x509.Certificate{SerialNumber: serial, DNSNames: []string{name}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: usage}
	der, err := x509.CreateCertificate(rand.Reader, leaf, p.ca, &key.PublicKey, p.key)
	if err != nil {
		t.Fatal(err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
}

// sessionConfig записывает одноразовые TLS credentials в приватный каталог теста.
func sessionConfig(t *testing.T, pki *testPKI) identity.SessionConfig {
	t.Helper()
	cert := pki.certificate(t, "identity.internal.cellestial.ru", x509.ExtKeyUsageServerAuth)
	keyDER, err := x509.MarshalPKCS8PrivateKey(cert.PrivateKey)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for name, block := range map[string]*pem.Block{"server.crt": {Type: "CERTIFICATE", Bytes: cert.Certificate[0]}, "server.key": {Type: "PRIVATE KEY", Bytes: keyDER}, "ca.crt": {Type: "CERTIFICATE", Bytes: pki.ca.Raw}} {
		if err = os.WriteFile(filepath.Join(dir, name), pem.EncodeToMemory(block), 0600); err != nil {
			t.Fatal(err)
		}
	}
	return identity.SessionConfig{Version: "test", TLSCertFile: filepath.Join(dir, "server.crt"), TLSKeyFile: filepath.Join(dir, "server.key"), TLSCAFile: filepath.Join(dir, "ca.crt")}
}

// sessionClient поднимает настоящий TLS gRPC listener и клиент с выбранной identity.
//
//nolint:ireturn // Generated gRPC clients expose only the generated interface.
func sessionClient(t *testing.T, validator grpcdelivery.SessionVerifier, pki *testPKI, logs *safeLogBuffer) (identityv1.SessionClient, func(string, string, *testPKI) identityv1.SessionClient) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server, err := identity.NewSessionRPCServer(sessionConfig(t, pki), validator, slog.New(slog.NewJSONHandler(logs, nil)))
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)
	connect := func(name, serverName string, authority *testPKI) identityv1.SessionClient {
		clientTLS := &tls.Config{MinVersion: tls.VersionTLS13, RootCAs: pki.pool, ServerName: serverName, Certificates: []tls.Certificate{authority.certificate(t, name, x509.ExtKeyUsageClientAuth)}}
		conn, err := grpc.NewClient(listener.Addr().String(), grpc.WithTransportCredentials(credentials.NewTLS(clientTLS)))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = conn.Close() })
		return identityv1.NewSessionClient(conn)
	}
	return connect("workspace.internal.cellestial.ru", "identity.internal.cellestial.ru", pki), connect
}

// TestRunSessionRPC проверяет production wiring, Redis DB и остановку с зависшим handshake.
func TestRunSessionRPC(t *testing.T) {
	addr := os.Getenv("REDIS_TEST_ADDR")
	if addr == "" {
		t.Skip("set REDIS_TEST_ADDR for lifecycle integration")
	}
	pki := newPKI(t)
	files := sessionConfig(t, pki)
	reservation, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	grpcAddr := reservation.Addr().String()
	_ = reservation.Close()
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatal(err)
	}
	values := map[string]string{"GRPC_ADDR": grpcAddr, "JWT_SECRET": "run-test-only-secret", "TLS_CERT_FILE": files.TLSCertFile, "TLS_KEY_FILE": files.TLSKeyFile, "TLS_CA_FILE": files.TLSCAFile, "REDIS_HOST": host, "REDIS_PORT": port, "REDIS_DB": "1"}
	cfg, err := identity.LoadSessionConfig(func(name string) (string, bool) { v, ok := values[name]; return v, ok })
	if err != nil {
		t.Fatal(err)
	}
	redis := goredis.NewClient(&goredis.Options{Addr: addr, DB: 1, MaxRetries: -1})
	defer func() { _ = redis.Close() }()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	userID := time.Now().UnixNano()
	sid := strconv.FormatInt(userID, 10)
	store := authredis.NewStore(redis, "", time.Second)
	version, err := store.CurrentVersion(ctx, userID)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = redis.Del(context.Background(), "user_session_version:"+sid, "auth_session:"+sid, "refresh_session:"+sid).Err()
	}()
	issuer := security.NewAccessToken([]byte(values["JWT_SECRET"]), time.Minute)
	claims := usecase.SessionClaims{UserID: userID, SessionID: sid, Version: version}
	token, expiry, err := issuer.IssueAccess(claims)
	if err != nil {
		t.Fatal(err)
	}
	if err = store.Create(ctx, usecase.NewSession{RefreshSession: usecase.RefreshSession{SessionClaims: claims, TokenHash: sid, ExpiresAt: time.Now().Add(time.Minute)}, AccessExpiresAt: expiry}); err != nil {
		t.Fatal(err)
	}
	logs := &safeLogBuffer{}
	previous := slog.Default()
	slog.SetDefault(slog.New(observability.NewHandler(slog.NewJSONHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug}))))
	defer slog.SetDefault(previous)
	runCtx, stop := context.WithCancel(context.Background())
	defer stop()
	done := make(chan error, 1)
	go func() { done <- identity.RunSessionRPC(runCtx, cfg, slog.New(slog.NewJSONHandler(logs, nil))) }()
	conn, err := grpc.NewClient(grpcAddr, grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{MinVersion: tls.VersionTLS13, RootCAs: pki.pool, ServerName: "identity.internal.cellestial.ru", Certificates: []tls.Certificate{pki.certificate(t, "workspace.internal.cellestial.ru", x509.ExtKeyUsageClientAuth)}})))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	callCtx := metadata.NewOutgoingContext(ctx, metadata.Pairs("x-request-id", "run-correlation"))
	response, err := identityv1.NewSessionClient(conn).ValidateSession(callCtx, &identityv1.ValidateSessionRequest{AccessToken: token}, grpc.WaitForReady(true))
	if err != nil || response.GetUserId() != userID {
		t.Fatalf("RunSessionRPC did not validate configured Redis DB: %v, %v", response, err)
	}
	if text := logs.String(); !strings.Contains(text, `"operation":"redis.eval"`) || !strings.Contains(text, `"request_id":"run-correlation"`) {
		t.Error("missing correlated Redis storage log")
	}
	raw, err := net.DialTimeout("tcp", grpcAddr, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = raw.Close() }()
	_, _ = raw.Write([]byte{0x16})
	// Неполная TLS record удерживает handshake; даём Accept начать её обработку.
	time.Sleep(20 * time.Millisecond)
	stop()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		_ = raw.Close()
		<-done
		t.Fatal("shutdown blocked by incomplete TLS handshake")
	}
	if c, err := net.DialTimeout("tcp", grpcAddr, 100*time.Millisecond); err == nil {
		_ = c.Close()
		t.Fatal("gRPC listener left open after shutdown")
	}
}

// TestIdentitySessionMTLS проверяет реальные JWT, Redis, peer identity и безопасные ответы.
func TestIdentitySessionMTLS(t *testing.T) {
	addr := os.Getenv("REDIS_TEST_ADDR")
	if addr == "" {
		t.Skip("set REDIS_TEST_ADDR for real Redis and gRPC integration")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	redis := goredis.NewClient(&goredis.Options{Addr: addr, MaxRetries: -1})
	t.Cleanup(func() { _ = redis.Close() })
	if err := redis.Ping(ctx).Err(); err != nil {
		t.Fatal(err)
	}
	prefix := "identity-rpc-test:" + t.Name() + ":" + time.Now().Format("150405.000000000") + ":"
	store := authredis.NewStore(redis, prefix, time.Second)
	t.Cleanup(func() {
		cleanup := goredis.NewClient(&goredis.Options{Addr: addr, MaxRetries: -1})
		defer func() { _ = cleanup.Close() }()
		keys, err := cleanup.Keys(context.Background(), prefix+"*").Result()
		if err == nil && len(keys) > 0 {
			_ = cleanup.Del(context.Background(), keys...).Err()
		}
	})
	issuer := security.NewAccessToken([]byte("test-only-jwt-secret"), time.Minute)
	claims := usecase.SessionClaims{UserID: 42, SessionID: "session-42"}
	version, err := store.CurrentVersion(ctx, claims.UserID)
	if err != nil {
		t.Fatal(err)
	}
	claims.Version = version
	token, expires, err := issuer.IssueAccess(claims)
	if err != nil {
		t.Fatal(err)
	}
	if err = store.Create(ctx, usecase.NewSession{RefreshSession: usecase.RefreshSession{SessionClaims: claims, TokenHash: "test-refresh-hash", ExpiresAt: time.Now().Add(time.Hour)}, AccessExpiresAt: expires}); err != nil {
		t.Fatal(err)
	}
	logs := &safeLogBuffer{}
	pki := newPKI(t)
	client, connect := sessionClient(t, usecase.NewSessionValidator(issuer, store), pki, logs)
	callCtx := metadata.NewOutgoingContext(ctx, metadata.Pairs("x-request-id", "lesson-request-42"))
	var header metadata.MD
	response, err := client.ValidateSession(callCtx, &identityv1.ValidateSessionRequest{AccessToken: token}, grpc.Header(&header))
	if err != nil {
		t.Fatal(err)
	}
	if response.UserId != 42 || response.SessionId != "session-42" || response.SessionVersion != uint64(version) || !response.ExpiresAt.AsTime().Equal(expires) {
		t.Fatalf("unexpected session: %v", response)
	}
	if got := header.Get("x-request-id"); len(got) != 1 || got[0] != "lesson-request-42" {
		t.Fatalf("request id: %v", got)
	}
	for _, tc := range []struct {
		name   string
		client identityv1.SessionClient
		want   codes.Code
	}{
		{name: "wrong client SAN", client: connect("bff.internal.cellestial.ru", "identity.internal.cellestial.ru", pki), want: codes.PermissionDenied},
		{name: "wrong server SAN", client: connect("workspace.internal.cellestial.ru", "environments.internal.cellestial.ru", pki), want: codes.Unavailable},
		{name: "untrusted client CA", client: connect("workspace.internal.cellestial.ru", "identity.internal.cellestial.ru", newPKI(t)), want: codes.Unavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, callErr := tc.client.ValidateSession(callCtx, &identityv1.ValidateSessionRequest{AccessToken: token})
			if status.Code(callErr) != tc.want {
				t.Fatalf("status=%v; want %v", callErr, tc.want)
			}
		})
	}
	_, err = client.ValidateSession(callCtx, &identityv1.ValidateSessionRequest{AccessToken: "invalid-private-token"})
	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("bad JWT: %v", err)
	}
	if err = store.RevokeAll(ctx, 42); err != nil {
		t.Fatal(err)
	}
	_, err = client.ValidateSession(callCtx, &identityv1.ValidateSessionRequest{AccessToken: token})
	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("revoked JWT: %v", err)
	}
	if err = redis.Close(); err != nil {
		t.Fatal(err)
	}
	_, err = client.ValidateSession(callCtx, &identityv1.ValidateSessionRequest{AccessToken: token})
	if status.Code(err) != codes.Unavailable {
		t.Fatalf("Redis unavailable: %v", err)
	}
	if strings.Contains(err.Error(), "redis") || strings.Contains(err.Error(), token) {
		t.Fatalf("private dependency error leaked: %v", err)
	}
	text := logs.String()
	if !strings.Contains(text, "lesson-request-42") || !strings.Contains(text, `"service":"identity"`) {
		t.Fatalf("missing correlation/service: %s", text)
	}
	if strings.Contains(text, token) || strings.Contains(text, "invalid-private-token") {
		t.Fatal("access token leaked into logs")
	}
}
