package rpc

import (
	"strings"
	"time"

	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/observability"
	"github.com/prometheus/client_golang/prometheus"
	"google.golang.org/protobuf/proto"
)

// Metrics объединяет измерения RPC всех направлений процесса в одном реестре.
type Metrics struct {
	requests        *prometheus.CounterVec
	unary, stream   *prometheus.HistogramVec
	active          *prometheus.GaugeVec
	messages, bytes *prometheus.CounterVec
}

// NewMetrics подключает общие семейства к реестру процесса.
func NewMetrics(reg prometheus.Registerer) *Metrics {
	method := []string{"grpc_service", "grpc_method", "rpc_type", "rpc_side"}
	calls := append(append([]string(nil), method...), "grpc_code")
	message := append(append([]string(nil), method...), "direction")
	m := &Metrics{
		requests: prometheus.NewCounterVec(prometheus.CounterOpts{Name: "cellestial_grpc_calls_total", Help: "Completed gRPC calls."}, calls),
		unary:    prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "cellestial_grpc_call_duration_seconds", Help: "Unary gRPC duration in seconds.", Buckets: observability.LatencyBuckets()}, calls),
		stream:   prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "cellestial_grpc_stream_duration_seconds", Help: "gRPC stream lifetime in seconds.", Buckets: observability.StreamBuckets()}, calls),
		active:   prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "cellestial_grpc_active_streams", Help: "Active gRPC streams."}, method),
		messages: prometheus.NewCounterVec(prometheus.CounterOpts{Name: "cellestial_grpc_stream_messages_total", Help: "Successfully transferred stream messages."}, message),
		bytes:    prometheus.NewCounterVec(prometheus.CounterOpts{Name: "cellestial_grpc_stream_message_bytes_total", Help: "Uncompressed protobuf stream bytes."}, message),
	}
	reg.MustRegister(m.requests, m.unary, m.stream, m.active, m.messages, m.bytes)
	return m
}

// rpcLabels разбирает заранее нормализованный interceptor полный метод.
func rpcLabels(method, kind, side string) []string {
	service, operation, ok := strings.Cut(strings.TrimPrefix(method, "/"), "/")
	if !ok {
		service, operation = "unmatched", "unmatched"
	}
	return []string{service, operation, kind, side}
}

// begin начинает измерение unary или stream и возвращает callback для итогового статуса.
func (m *Metrics) begin(method, kind, side string) func(string) {
	if m == nil {
		return func(string) {}
	}
	labels := rpcLabels(method, kind, side)
	histogram := m.unary
	if kind == "stream" {
		histogram = m.stream
		m.active.WithLabelValues(labels...).Inc()
	}
	started := time.Now()
	return func(code string) {
		if kind == "stream" {
			m.active.WithLabelValues(labels...).Dec()
		}
		values := append(rpcLabels(method, kind, side), code)
		m.requests.WithLabelValues(values...).Inc()
		histogram.WithLabelValues(values...).Observe(time.Since(started).Seconds())
	}
}

// message измеряет успешно переданное protobuf-сообщение потока.
func (m *Metrics) message(method, side, direction string, value any) {
	if m == nil {
		return
	}
	msg, ok := value.(proto.Message)
	if !ok {
		return
	}
	values := append(rpcLabels(method, "stream", side), direction)
	m.messages.WithLabelValues(values...).Inc()
	m.bytes.WithLabelValues(values...).Add(float64(proto.Size(msg)))
}
