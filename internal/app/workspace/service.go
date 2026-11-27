package workspace

import (
	"context"
	"errors"
	"io"
	"log"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	environmentsv1 "github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/gen/cellestial/environments/v1"
	filesv1 "github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/gen/cellestial/files/v1"
	identityv1 "github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/gen/cellestial/identity/v1"
	workspacev1 "github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/gen/cellestial/workspace/v1"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/lifecycle"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/migration"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/notebook/adapters/cursor"
	notebookidentity "github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/notebook/adapters/identityrpc"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/notebook/adapters/ipynb"
	notebookpostgres "github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/notebook/adapters/postgres"
	notebookdelivery "github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/notebook/delivery/http"
	notebooks "github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/notebook/usecase"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/observability"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/runtimesession/adapters/environmentsrpc"
	sessionidentity "github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/runtimesession/adapters/identityrpc"
	sessionpostgres "github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/runtimesession/adapters/postgres"
	sessiongrpc "github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/runtimesession/delivery/grpc"
	sessiondelivery "github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/runtimesession/delivery/http"
	sessions "github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/runtimesession/usecase"
	httptransport "github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/transport/http"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/transport/mtls"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/transport/rpc"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/health"
	healthv1 "google.golang.org/grpc/health/grpc_health_v1"
)

const serviceName = "workspace"

// Run открывает HTTP, gRPC health и admin Workspace. Миграции выполняются владельцем отдельно.
func Run(ctx context.Context, c Config, logger *slog.Logger) (err error) {
	pg, err := pgxpool.ParseConfig(c.DatabaseURL)
	if err != nil {
		return errors.New("invalid Workspace database configuration")
	}
	pg.ConnConfig.Tracer = observability.PostgresTracer{}
	pool, err := pgxpool.NewWithConfig(ctx, pg)
	if err != nil {
		return errors.New("cannot initialize Workspace database")
	}
	defer func() {
		if !errors.Is(err, lifecycle.ErrDrainTimeout) {
			pool.Close()
		}
	}()
	startup, cancel := context.WithTimeout(ctx, 5*time.Second)
	err = migration.CheckCompatible(startup, pool, migration.Workspace)
	cancel()
	if err != nil {
		return err
	}
	registry := lifecycle.Registry(serviceName, c.Version, pool)
	identity, err := client(c, c.IdentityAddr, "identity", 2*time.Second, logger, registry)
	if err != nil {
		return err
	}
	defer func() {
		if !errors.Is(err, lifecycle.ErrDrainTimeout) {
			_ = identity.Close()
		}
	}()
	environments, err := client(c, c.EnvironmentsAddr, "environments", 5*time.Second, logger, registry)
	if err != nil {
		return err
	}
	defer func() {
		if !errors.Is(err, lifecycle.ErrDrainTimeout) {
			_ = environments.Close()
		}
	}()
	store := sessionpostgres.New(pool)
	control := environmentsrpc.NewControl(environmentsv1.NewControlClient(environments))
	agent := environmentsrpc.NewAgent(environmentsv1.NewSessionAgentClient(environments))
	users := notebookidentity.NewUsers(identityv1.NewUsersClient(identity))
	verify := sessionidentity.NewSession(identityv1.NewSessionClient(identity))
	key := []byte(c.CursorKey)
	books := notebooks.NewNotebookUsecase(notebookpostgres.NewNotebookRepository(pool), store, cursor.New(key),
		notebooks.WithImportDecoder(ipynb.DecodeImport), notebooks.WithImportStream(ipynb.DecodeImportStream), notebooks.WithExport(ipynb.EncodeDocumentStream), notebooks.WithCommentCursor(cursor.NewCommentCodec(key)), notebooks.WithUsers(users))
	worker := sessions.NewWorker(store, control, agent, nil)
	api, err := httptransport.NewWorkspaceRouter(httptransport.WorkspaceHandlers{
		NotebookHandler: notebookdelivery.NewHandler(books),
		RuntimeHandler:  sessiondelivery.NewHandler(sessions.NewService(store, nil, agent)),
		Files:           sessiondelivery.NewWorkspaceFileHandler(sessions.NewWorkspaceFiles(store, environmentsrpc.NewFiles(filesv1.NewFilesClient(environments)))),
	}, verify, c.BFFKey, httpObserver(registry))
	if err != nil {
		return err
	}
	httpTLS, err := mtls.HTTPServerConfig(c.TLS, "bff.internal.cellestial.ru")
	if err != nil {
		return err
	}
	httpServer := &http.Server{
		Handler: boundHTTP(activeHTTP(registry, api), c.HTTPTimeout), TLSConfig: httpTLS, ReadHeaderTimeout: 2 * time.Second,
		ReadTimeout: c.HTTPTimeout, WriteTimeout: c.HTTPTimeout, IdleTimeout: 30 * time.Second,
		MaxHeaderBytes: 16 << 10, ErrorLog: log.New(io.Discard, "", 0),
	}
	rpcTLS, err := mtls.ServerConfig(c.TLS)
	if err != nil {
		return err
	}
	healthServer := health.NewServer()
	ready := lifecycle.NewReadiness(healthServer)
	peers := []string{"identity.internal.cellestial.ru", "probe.internal.cellestial.ru"}
	options := rpc.ServerOptions(rpc.UnaryConfig{
		Logger: logger, Service: serviceName, Version: c.Version, Timeout: 5 * time.Second,
		AllowedPeers: map[string][]string{
			healthv1.Health_Check_FullMethodName: peers, healthv1.Health_Watch_FullMethodName: peers,
			workspacev1.UserCleanup_BeginUserCleanup_FullMethodName: {"identity.internal.cellestial.ru"},
			workspacev1.UserCleanup_GetUserCleanup_FullMethodName:   {"identity.internal.cellestial.ru"},
		},
		Metrics: rpc.NewMetrics(registry, serviceName),
	})
	rpcServer := grpc.NewServer(append(options, grpc.Creds(credentials.NewTLS(rpcTLS)))...)
	healthv1.RegisterHealthServer(rpcServer, healthServer)
	workspacev1.RegisterUserCleanupServer(rpcServer, sessiongrpc.NewUserCleanup(store))
	probe := func(ctx context.Context) error {
		return ready.Poll(ctx, func(check context.Context) bool {
			return pool.Ping(check) == nil && serving(check, identity) && serving(check, environments)
		})
	}
	background := []func(context.Context) error{probe, queueMetrics(registry, pool)}
	if !c.WorkersPaused {
		background = append(background, worker.Run)
	}
	return lifecycle.Run(ctx, c.ShutdownTimeout, []lifecycle.Endpoint{
		lifecycle.HTTP(c.HTTPAddr, httpServer), lifecycle.RPC(c.GRPCAddr, rpcServer), lifecycle.HTTP(c.AdminAddr, lifecycle.Admin(ready, registry)),
	}, ready.Stop, background...)
}

