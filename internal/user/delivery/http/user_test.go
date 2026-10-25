package delivery

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	notebookdelivery "github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/notebook/delivery/http"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/observability"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/transport/http/api"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/transport/http/cookies"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/transport/http/httperr"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/transport/http/middleware"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/transport/http/security"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/user/domain"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/user/usecase"
	"github.com/google/go-cmp/cmp"
	"github.com/gorilla/mux"
	"go.uber.org/mock/gomock"
)

// Встроить два типа с именем Handler нельзя, поэтому псевдоним.
type userHandler = Handler

type testServer struct {
	*userHandler
	*notebookdelivery.Handler
}

// newRouter собирает сервер как в main, но без валидатора:
// проверяется хендлер вместе со сгенерированным кодом и httperr.
func newRouter(t *testing.T, uc usecase.Usecase) http.Handler {
	h := NewHandler(uc, cookies.Config{CSRFTTL: time.Hour, AccessPath: "/api/v1", RefreshPath: "/api/v1/auth", AccessTTL: 15 * time.Minute}, testCSRF())
	strict := api.NewStrictHandlerWithOptions(testServer{h, notebookdelivery.NewHandler(nil)}, nil, api.StrictHTTPServerOptions{
		RequestErrorHandlerFunc:  httperr.RequestError,
		ResponseErrorHandlerFunc: httperr.ResponseError,
	})
	return api.HandlerWithOptions(strict, api.GorillaServerOptions{
		BaseURL:          "/api/v1",
		BaseRouter:       mux.NewRouter(),
		Middlewares:      []api.MiddlewareFunc{security.Authenticate(testTokens(t))},
		ErrorHandlerFunc: httperr.RequestError,
	})
}

func newSession(userID int64) usecase.Session {
	return usecase.Session{
		UserID:       userID,
		AccessToken:  "access-" + strconv.FormatInt(userID, 10),
		RefreshToken: "new-refresh",
		ExpiresAt:    time.Now().Add(time.Hour),
	}
}

func TestHandlerErrors(t *testing.T) {
	tests := []struct {
		name       string
		method     string
		path       string
		body       string
		refresh    string
		err        error
		wantStatus int
		want       api.Error
	}{
		{
			name: "регистрация: логин занят", method: http.MethodPost, path: "/auth/register",
			body: `{"login":"alice","password":"password123"}`, err: usecase.ErrLoginTaken,
			wantStatus: http.StatusConflict,
			want:       api.Error{Code: api.LoginTaken, Message: "login is already taken"},
		},
		{
			name: "регистрация: длинный пароль", method: http.MethodPost, path: "/auth/register",
			body: `{"login":"alice","password":"password123"}`, err: usecase.ErrPasswordTooLong,
			wantStatus: http.StatusBadRequest,
			want:       api.Error{Code: api.ValidationError, Message: "password is longer than 72 bytes"},
		},
		{
			name: "регистрация: невалидный JSON", method: http.MethodPost, path: "/auth/register",
			body:       `{"login":`,
			wantStatus: http.StatusBadRequest,
			want:       api.Error{Code: api.ValidationError, Message: "can't decode JSON body: unexpected EOF"},
		},
		{
			name: "регистрация: ошибка базы", method: http.MethodPost, path: "/auth/register",
			body: `{"login":"alice","password":"password123"}`, err: errors.New("connection refused"),
			wantStatus: http.StatusInternalServerError,
			want:       api.Error{Code: api.Internal, Message: "internal server error"},
		},
		{
			name: "вход: неверный пароль", method: http.MethodPost, path: "/auth/login",
			body: `{"login":"alice","password":"wrong-password"}`, err: usecase.ErrInvalidCredentials,
			wantStatus: http.StatusUnauthorized,
			want:       api.Error{Code: api.InvalidCredentials, Message: "invalid login or password"},
		},
		{
			name: "refresh без cookie", method: http.MethodPost, path: "/auth/refresh",
			wantStatus: http.StatusUnauthorized,
			want:       api.Error{Code: api.Unauthorized, Message: "refresh token is missing"},
		},
		{
			name: "refresh: чужой токен", method: http.MethodPost, path: "/auth/refresh",
			refresh: "stolen", err: usecase.ErrInvalidRefresh,
			wantStatus: http.StatusUnauthorized,
			want:       api.Error{Code: api.Unauthorized, Message: "refresh token is invalid or expired"},
		},
		{
			name: "logout: чужой токен", method: http.MethodPost, path: "/auth/logout",
			refresh: "stolen", err: usecase.ErrInvalidRefresh,
			wantStatus: http.StatusUnauthorized,
			want:       api.Error{Code: api.Unauthorized, Message: "refresh token is invalid or expired"},
		},
		{
			name: "профиль: пользователя удалили", method: http.MethodGet, path: "/users/me",
			err:        usecase.ErrUserNotFound,
			wantStatus: http.StatusUnauthorized,
			want:       api.Error{Code: api.Unauthorized, Message: "user no longer exists"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, "/api/v1"+tt.path, strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")
			req.AddCookie(&http.Cookie{Name: "access_token", Value: "access-1"})
			if tt.refresh != "" {
				req.AddCookie(&http.Cookie{Name: "refresh_token", Value: tt.refresh})
			}
			rec := httptest.NewRecorder()

			uc := NewMockUsecase(gomock.NewController(t))
			if tt.err != nil {
				switch tt.path {
				case "/auth/register":
					uc.EXPECT().Register(gomock.Any(), "alice", "password123").Return(domain.User{}, usecase.Session{}, tt.err)
				case "/auth/login":
					uc.EXPECT().Login(gomock.Any(), "alice", "wrong-password").Return(domain.User{}, usecase.Session{}, tt.err)
				case "/auth/refresh":
					uc.EXPECT().RefreshUserID(gomock.Any(), tt.refresh).Return(int64(0), tt.err)
				case "/auth/logout":
					uc.EXPECT().RefreshUserID(gomock.Any(), tt.refresh).Return(int64(0), tt.err)
				case "/users/me":
					uc.EXPECT().CurrentUser(gomock.Any(), int64(1)).Return(domain.User{}, tt.err)
				}
			}
			newRouter(t, uc).ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d; body %s", rec.Code, tt.wantStatus, rec.Body)
			}
			if len(rec.Header().Values("Set-Cookie")) != 0 || rec.Header().Get("Authorization") != "" {
				t.Error("error response issued tokens")
			}
			var got api.Error
			if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
				t.Fatalf("тело не в формате Error: %v; body %s", err, rec.Body)
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("тело ответа (-want +got):\n%s", diff)
			}
		})
	}
}

