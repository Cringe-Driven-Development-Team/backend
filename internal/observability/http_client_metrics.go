package observability

import (
	"errors"
	"io"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// HTTPClientMetrics измеряет каждую попытку обращения к HTTP-зависимости.
type HTTPClientMetrics struct {
	requests               *prometheus.CounterVec
	duration, bodyDuration *prometheus.HistogramVec
}

// NewHTTPClientMetrics регистрирует метрики исходящего HTTP.
func NewHTTPClientMetrics(reg prometheus.Registerer) *HTTPClientMetrics {
	labels := []string{"dependency", "operation", "method", "status_code"}
	m := &HTTPClientMetrics{
		requests:     prometheus.NewCounterVec(prometheus.CounterOpts{Name: "cellestial_http_client_requests_total", Help: "Outgoing HTTP attempts."}, labels),
		duration:     prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "cellestial_http_client_request_duration_seconds", Help: "Outgoing HTTP attempt duration.", Buckets: LatencyBuckets()}, labels),
		bodyDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "cellestial_http_client_response_body_duration_seconds", Help: "Time from HTTP headers to completion or disposal of the response body.", Buckets: LatencyBuckets()}, append(append([]string(nil), labels...), "outcome")),
	}
	reg.MustRegister(m.requests, m.duration, m.bodyDuration)
	return m
}

// Measure вызывает транспорт и учитывает итог. operation возвращает только известное имя операции.
func (m *HTTPClientMetrics) Measure(dependency string, operation func(*http.Request) string, send func(*http.Request) (*http.Response, error), req *http.Request) (*http.Response, error) {
	start := time.Now()
	resp, err := send(req)
	code := 0
	if resp != nil {
		code = resp.StatusCode
	}
	values := []string{dependency, operation(req), HTTPMethod(req.Method), strconv.Itoa(code)}
	m.requests.WithLabelValues(values...).Inc()
	m.duration.WithLabelValues(values...).Observe(time.Since(start).Seconds())
	if err == nil && resp != nil && resp.Body != nil && resp.Body != http.NoBody && code != http.StatusSwitchingProtocols {
		bodyStart := time.Now()
		resp.Body = &observedResponseBody{ReadCloser: resp.Body, observe: func(outcome string) {
			m.bodyDuration.WithLabelValues(append(values, outcome)...).Observe(time.Since(bodyStart).Seconds())
		}}
	}
	return resp, err
}

// HTTPRoundTrip адаптирует функцию отправки к стандартному transport.
type HTTPRoundTrip func(*http.Request) (*http.Response, error)

// RoundTrip выполняет одну попытку HTTP.
func (send HTTPRoundTrip) RoundTrip(req *http.Request) (*http.Response, error) { return send(req) }

// observedResponseBody наблюдает итог чтения без буферизации и дополнительных запросов.
type observedResponseBody struct {
	io.ReadCloser
	once    sync.Once
	observe func(string)
}

// Read сохраняет исходный результат и отмечает EOF или ошибку чтения.
func (b *observedResponseBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	switch {
	case errors.Is(err, io.EOF):
		b.once.Do(func() { b.observe("complete") })
	case err != nil:
		b.once.Do(func() { b.observe("read_error") })
	}
	return n, err
}

// Close учитывает завершение без EOF и сохраняет ошибку исходного Close.
func (b *observedResponseBody) Close() error {
	err := b.ReadCloser.Close()
	b.once.Do(func() {
		if err != nil {
			b.observe("close_error")
			return
		}
		b.observe("closed")
	})
	return err
}