// client выбирает перечень повторяемых операций, не включая управление kernel.
func client(c Config, address, service string, timeout time.Duration, logger *slog.Logger, registry *prometheus.Registry) (*grpc.ClientConn, error) {
	methods := map[string]rpc.MethodPolicy{healthv1.Health_Check_FullMethodName: {Retry: true}}
	switch service {
	case "identity":
		for _, method := range []string{identityv1.Session_ValidateSession_FullMethodName, identityv1.Users_BatchGetUsers_FullMethodName, identityv1.Users_BatchResolveUsers_FullMethodName} {
			methods[method] = rpc.MethodPolicy{Retry: true}
		}
	case "environments":
		for _, method := range []string{environmentsv1.Control_RequestCreate_FullMethodName, environmentsv1.Control_GetEnvironment_FullMethodName,
			environmentsv1.Control_RequestDelete_FullMethodName, environmentsv1.SessionAgent_Health_FullMethodName, filesv1.Files_Check_FullMethodName} {
			methods[method] = rpc.MethodPolicy{Retry: true}
		}
		for _, method := range []string{
			environmentsv1.SessionAgent_Execute_FullMethodName, environmentsv1.SessionAgent_GetExecution_FullMethodName,
			environmentsv1.SessionAgent_ReadEvents_FullMethodName, environmentsv1.SessionAgent_InspectVariables_FullMethodName,
		} {
			methods[method] = rpc.MethodPolicy{StreamTimeout: 60 * time.Second}
		}
		for _, method := range []string{environmentsv1.SessionAgent_Activity_FullMethodName, environmentsv1.SessionAgent_Drain_FullMethodName,
			environmentsv1.SessionAgent_Stop_FullMethodName, environmentsv1.SessionAgent_Interrupt_FullMethodName,
			filesv1.Files_Command_FullMethodName, filesv1.Files_CloseSocket_FullMethodName,
			environmentsv1.SessionAgent_Restart_FullMethodName, environmentsv1.SessionAgent_StopExecutions_FullMethodName} {
			methods[method] = rpc.MethodPolicy{}
		}
		methods[filesv1.Files_Subscribe_FullMethodName] = rpc.MethodPolicy{StreamTimeout: 30 * time.Second}
		methods[filesv1.Files_Upload_FullMethodName] = rpc.MethodPolicy{StreamTimeout: 30 * time.Second}
		methods[filesv1.Files_Download_FullMethodName] = rpc.MethodPolicy{StreamTimeout: 2 * time.Hour}

	}
	options, err := rpc.ClientOptions(rpc.ClientConfig{
		TLS: c.TLS, ServerName: service + ".internal.cellestial.ru", Service: serviceName, Version: c.Version,
		Timeout: timeout, Logger: logger, Methods: methods, Metrics: rpc.NewMetrics(registry, serviceName+"_to_"+service),
	})
	if err != nil {
		return nil, err
	}
	return grpc.NewClient(address, options...)
}

