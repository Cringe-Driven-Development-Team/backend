package httptransport

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	notebookdelivery "github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/notebook/delivery/http"
	notebookmodels "github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/notebook/domain"
	notebookusecase "github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/notebook/usecase"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/transport/http/api"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/transport/http/cookies"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/transport/http/middleware"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/transport/http/security"
	usersecurity "github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/user/adapters/security"
	userdelivery "github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/user/delivery/http"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/user/domain"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/user/usecase"
	"go.uber.org/mock/gomock"
)

func csrfRouter(t *testing.T, notebooks ...notebookusecase.Usecase) (http.Handler, *security.CSRFSigner, *csrfUsers, string, string) {
	t.Helper()
	return csrfRouterOrigins(t, []string{"https://cellestial.ru"}, middleware.DefaultAppOrigin, notebooks...)
}

func csrfRouterOrigins(t *testing.T, corsOrigins []string, appOrigin string, notebooks ...notebookusecase.Usecase) (http.Handler, *security.CSRFSigner, *csrfUsers, string, string) {
	t.Helper()
	tokens := usersecurity.NewAccessToken([]byte("jwt-test-secret"), time.Hour)
	access, err := tokens.Issue(1)
	if err != nil {
		t.Fatal(err)
	}
	expired, err := usersecurity.NewAccessToken([]byte("jwt-test-secret"), -time.Hour).Issue(1)
	if err != nil {
		t.Fatal(err)
	}
	csrf := security.NewCSRFSigner([]byte("csrf-test-secret"))
	users := &csrfUsers{session: usecase.Session{UserID: 1, AccessToken: access, RefreshToken: "refresh", ExpiresAt: time.Now().Add(30 * 24 * time.Hour)}}
	var notebook notebookusecase.Usecase
	if len(notebooks) > 0 {
		notebook = notebooks[0]
	} else {
		notebook = NewMockNotebookUsecase(gomock.NewController(t))
	}
	handler, err := NewRouter(Server{
		UserHandler:     userdelivery.NewHandler(users, cookies.Config{AccessPath: BaseURL, RefreshPath: BaseURL + "/auth", AccessTTL: time.Hour, CSRFTTL: 30 * 24 * time.Hour, Secure: true}, csrf),
		NotebookHandler: notebookdelivery.NewHandler(notebook),
	}, tokens, corsOrigins, csrf, 30*24*time.Hour, userdelivery.NewRefreshResolver(users), appOrigin)
	if err != nil {
		t.Fatal(err)
	}
	return handler, csrf, users, access, expired
}

func csrfRequest(method, path, token, access, refresh string) *http.Request {
	body := `{"login":"bob","password":"password123"}`
	if path == BaseURL+"/notebooks" {
		body = `{"name":"Notebook"}`
	}
	if strings.HasSuffix(path, "/cells") {
		body = `{"kind":"code"}`
	}
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.AddCookie(&http.Cookie{Name: security.CSRFCookieName, Value: token})
		req.Header.Set(security.CSRFHeaderName, token)
	}
	if access != "" {
		req.AddCookie(&http.Cookie{Name: "access_token", Value: access})
	}
	if refresh != "" {
		req.AddCookie(&http.Cookie{Name: "refresh_token", Value: refresh})
	}
	return req
}
func serveCSRF(handler http.Handler, req *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}
func responseCSRF(t *testing.T, rec *httptest.ResponseRecorder) *http.Cookie {
	t.Helper()
	var result *http.Cookie
	for _, cookie := range rec.Result().Cookies() {
		if cookie.Name == security.CSRFCookieName {
			if result != nil {
				t.Fatal("multiple CSRF Set-Cookie headers")
			}
			result = cookie
		}
	}
	return result
}
func assertCSRFError(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	var result api.Error
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusForbidden || result.Code != api.CsrfInvalid {
		t.Fatalf("expected csrf_invalid 403, got %d %s", rec.Code, rec.Body)
	}
}