func TestHandlerLoginVerificationFailure(t *testing.T) {
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })

	failure := errors.New("unsupported password hash version")
	uc := NewMockUsecase(gomock.NewController(t))
	uc.EXPECT().Login(gomock.Any(), "alice", "password123").Return(domain.User{}, usecase.Session{}, observability.WithCode(fmt.Errorf("compare password: %w", failure), "password_compare_failed"))
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(`{"login":"alice","password":"password123"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	newRouter(t, uc).ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500; body %s", rec.Code, rec.Body)
	}
	var got api.Error
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	want := api.Error{Code: api.Internal, Message: "internal server error"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("error response (-want +got):\n%s", diff)
	}
	if strings.Contains(rec.Body.String(), failure.Error()) {
		t.Error("response exposed verification failure")
	}
	if !strings.Contains(logs.String(), "password_compare_failed") || strings.Contains(logs.String(), failure.Error()) {
		t.Error("log must contain only a safe verification failure code")
	}
	for _, cookie := range rec.Result().Cookies() {
		if cookie.Name == cookies.AccessName || cookie.Name == cookies.RefreshName {
			t.Errorf("verification failure issued %s", cookie.Name)
		}
	}
}

func TestHandlerRegister(t *testing.T) {
	u := domain.User{ID: 1, Login: "bob"}
	uc := NewMockUsecase(gomock.NewController(t))
	uc.EXPECT().Register(gomock.Any(), "bob", "password123").Return(u, newSession(u.ID), nil)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register",
		strings.NewReader(`{"login":"bob","password":"password123"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	newRouter(t, uc).ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body %s", rec.Code, rec.Body)
	}
	var got api.User
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff(api.User{Id: u.ID, Login: "bob"}, got); diff != "" {
		t.Errorf("тело ответа (-want +got):\n%s", diff)
	}
	assertTokenCookies(t, rec, false, false)
}

