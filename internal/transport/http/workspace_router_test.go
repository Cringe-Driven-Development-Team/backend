package httptransport

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/authn"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/observability"
	runtimedelivery "github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/runtimesession/delivery/http"
	"github.com/google/uuid"
)

type routerVerifier func(context.Context, string) (authn.Principal, error)

func (f routerVerifier) Verify(ctx context.Context, token string) (authn.Principal, error) {
	return f(ctx, token)
}

// TestWorkspaceRouterBoundary проверяет публичные чтения, закрытые файлы и маршруты чужих сервисов.
func TestWorkspaceRouterBoundary(t *testing.T) {
	fileRuntime := &filesRuntime{}
	calls := 0
	verify := routerVerifier(func(ctx context.Context, token string) (authn.Principal, error) {
		calls++
		if token != "session-token" || observability.RequestID(ctx) != "workspace-test" {
			t.Errorf("unexpected session verification input")
		}
		return authn.Principal{UserID: 7}, nil
	})
	handler, err := NewWorkspaceRouter(WorkspaceHandlers{
		NotebookHandler: &NotebookHandler{},
		RuntimeHandler:  runtimedelivery.NewHandler(fileRuntime),
	}, verify, "workspace-test-key", nil)
	if err != nil {
		t.Fatal(err)
	}
	files := BaseURL + "/runtime-sessions/" + uuid.NewString() + "/workspace/"
	for _, tc := range []struct {
		name, method, path, key, auth string
		status, verifyCalls           int
		reachesFiles                  bool
	}{
		{"public missing BFF key", "GET", BaseURL + "/notebooks/invalid", "", "", 403, 0, false},
		{"session before validation", "GET", BaseURL + "/notebooks/invalid", "workspace-test-key", "", 401, 0, false},
		{"files require session", "POST", files + "command", "workspace-test-key", "", 401, 0, false},
		{"files verified", "POST", files + "command", "workspace-test-key", "Bearer session-token", 200, 1, true},
		{"unknown file route", "GET", files + "unknown", "workspace-test-key", "Bearer session-token", 404, 1, false},
		{"wrong file method", "GET", files + "command", "workspace-test-key", "Bearer session-token", 405, 1, false},
		{"identity route not owned", "GET", BaseURL + "/users/me", "workspace-test-key", "Bearer session-token", 404, 1, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls = 0
			fileRuntime.called = false
			req := httptest.NewRequest(tc.method, tc.path, strings.NewReader("{}"))
			req.Header.Set("X-Request-ID", "workspace-test")
			if tc.key != "" {
				req.Header.Set("X-BFF-Key", tc.key)
			}
			if tc.auth != "" {
				req.Header.Set("Authorization", tc.auth)
			}
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)
			if w.Code != tc.status || calls != tc.verifyCalls || fileRuntime.called != tc.reachesFiles || w.Header().Get("X-Request-ID") != "workspace-test" {
				t.Fatalf("status=%d verifier=%d files=%v request_id=%q", w.Code, calls, fileRuntime.called, w.Header().Get("X-Request-ID"))
			}
		})
	}
}
