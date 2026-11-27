package identity

import (
	"context"
	"errors"
	"io"
	"log"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"

	workspacev1 "github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/gen/cellestial/workspace/v1"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/lifecycle"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/observability"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/storage/s3client"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/transport/mtls"
	rpctransport "github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/transport/rpc"
	avatarcodec "github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/user/adapters/avatar"
	userpostgres "github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/user/adapters/postgres"
	authredis "github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/user/adapters/redis"
	avatars3 "github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/user/adapters/s3"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/user/adapters/security"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/user/adapters/workspacerpc"
	userdelivery "github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/user/delivery/http"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/user/usecase"
	identitymigrations "github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/migrations/identity"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	goredis "github.com/redis/go-redis/v9"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	healthv1 "google.golang.org/grpc/health/grpc_health_v1"
)

// Service владеет Identity transport, DB/Redis и lifecycle; Serve вызывается один раз.
type Service struct {
	cfg         Config
	http, admin *http.Server
	rpc         *grpc.Server
	health      *health.Server
	pool        *pgxpool.Pool
	redis       *goredis.Client
	workspace   *grpc.ClientConn
	deletions   *usecase.DeletionService
	closeOnce   sync.Once
}

// Build собирает независимый Identity и проверяет совместимость уже применённых миграций.
// При ошибке освобождает ресурсы; вызывающий обязан вызвать Close после успешного Build.
func Build(ctx context.Context, c Config, logger *slog.Logger) (result *Service, err error) {
	s := &Service{cfg: c}
	defer func() {
		if err != nil {
			s.Close()
		}
	}()
	poolConfig, e := pgxpool.ParseConfig(c.DatabaseURL)
	if e != nil {
		return nil, errors.New("invalid Identity database configuration")
	}
	poolConfig.ConnConfig.Tracer = observability.PostgresTracer{}
	s.pool, e = pgxpool.NewWithConfig(ctx, poolConfig)
	if e != nil {
		return nil, errors.New("identity database unavailable")
	}
	startup, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if e = identitymigrations.Check(startup, s.pool); e != nil {
		return nil, e
	}
	s.redis = goredis.NewClient(&goredis.Options{Addr: c.RedisAddr, Password: c.RedisPassword, DB: c.RedisDB, MaxRetries: -1, DialTimeout: 2 * time.Second, ReadTimeout: c.RedisTimeout, WriteTimeout: c.RedisTimeout, PoolTimeout: c.RedisTimeout, ContextTimeoutEnabled: true})
	s.redis.AddHook(observability.RedisHook{})
	if e = s.redis.Ping(startup).Err(); e != nil {
		return nil, errors.New("identity session storage unavailable")
	}
	repo := userpostgres.NewIdentityUserRepository(s.pool)
	deletions := userpostgres.NewDeletions(s.pool)
	store := authredis.NewStore(s.redis, "", c.RedisTimeout)
	tokens := security.NewAccessToken([]byte(c.JWTSecret), c.AccessTTL)
	totp, e := security.NewTOTP(c.TOTPEncryptionKey)
	if e != nil {
		return nil, errors.New("invalid Identity TOTP configuration")
	}
	uc := usecase.NewUserUsecase(repo, store, tokens, security.PasswordHasher{}, security.RefreshTokens{}, c.RefreshTTL, usecase.WithTwoFactor(totp, authredis.NewAttemptLimiter(s.redis, "", c.RedisTimeout)), usecase.WithActiveAccounts(deletions))
	s3, e := s3client.NewClient(startup)
	if e != nil {
		return nil, errors.New("cannot initialize Identity avatar storage")
	}
	avatarStorage := avatars3.NewStorage(s3, c.AvatarsBucket)
	avatars := usecase.NewAvatarUsecase(repo, avatarStorage, sanitizeAvatar)
	access := usecase.GuardedAccessSessions{Sessions: store, Accounts: deletions}
	handler, e := userdelivery.NewIdentityRouter(userdelivery.NewHandler(uc, userdelivery.WithProfile(avatars, c.AvatarsPublicURL)), tokens, access, c.BFFAPIKey)
	if e != nil {
		return nil, errors.New("cannot initialize Identity HTTP router")
	}
	metrics := newServiceMetrics(c.Version, s.pool)
	tlsConfig, e := mtls.HTTPServerConfig(mtls.Config{CertFile: c.TLSCertFile, KeyFile: c.TLSKeyFile, CAFile: c.TLSCAFile}, "bff.internal.cellestial.ru")
	if e != nil {
		return nil, e
	}
	clientOptions, e := rpctransport.ClientOptions(rpctransport.ClientConfig{
		TLS: mtls.Config{CertFile: c.TLSCertFile, KeyFile: c.TLSKeyFile, CAFile: c.TLSCAFile}, ServerName: "workspace.internal.cellestial.ru",
		Service: "identity", Version: c.Version, Logger: logger, Timeout: 5 * time.Second,
		Methods: map[string]rpctransport.MethodPolicy{workspacev1.UserCleanup_BeginUserCleanup_FullMethodName: {Retry: true}},
		Metrics: rpctransport.NewMetrics(metrics.registry, "identity_to_workspace"),
	})
	if e != nil {
		return nil, e
	}
	s.workspace, e = grpc.NewClient(c.WorkspaceAddr, clientOptions...)
	if e != nil {
		return nil, errors.New("invalid Workspace gRPC address")
	}
	s.deletions = usecase.NewDeletionService(deletions, store, workspacerpc.NewCleanup(workspacev1.NewUserCleanupClient(s.workspace)), avatarStorage)
	bounded := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), c.HTTPTimeout)
		defer cancel()
		handler.ServeHTTP(w, r.WithContext(ctx))
	})
	s.http = &http.Server{Handler: metrics.middleware(bounded), TLSConfig: tlsConfig, ReadHeaderTimeout: 2 * time.Second, ReadTimeout: c.HTTPTimeout, WriteTimeout: c.HTTPTimeout, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 16 << 10, ErrorLog: log.New(io.Discard, "", 0)}
	s.health = health.NewServer()
	s.rpc, e = newRPCServer(c.SessionConfig, usecase.NewSessionValidator(tokens, access), logger, usecase.NewDirectory(repo), s.health, rpctransport.NewMetrics(metrics.registry, "identity"), s.deletions)
	if e != nil {
		return nil, e
	}
	admin := http.NewServeMux()
	admin.HandleFunc("GET /livez", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	admin.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		if !s.ready(r.Context()) {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	admin.Handle("GET /metrics", promhttp.HandlerFor(metrics.registry, promhttp.HandlerOpts{Timeout: 2 * time.Second}))
	s.admin = &http.Server{Handler: admin, ReadHeaderTimeout: 2 * time.Second, ReadTimeout: 3 * time.Second, WriteTimeout: 3 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 8 << 10}
	return s, nil
}

// sanitizeAvatar переиспользует проверенный codec, переводя только его бизнес-ошибки.
func sanitizeAvatar(ctx context.Context, source io.ReadSeeker, mime string) (usecase.SanitizedAvatar, error) {
	file, err := (avatarcodec.Codec{}).Sanitize(ctx, source, mime)
	switch {
	case errors.Is(err, avatarcodec.ErrInvalidImage):
		return usecase.SanitizedAvatar{}, usecase.ErrInvalidImage
	case errors.Is(err, avatarcodec.ErrUnsupportedMediaType):
		return usecase.SanitizedAvatar{}, usecase.ErrUnsupportedMediaType
	case err != nil:
		return usecase.SanitizedAvatar{}, err
	}
	return usecase.SanitizedAvatar{Body: file, Size: file.Size, ContentType: file.ContentType}, nil
}

// ready проверяет зависимости с отдельным коротким бюджетом без DDL/мутаций.
func (s *Service) ready(ctx context.Context) bool {
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	return s.pool.Ping(ctx) == nil && s.redis.Ping(ctx).Err() == nil
}

// Close освобождает клиентские ресурсы; вызывать после завершения Serve.
func (s *Service) Close() {
	s.closeOnce.Do(func() {
		if s.rpc != nil {
			s.rpc.Stop()
		}
		if s.workspace != nil {
			_ = s.workspace.Close()
		}
		if s.redis != nil {
			_ = s.redis.Close()
		}
		if s.pool != nil {
			s.pool.Close()
		}
	})
}

// Serve принимает владение тремя listeners, останавливает transport и health worker до Close.
func (s *Service) Serve(ctx context.Context, httpListener, rpcListener, adminListener net.Listener) error {
	defer httpListener.Close()
	defer rpcListener.Close()
	defer adminListener.Close()
	serveErr := make(chan error, 3)
	go func() { serveErr <- s.http.ServeTLS(httpListener, "", "") }()
	go func() { serveErr <- s.rpc.Serve(rpcListener) }()
	go func() { serveErr <- s.admin.Serve(adminListener) }()
	workerCtx, stopWorker := context.WithCancel(ctx)
	workerDone := make(chan struct{})
	deletionDone := make(chan struct{})
	if s.cfg.WorkersPaused {
		close(deletionDone)
	} else {
		go func() { defer close(deletionDone); _ = s.deletions.Run(workerCtx) }()
	}
	go func() {
		defer close(workerDone)
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			state := healthv1.HealthCheckResponse_NOT_SERVING
			if s.ready(workerCtx) {
				state = healthv1.HealthCheckResponse_SERVING
			}
			s.health.SetServingStatus("", state)
			select {
			case <-workerCtx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
	var result error
	received := 0
	select {
	case <-ctx.Done():
	case e := <-serveErr:
		received++
		if !errors.Is(e, http.ErrServerClosed) && !errors.Is(e, grpc.ErrServerStopped) {
			result = errors.New("identity listener failed")
		}
	}
	stopWorker()
	<-workerDone
	s.health.Shutdown()
	shutdown, cancel := context.WithTimeout(context.Background(), s.cfg.ShutdownTimeout)
	defer cancel()
	var wg sync.WaitGroup
	wg.Go(func() {
		if e := s.http.Shutdown(shutdown); e != nil {
			_ = s.http.Close()
		}
	})
	wg.Go(func() {
		if e := s.admin.Shutdown(shutdown); e != nil {
			_ = s.admin.Close()
		}
	})
	wg.Go(func() {
		done := make(chan struct{})
		go func() { s.rpc.GracefulStop(); close(done) }()
		select {
		case <-done:
		case <-shutdown.Done():
			s.rpc.Stop()
			<-done
		}
	})
	wg.Wait()
	select {
	case <-deletionDone:
	case <-shutdown.Done():
		return lifecycle.ErrDrainTimeout
	}
	for ; received < 3; received++ {
		<-serveErr
	}
	return result
}

// Run открывает listeners процесса; сигналы отменяют общий context через cmd/identity.
func Run(ctx context.Context, c Config, logger *slog.Logger) (result error) {
	s, err := Build(ctx, c, logger)
	if err != nil {
		return err
	}
	defer func() {
		if !errors.Is(result, lifecycle.ErrDrainTimeout) {
			s.Close()
		}
	}()
	listeners := make([]net.Listener, 0, 3)
	defer func() {
		for _, l := range listeners {
			_ = l.Close()
		}
	}()
	for _, addr := range []string{c.HTTPAddr, c.GRPCAddr, c.AdminAddr} {
		l, e := net.Listen("tcp", addr)
		if e != nil {
			return errors.New("cannot listen on Identity address")
		}
		listeners = append(listeners, l)
	}
	return s.Serve(ctx, listeners[0], listeners[1], listeners[2])
}
