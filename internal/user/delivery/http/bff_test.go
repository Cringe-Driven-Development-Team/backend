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
)

func TestBFFAuthContract(t *testing.T) {
	uc := &fakeUsecase{user: domain.User{ID: 1, Login: "bob"}, sess: newSession(1)}
	spec, err := api.GetSpec()
	if err != nil {
		t.Fatal(err)
	}
	strict := api.NewStrictHandlerWithOptions(testServer{userHandler: NewBFFHandler(uc)}, nil, api.StrictHTTPServerOptions{RequestErrorHandlerFunc: httperr.RequestError, ResponseErrorHandlerFunc: httperr.ResponseError})
	handler := security.AuthenticateBearer(fakeTokens{})(api.HandlerWithOptions(strict, api.GorillaServerOptions{BaseURL: "/api/v1", Middlewares: []api.MiddlewareFunc{middleware.Validator(spec, "/api/v1")}}))
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
			req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/"+tc.operation, strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
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
				if err != nil || pair.AccessToken != uc.sess.AccessToken || pair.RefreshToken != uc.sess.RefreshToken || pair.AccessExpiresIn < 899 || pair.AccessExpiresIn > 900 || pair.RefreshExpiresIn < 3599 || pair.RefreshExpiresIn > 3600 {
					t.Fatalf("invalid token response: %s", rec.Body)
				}
			}
			if tc.operation == "refresh" && tc.status == 200 && uc.gotRefresh != "body-refresh" {
				t.Fatal("refresh must use JSON token")
			}
			if tc.status == 204 && rec.Body.Len() != 0 {
				t.Fatal("logout must have no body")
			}
		})
	}
}
