package observability

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
)

// TestResponseBodyFailure проверяет ошибку потока после успешных заголовков.
func TestResponseBodyFailure(t *testing.T) {
	registry := prometheus.NewRegistry()
	m := NewHTTPClientMetrics(registry)
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	resp, err := m.Measure("s3", func(*http.Request) string { return "GetObject" }, func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(brokenResponse{})}, nil
	}, req)
	if err != nil {
		t.Fatal(err)
	}
	_, err = io.ReadAll(resp.Body)
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatal(err)
	}
	if err = resp.Body.Close(); err != nil {
		t.Fatal(err)
	}
	families, err := registry.Gather()
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range families {
		if f.GetName() == "cellestial_http_client_response_body_duration_seconds" && len(f.Metric) == 1 {
			if f.Metric[0].GetHistogram().GetSampleCount() != 1 {
				t.Fatal("duplicate body observation")
			}
			for _, label := range f.Metric[0].Label {
				if label.GetName() == "outcome" && label.GetValue() == "read_error" {
					return
				}
			}
		}
	}
	t.Fatal("response body failure is missing")
}

type brokenResponse struct{}

// Read имитирует обрыв передачи.
func (brokenResponse) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }
