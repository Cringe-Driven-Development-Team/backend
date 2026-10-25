package httptransport

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	notebookdomain "github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/notebook/domain"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/observability"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/user/domain"
	"github.com/google/uuid"
	"go.uber.org/mock/gomock"
)

func TestRouterRequestIDOnEveryResponse(t *testing.T) {
	base := newRouterFixture(t)
	csrf, access := base.csrf, base.access
	for _, tt := range []struct {
		name, method, path, token, access, origin string
		status                                    int
	}{
		{name: "health", method: "GET", path: "/health", status: 200},
		{name: "success", method: "GET", path: BaseURL + "/users/me", access: access, status: 200},
		{name: "unauthorized", method: "GET", path: BaseURL + "/users/me", status: 401},
		{name: "csrf copies", method: "POST", path: BaseURL + "/auth/login", status: 403},
		{name: "csrf signature", method: "POST", path: BaseURL + "/notebooks", token: csrf.Signed(2), access: access, status: 403},
		{name: "csrf origin", method: "POST", path: BaseURL + "/auth/login", token: csrf.Anonymous(), origin: "https://evil.example", status: 403},
		{name: "preflight", method: "OPTIONS", path: BaseURL + "/users/me", origin: "https://cellestial.ru", status: 204},
		{name: "not found", method: "GET", path: "/missing", status: 404},
		{name: "method not allowed", method: "POST", path: "/health", status: 405},
		{name: "invalid parameter", method: "GET", path: BaseURL + "/notebooks/invalid", status: 400},
		// Явная паника зависимости должна превратиться в 500 с request ID.
		{name: "recovered panic", method: "GET", path: BaseURL + "/notebooks", access: access, status: 500},
	} {
		for _, requestID := range []string{"", "client-request-id"} {
			t.Run(tt.name+"/"+requestID, func(t *testing.T) {
				f := newRouterFixture(t)
				if tt.name == "success" {
					f.users.EXPECT().CurrentUser(gomock.Any(), int64(1)).Return(domain.User{ID: 1, Login: "bob"}, nil)
				}
				if tt.name == "recovered panic" {
					f.notebooks.EXPECT().List(gomock.Any(), int64(1)).DoAndReturn(func(_ context.Context, _ int64) ([]notebookdomain.NotebookSummary, error) { panic("SECRET-panic") })
				}
				req := csrfRequest(tt.method, tt.path, tt.token, tt.access, "")
				if requestID != "" {
					req.Header.Set("X-Request-ID", requestID)
				}
				if tt.origin != "" {
					req.Header.Set("Origin", tt.origin)
				}
				if tt.method == http.MethodOptions {
					req.Header.Set("Access-Control-Request-Method", "POST")
				}
				rec := serveCSRF(f.handler, req)
				if rec.Code != tt.status {
					t.Fatalf("status = %d, want %d: %s", rec.Code, tt.status, rec.Body)
				}
				got := rec.Header().Get("X-Request-ID")
				if requestID != "" {
					if got != requestID {
						t.Fatalf("request ID = %q, want %q", got, requestID)
					}
				} else if _, err := uuid.Parse(got); err != nil {
					t.Fatalf("missing or invalid generated request ID: %q", got)
				}
				if tt.name == "preflight" && rec.Body.Len() != 0 {
					t.Fatal("preflight must have no body")
				}
			})
		}
	}
}

func TestRouterLogsTemplateAndEarlyFailure(t *testing.T) {
	base := newRouterFixture(t)
	access := base.access
	var out bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(observability.NewHandler(slog.NewJSONHandler(&out, nil))))
	t.Cleanup(func() { slog.SetDefault(old) })
	for _, tc := range []struct {
		name, method, path, access, origin, route string
		status                                    int
	}{
		{"success", http.MethodGet, BaseURL + "/users/me", access, "", BaseURL + "/users/me", 200},
		{"unauthorized", http.MethodGet, BaseURL + "/users/me", "", "", BaseURL + "/users/me", 401},
		{"csrf", http.MethodPost, BaseURL + "/auth/login", "", "", "unmatched", 403},
		{"preflight", http.MethodOptions, BaseURL + "/users/me", "", "https://cellestial.ru", "unmatched", 204},
		{"not_found", http.MethodGet, "/missing/SECRET?token=SECRET", "", "", "unmatched", 404},
		{"method_not_allowed", http.MethodPost, "/health", "", "", "unmatched", 405},
		{"parameterized", http.MethodGet, BaseURL + "/notebooks/invalid", access, "", BaseURL + "/notebooks/{id}", 400},
		{"panic", http.MethodGet, BaseURL + "/notebooks", access, "", BaseURL + "/notebooks", 500},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out.Reset()
			f := newRouterFixture(t)
			if tc.name == "success" {
				f.users.EXPECT().CurrentUser(gomock.Any(), int64(1)).Return(domain.User{ID: 1, Login: "bob"}, nil)
			}
			if tc.name == "panic" {
				f.notebooks.EXPECT().List(gomock.Any(), int64(1)).DoAndReturn(func(_ context.Context, _ int64) ([]notebookdomain.NotebookSummary, error) { panic("SECRET-panic") })
			}
			req := csrfRequest(tc.method, tc.path, "", tc.access, "")
			if tc.origin != "" {
				req.Header.Set("Origin", tc.origin)
				req.Header.Set("Access-Control-Request-Method", http.MethodPost)
			}
			rec := serveCSRF(f.handler, req)
			if rec.Code != tc.status {
				t.Fatalf("status=%d, want %d", rec.Code, tc.status)
			}
			var summary map[string]any
			for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
				var entry map[string]any
				if err := json.Unmarshal([]byte(line), &entry); err != nil {
					t.Fatalf("bad JSON log: %v", err)
				}
				if entry["msg"] == "http.request" {
					if summary != nil {
						t.Fatalf("multiple HTTP summaries: %s", out.String())
					}
					summary = entry
				}
			}
			if summary == nil || summary["status"] != float64(tc.status) || summary["route"] != tc.route || summary["method"] != tc.method || summary["request_id"] != rec.Header().Get("X-Request-ID") || strings.Contains(out.String(), "SECRET") {
				t.Fatalf("wrong HTTP summary: %s", out.String())
			}
		})
	}
}