func TestHandlerRefreshAndLogout(t *testing.T) {
	uc := NewMockUsecase(gomock.NewController(t))
	uc.EXPECT().RefreshUserID(gomock.Any(), "old-refresh").Return(int64(1), nil)
	uc.EXPECT().Refresh(gomock.Any(), "old-refresh").Return(newSession(1), nil)
	uc.EXPECT().RefreshUserID(gomock.Any(), "old-refresh").Return(int64(1), nil)
	uc.EXPECT().Logout(gomock.Any(), "old-refresh").Return(nil)
	post := func(path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/v1"+path, nil)
		req.AddCookie(&http.Cookie{Name: "refresh_token", Value: "old-refresh"})
		req.Header.Set(security.CSRFHeaderName, testCSRF().Signed(1))
		rec := httptest.NewRecorder()
		newRouter(t, uc).ServeHTTP(rec, req)
		return rec
	}

	rec := post("/auth/refresh")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("refresh: status = %d, want 204; body %s", rec.Code, rec.Body)
	}
	assertTokenCookies(t, rec, false, false)

	rec = post("/auth/logout")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("logout: status = %d, want 204; body %s", rec.Code, rec.Body)
	}
	assertTokenCookies(t, rec, false, true)
}

func TestHandlerGetCurrentUser(t *testing.T) {
	u := domain.User{ID: 1, Login: "alice"}
	uc := NewMockUsecase(gomock.NewController(t))
	uc.EXPECT().CurrentUser(gomock.Any(), u.ID).Return(u, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/users/me", nil)
	req.AddCookie(&http.Cookie{Name: "access_token", Value: "access-" + strconv.FormatInt(u.ID, 10)})
	rec := httptest.NewRecorder()

	newRouter(t, uc).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body %s", rec.Code, rec.Body)
	}

	var got api.User
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff(api.User{Id: u.ID, Login: "alice"}, got); diff != "" {
		t.Errorf("тело ответа (-want +got):\n%s", diff)
	}
}

func assertTokenCookies(t *testing.T, rec *httptest.ResponseRecorder, secure, deleted bool) {
	t.Helper()
	if rec.Header().Get("Authorization") != "" {
		t.Error("response exposes access token in Authorization")
	}
	cookies := rec.Result().Cookies()
	if len(cookies) != 3 || len(rec.Header().Values("Set-Cookie")) != 3 {
		t.Fatalf("expected three separate cookies, got %v", rec.Header().Values("Set-Cookie"))
	}
	for i, cookie := range cookies {
		if cookie.Name == security.CSRFCookieName {
			if cookie.Path != "/" || !cookie.Secure || cookie.HttpOnly || cookie.Domain != "" || cookie.SameSite != http.SameSiteLaxMode || cookie.MaxAge != 3600 {
				t.Errorf("unexpected CSRF cookie: %+v", cookie)
			}
			if deleted {
				if strings.Contains(cookie.Value, ".") || cookie.Value == "" {
					t.Error("logout did not issue anonymous CSRF")
				}
			} else if !testCSRF().Valid(cookie.Value, 1) {
				t.Error("session CSRF signature is invalid")
			}
			continue
		}

		name, path, value := "access_token", "/api/v1", "access-1"
		if i == 1 {
			name, path, value = "refresh_token", "/api/v1/auth", "new-refresh"
		}
		if deleted {
			value = ""
		}
		if cookie.Name != name || cookie.Path != path || cookie.Value != value || !cookie.HttpOnly || cookie.Secure != secure || cookie.SameSite != http.SameSiteLaxMode || cookie.Domain != "" {
			t.Errorf("unexpected cookie: %+v", cookie)
		}
		switch {
		case deleted:
			if cookie.MaxAge != -1 {
				t.Errorf("cookie not deleted: %+v", cookie)
			}
		case i == 0:
			if cookie.MaxAge != 900 {
				t.Errorf("access MaxAge = %d, want 900", cookie.MaxAge)
			}
		case cookie.MaxAge < 3590 || cookie.MaxAge > 3600:
			t.Errorf("refresh MaxAge = %d", cookie.MaxAge)
		}
	}
}

