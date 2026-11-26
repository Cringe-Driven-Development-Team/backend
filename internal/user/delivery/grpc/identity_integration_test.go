package grpcdelivery_test

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	identityv1 "github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/gen/cellestial/identity/v1"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/app/identity"
	identitymigrations "github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/migrations/identity"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pquerna/otp/totp"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	healthv1 "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// TestIdentityService проверяет полный production wiring по HTTPS/gRPC с ограниченной DB-role.
func TestIdentityService(t *testing.T) {
	dbURL, redisAddr := os.Getenv("IDENTITY_TEST_DATABASE_URL"), os.Getenv("REDIS_TEST_ADDR")
	if dbURL == "" || redisAddr == "" {
		t.Skip("set IDENTITY_TEST_DATABASE_URL and REDIS_TEST_ADDR for full Identity integration")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	admin, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	name := fmt.Sprintf("identity_test_%d", time.Now().UnixNano())
	ident := pgx.Identifier{name}.Sanitize()
	if _, err = admin.Exec(ctx, "CREATE DATABASE "+ident); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = admin.Exec(context.Background(), "DROP DATABASE "+ident+" WITH (FORCE)")
		_, _ = admin.Exec(context.Background(), "DROP ROLE IF EXISTS "+ident)
	}()
	parsed, err := url.Parse(dbURL)
	if err != nil {
		t.Fatal(err)
	}
	parsed.Path = "/" + name
	owner, err := pgxpool.New(ctx, parsed.String())
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	if err = identitymigrations.Up(ctx, owner); err != nil {
		t.Fatal(err)
	}
	// Уникальный диапазон ID не делит Redis account rate limits с прошлым запуском.
	if _, err = owner.Exec(ctx, "ALTER SEQUENCE identity.users_id_seq RESTART WITH "+strconv.FormatInt(time.Now().UnixMicro(), 10)); err != nil {
		t.Fatal(err)
	}
	// Повтор миграции не изменяет схему и её версию.
	if err = identitymigrations.Up(ctx, owner); err != nil {
		t.Fatal(err)
	}
	if _, err = admin.Exec(ctx, "CREATE ROLE "+ident+" LOGIN PASSWORD 'test-only-role-password'"); err != nil {
		t.Fatal(err)
	}

	for _, sql := range []string{"GRANT USAGE ON SCHEMA identity TO " + ident, "GRANT SELECT,INSERT,UPDATE,DELETE ON identity.users,identity.user_deletions TO " + ident, "GRANT SELECT ON identity.goose_db_version TO " + ident, "GRANT USAGE,SELECT ON ALL SEQUENCES IN SCHEMA identity TO " + ident, "CREATE SCHEMA workspace", "CREATE TABLE workspace.private_data(id bigint)"} {
		if _, err = owner.Exec(ctx, sql); err != nil {
			t.Fatal(err)
		}
	}
	parsed.User = url.UserPassword(name, "test-only-role-password")
	appDB, err := pgxpool.New(ctx, parsed.String())
	if err != nil {
		t.Fatal(err)
	}
	defer appDB.Close()
	if _, err = appDB.Exec(ctx, "SELECT * FROM workspace.private_data"); err == nil {
		t.Fatal("Identity role can read Workspace")
	}
	if _, err = owner.Exec(ctx, "GRANT CREATE ON SCHEMA identity TO "+ident); err != nil {
		t.Fatal(err)
	}
	if err = identitymigrations.Check(ctx, appDB); err == nil {
		t.Fatal("application role with DDL privileges accepted")
	}
	if _, err = owner.Exec(ctx, "REVOKE CREATE ON SCHEMA identity FROM "+ident); err != nil {
		t.Fatal(err)
	}
	if _, err = owner.Exec(ctx, "REVOKE DELETE ON identity.users FROM "+ident); err != nil {
		t.Fatal(err)
	}
	if err = identitymigrations.Check(ctx, appDB); err == nil {
		t.Fatal("application role with incomplete DML privileges accepted")
	}
	if _, err = owner.Exec(ctx, "GRANT DELETE ON identity.users TO "+ident); err != nil {
		t.Fatal(err)
	}
	var ownerName string
	if err = owner.QueryRow(ctx, "SELECT current_user").Scan(&ownerName); err != nil {
		t.Fatal(err)
	}
	for _, sql := range []string{"GRANT CREATE ON SCHEMA identity TO " + ident, "ALTER TABLE identity.users OWNER TO " + ident, "REVOKE CREATE ON SCHEMA identity FROM " + ident} {
		if _, err = owner.Exec(ctx, sql); err != nil {
			t.Fatal(err)
		}
	}
	if err = identitymigrations.Check(ctx, appDB); err == nil {
		t.Fatal("application table owner accepted")
	}
	if _, err = owner.Exec(ctx, "ALTER TABLE identity.users OWNER TO "+pgx.Identifier{ownerName}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	// ALTER OWNER переписывает ACL прежнего владельца; восстанавливаем только app grants.
	for _, sql := range []string{"GRANT SELECT,INSERT,UPDATE,DELETE ON identity.users TO " + ident, "GRANT USAGE ON SEQUENCE identity.users_id_seq TO " + ident} {
		if _, err = owner.Exec(ctx, sql); err != nil {
			t.Fatal(err)
		}
	}
	pki := newPKI(t)
	session := sessionConfig(t, pki)
	host, port, err := net.SplitHostPort(redisAddr)
	if err != nil {
		t.Fatal(err)
	}
	env := map[string]string{"DATABASE_URL": parsed.String(), "JWT_SECRET": "full-service-jwt-secret", "BFF_API_KEY": "full-service-bff-key", "TOTP_ENCRYPTION_KEY": "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=", "TLS_CERT_FILE": session.TLSCertFile, "TLS_KEY_FILE": session.TLSKeyFile, "TLS_CA_FILE": session.TLSCAFile, "REDIS_HOST": host, "REDIS_PORT": port, "REDIS_DB": "2", "S3_AVATARS_BUCKET": "avatars", "S3_AVATARS_PUBLIC_URL": "https://cdn.example/avatars"}
	cfg, err := identity.LoadConfig(func(name string) (string, bool) { v, ok := env[name]; return v, ok })
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("AWS_REGION", "us-east-1")
	t.Setenv("AWS_ACCESS_KEY_ID", "test-only")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "test-only")
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")
	logs := &safeLogBuffer{}
	service, err := identity.Build(ctx, cfg, slog.New(slog.NewJSONHandler(logs, nil)))
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	listen := func() net.Listener {
		l, e := net.Listen("tcp", "127.0.0.1:0")
		if e != nil {
			t.Fatal(e)
		}
		return l
	}
	httpListener, rpcListener, adminListener := listen(), listen(), listen()
	runCtx, stop := context.WithCancel(ctx)
	defer stop()
	done := make(chan error, 1)
	go func() { done <- service.Serve(runCtx, httpListener, rpcListener, adminListener) }()
	defer func() {
		stop()
		select {
		case e := <-done:
			if e != nil {
				t.Error(e)
			}
		case <-time.After(5 * time.Second):
			t.Error("Identity did not stop")
		}
	}()
	tlsFor := func(name string) *tls.Config {
		return &tls.Config{MinVersion: tls.VersionTLS13, RootCAs: pki.pool, ServerName: "identity.internal.cellestial.ru", Certificates: []tls.Certificate{pki.certificate(t, name, x509.ExtKeyUsageClientAuth)}}
	}
	tr := &http.Transport{TLSClientConfig: tlsFor("bff.internal.cellestial.ru")}
	defer tr.CloseIdleConnections()
	client := &http.Client{Transport: tr, Timeout: 10 * time.Second}
	request := func(method, path, key, bearer, body string, want int) map[string]any {
		t.Helper()
		r, e := http.NewRequestWithContext(ctx, method, "https://"+httpListener.Addr().String()+path, strings.NewReader(body))
		if e != nil {
			t.Fatal(e)
		}
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-BFF-Key", key)
		r.Header.Set("X-Request-ID", "full-identity-correlation")
		if bearer != "" {
			r.Header.Set("Authorization", "Bearer "+bearer)
		}
		resp, e := client.Do(r)
		if e != nil {
			t.Fatal(e)
		}
		defer resp.Body.Close()
		raw, e := io.ReadAll(resp.Body)
		if e != nil {
			t.Fatal(e)
		}
		if resp.StatusCode != want {
			t.Fatalf("%s %s: status=%d want=%d", method, path, resp.StatusCode, want)
		}
		if resp.Header.Get("X-Request-ID") != "full-identity-correlation" {
			t.Fatal("HTTP correlation lost")
		}
		var value map[string]any
		if len(raw) > 0 {
			if e = json.Unmarshal(raw, &value); e != nil {
				t.Fatal(e)
			}
		}
		return value
	}
	for _, key := range []string{"", "wrong"} {
		v := request("POST", "/api/v1/auth/register", key, "", "{", 403)
		if v["code"] != "s2s_forbidden" {
			t.Fatal("wrong S2S error")
		}
	}
	badTransport := &http.Transport{TLSClientConfig: tlsFor("workspace.internal.cellestial.ru")}
	defer badTransport.CloseIdleConnections()
	badClient := &http.Client{Transport: badTransport, Timeout: time.Second}
	resp, e := badClient.Get("https://" + httpListener.Addr().String() + "/api/v1/auth/register")
	if e == nil {
		resp.Body.Close()
		t.Fatal("wrong service SAN accepted by HTTPS")
	}
	registered := request("POST", "/api/v1/auth/register", cfg.BFFAPIKey, "", `{"login":"reader","password":"long-test-password"}`, 201)
	token, ok := registered["tokens"].(map[string]any)["access_token"].(string)
	if !ok {
		t.Fatal("register missing access token")
	}
	profile := request("GET", "/api/v1/users/me", cfg.BFFAPIKey, token, "", 200)
	userID := int64(profile["id"].(float64))
	if profile["login"] != "reader" {
		t.Fatal("profile mismatch")
	}
	conn, err := grpc.NewClient(rpcListener.Addr().String(), grpc.WithTransportCredentials(credentials.NewTLS(tlsFor("workspace.internal.cellestial.ru"))))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	rpcCtx := metadata.NewOutgoingContext(ctx, metadata.Pairs("x-request-id", "full-identity-correlation"))
	// Не отправляем даже начальный Health/Watch request: server timeout обязан прервать RecvMsg.
	streamCtx, streamCancel := context.WithTimeout(context.Background(), 4*time.Second)
	stream, err := conn.NewStream(streamCtx, &grpc.StreamDesc{ServerStreams: true}, healthv1.Health_Watch_FullMethodName)
	if err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	err = stream.RecvMsg(&healthv1.HealthCheckResponse{})
	streamCancel()
	if status.Code(err) != codes.DeadlineExceeded || time.Since(started) > 3*time.Second {
		t.Fatalf("server stream budget not enforced: %v after %s", err, time.Since(started))
	}

	sessionClient := identityv1.NewSessionClient(conn)
	verified, err := sessionClient.ValidateSession(rpcCtx, &identityv1.ValidateSessionRequest{AccessToken: token})
	if err != nil || verified.GetUserId() != userID {
		t.Fatalf("session verification: %v", err)
	}
	users := identityv1.NewUsersClient(conn)
	batch, err := users.BatchGetUsers(rpcCtx, &identityv1.BatchGetUsersRequest{UserIds: []int64{userID, userID, 999999}})
	if err != nil || len(batch.GetUsers()) != 1 {
		t.Fatalf("batch lookup: %v", err)
	}
	batch, err = users.BatchResolveUsers(rpcCtx, &identityv1.BatchResolveUsersRequest{Logins: []string{"READER", "missing"}})
	if err != nil || len(batch.GetUsers()) != 1 {
		t.Fatalf("login resolution: %v", err)
	}
	_, err = users.BatchGetUsers(rpcCtx, &identityv1.BatchGetUsersRequest{UserIds: []int64{-1}})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatal("invalid batch accepted")
	}
	// Полный HTTP путь refresh/profile/2FA использует ту же Identity schema.
	refreshed := request("POST", "/api/v1/auth/refresh", cfg.BFFAPIKey, "", fmt.Sprintf(`{"refresh_token":%q}`, registered["tokens"].(map[string]any)["refresh_token"]), 200)
	token = refreshed["access_token"].(string)
	request("PATCH", "/api/v1/users/me", cfg.BFFAPIKey, token, `{"login":"reader_new"}`, 200)
	setup := request("POST", "/api/v1/auth/enable2fa", cfg.BFFAPIKey, token, "", 200)
	secret := setup["secret"].(string)
	code, e := totp.GenerateCode(secret, time.Now())
	if e != nil {
		t.Fatal(e)
	}
	request("POST", "/api/v1/auth/confirm2fa", cfg.BFFAPIKey, token, fmt.Sprintf(`{"user_code":%q}`, code), 200)
	request("POST", "/api/v1/auth/login", cfg.BFFAPIKey, "", `{"login":"reader_new","password":"long-test-password"}`, 412)
	request("POST", "/api/v1/auth/disable2fa", cfg.BFFAPIKey, token, `{"password":"long-test-password"}`, 200)
	request("PATCH", "/api/v1/users/me", cfg.BFFAPIKey, token, `{"login":"reader"}`, 200)
	request("POST", "/api/v1/auth/logout-all", cfg.BFFAPIKey, token, "", 204)
	_, err = sessionClient.ValidateSession(rpcCtx, &identityv1.ValidateSessionRequest{AccessToken: token})
	if status.Code(err) != codes.Unauthenticated {
		t.Fatal("revoked session remains valid")
	}
	request("GET", "/api/v1/users/me", cfg.BFFAPIKey, token, "", 401)
	request("POST", "/api/v1/auth/login", cfg.BFFAPIKey, "", `{"login":"reader","password":"long-test-password"}`, 200)
	loggedIn := request("POST", "/api/v1/auth/login", cfg.BFFAPIKey, "", `{"login":"reader","password":"long-test-password"}`, 200)
	loginToken := loggedIn["tokens"].(map[string]any)["access_token"].(string)
	request("PUT", "/api/v1/users/me/password", cfg.BFFAPIKey, loginToken, `{"current_password":"long-test-password","new_password":"new-long-test-password"}`, 204)
	request("GET", "/api/v1/users/me", cfg.BFFAPIKey, loginToken, "", 401)
	adminClient := &http.Client{Timeout: time.Second}
	for _, path := range []string{"/livez", "/readyz", "/metrics"} {
		r, e := adminClient.Get("http://" + adminListener.Addr().String() + path)
		if e != nil {
			t.Fatal(e)
		}
		raw, _ := io.ReadAll(r.Body)
		r.Body.Close()
		if r.StatusCode != 200 {
			t.Fatalf("admin %s=%d", path, r.StatusCode)
		}
		if path == "/metrics" {
			for _, name := range []string{"identity_build_info", "identity_http_requests_total", "identity_rpc_requests_total", "identity_db_acquired_connections", "go_goroutines", "process_cpu_seconds_total"} {
				if !strings.Contains(string(raw), name) {
					t.Errorf("missing metric %s", name)
				}
			}
			for _, secret := range []string{token, "reader", "full-identity-correlation"} {
				if strings.Contains(string(raw), secret) {
					t.Fatal("metric contains sensitive or unbounded label")
				}
			}
		}
	}
	if binary := os.Getenv("IDENTITY_TEST_BINARY"); binary != "" {
		t.Run("independent process", func(t *testing.T) { testIdentityProcess(t, binary, env, pki) })
	}
	for _, secret := range []string{token, secret, "long-test-password", "full-service-bff-key"} {
		if strings.Contains(logs.String(), secret) {
			t.Fatal("secret in service log")
		}
	}
}

