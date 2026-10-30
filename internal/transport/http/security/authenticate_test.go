package security

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/transport/http/principal"
	"go.uber.org/mock/gomock"
)

func TestAuthenticateTokenBoundary(t *testing.T) {
	for _, tc := range []struct {
		name, token    string
		present, valid bool
	}{
		{name: "missing"}, {name: "empty", present: true}, {name: "invalid", present: true, token: "bad"}, {name: "valid", present: true, token: "access", valid: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			parser := NewMockTokenParser(gomock.NewController(t))
			if tc.token != "" {
				var err error
				if !tc.valid {
					err = errors.New("invalid token")
				}
				parser.EXPECT().Parse(tc.token).Return(testClaims(7), err)
			}
			type contextKey struct{}
			req := httptest.NewRequest(http.MethodGet, "/", nil).WithContext(context.WithValue(context.Background(), contextKey{}, "preserved"))
			if tc.present {
				req.Header.Set("Authorization", "Bearer "+tc.token)
			}
			called := false
			next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				called = true
				id, ok := principal.UserIDFromContext(r.Context())
				if ok != tc.valid || (ok && id != 7) || r.Context().Value(contextKey{}) != "preserved" {
					t.Fatalf("principal=%d present=%v context=%v", id, ok, r.Context())
				}
				w.WriteHeader(http.StatusNoContent)
			})
			rec := httptest.NewRecorder()
			AuthenticateBearer(parser, allowSessions{})(next).ServeHTTP(rec, req)
			if !called || rec.Code != http.StatusNoContent {
				t.Fatalf("called=%v status=%d", called, rec.Code)
			}
		})
	}
}
