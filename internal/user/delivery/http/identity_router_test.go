package delivery

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/user/usecase"
)

// identityTokens отклоняет токены в проверке границ HTTP без вызова прикладного слоя.
type identityTokens struct{}

func (identityTokens) Parse(string) (usecase.SessionClaims, error) {
	return usecase.SessionClaims{}, usecase.ErrInvalidSession
}

// identitySessions задаёт доступное хранилище для проверки порядка middleware.
type identitySessions struct{}

func (identitySessions) ValidateAccess(context.Context, usecase.SessionClaims) error { return nil }

// TestIdentityHTTPBoundary проверяет S2S до JSON и авторизации, а также отсутствие чужих маршрутов.
func TestIdentityHTTPBoundary(t *testing.T) {
	h, err := NewIdentityRouter(NewHandler(nil), identityTokens{}, identitySessions{}, "test-bff")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, method, path, key, body string
		want                          int
	}{
		{"no key", http.MethodPost, "/api/v1/auth/register", "", "{", 403},
		{"wrong key", http.MethodPost, "/api/v1/auth/login", "wrong", "{", 403},
		{"options guarded", http.MethodOptions, "/api/v1/auth/login", "", "", 403},
		{"unknown guarded", http.MethodGet, "/api/v1/notebooks", "", "", 403},
		{"workspace unavailable", http.MethodGet, "/api/v1/notebooks", "test-bff", "", 404},
		{"profile requires bearer", http.MethodGet, "/api/v1/users/me", "test-bff", "", 401},
		{"invalid JSON", http.MethodPost, "/api/v1/auth/register", "test-bff", "{", 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			if tc.key != "" {
				r.Header.Set("X-BFF-Key", tc.key)
			}
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set("X-Request-ID", "identity-http-test")
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != tc.want {
				t.Fatalf("status %d, want %d: %s", w.Code, tc.want, w.Body.String())
			}
			if w.Header().Get("X-Request-ID") != "identity-http-test" {
				t.Fatal("correlation lost")
			}
			if tc.want == 403 && !strings.Contains(w.Body.String(), "s2s_forbidden") {
				t.Fatal(w.Body.String())
			}
		})
	}
}