func TestRouterCSRFBootstrap(t *testing.T) {
	f := newRouterFixture(t)
	handler, csrf := f.handler, f.csrf
	for _, tt := range []struct {
		method, path string
		status       int
		issue        bool
	}{
		{http.MethodGet, BaseURL + "/users/me", 401, true},
		{http.MethodPost, BaseURL + "/auth/login", 403, true},
		{http.MethodGet, BaseURL + "/missing", 404, true},
		{http.MethodGet, BaseURL + "/notebooks/invalid", 400, true},
		{http.MethodHead, BaseURL + "/users/me", 405, true},
		{http.MethodGet, "/api/v10/users/me", 404, false},
		{http.MethodGet, "/health", 200, false},
		{http.MethodOptions, BaseURL + "/users/me", 204, false},
	} {
		t.Run(tt.method+tt.path, func(t *testing.T) {
			req := csrfRequest(tt.method, tt.path, "", "", "")
			if tt.method == http.MethodOptions {
				req.Header.Set("Origin", "https://cellestial.ru")
				req.Header.Set("Access-Control-Request-Method", "GET")
			}
			rec := serveCSRF(handler, req)
			if rec.Code != tt.status {
				t.Fatalf("status %d: %s", rec.Code, rec.Body)
			}
			cookie := responseCSRF(t, rec)
			if (cookie != nil) != tt.issue {
				t.Fatalf("unexpected bootstrap cookie: %+v", cookie)
			}
			if cookie != nil && (strings.Contains(cookie.Value, ".") || !cookie.Secure || cookie.HttpOnly || cookie.Path != "/" || cookie.Domain != "" || cookie.SameSite != http.SameSiteLaxMode || cookie.MaxAge != 30*24*3600) {
				t.Fatalf("invalid bootstrap cookie: %+v", cookie)
			}
		})
	}
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		req := csrfRequest(method, BaseURL+"/users/me", csrf.Anonymous(), "", "")
		req.Header.Del(security.CSRFHeaderName)
		if responseCSRF(t, serveCSRF(handler, req)) != nil {
			t.Fatal("existing cookie was reissued")
		}
	}
}

func TestRouterCSRFMutations(t *testing.T) {
	f := newRouterFixture(t)
	handler, csrf, access := f.handler, f.csrf, f.access
	valid := csrf.Signed(1)
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
		for _, invalid := range []string{"missing", "empty", "missing cookie", "empty cookie", "mismatch", "anonymous", "other user", "bad signature", "duplicate header", "duplicate cookie"} {
			t.Run(method+"/"+invalid, func(t *testing.T) {
				token := valid
				switch invalid {
				case "anonymous":
					token = csrf.Anonymous()
				case "other user":
					token = csrf.Signed(2)
				case "bad signature":
					token = strings.Split(valid, ".")[0] + ".broken"
				}
				req := csrfRequest(method, BaseURL+"/notebooks", token, access, "")
				switch invalid {
				case "missing":
					req.Header.Del(security.CSRFHeaderName)
				case "empty":
					req.Header.Set(security.CSRFHeaderName, "")
				case "missing cookie":
					req.Header.Del("Cookie")
					req.AddCookie(&http.Cookie{Name: "access_token", Value: access})
				case "empty cookie":
					req.Header.Del("Cookie")
					req.AddCookie(&http.Cookie{Name: "access_token", Value: access})
					req.AddCookie(&http.Cookie{Name: security.CSRFCookieName, Value: ""})
				case "mismatch":
					req.Header.Set(security.CSRFHeaderName, csrf.Signed(1))
				case "duplicate header":
					req.Header.Add(security.CSRFHeaderName, token)
				case "duplicate cookie":
					req.AddCookie(&http.Cookie{Name: security.CSRFCookieName, Value: token})
				}
				assertCSRFError(t, serveCSRF(handler, req))
			})
		}
	}
	notebooks := f.notebooks
	notebooks.EXPECT().Create(gomock.Any(), int64(1), "Notebook").Return(notebookmodels.Notebook{ID: 1, Name: "Notebook"}, nil)
	notebooks.EXPECT().CreateCell(gomock.Any(), int64(1), int64(1), notebookmodels.CellKindCode, (*int)(nil)).Return(notebookmodels.Cell{ID: "cell", Kind: notebookmodels.CellKindCode}, nil)
	notebooks.EXPECT().DeleteCell(gomock.Any(), int64(1), int64(1), 0).Return(nil)
	for _, tt := range []struct {
		method, path string
		status       int
	}{
		{"POST", BaseURL + "/notebooks", 201}, {"POST", BaseURL + "/notebooks/1/cells", 201}, {"DELETE", BaseURL + "/notebooks/1/cells/0", 204},
	} {
		rec := serveCSRF(handler, csrfRequest(tt.method, tt.path, valid, access, ""))
		if rec.Code != tt.status {
			t.Fatalf("valid mutation %s: %d %s", tt.path, rec.Code, rec.Body)
		}
	}
	rec := serveCSRF(handler, csrfRequest("POST", BaseURL+"/notebooks", csrf.Anonymous(), "", ""))
	if rec.Code != 401 {
		t.Fatalf("missing access should remain 401: %d %s", rec.Code, rec.Body)
	}
	// User lookup is checked by the dedicated strict router fixture.
	f.users.EXPECT().CurrentUser(gomock.Any(), int64(1)).Return(domain.User{ID: 1, Login: "bob"}, nil)
	rec = serveCSRF(f.handler, csrfRequest("GET", BaseURL+"/users/me", "", access, ""))
	if rec.Code != 200 {
		t.Fatalf("GET needs no CSRF: %d", rec.Code)
	}
}

