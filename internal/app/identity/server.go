package identity

import (
	"context"
	"crypto/tls"
	"errors"
	"log/slog"
	"net"
	"time"

	identityv1 "github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/gen/cellestial/identity/v1"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/observability"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/transport/mtls"
	rpctransport "github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/transport/rpc"
	authredis "github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/user/adapters/redis"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/user/adapters/security"
	grpcdelivery "github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/user/delivery/grpc"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/user/usecase"
	goredis "github.com/redis/go-redis/v9"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/health"
	healthv1 "google.golang.org/grpc/health/grpc_health_v1"
)

// NewSessionRPCServer создаёт только mTLS listener с разрешённым Workspace principal.
// Ошибки чтения credentials не раскрывают ключи и содержимое файлов.
func NewSessionRPCServer(c SessionConfig, verifier grpcdelivery.SessionVerifier, logger *slog.Logger) (*grpc.Server, error) {
	return newRPCServer(c, verifier, logger, nil, nil, nil, nil)
}

// identityTLS загружает общий сертификат сервера и доверенный клиентский CA.
func identityTLS(c SessionConfig) (*tls.Config, error) {
	return mtls.ServerConfig(mtls.Config{CertFile: c.TLSCertFile, KeyFile: c.TLSKeyFile, CAFile: c.TLSCAFile})
}

// newRPCServer разрешает только зарегистрированные методы заданным service identities.
func newRPCServer(c SessionConfig, verifier grpcdelivery.SessionVerifier, logger *slog.Logger, users *grpcdelivery.UsersServer, healthServer *health.Server, metrics *rpctransport.Metrics, admin grpcdelivery.UserDeletionService) (*grpc.Server, error) {
	tlsConfig, err := identityTLS(c)
	if err != nil {
		return nil, err
	}
	allowed := map[string][]string{identityv1.Session_ValidateSession_FullMethodName: {"workspace.internal.cellestial.ru"}}
	if users != nil {
		allowed[identityv1.Users_BatchGetUsers_FullMethodName] = []string{"workspace.internal.cellestial.ru"}
		allowed[identityv1.Users_BatchResolveUsers_FullMethodName] = []string{"workspace.internal.cellestial.ru"}
	}
	if healthServer != nil {
		for _, method := range []string{healthv1.Health_Check_FullMethodName, healthv1.Health_Watch_FullMethodName} {
			allowed[method] = []string{"workspace.internal.cellestial.ru", "probe.internal.cellestial.ru"}
		}
	}
	if admin != nil {
		allowed[identityv1.UserAdministration_RequestUserDeletion_FullMethodName] = []string{"identity-admin.internal.cellestial.ru"}
		allowed[identityv1.UserAdministration_GetUserDeletion_FullMethodName] = []string{"identity-admin.internal.cellestial.ru"}
	}
	options := rpctransport.ServerOptions(rpctransport.UnaryConfig{Logger: logger, Service: "identity", Version: c.Version, Timeout: 2 * time.Second, AllowedPeers: allowed, Metrics: metrics})
	options = append(options, grpc.Creds(credentials.NewTLS(tlsConfig)))
	server := grpc.NewServer(options...)
	identityv1.RegisterSessionServer(server, grpcdelivery.NewSessionServer(verifier))
	if users != nil {
		identityv1.RegisterUsersServer(server, users)
	}
	if healthServer != nil {
		healthv1.RegisterHealthServer(server, healthServer)
	}
	if admin != nil {
		identityv1.RegisterUserAdministrationServer(server, grpcdelivery.NewUserAdministration(admin))
	}
	return server, nil
}

// RunSessionRPC запускает проверку сессий отдельно от монолита и ожидает bounded shutdown.
// Redis key namespace совпадает с существующим API, мутации Redis не повторяются.
func RunSessionRPC(ctx context.Context, c SessionConfig, logger *slog.Logger) error {
	client := goredis.NewClient(&goredis.Options{Addr: c.RedisAddr, Password: c.RedisPassword, DB: c.RedisDB, MaxRetries: -1, DialTimeout: 2 * time.Second, ContextTimeoutEnabled: true})
	client.AddHook(observability.RedisHook{})
	defer func() { _ = client.Close() }()
	store := authredis.NewStore(client, "", c.RedisTimeout)
	verifier := usecase.NewSessionValidator(security.NewAccessToken([]byte(c.JWTSecret), time.Minute), store)
	server, err := NewSessionRPCServer(c, verifier, logger)
	if err != nil {
		return err
	}
	defer server.Stop()
	checkCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	err = client.Ping(checkCtx).Err()
	cancel()
	if err != nil {
		return errors.New("identity session storage unavailable")
	}
	listener, err := net.Listen("tcp", c.GRPCAddr)
	if err != nil {
		return errors.New("cannot listen on Identity gRPC address")
	}
	defer func() { _ = listener.Close() }()
	serveErr := make(chan error, 1)
	go func() { serveErr <- server.Serve(listener) }()
	select {
	case err = <-serveErr:
		return err
	case <-ctx.Done():
	}
	done := make(chan struct{})
	go func() { server.GracefulStop(); close(done) }()
	timer := time.NewTimer(30 * time.Second)
	defer timer.Stop()
	select {
	case <-done:
	case <-timer.C:
		server.Stop()
		<-done
	}
	return nil
}
