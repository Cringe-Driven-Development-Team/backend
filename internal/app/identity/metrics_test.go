package identity

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// TestMetricsBoundedLabels не позволяет случайным URL/методам создавать серии на каждый запрос.
func TestMetricsBoundedLabels(t *testing.T) {
	pool, err := pgxpool.New(context.Background(), "postgres://unused@127.0.0.1/unused")
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	metrics := newServiceMetrics("test", pool)
	h := metrics.middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(403) }))
	for i := range 1000 {
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(fmt.Sprintf("RANDOM%d", i), fmt.Sprintf("/api/v1/private/%d", i), nil))
	}
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", nil))
	families, err := metrics.registry.Gather()
	if err != nil {
		t.Fatal(err)
	}
	authFound := false
	for _, f := range families {
		switch f.GetName() {
		case "identity_http_requests_total":
			if len(f.Metric) != 2 {
				t.Fatalf("unbounded HTTP cardinality: %d", len(f.Metric))
			}
		case "identity_auth_operations_total":
			authFound = true
			if len(f.Metric) != 1 {
				t.Fatal("unexpected auth series")
			}
		}
	}
	if !authFound {
		t.Fatal("auth outcome metric missing")
	}
}
