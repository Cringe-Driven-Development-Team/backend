package onlineconf

import (
	"context"
	"errors"
	"io"
	"log"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	identityv1 "github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/gen/cellestial/identity/v1"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/lifecycle"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/migration"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/observability"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/transport/http/middleware"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/transport/mtls"
	rpctransport "github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/transport/rpc"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
)

// Run читает настройки процесса из окружения и обслуживает HTTP до отмены ctx.
func Run(ctx context.Context, logger *slog.Logger) (runErr error) {
	redisConfig, err := snapshotRedisOptions(os.LookupEnv)
	if err != nil {
		return err
	}
	bffKey := os.Getenv("BFF_API_KEY")
	if strings.TrimSpace(bffKey) == "" {
		return errors.New("missing BFF_API_KEY")
	}
	workspaceKey := os.Getenv("ONLINECONF_WORKSPACE_KEY")
	if workspaceKey == bffKey {
		return errors.New("OnlineConf Workspace key must differ from BFF key")
	}
	addr := envOr("HTTP_ADDR", ":8080")
	adminAddr := envOr("ADMIN_ADDR", ":9091")
	version := os.Getenv("VERSION")
	tlsConfig := mtls.Config{
		CertFile: os.Getenv("TLS_CERT_FILE"),
		KeyFile:  os.Getenv("TLS_KEY_FILE"),
		CAFile:   os.Getenv("TLS_CA_FILE"),
	}
	// HTTP API вызывают только BFF (админка) и Workspace (продуктовые настройки).
	httpTLS, err := mtls.HTTPServerConfig(tlsConfig, "bff.internal.cellestial.ru", "workspace.internal.cellestial.ru")
	if err != nil {
		return err
	}
	target := os.Getenv("IDENTITY_GRPC_ADDR")
	host, port, err := net.SplitHostPort(target)
	if err != nil || host == "" || port == "" {
		return errors.New("invalid IDENTITY_GRPC_ADDR")
	}
	options, err := rpctransport.ClientOptions(rpctransport.ClientConfig{
		TLS:        tlsConfig,
		ServerName: "identity.internal.cellestial.ru",
		Service:    "onlineconf",
		Version:    version,
		Logger:     logger,
		Timeout:    2 * time.Second,
		Methods: map[string]rpctransport.MethodPolicy{
			identityv1.OnlineConfAccess_Authorize_FullMethodName: {},
			identityv1.Session_ValidateSession_FullMethodName:    {},
			identityv1.Users_BatchGetUsers_FullMethodName:        {},
			identityv1.Users_BatchResolveUsers_FullMethodName:    {},
		},
	})
	if err != nil {
		return err
	}
	conn, err := grpc.NewClient(target, options...)
	if err != nil {
		return errors.New("cannot initialize Identity gRPC client")
	}
	defer func() {
		if !errors.Is(runErr, lifecycle.ErrDrainTimeout) {
			_ = conn.Close()
		}
	}()

	pool, err := openKeyPool(ctx)
	if err != nil {
		return err
	}
	cacheClient := redis.NewClient(redisConfig)
	defer func() {
		if !errors.Is(runErr, lifecycle.ErrDrainTimeout) {
			_ = cacheClient.Close()
			pool.Close()
		}
	}()
	store := keyStore{pool: pool}
	cache := &redisSnapshots{client: cacheClient, prefix: "{onlineconf:v1}"}
	refresher := newSnapshotRefresher(store, cache, logger)
	admin := newKeysHandler(store, identityv1.NewUsersClient(conn), refresher.RequestRefresh, cache.ProductView)
	server := &http.Server{
		Handler:           newRouter(identityv1.NewOnlineConfAccessClient(conn), admin, bffKey, productHandler(identityv1.NewSessionClient(conn), cache.ProductView, bffKey, workspaceKey)),
		TLSConfig:         httpTLS,
		ErrorLog:          log.New(io.Discard, "", 0),
		ReadHeaderTimeout: 2 * time.Second,
		ReadTimeout:       5 * time.Second,
		WriteTimeout:      5 * time.Second,
		IdleTimeout:       30 * time.Second,
		MaxHeaderBytes:    16 << 10,
	}
	registry := lifecycle.Registry("onlineconf", version, pool)
	ready := lifecycle.NewReadiness(health.NewServer())
	probe := func(ctx context.Context) error {
		return ready.Poll(ctx, func(check context.Context) bool {
			return pool.Ping(check) == nil && cacheClient.Ping(check).Err() == nil
		})
	}
	return lifecycle.Run(ctx, 5*time.Second, []lifecycle.Endpoint{
		lifecycle.HTTP(addr, server), lifecycle.HTTP(adminAddr, lifecycle.Admin(ready, registry, middleware.NewHTTPMetrics(registry))),
	}, ready.Stop, refresher.Run, probe)
}

// openKeyPool открывает пул приложения и проверяет миграции без изменения схемы.
func openKeyPool(ctx context.Context) (*pgxpool.Pool, error) {
	dsn := os.Getenv("DATABASE_URL")
	if strings.TrimSpace(dsn) == "" {
		return nil, errors.New("missing DATABASE_URL")
	}
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, errors.New("invalid OnlineConf database configuration")
	}
	config.ConnConfig.Tracer = observability.PostgresTracer{}
	startup, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	pool, err := pgxpool.NewWithConfig(startup, config)
	if err != nil {
		return nil, errors.New("OnlineConf database unavailable")
	}
	if err := migration.CheckCompatible(startup, pool, migration.OnlineConf); err != nil {
		pool.Close()
		return nil, err
	}
	return pool, nil
}

// envOr возвращает значение переменной окружения name или fallback, если она пуста.
func envOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
