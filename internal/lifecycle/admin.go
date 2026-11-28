package lifecycle

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/transport/http/middleware"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"google.golang.org/grpc/health"
	pb "google.golang.org/grpc/health/grpc_health_v1"
)

// Readiness публикует одно состояние для HTTP и gRPC на протяжении жизни процесса.
type Readiness struct {
	mu               sync.RWMutex
	healthy, stopped bool
	grpc             *health.Server
}

// NewReadiness начинает с NOT_SERVING до первой проверки зависимостей.
func NewReadiness(server *health.Server) *Readiness {
	server.SetServingStatus("", pb.HealthCheckResponse_NOT_SERVING)
	return &Readiness{grpc: server}
}

// Stop немедленно снимает readiness. Health.Shutdown не примет устаревшее успешное обновление.
func (r *Readiness) Stop() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.stopped = true
	r.healthy = false
	r.grpc.Shutdown()
}

// Poll проверяет зависимости в отдельном кратком контексте.
func (r *Readiness) Poll(ctx context.Context, probe func(context.Context) bool) error {
	timer := time.NewTicker(time.Second)
	defer timer.Stop()
	for ctx.Err() == nil {
		attempt, cancel := context.WithTimeout(ctx, time.Second)
		ok := probe(attempt)
		cancel()
		if ctx.Err() != nil {
			return nil
		}
		status := pb.HealthCheckResponse_NOT_SERVING
		if ok {
			status = pb.HealthCheckResponse_SERVING
		}
		r.mu.Lock()
		if !r.stopped {
			r.healthy = ok
			r.grpc.SetServingStatus("", status)
		}
		r.mu.Unlock()
		select {
		case <-ctx.Done():
		case <-timer.C:
		}
	}
	return nil
}

// Admin предоставляет отдельные liveness/readiness и scrape.
func Admin(r *Readiness, registry *prometheus.Registry, metrics *middleware.HTTPMetrics) *http.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /livez", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, _ *http.Request) {
		r.mu.RLock()
		healthy := r.healthy
		r.mu.RUnlock()
		if !healthy {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	mux.Handle("GET /metrics", promhttp.HandlerFor(registry, promhttp.HandlerOpts{Timeout: 2 * time.Second}))
	return &http.Server{Handler: ObserveAdmin(mux, metrics), ReadHeaderTimeout: 2 * time.Second, ReadTimeout: 3 * time.Second,
		WriteTimeout: 3 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 8 << 10}
}

// ObserveAdmin измеряет health и scrape на приватном HTTP-входе.
func ObserveAdmin(handler http.Handler, metrics *middleware.HTTPMetrics) http.Handler {
	return middleware.RequestID(middleware.ObserveHTTP(middleware.Recover(handler), nil, metrics, middleware.AdminRoute))
}

// Registry регистрирует runtime/process, build-info и ограниченные DB-pool показатели сервиса.
func Registry(service, version string, pool *pgxpool.Pool) *prometheus.Registry {
	reg := prometheus.NewRegistry()
	build := prometheus.NewGaugeVec(prometheus.GaugeOpts{Namespace: service, Name: "build_info", Help: "Running service version."}, []string{"version"})
	build.WithLabelValues(version).Set(1)
	reg.MustRegister(build, collectors.NewGoCollector(), collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
	for _, metric := range []struct {
		name, help string
		value      func(*pgxpool.Stat) int32
	}{
		{"db_acquired_connections", "Acquired database connections.", (*pgxpool.Stat).AcquiredConns},
		{"db_idle_connections", "Idle database connections.", (*pgxpool.Stat).IdleConns},
		{"db_max_connections", "Maximum database connections.", (*pgxpool.Stat).MaxConns},
	} {
		reg.MustRegister(prometheus.NewGaugeFunc(prometheus.GaugeOpts{Namespace: service, Name: metric.name, Help: metric.help},
			func() float64 { return float64(metric.value(pool.Stat())) }))
	}
	return reg
}
