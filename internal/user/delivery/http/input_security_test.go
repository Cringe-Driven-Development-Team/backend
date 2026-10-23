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
)

type credentialCapture struct {
	fakeUsecase
	password string
	calls    int
}

func (uc *credentialCapture) Register(ctx context.Context, login, password string) (domain.User, usecase.Session, error) {
	uc.calls++
	uc.password = password
	return uc.fakeUsecase.Register(ctx, login, password)
}

func (uc *credentialCapture) Login(ctx context.Context, login, password string) (domain.User, usecase.Session, error) {
	uc.calls++
	uc.password = password
	return uc.fakeUsecase.Login(ctx, login, password)
}

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
	req.Header.Set("Content-Type", "application/json")
	// Здесь изолирован контракт ввода. Полная CSRF-цепочка проверяется в transport/http.
	req.Header.Set("X-CSRF-Token", "contract-test")
	rec := httptest.NewRecorder()
	middleware.Validator(spec, "/api/v1")(newRouter(uc)).ServeHTTP(rec, req)
	return rec
}

func TestRejectsInvalidLoginBeforeUsecase(t *testing.T) {
	for _, operation := range []string{"register", "login"} {
		for _, login := range []string{`x' OR '1'='1' --`, `<script>alert(1)</script>`} {
			t.Run(operation+"/"+login, func(t *testing.T) {
				uc := &credentialCapture{}
				rec := validatedCredentialsRequest(t, uc, operation, login, "password123")
				var result api.Error
				if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
					t.Fatal(err)
				}
				if rec.Code != http.StatusBadRequest || result.Code != api.ValidationError || uc.calls != 0 || rec.Header().Get("Content-Type") != "application/json" {
					t.Fatalf("invalid login reached usecase or unsafe response: calls=%d status=%d body=%s", uc.calls, rec.Code, rec.Body)
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
			uc := &credentialCapture{fakeUsecase: fakeUsecase{user: domain.User{ID: 1, Login: "alice"}, sess: newSession(1)}}
			rec := validatedCredentialsRequest(t, uc, tc.operation, "alice", password)
			if rec.Code != tc.status || uc.calls != 1 || uc.password != password {
				t.Fatalf("password changed: status=%d calls=%d got=%q body=%s", rec.Code, uc.calls, uc.password, rec.Body)
			}
		})
	}
}
