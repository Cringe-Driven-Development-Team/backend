package rpc

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"google.golang.org/protobuf/proto"
)

// Metrics владеет ограниченными labels unary RPC; имя метода нормализует interceptor.
type Metrics struct {
	requests        *prometheus.CounterVec
	duration        *prometheus.HistogramVec
	active          *prometheus.GaugeVec
	messages, bytes *prometheus.CounterVec
}

// NewMetrics регистрирует счётчики в отдельном registry процесса.
func NewMetrics(reg prometheus.Registerer, namespace string) *Metrics {
	m := &Metrics{requests: prometheus.NewCounterVec(prometheus.CounterOpts{Namespace: namespace, Name: "rpc_requests_total", Help: "Completed RPC calls."}, []string{"method", "status"}), duration: prometheus.NewHistogramVec(prometheus.HistogramOpts{Namespace: namespace, Name: "rpc_duration_seconds", Help: "RPC duration.", Buckets: prometheus.DefBuckets}, []string{"method", "status"}), active: prometheus.NewGaugeVec(prometheus.GaugeOpts{Namespace: namespace, Name: "rpc_active", Help: "Active RPC calls."}, []string{"method"})}
	m.messages = prometheus.NewCounterVec(prometheus.CounterOpts{Namespace: namespace, Name: "rpc_messages_total", Help: "Successfully transferred RPC messages."}, []string{"method", "direction"})
	m.bytes = prometheus.NewCounterVec(prometheus.CounterOpts{Namespace: namespace, Name: "rpc_message_bytes_total", Help: "Uncompressed protobuf bytes transferred."}, []string{"method", "direction"})
	reg.MustRegister(m.requests, m.duration, m.active, m.messages, m.bytes)
	return m
}

// begin возвращает завершение измерения без идентификаторов пользователей/запросов.
func (m *Metrics) begin(method string) func(string) {
	if m == nil {
		return func(string) {}
	}
	m.active.WithLabelValues(method).Inc()
	started := time.Now()
	return func(code string) {
		m.active.WithLabelValues(method).Dec()
		m.requests.WithLabelValues(method, code).Inc()
		m.duration.WithLabelValues(method, code).Observe(time.Since(started).Seconds())
	}
}

// message измеряет размер protobuf без преобразования payload в labels или лог.
func (m *Metrics) message(method, direction string, value any) {
	if m == nil {
		return
	}
	msg, ok := value.(proto.Message)
	if !ok {
		return
	}
	m.messages.WithLabelValues(method, direction).Inc()
	m.bytes.WithLabelValues(method, direction).Add(float64(proto.Size(msg)))
}
