package delivery

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/transport/http/api"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/transport/http/httperr"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/transport/http/middleware"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/transport/http/security"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/user/domain"
	"go.uber.org/mock/gomock"
)

func TestBFFAuthContract(t *testing.T) {
	sess := newSession(1)
	spec, err := api.GetSpec()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		operation, body, bearer string
		status                  int
	}{
		{"register", `{"login":"bob","password":"password123"}`, "", 201},
		{"login", `{"login":"bob","password":"password123"}`, "", 200},
		{"refresh", `{"refresh_token":"body-refresh"}`, "", 200},
		{"refresh", `{}`, "", 400},
		{"refresh", `{"refresh_token":""}`, "", 400},
		{"refresh", `{`, "", 400},
		{"logout", `{"refresh_token":"body-refresh"}`, "Bearer access-1", 204},
		{"logout", `{"refresh_token":"body-refresh"}`, "", 401},
	} {
		t.Run(tc.operation+tc.body+tc.bearer, func(t *testing.T) {
			uc := NewMockUsecase(gomock.NewController(t))
			switch {
			case tc.operation == "register":
				uc.EXPECT().Register(gomock.Any(), "bob", "password123").Return(domain.User{ID: 1, Login: "bob"}, sess, nil)
			case tc.operation == "login":
				uc.EXPECT().Login(gomock.Any(), "bob", "password123").Return(domain.User{ID: 1, Login: "bob"}, sess, nil)
			case tc.operation == "refresh" && tc.status == 200:
				uc.EXPECT().Refresh(gomock.Any(), "body-refresh").Return(sess, nil)
			case tc.operation == "logout" && tc.status == 204:
				uc.EXPECT().Logout(gomock.Any(), "body-refresh").Return(nil)
			}
			strict := api.NewStrictHandlerWithOptions(testServer{userHandler: NewHandler(uc)}, nil, api.StrictHTTPServerOptions{RequestErrorHandlerFunc: httperr.RequestError, ResponseErrorHandlerFunc: httperr.ResponseError})
			handler := security.AuthenticateBearer(testTokens(t))(api.HandlerWithOptions(strict, api.GorillaServerOptions{BaseURL: "/api/v1", Middlewares: []api.MiddlewareFunc{middleware.Validator(spec, "/api/v1")}}))
			req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/"+tc.operation, strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-BFF-Key", "test-key")
			req.Header.Set("Authorization", tc.bearer)
			req.AddCookie(&http.Cookie{Name: "refresh_token", Value: "cookie-refresh"})
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != tc.status {
				t.Fatalf("status=%d want=%d body=%s", rec.Code, tc.status, rec.Body)
			}
			if len(rec.Result().Cookies()) != 0 {
				t.Fatal("BFF response must not set browser cookies")
			}
			if tc.status == 200 || tc.status == 201 {
				var pair api.TokenPair
				if tc.operation == "refresh" {
					err = json.Unmarshal(rec.Body.Bytes(), &pair)
				} else {
					var auth api.AuthResponse
					err = json.Unmarshal(rec.Body.Bytes(), &auth)
					pair = auth.Tokens
					if auth.User.Id != 1 {
						t.Fatal("missing user")
					}
				}
				if err != nil || pair.AccessToken != sess.AccessToken || pair.RefreshToken != sess.RefreshToken || pair.AccessExpiresIn < 899 || pair.AccessExpiresIn > 900 || pair.RefreshExpiresIn < 3599 || pair.RefreshExpiresIn > 3600 {
					t.Fatalf("invalid token response: %s", rec.Body)
				}
			}
			if tc.status == 204 && rec.Body.Len() != 0 {
				t.Fatal("logout must have no body")
			}
		})
	}
}
