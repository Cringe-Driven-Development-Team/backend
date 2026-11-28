package productconfig

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/observability"
	"github.com/prometheus/client_golang/prometheus"
)

// TestReadSettings проверяет передачу сессии, идентичности Workspace и request ID.
func TestReadSettings(t *testing.T) {
	for _, tc := range []struct {
		name    string
		status  int
		body    string
		want    Settings
		wantErr error
	}{
		{"success", 200, `{"notebook_max_cells":3,"notebook_run_all":false}`, Settings{MaxCells: 3}, nil},
		{"unavailable", 503, `{"code":"onlineconf_unavailable"}`, Settings{}, ErrUnavailable},
		{"incomplete response", 200, `{"notebook_max_cells":3}`, Settings{}, ErrUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet || r.URL.Path != Path || r.Header.Get("Authorization") != "Bearer session" ||
					r.Header.Get(WorkspaceKeyHeader) != "workspace" || r.Header.Get("X-BFF-Key") != "" || r.Header.Get("X-Request-ID") != "request-1" {
					t.Error("unexpected OnlineConf request")
				}
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			read := NewReader(server.URL, "workspace", nil, observability.NewHTTPClientMetrics(prometheus.NewRegistry()))
			ctx := WithBearer(observability.WithRequestID(t.Context(), "request-1"), "session")
			if got, err := read(ctx); got != tc.want || !errors.Is(err, tc.wantErr) {
				t.Fatalf("settings = %+v, %v", got, err)
			}
		})
	}
}
