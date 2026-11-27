package observability

import "net/http"

// LatencyBuckets задаёт границы HTTP и unary RPC в секундах.
func LatencyBuckets() []float64 {
	return []float64{.005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10, 30, 60, 120, 300, 600}
}

// StreamBuckets задаёт границы времени жизни RPC-потока в секундах.
func StreamBuckets() []float64 {
	return []float64{.01, .05, .1, .25, .5, 1, 2.5, 5, 10, 30, 60, 120, 300, 600, 1800, 3600, 7200}
}

// HTTPMethod ограничивает набор меток стандартными HTTP-методами.
func HTTPMethod(method string) string {
	switch method {
	case http.MethodConnect, http.MethodDelete, http.MethodGet, http.MethodHead,
		http.MethodOptions, http.MethodPatch, http.MethodPost, http.MethodPut, http.MethodTrace:
		return method
	default:
		return "other"
	}
}
