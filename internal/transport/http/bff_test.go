package httptransport

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	notebookdelivery "github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/notebook/delivery/http"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/observability"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/transport/http/api"
	usersecurity "github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/user/adapters/security"
	userdelivery "github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/user/delivery/http"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/user/usecase"
)

func bffRouter(t *testing.T, key string) (http.Handler, *bffUsers, string) {
	t.Helper()
	issuer := usersecurity.NewAccessToken([]byte("bff-jwt-test-key"), time.Minute)
	access, expires, err := issuer.IssueAccess(testClaims(1))
	if err != nil {
		t.Fatal(err)
	}
	users := &bffUsers{session: usecase.Session{SessionClaims: testClaims(1), AccessToken: access, AccessExpiresAt: expires, RefreshToken: "private-refresh-token", ExpiresAt: time.Now().Add(time.Hour)}}
	router, err := NewRouter(Server{UserHandler: userdelivery.NewHandler(users), NotebookHandler: notebookdelivery.NewHandler(bffNotebooks{})}, issuer, allowSessions{}, key)
	if err != nil {
		t.Fatal(err)
	}
	return router, users, access
}

func TestBFFRouterS2SPriorityAndRequestID(t *testing.T) {
	h, users, access := bffRouter(t, "current-private-key")
	for _, tc := range []struct {
		name, method, path, body, key, bearer string
		want                                  int
	}{
		{"register gated", "POST", "/api/v1/auth/register", `{`, "", "", 403},
		{"login gated", "POST", "/api/v1/auth/login", `{`, "", "", 403},
		{"refresh gated", "POST", "/api/v1/auth/refresh", `{`, "bad-key", "Bearer malformed", 403},
		{"logout gated", "POST", "/api/v1/auth/logout", `{`, "", "", 403},
		{"data gated", "GET", "/api/v1/users/me", "", "bad-key", "Bearer " + access, 403},
		{"valid current", "GET", "/api/v1/users/me", "", "current-private-key", "Bearer " + access, 200},
		{"previous rejected", "GET", "/api/v1/users/me", "", "previous-private-key", "Bearer " + access, 403},
		{"bearer missing", "GET", "/api/v1/users/me", "", "current-private-key", "", 401},
		{"bearer invalid", "GET", "/api/v1/users/me", "", "current-private-key", "Bearer invalid", 401},
		{"malformed json", "POST", "/api/v1/auth/refresh", `{`, "current-private-key", "", 400},
		{"health exempt", "GET", "/health", "", "", "", 200},
		{"unknown", "GET", "/api/v1/missing", "", "current-private-key", "", 404},
		{"wrong method", "POST", "/health", "", "", "", 405},
		{"no cors bypass", "OPTIONS", "/api/v1/users/me", "", "", "", 403},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-BFF-Key", tc.key)
			req.Header.Set("Authorization", tc.bearer)
			req.Header.Set("Origin", "https://foreign.example")
			req.AddCookie(&http.Cookie{Name: "access_token", Value: access})
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != tc.want {
				t.Fatalf("status=%d want=%d body=%s", rec.Code, tc.want, rec.Body)
			}
			if rec.Header().Get("X-Request-ID") == "" {
				t.Fatal("missing request ID")
			}
			if len(rec.Result().Cookies()) != 0 || rec.Header().Get("Access-Control-Allow-Origin") != "" {
				t.Fatal("BFF mode emitted browser policy headers")
			}
			if tc.want == 403 && !strings.Contains(rec.Body.String(), `"s2s_forbidden"`) {
				t.Fatal("wrong infrastructure error")
			}
		})
	}
	if users.mutations != 0 {
		t.Fatal("rejected requests modified session")
	}
}

func TestBFFSessionLifecycleAndLogs(t *testing.T) {
	old := slog.Default()
	var logs bytes.Buffer
	slog.SetDefault(slog.New(observability.NewHandler(slog.NewJSONHandler(&logs, nil))))
	t.Cleanup(func() { slog.SetDefault(old) })
	h, users, access := bffRouter(t, "private-s2s-key")
	refresh := users.session.RefreshToken
	for _, operation := range []string{"login", "refresh", "logout"} {
		body := `{"login":"bob","password":"private-password"}`
		if operation != "login" {
			encoded, _ := json.Marshal(map[string]string{"refresh_token": refresh})
			body = string(encoded)
		}
		req := httptest.NewRequest("POST", "/api/v1/auth/"+operation, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-BFF-Key", "private-s2s-key")
		if operation == "logout" {
			req.Header.Set("Authorization", "Bearer "+access)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if operation == "logout" {
			if rec.Code != 204 || rec.Body.Len() != 0 {
				t.Fatalf("logout: %d %s", rec.Code, rec.Body)
			}
			continue
		}
		if rec.Code != 200 {
			t.Fatalf("%s: %d %s", operation, rec.Code, rec.Body)
		}
		if operation == "refresh" {
			var pair api.TokenPair
			if err := json.Unmarshal(rec.Body.Bytes(), &pair); err != nil {
				t.Fatal(err)
			}
			refresh = pair.RefreshToken
		}
	}
	if users.mutations != 2 || users.session.RefreshToken != "" {
		t.Fatal("refresh/logout did not rotate/revoke session")
	}
	h2, _, _ := bffRouter(t, "private-s2s-key")
	bad := httptest.NewRequest("POST", "/api/v1/auth/refresh", strings.NewReader(`{"refresh_token":"private-refresh-token"}`))
	bad.Header.Set("X-BFF-Key", "wrong-private-key")
	bad.Header.Set("Authorization", "Bearer "+access)
	rec := httptest.NewRecorder()
	h2.ServeHTTP(rec, bad)
	for _, secret := range []string{"private-s2s-key", "wrong-private-key", "private-refresh-token", "private-password", access} {
		if strings.Contains(logs.String(), secret) {
			t.Fatal("secret leaked into logs")
		}
	}
	if !strings.Contains(logs.String(), "http.request") {
		t.Fatal("request logs missing")
	}
}

func TestRouterRejectsEmptyBFFKey(t *testing.T) {
	for _, key := range []string{"", " \t"} {
		if _, err := NewRouter(Server{}, nil, allowSessions{}, key); err == nil {
			t.Fatal("empty key accepted")
		}
	}
}
