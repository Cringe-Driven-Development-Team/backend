package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/mux"
	"github.com/prometheus/client_golang/prometheus"
)

// TestHTTPMonitoringRoutes проверяет ранний отказ, 405 и ошибку после заголовков.
func TestHTTPMonitoringRoutes(t *testing.T) {
	registry := prometheus.NewRegistry()
	router := mux.NewRouter()
	router.HandleFunc("/files/{id}", func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(15 * time.Millisecond)
		w.WriteHeader(http.StatusPartialContent)
		MarkResponseFailure(r)
	}).Methods(http.MethodGet)
	router.MethodNotAllowedHandler = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusMethodNotAllowed)
	})
	early := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Test-Reject") != "" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		router.ServeHTTP(w, r)
	})
	handler := ObserveHTTP(early, nil, NewHTTPMetrics(registry), func(r *http.Request) string {
		return RegisteredRoute(router, r)
	})
	for _, sample := range []struct {
		method, path string
		reject       bool
	}{
		{"GET", "/files/secret-id?token=secret-value", true},
		{"POST", "/files/987", false},
		{"GET", "/files/123", false},
		{"CUSTOM", "/elsewhere", false},
	} {
		request := httptest.NewRequest(sample.method, sample.path, nil)
		if sample.reject {
			request.Header.Set("X-Test-Reject", "yes")
		}
		handler.ServeHTTP(httptest.NewRecorder(), request)
	}
	families, err := registry.Gather()
	if err != nil {
		t.Fatal(err)
	}
	results := map[string]string{}
	var latencyOK, streamFailure bool
	for _, f := range families {
		for _, sample := range f.Metric {
			labels := map[string]string{}
			for _, label := range sample.Label {
				labels[label.GetName()] = label.GetValue()
			}
			if strings.Contains(labels["route"], "secret") {
				t.Fatal("request content leaked to route label")
			}
			switch f.GetName() {
			case "cellestial_http_requests_total":
				results[labels["status_code"]] = labels["method"] + " " + labels["route"]
				if sample.Counter.GetValue() != 1 {
					t.Fatal("request counted more than once")
				}
			case "cellestial_http_stream_errors_total":
				streamFailure = true
				if labels["status_code"] != "206" || sample.Counter.GetValue() != 1 {
					t.Fatal("stream failure should keep the committed status")
				}
			case "cellestial_http_request_duration_seconds":
				if labels["status_code"] == "206" {
					latencyOK = sample.Histogram.GetSampleSum() >= 0.015
				}
			}
		}
	}
	if len(results) != 4 || results["403"] != "GET /files/{id}" || results["405"] != "POST /files/{id}" ||
		results["206"] != "GET /files/{id}" || results["404"] != "other unmatched" || !latencyOK || !streamFailure {
		t.Fatalf("unexpected metrics: %v, injected latency recorded = %v, stream failure recorded = %v", results, latencyOK, streamFailure)
	}
}