func TestHandlerSessionCookies(t *testing.T) {
	for _, secure := range []bool{false, true} {
		for _, operation := range []string{"login", "register", "refresh", "logout"} {
			t.Run(operation+"/secure="+strconv.FormatBool(secure), func(t *testing.T) {
				uc := NewMockUsecase(gomock.NewController(t))
				switch operation {
				case "login":
					uc.EXPECT().Login(gomock.Any(), "bob", "password123").Return(domain.User{ID: 1, Login: "bob"}, newSession(1), nil)
				case "register":
					uc.EXPECT().Register(gomock.Any(), "bob", "password123").Return(domain.User{ID: 1, Login: "bob"}, newSession(1), nil)
				case "refresh":
					uc.EXPECT().RefreshUserID(gomock.Any(), "old-refresh").Return(int64(1), nil)
					uc.EXPECT().Refresh(gomock.Any(), "old-refresh").Return(newSession(1), nil)
				case "logout":
					uc.EXPECT().RefreshUserID(gomock.Any(), "old-refresh").Return(int64(1), nil)
					uc.EXPECT().Logout(gomock.Any(), "old-refresh").Return(nil)
				}
				handler := NewHandler(uc, cookies.Config{CSRFTTL: time.Hour, AccessPath: "/api/v1", RefreshPath: "/api/v1/auth", AccessTTL: 15 * time.Minute, Secure: secure}, testCSRF())
				strict := api.NewStrictHandlerWithOptions(testServer{handler, notebookdelivery.NewHandler(nil)}, nil, api.StrictHTTPServerOptions{})
				router := api.HandlerWithOptions(strict, api.GorillaServerOptions{BaseURL: "/api/v1"})
				req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/"+operation, strings.NewReader(`{"login":"bob","password":"password123"}`))
				req.Header.Set("Content-Type", "application/json")
				req.AddCookie(&http.Cookie{Name: "refresh_token", Value: "old-refresh"})
				req.Header.Set(security.CSRFHeaderName, testCSRF().Signed(1))
				rec := httptest.NewRecorder()
				router.ServeHTTP(rec, req)
				want := http.StatusNoContent
				if operation == "login" {
					want = http.StatusOK
				}
				if operation == "register" {
					want = http.StatusCreated
				}
				if rec.Code != want {
					t.Fatalf("status = %d, want %d: %s", rec.Code, want, rec.Body)
				}
				assertTokenCookies(t, rec, secure, operation == "logout")
			})
		}
	}
}

func TestHandlerCookieSessionLifecycle(t *testing.T) {
	uc := NewMockUsecase(gomock.NewController(t))
	gomock.InOrder(uc.EXPECT().Login(gomock.Any(), "bob", "password123").Return(domain.User{ID: 1, Login: "bob"}, newSession(1), nil), uc.EXPECT().CurrentUser(gomock.Any(), int64(1)).Return(domain.User{ID: 1, Login: "bob"}, nil), uc.EXPECT().RefreshUserID(gomock.Any(), "new-refresh").Return(int64(1), nil), uc.EXPECT().Logout(gomock.Any(), "new-refresh").Return(nil))
	spec, err := api.GetSpec()
	if err != nil {
		t.Fatal(err)
	}
	router := security.Authenticate(testTokens(t))(middleware.Validator(spec, "/api/v1")(newRouter(t, uc)))
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, step := range []struct {
		method, path string
		status       int
	}{
		{http.MethodPost, "/auth/login", http.StatusOK},
		{http.MethodGet, "/users/me", http.StatusOK},
		{http.MethodPost, "/auth/logout", http.StatusNoContent},
		{http.MethodGet, "/users/me", http.StatusUnauthorized},
	} {
		req, err := http.NewRequest(step.method, "https://localhost/api/v1"+step.path, strings.NewReader(`{"login":"bob","password":"password123"}`))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		for _, cookie := range jar.Cookies(req.URL) {
			req.AddCookie(cookie)
			if cookie.Name == security.CSRFCookieName {
				req.Header.Set(security.CSRFHeaderName, cookie.Value)
			}
		}
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		response := rec.Result()
		jar.SetCookies(req.URL, response.Cookies())
		if err := response.Body.Close(); err != nil {
			t.Fatal(err)
		}
		if response.StatusCode != step.status {
			t.Fatalf("%s: status = %d, want %d", step.path, response.StatusCode, step.status)
		}
		if step.path == "/auth/logout" && len(jar.Cookies(req.URL)) != 1 {
			t.Error("logout must leave only anonymous CSRF cookie")
		}
	}
}

func testCSRF() *security.CSRFSigner { return security.NewCSRFSigner([]byte("test-csrf-secret")) }

func testTokens(t *testing.T) *MockTokenParser {
	t.Helper()
	tokens := NewMockTokenParser(gomock.NewController(t))
	// Routing helpers also serve anonymous requests; token parsing is optional here.
	tokens.EXPECT().Parse("access-1").Return(int64(1), nil).AnyTimes()
	return tokens
}