// serving проверяет готовность удалённого сервиса через тот же mTLS клиент.
func serving(ctx context.Context, conn *grpc.ClientConn) bool {
	value, err := healthv1.NewHealthClient(conn).Check(ctx, &healthv1.HealthCheckRequest{})
	return err == nil && value.GetStatus() == healthv1.HealthCheckResponse_SERVING
}

// boundHTTP применяет общий deadline и отдельный бюджет потокового скачивания.
func boundHTTP(next http.Handler, timeout time.Duration) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		budget := timeout
		if r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/v1/runtime-sessions/") &&
			strings.Contains(r.URL.Path, "/workspace/transfers/") && strings.HasSuffix(r.URL.Path, "/download") {
			budget = 2 * time.Hour
		}
		ctx, cancel := context.WithTimeout(r.Context(), budget)
		defer cancel()
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// httpObserver использует шаблоны router и конечный набор HTTP-методов.
func httpObserver(reg *prometheus.Registry) func(string, string, int, time.Duration) {
	labels := []string{"method", "route", "status"}
	count := prometheus.NewCounterVec(prometheus.CounterOpts{Namespace: serviceName, Name: "http_requests_total", Help: "Completed HTTP calls."}, labels)
	duration := prometheus.NewHistogramVec(prometheus.HistogramOpts{Namespace: serviceName, Name: "http_duration_seconds", Help: "HTTP duration in seconds."}, labels)
	books := prometheus.NewCounterVec(prometheus.CounterOpts{Namespace: serviceName, Name: "notebook_operations_total", Help: "Notebook operation outcomes."},
		[]string{"method", "route", "outcome"})
	reg.MustRegister(count, duration, books)
	return func(method, route string, code int, elapsed time.Duration) {
		switch method {
		case "GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS":
		default:
			method = "other"
		}
		values := []string{method, route, strconv.Itoa(code)}
		count.WithLabelValues(values...).Inc()
		duration.WithLabelValues(values...).Observe(elapsed.Seconds())
		if strings.HasPrefix(route, "/api/v1/notebooks") {
			outcome := "success"
			if code >= 500 {
				outcome = "failure"
			} else if code >= 400 {
				outcome = "rejected"
			}
			books.WithLabelValues(method, route, outcome).Inc()
		}
	}
}

// activeHTTP считает текущие запросы в том числе при backpressure скачивания.
func activeHTTP(reg *prometheus.Registry, next http.Handler) http.Handler {
	active := prometheus.NewGauge(prometheus.GaugeOpts{Namespace: serviceName, Name: "http_active", Help: "Active HTTP calls."})
	reg.MustRegister(active)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		active.Inc()
		defer active.Dec()
		next.ServeHTTP(w, r)
	})
}