// testIdentityProcess запускает собранный cmd/identity и проверяет HTTPS/gRPC и SIGTERM.
func testIdentityProcess(t *testing.T, binary string, env map[string]string, pki *testPKI) {
	t.Helper()
	addresses := make([]string, 3)
	for i := range addresses {
		l, e := net.Listen("tcp", "127.0.0.1:0")
		if e != nil {
			t.Fatal(e)
		}
		addresses[i] = l.Addr().String()
		_ = l.Close()
	}
	logs := &safeLogBuffer{}
	cmd := exec.Command(binary)
	cmd.Env = os.Environ()
	for k, v := range env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	cmd.Env = append(cmd.Env, "HTTP_ADDR="+addresses[0], "GRPC_ADDR="+addresses[1], "ADMIN_ADDR="+addresses[2], "SHUTDOWN_TIMEOUT=2s")
	cmd.Stdout = logs
	cmd.Stderr = logs
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	finished := false
	defer func() {
		if !finished {
			_ = cmd.Process.Kill()
			<-done
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	admin := &http.Client{Timeout: 200 * time.Millisecond}
	ready := false
	for !ready && ctx.Err() == nil {
		r, e := admin.Get("http://" + addresses[2] + "/readyz")
		if e == nil {
			ready = r.StatusCode == 200
			r.Body.Close()
		}
		if !ready {
			select {
			case e := <-done:
				finished = true
				t.Fatalf("Identity process exited before readiness: %v", e)
			case <-time.After(20 * time.Millisecond):
			}
		}
	}
	if !ready {
		t.Fatal("Identity process never became ready")
	}
	clientTLS := &tls.Config{MinVersion: tls.VersionTLS13, RootCAs: pki.pool, ServerName: "identity.internal.cellestial.ru", Certificates: []tls.Certificate{pki.certificate(t, "bff.internal.cellestial.ru", x509.ExtKeyUsageClientAuth)}}
	transport := &http.Transport{TLSClientConfig: clientTLS}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: time.Second}
	req, err := http.NewRequestWithContext(ctx, "POST", "https://"+addresses[0]+"/api/v1/auth/login", strings.NewReader(`{"login":"reader","password":"new-long-test-password"}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-BFF-Key", env["BFF_API_KEY"])
	req.Header.Set("X-Request-ID", "process-correlation")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("process login status=%d", resp.StatusCode)
	}
	var auth struct {
		Tokens struct {
			AccessToken string `json:"access_token"`
		} `json:"tokens"`
	}
	if err = json.NewDecoder(resp.Body).Decode(&auth); err != nil {
		t.Fatal(err)
	}
	workspaceTLS := clientTLS.Clone()
	workspaceTLS.Certificates = []tls.Certificate{pki.certificate(t, "workspace.internal.cellestial.ru", x509.ExtKeyUsageClientAuth)}
	conn, err := grpc.NewClient(addresses[1], grpc.WithTransportCredentials(credentials.NewTLS(workspaceTLS)))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	rpcCtx := metadata.NewOutgoingContext(ctx, metadata.Pairs("x-request-id", "process-correlation"))
	if _, err = identityv1.NewSessionClient(conn).ValidateSession(rpcCtx, &identityv1.ValidateSessionRequest{AccessToken: auth.Tokens.AccessToken}); err != nil {
		t.Fatal(err)
	}
	_ = conn.Close()
	transport.CloseIdleConnections()
	if err = cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	select {
	case err = <-done:
		finished = true
		if err != nil {
			t.Fatalf("Identity process shutdown: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Identity process did not stop after SIGTERM")
	}
	if !strings.Contains(logs.String(), `"request_id":"process-correlation"`) || strings.Contains(logs.String(), auth.Tokens.AccessToken) || strings.Contains(logs.String(), "new-long-test-password") {
		t.Fatal("unsafe or uncorrelated process log")
	}
}
