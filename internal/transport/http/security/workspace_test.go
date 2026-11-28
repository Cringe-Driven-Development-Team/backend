package security

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/authn"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/transport/http/principal"
)

type workspaceVerifier func(context.Context, string) (authn.Principal, error)

func (f workspaceVerifier) Verify(ctx context.Context, token string) (authn.Principal, error) {
	return f(ctx, token)
}

func TestWorkspaceAuthUsesIdentity(t *testing.T) {
	for _, tc := range []struct {
		name, method, key, token string
		verifyErr                error
		status, calls            int
		anonymous                bool
	}{
		{"service key checked first", "GET", "", "Bearer token", nil, 403, 0, false},
		{"guest", "GET", "key", "", nil, 204, 0, true},
		{"private", "POST", "key", "", nil, 401, 0, false},
		{"bad bearer on public", "GET", "key", "Bearer x y", nil, 401, 0, false},
		{"revoked on public", "GET", "key", "Bearer revoked", authn.ErrInvalidSession, 401, 1, false},
		{"dependency outage", "GET", "key", "Bearer token", authn.ErrSessionUnavailable, 503, 1, false},
		{"dependency deadline", "GET", "key", "Bearer token", context.DeadlineExceeded, 504, 1, false},
		{"verified", "POST", "key", "Bearer token", nil, 204, 1, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			verify := workspaceVerifier(func(_ context.Context, _ string) (authn.Principal, error) {
				calls++
				return authn.Principal{UserID: 42}, tc.verifyErr
			})
			handler := S2S("key", "/api/v1")(AuthenticateWorkspace(verify)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				p, ok := authn.FromContext(r.Context())
				id, identified := principal.UserIDFromContext(r.Context())
				if !ok || p.Anonymous != tc.anonymous || identified == tc.anonymous || (identified && id != 42) {
					t.Errorf("principal: %+v, user %d, present %v", p, id, identified)
				}
				w.WriteHeader(204)
			})))
			req := httptest.NewRequest(tc.method, "/api/v1/notebooks/42", nil)
			req.Header.Set("X-BFF-Key", tc.key)
			if tc.token != "" {
				req.Header.Set("Authorization", tc.token)
			}
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != tc.status || calls != tc.calls {
				t.Fatalf("status %d calls %d, body %s", rec.Code, calls, rec.Body)
			}
		})
	}
}