func TestRouterCSRFSessions(t *testing.T) {
	for _, operation := range []string{"login", "register", "refresh", "logout"} {
		t.Run(operation, func(t *testing.T) {
			handler, csrf, users, _, expired := csrfRouter(t)
			token := csrf.Anonymous()
			if operation == "refresh" || operation == "logout" {
				token = csrf.Signed(1)
			}
			path := BaseURL + "/auth/" + operation
			missing := csrfRequest("POST", path, token, expired, "refresh")
			missing.Header.Del(security.CSRFHeaderName)
			assertCSRFError(t, serveCSRF(handler, missing))
			rec := serveCSRF(handler, csrfRequest("POST", path, token, expired, "refresh"))
			want := 204
			if operation == "login" {
				want = 200
			}
			if operation == "register" {
				want = 201
			}
			if rec.Code != want {
				t.Fatalf("session change failed: %d %s", rec.Code, rec.Body)
			}
			cookie := responseCSRF(t, rec)
			if cookie == nil || cookie.Value == token {
				t.Fatal("CSRF not rotated")
			}
			if operation == "logout" {
				if strings.Contains(cookie.Value, ".") {
					t.Fatal("logout token is signed")
				}
				rec = serveCSRF(handler, csrfRequest("POST", BaseURL+"/auth/login", cookie.Value, "", ""))
				if rec.Code != 200 {
					t.Fatalf("login after logout failed: %d %s", rec.Code, rec.Body)
				}
			} else if !csrf.Valid(cookie.Value, 1) {
				t.Fatal("new token not bound to user")
			}
			if operation == "refresh" {
				rec = serveCSRF(handler, csrfRequest("POST", path, cookie.Value, expired, "refresh"))
				if rec.Code != 401 || users.mutations != 1 {
					t.Fatal("old refresh accepted twice")
				}
			}
		})
	}
}

func TestRouterCSRFRefreshRejectionPreservesSession(t *testing.T) {
	for _, operation := range []string{"refresh", "logout"} {
		handler, csrf, users, _, expired := csrfRouter(t)
		for _, token := range []string{csrf.Anonymous(), csrf.Signed(2), "broken.signature"} {
			assertCSRFError(t, serveCSRF(handler, csrfRequest("POST", BaseURL+"/auth/"+operation, token, expired, "refresh")))
			if users.mutations != 0 || users.session.RefreshToken != "refresh" {
				t.Fatal("CSRF rejection changed session")
			}
		}
		rec := serveCSRF(handler, csrfRequest("POST", BaseURL+"/auth/"+operation, csrf.Signed(1), expired, "refresh"))
		if rec.Code != 204 {
			t.Fatalf("session unavailable after rejection: %d %s", rec.Code, rec.Body)
		}
	}
}

func TestRouterCSRFOrigin(t *testing.T) {
	for _, operation := range []string{"login", "register", "refresh"} {
		for _, origin := range []string{"https://evil.example", "https://avatars.cellestial.ru", "null", "https://cellestial.ru", ""} {
			t.Run(operation+"/"+origin, func(t *testing.T) {
				f := newRouterFixture(t)
				handler, csrf := f.handler, f.csrf
				if origin == "" || origin == "https://cellestial.ru" {
					expectSessionRequest(f, operation, true)
				}
				token := csrf.Anonymous()
				if operation == "refresh" {
					token = csrf.Signed(1)
				}
				req := csrfRequest("POST", BaseURL+"/auth/"+operation, token, "", "refresh")
				if origin != "" {
					req.Header.Set("Origin", origin)
				}
				rec := serveCSRF(handler, req)
				if origin != "" && origin != "https://cellestial.ru" {
					assertCSRFError(t, rec)
				} else if rec.Code >= 400 {
					t.Fatalf("allowed origin rejected: %d %s", rec.Code, rec.Body)
				}
			})
		}
	}
	// Origin проверяется до разбора некорректного JSON.
	handler, _, _, _, _ := csrfRouter(t)
	req := httptest.NewRequest("POST", BaseURL+"/auth/login", strings.NewReader("broken"))
	req.Header.Set("Origin", "https://evil.example")
	assertCSRFError(t, serveCSRF(handler, req))
}

func TestRouterCSRFBeforeGeneratedParameters(t *testing.T) {
	f := newRouterFixture(t)
	handler, csrf := f.handler, f.csrf
	// Отсутствие обязательной refresh-cookie не должно скрыть отказ CSRF за 401.
	for _, operation := range []string{"refresh", "logout"} {
		rec := serveCSRF(handler, csrfRequest("POST", BaseURL+"/auth/"+operation, "", "", ""))
		assertCSRFError(t, rec)
		if responseCSRF(t, rec) == nil {
			t.Fatal("early error did not bootstrap cookie")
		}
	}
	req := csrfRequest("POST", BaseURL+"/auth/login", csrf.Anonymous(), "", "")
	req.Body = http.NoBody
	req.ContentLength = 0
	rec := serveCSRF(handler, req)
	if rec.Code != 400 {
		t.Fatalf("valid CSRF should allow body validation: %d %s", rec.Code, rec.Body)
	}
	if responseCSRF(t, rec) != nil {
		t.Fatal("body error reissued existing CSRF cookie")
	}
}
