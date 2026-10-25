package delivery

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/transport/http/api"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/transport/http/middleware"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/user/domain"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/user/usecase"
	"go.uber.org/mock/gomock"
)

type credentialsRequestKey struct{}

func validatedCredentialsRequest(t *testing.T, uc usecase.Usecase, operation, login, password string) *httptest.ResponseRecorder {
	t.Helper()
	spec, err := api.GetSpec()
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(api.Credentials{Login: login, Password: password})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/"+operation, strings.NewReader(string(body)))
	req = req.WithContext(context.WithValue(req.Context(), credentialsRequestKey{}, t.Name()))
	req.Header.Set("Content-Type", "application/json")
	// Здесь изолирован контракт ввода. Полная CSRF-цепочка проверяется в transport/http.
	req.Header.Set("X-CSRF-Token", "contract-test")
	rec := httptest.NewRecorder()
	middleware.Validator(spec, "/api/v1")(newRouter(t, uc)).ServeHTTP(rec, req)
	return rec
}

func TestRejectsInvalidLoginBeforeUsecase(t *testing.T) {
	for _, operation := range []string{"register", "login"} {
		for _, login := range []string{`x' OR '1'='1' --`, `<script>alert(1)</script>`} {
			t.Run(operation+"/"+login, func(t *testing.T) {
				uc := NewMockUsecase(gomock.NewController(t))
				rec := validatedCredentialsRequest(t, uc, operation, login, "password123")
				var result api.Error
				if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
					t.Fatal(err)
				}
				if rec.Code != http.StatusBadRequest || result.Code != api.ValidationError || rec.Header().Get("Content-Type") != "application/json" {
					t.Fatalf("invalid login reached usecase or unsafe response: status=%d body=%s", rec.Code, rec.Body)
				}
			})
		}
	}
}

func TestPasswordPreservesSpecialCharacters(t *testing.T) {
	const password = `a<&>"'12345`
	for _, tc := range []struct {
		operation string
		status    int
	}{{"register", http.StatusCreated}, {"login", http.StatusOK}} {
		t.Run(tc.operation, func(t *testing.T) {
			uc := NewMockUsecase(gomock.NewController(t))
			if tc.operation == "register" {
				uc.EXPECT().Register(gomock.Cond(func(ctx context.Context) bool { return ctx.Value(credentialsRequestKey{}) == t.Name() }), "alice", password).Return(domain.User{ID: 1, Login: "alice"}, newSession(1), nil)
			} else {
				uc.EXPECT().Login(gomock.Cond(func(ctx context.Context) bool { return ctx.Value(credentialsRequestKey{}) == t.Name() }), "alice", password).Return(domain.User{ID: 1, Login: "alice"}, newSession(1), nil)
			}
			rec := validatedCredentialsRequest(t, uc, tc.operation, "alice", password)
			if rec.Code != tc.status {
				t.Fatalf("password changed: status=%d body=%s", rec.Code, rec.Body)
			}
		})
	}
}
