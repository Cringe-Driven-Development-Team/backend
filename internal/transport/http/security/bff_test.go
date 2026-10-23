package security

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/transport/http/principal"
)

type bffParser struct{ calls int }

func (p *bffParser) Parse(string) (int64, error) { p.calls++; return 42, nil }

func TestS2SBeforeBearerAndRotation(t *testing.T) {
	for _, tc := range []struct {
		name     string
		keys     []string
		headers  []string
		path     string
		required bool
		want     int
	}{
		{"missing", []string{"current", "previous"}, nil, "/api/v1/auth/login", true, 403},
		{"wrong", []string{"current", "previous"}, []string{"wrong"}, "/api/v1/users/me", true, 403},
		{"current", []string{"current", "previous"}, []string{"current"}, "/api/v1/users/me", true, 200},
		{"previous", []string{"current", "previous"}, []string{"previous"}, "/api/v1/users/me", true, 200},
		{"retired", []string{"current"}, []string{"previous"}, "/api/v1/users/me", true, 403},
		{"duplicate", []string{"current"}, []string{"current", "current"}, "/api/v1/users/me", true, 403},
		{"health", []string{"current"}, nil, "/health", true, 200},
		{"disabled", nil, nil, "/api/v1/users/me", false, 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := &bffParser{}
			h := S2S(tc.required, tc.keys, "/api/v1")(AuthenticateBearer(p)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })))
			req := httptest.NewRequest("GET", tc.path, nil)
			req.Header.Set("Authorization", "Bearer access")
			for _, v := range tc.headers {
				req.Header.Add("X-BFF-Key", v)
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != tc.want {
				t.Fatalf("status=%d body=%s", rec.Code, rec.Body)
			}
			if tc.want == 403 && (p.calls != 0 || !strings.Contains(rec.Body.String(), `"code":"s2s_forbidden"`)) {
				t.Fatal("S2S must reject before parsing Bearer")
			}
		})
	}
}

func TestBearerNeverFallsBackToCookie(t *testing.T) {
	for _, headers := range [][]string{nil, {"Basic access"}, {"Bearer"}, {"Bearer a b"}, {"Bearer access", "Bearer access"}, {"Bearer access"}, {"bearer access"}} {
		p := &bffParser{}
		req := httptest.NewRequest("GET", "/users/me", nil)
		req.AddCookie(&http.Cookie{Name: "access_token", Value: "cookie-access"})
		for _, v := range headers {
			req.Header.Add("Authorization", v)
		}
		got := false
		AuthenticateBearer(p)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { _, got = principal.UserIDFromContext(r.Context()) })).ServeHTTP(httptest.NewRecorder(), req)
		want := len(headers) == 1 && (headers[0] == "Bearer access" || headers[0] == "bearer access")
		if got != want || (p.calls == 1) != want {
			t.Fatalf("headers=%v authenticated=%v calls=%d", headers, got, p.calls)
		}
	}
}
