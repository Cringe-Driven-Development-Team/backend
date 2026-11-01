package httptransport

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	usersecurity "github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/user/adapters/security"
	userdelivery "github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/user/delivery/http"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/user/usecase"
)

type logoutAllUsecase struct {
	usecase.Usecase
	err          error
	userID       int64
	requestValue any
}

type logoutAllContextKey struct{}

func (u *logoutAllUsecase) RevokeAll(ctx context.Context, id int64) error {
	u.userID = id
	u.requestValue = ctx.Value(logoutAllContextKey{})
	return u.err
}

func TestLogoutAllUserSessions(t *testing.T) {
	for _, tc := range []struct {
		name        string
		key, bearer string
		err         error
		want        int
		wantUserID  int64
	}{
		{name: "all sessions revoked without request body", key: "test-key", bearer: "valid", want: 204, wantUserID: 42},
		{name: "missing bearer", key: "test-key", want: 401},
		{name: "invalid bearer", key: "test-key", bearer: "invalid", want: 401},
		{name: "S2S before bearer", want: 403},
		{name: "wrong S2S key", key: "wrong", bearer: "valid", want: 403},
		{name: "revocation failure", key: "test-key", bearer: "valid", err: errors.New("session store unavailable"), want: 500, wantUserID: 42},
	} {
		t.Run(tc.name, func(t *testing.T) {
			users := &logoutAllUsecase{err: tc.err}
			tokens := usersecurity.NewAccessToken([]byte("logout-all-test"), time.Minute)
			access := tc.bearer
			if access == "valid" {
				var err error
				access, err = tokens.Issue(testClaims(42))
				if err != nil {
					t.Fatal(err)
				}
			}
			h, err := NewRouter(Server{UserHandler: userdelivery.NewHandler(users)}, tokens, allowSessions{}, "test-key")
			if err != nil {
				t.Fatal(err)
			}
			req := httptest.NewRequest(http.MethodPost, BaseURL+"/auth/logout-all", nil)
			req.Header.Set("X-BFF-Key", tc.key)
			if access != "" {
				req.Header.Set("Authorization", "Bearer "+access)
			}
			req = req.WithContext(context.WithValue(req.Context(), logoutAllContextKey{}, "request-context"))
			res := serveRouter(h, req)
			if res.Code != tc.want {
				t.Fatalf("status = %d, want %d: %s", res.Code, tc.want, res.Body)
			}
			if users.userID != tc.wantUserID {
				t.Fatalf("revoked user = %d, want %d", users.userID, tc.wantUserID)
			}
			if tc.wantUserID != 0 && users.requestValue != "request-context" {
				t.Fatal("request context lost")
			}
			if tc.want == 204 && res.Body.Len() != 0 {
				t.Fatal("logout-all must return no body")
			}
		})
	}
}
