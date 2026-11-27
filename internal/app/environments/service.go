package environments

import (
	"context"
	"errors"
	"log/slog"
	"time"

	pb "github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/gen/cellestial/environments/v1"
	filesv1 "github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/gen/cellestial/files/v1"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/lifecycle"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/migration"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/observability"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/runtime"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/runtime/adapters/agentrpc"
	delivery "github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/runtime/delivery/grpc"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/transport/mtls"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/transport/rpc"
	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/health"
	healthv1 "google.golang.org/grpc/health/grpc_health_v1"
)

const serviceName = "environments"

// Run запускает Environments со своей БД, worker и двумя listeners.
func Run(ctx context.Context, c Config, logger *slog.Logger) (err error) {
	cfg, err := pgxpool.ParseConfig(c.DatabaseURL)
	if err != nil {
		return errors.New("invalid Environments database configuration")
	}
	cfg.ConnConfig.Tracer = observability.PostgresTracer{}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return errors.New("cannot initialize Environments database")
	}
	defer func() {
		if !errors.Is(err, lifecycle.ErrDrainTimeout) {
			pool.Close()
		}
	}()
	startup, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if err = migration.CheckCompatible(startup, pool, migration.Environments); err != nil {
		return err
	}
	reg := lifecycle.Registry(serviceName, c.Version, pool)
	dialer, err := agentrpc.NewDialer(c.AgentTLS, c.AgentPort, logger, c.Version, rpc.NewMetrics(reg, "environments_to_agent"))
	if err != nil {
		return err
	}
	components, err := runtime.BuildIndependent(startup, c.Runtime, pool, dialer)
	if err != nil {
		logger.Warn("environments dependency initialization failed", "error_code", observability.ErrorCode(err))
		return errors.New("cannot initialize Environments dependencies")
	}
	tlsConfig, err := mtls.ServerConfig(c.TLS)
	if err != nil {
		return err
	}
	healthServer := health.NewServer()
	ready := lifecycle.NewReadiness(healthServer)
	options := rpc.ServerOptions(rpc.UnaryConfig{
		Logger: logger, Service: serviceName, Version: c.Version, Timeout: 5 * time.Second, StreamTimeout: 60 * time.Second,
		StreamMethods: map[string]time.Duration{filesv1.Files_Download_FullMethodName: 2 * time.Hour},
		AllowedPeers:  allowedPeers(), Metrics: rpc.NewMetrics(reg, serviceName),
	})
	server := grpc.NewServer(append(options, grpc.Creds(credentials.NewTLS(tlsConfig)))...)
	pb.RegisterControlServer(server, delivery.NewControl(components.Control))
	agent := delivery.NewSessionAgent(components.Targets, dialer)
	pb.RegisterSessionAgentServer(server, agent)
	filesv1.RegisterFilesServer(server, delivery.NewFiles(agent))
	healthv1.RegisterHealthServer(server, healthServer)
	probe := func(ctx context.Context) error {
		return ready.Poll(ctx, func(ctx context.Context) bool { return pool.Ping(ctx) == nil })
	}
	background := []func(context.Context) error{probe, runtimeMetrics(reg, pool)}
	if !c.WorkersPaused {
		background = append(background, components.Worker.Run)
	}
	return lifecycle.Run(ctx, c.ShutdownTimeout, []lifecycle.Endpoint{
		lifecycle.RPC(c.GRPCAddr, server), lifecycle.HTTP(c.AdminAddr, lifecycle.Admin(ready, reg)),
	}, ready.Stop, background...)
}

// allowedPeers whitelist перечисляет только сервисные полномочия каждого зарегистрированного метода.
func allowedPeers() map[string][]string {
	workspace := []string{"workspace.internal.cellestial.ru"}
	health := []string{"workspace.internal.cellestial.ru", "probe.internal.cellestial.ru"}
	allowed := map[string][]string{healthv1.Health_Check_FullMethodName: health, healthv1.Health_Watch_FullMethodName: health}
	for _, descriptor := range []grpc.ServiceDesc{pb.Control_ServiceDesc, pb.SessionAgent_ServiceDesc, filesv1.Files_ServiceDesc} {
		for _, method := range descriptor.Methods {
			allowed["/"+descriptor.ServiceName+"/"+method.MethodName] = workspace
		}
		for _, stream := range descriptor.Streams {
			allowed["/"+descriptor.ServiceName+"/"+stream.StreamName] = workspace
		}
	}
	return allowed
}
