package middleware

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/observability"
	"github.com/gorilla/mux"
	"github.com/prometheus/client_golang/prometheus"
)

// HTTPMetrics содержит общие метрики всех HTTP-входов процесса.
type HTTPMetrics struct {
	requests, streamErrors *prometheus.CounterVec
	duration               *prometheus.HistogramVec
}

// NewHTTPMetrics регистрирует HTTP-счётчики и histogram в секундах.
func NewHTTPMetrics(reg prometheus.Registerer) *HTTPMetrics {
	labels := []string{"method", "route", "status_code"}
	m := &HTTPMetrics{
		requests:     prometheus.NewCounterVec(prometheus.CounterOpts{Name: "cellestial_http_requests_total", Help: "Completed HTTP requests."}, labels),
		duration:     prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "cellestial_http_request_duration_seconds", Help: "HTTP request duration in seconds.", Buckets: observability.LatencyBuckets()}, labels),
		streamErrors: prometheus.NewCounterVec(prometheus.CounterOpts{Name: "cellestial_http_stream_errors_total", Help: "Response failures after HTTP headers were sent."}, labels),
	}
	reg.MustRegister(m.requests, m.duration, m.streamErrors)
	return m
}

// observe учитывает фактический HTTP-статус и отдельно ошибку после заголовков.
func (m *HTTPMetrics) observe(method, route string, status int, duration time.Duration, failed bool) {
	if m == nil {
		return
	}
	values := []string{method, route, strconv.Itoa(status)}
	m.requests.WithLabelValues(values...).Inc()
	m.duration.WithLabelValues(values...).Observe(duration.Seconds())
	if failed {
		m.streamErrors.WithLabelValues(values...).Inc()
	}
}

// RegisteredRoute находит безопасный шаблон при отказе до вызова роутера, включая 405.
func RegisteredRoute(router *mux.Router, r *http.Request) string {
	var match mux.RouteMatch
	if router.Match(r, &match) && match.Route != nil {
		template, _ := match.Route.GetPathTemplate()
		if template != "" {
			return template
		}
	}
	if errors.Is(match.MatchErr, mux.ErrMethodMismatch) {
		probe := *r
		for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodHead, http.MethodOptions} {
			probe.Method = method
			match = mux.RouteMatch{}
			if router.Match(&probe, &match) && match.Route != nil {
				template, _ := match.Route.GetPathTemplate()
				if template != "" {
					return template
				}
			}
		}
	}
	return "unmatched"
}

// AdminRoute возвращает только зарегистрированные административные пути.
func AdminRoute(r *http.Request) string {
	switch r.URL.Path {
	case "/livez", "/readyz", "/metrics":
		return r.URL.Path
	default:
		return "unmatched"
	}
}

// MarkResponseFailure отмечает ошибку чтения или передачи после отправки заголовков.
func MarkResponseFailure(r *http.Request) {
	if meta, ok := r.Context().Value(requestMetaKey{}).(*requestMeta); ok {
		meta.responseFailed = true
	}
}
