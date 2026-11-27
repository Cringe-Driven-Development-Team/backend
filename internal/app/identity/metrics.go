package identity

import (
	"net/http"
	"strconv"
	"time"

	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/transport/http/identityapi"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
)

// serviceMetrics регистрирует показатели только данного процесса и конечные labels.
type serviceMetrics struct {
	registry *prometheus.Registry
	requests *prometheus.CounterVec
	auth     *prometheus.CounterVec
	duration *prometheus.HistogramVec
	active   prometheus.Gauge
	paths    map[string]bool
}

// newServiceMetrics добавляет build-info, runtime/process, DB-pool и HTTP counters.
func newServiceMetrics(version string, pool *pgxpool.Pool) *serviceMetrics {
	reg := prometheus.NewRegistry()
	m := &serviceMetrics{registry: reg, requests: prometheus.NewCounterVec(prometheus.CounterOpts{Namespace: "identity", Name: "http_requests_total", Help: "Completed application HTTP calls."}, []string{"method", "route", "status"}), duration: prometheus.NewHistogramVec(prometheus.HistogramOpts{Namespace: "identity", Name: "http_duration_seconds", Help: "Application HTTP duration.", Buckets: prometheus.DefBuckets}, []string{"method", "route", "status"}), active: prometheus.NewGauge(prometheus.GaugeOpts{Namespace: "identity", Name: "http_active", Help: "Active application HTTP calls."}), paths: map[string]bool{}}
	build := prometheus.NewGaugeVec(prometheus.GaugeOpts{Namespace: "identity", Name: "build_info", Help: "Running Identity version."}, []string{"version"})
	build.WithLabelValues(version).Set(1)
	m.auth = prometheus.NewCounterVec(prometheus.CounterOpts{Namespace: "identity", Name: "auth_operations_total", Help: "Authentication operation outcomes."}, []string{"operation", "outcome"})
	reg.MustRegister(m.auth, m.requests, m.duration, m.active, build, collectors.NewGoCollector(), collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}), prometheus.NewGaugeFunc(prometheus.GaugeOpts{Namespace: "identity", Name: "db_acquired_connections", Help: "Connections currently acquired from the Identity pool."}, func() float64 { return float64(pool.Stat().AcquiredConns()) }), prometheus.NewGaugeFunc(prometheus.GaugeOpts{Namespace: "identity", Name: "db_idle_connections", Help: "Idle Identity pool connections."}, func() float64 { return float64(pool.Stat().IdleConns()) }), prometheus.NewGaugeFunc(prometheus.GaugeOpts{Namespace: "identity", Name: "db_max_connections", Help: "Maximum Identity pool connections."}, func() float64 { return float64(pool.Stat().MaxConns()) }))
	spec, err := identityapi.GetSpec()
	if err == nil {
		for path := range spec.Paths.Map() {
			m.paths["/api/v1"+path] = true
		}
	}
	return m
}

// metricWriter записывает итоговый статус, сохраняя поддержку Unwrap стандартного ResponseController.
type metricWriter struct {
	http.ResponseWriter
	status int
}

// WriteHeader сохраняет только первый статус.
func (w *metricWriter) WriteHeader(code int) {
	if w.status == 0 {
		w.status = code
		w.ResponseWriter.WriteHeader(code)
	}
}

// Write учитывает неявный HTTP 200.
func (w *metricWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(b)
}

// Unwrap открывает исходный ResponseWriter стандартному ResponseController.
func (w *metricWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// middleware нормализует method/route; случайные URL, ID и заголовки labels не создают.
func (m *serviceMetrics) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method := r.Method
		switch method {
		case "GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS", "HEAD":
		default:
			method = "other"
		}
		route := "unmatched"
		if m.paths[r.URL.Path] {
			route = r.URL.Path
		}
		started := time.Now()
		m.active.Inc()
		mw := &metricWriter{ResponseWriter: w}
		defer func() {
			m.active.Dec()
			code := mw.status
			if code == 0 {
				code = 200
			}
			labels := []string{method, route, strconv.Itoa(code)}
			m.requests.WithLabelValues(labels...).Inc()
			m.duration.WithLabelValues(labels...).Observe(time.Since(started).Seconds())
			operation := ""
			switch r.URL.Path {
			case "/api/v1/auth/register":
				operation = "register"
			case "/api/v1/auth/login":
				operation = "login"
			case "/api/v1/auth/refresh":
				operation = "refresh"
			case "/api/v1/auth/logout":
				operation = "logout"
			case "/api/v1/auth/logout-all":
				operation = "logout_all"
			case "/api/v1/auth/enable2fa":
				operation = "totp_setup"
			case "/api/v1/auth/confirm2fa":
				operation = "totp_confirm"
			case "/api/v1/auth/disable2fa":
				operation = "totp_disable"
			}
			if operation != "" {
				outcome := "success"
				if code >= 500 {
					outcome = "failure"
				} else if code >= 400 {
					outcome = "rejected"
				}
				m.auth.WithLabelValues(operation, outcome).Inc()
			}

		}()
		next.ServeHTTP(mw, r)
	})
}
