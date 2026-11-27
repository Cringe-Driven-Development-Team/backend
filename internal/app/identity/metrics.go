package identity

import (
	"net/http"
	"time"

	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/lifecycle"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/transport/http/middleware"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
)

// serviceMetrics регистрирует показатели только данного процесса и конечные labels.
type serviceMetrics struct {
	registry *prometheus.Registry
	auth     *prometheus.CounterVec
	active   prometheus.Gauge
	http     *middleware.HTTPMetrics
}

// newServiceMetrics добавляет профильные метрики Identity к общим collectors.
func newServiceMetrics(version string, pool *pgxpool.Pool) *serviceMetrics {
	reg := lifecycle.Registry("identity", version, pool)
	m := &serviceMetrics{registry: reg, http: middleware.NewHTTPMetrics(reg)}
	m.auth = prometheus.NewCounterVec(prometheus.CounterOpts{Namespace: "identity", Name: "auth_operations_total", Help: "Authentication operation outcomes."}, []string{"operation", "outcome"})
	m.active = prometheus.NewGauge(prometheus.GaugeOpts{Namespace: "identity", Name: "http_active", Help: "Active application HTTP calls."})
	reg.MustRegister(m.auth, m.active)
	return m
}

// middleware измеряет Identity API и отдельно операции авторизации.
func (m *serviceMetrics) middleware(next http.Handler, route func(*http.Request) string) http.Handler {
	observed := middleware.ObserveHTTP(next, func(_, path string, code int, _ time.Duration) {
		operation := ""
		switch path {
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
	}, m.http, route)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		m.active.Inc()
		defer m.active.Dec()
		observed.ServeHTTP(w, r)
	})
}
