package onlineconf

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	identityv1 "github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/gen/cellestial/identity/v1"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/transport/http/principal"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// TestAdminAuthorization проверяет общий барьер для чтения и записи.
func TestAdminAuthorization(t *testing.T) {
	for _, tc := range []struct {
		name, key, token string
		rpcCode          codes.Code
		userID           int64
		want             int
		errorCode        string
	}{
		{"missing key and token", "", "", codes.OK, 0, 403, "s2s_forbidden"},
		{"admin token only", "", "Bearer admin", codes.OK, 42, 403, "s2s_forbidden"},
		{"wrong key", "wrong", "Bearer admin", codes.OK, 42, 403, "s2s_forbidden"},
		{"bff key only", "test", "", codes.OK, 0, 401, "unauthorized"},
		{"invalid session", "test", "Bearer revoked", codes.Unauthenticated, 0, 401, "unauthorized"},
		{"missing role", "test", "Bearer regular", codes.PermissionDenied, 0, 403, "forbidden"},
		{"administrator", "test", "Bearer admin", codes.OK, 42, 204, ""},
		{"identity unavailable", "test", "Bearer admin", codes.Unavailable, 0, 503, "dependency_unavailable"},
		{"missing identity", "test", "Bearer admin", codes.OK, 0, 503, "dependency_unavailable"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			admin := http.NewServeMux()
			for _, method := range []string{"GET", "POST"} {
				admin.HandleFunc(method+" /admin/access", func(w http.ResponseWriter, r *http.Request) {
					id, ok := principal.UserIDFromContext(r.Context())
					if !ok || id != 42 {
						t.Fatalf("verified actor = %d, present = %v", id, ok)
					}
					w.WriteHeader(http.StatusNoContent)
				})
			}
			client := accessClientStub{err: status.Error(tc.rpcCode, "test"), userID: tc.userID}
			handler := newRouter(&client, admin, "test")
			for _, method := range []string{"GET", "POST"} {
				req := httptest.NewRequest(method, "/admin/access", strings.NewReader(`{"created_by":7,"updated_by":7}`))
				if tc.key != "" {
					req.Header.Set("X-BFF-Key", tc.key)
				}
				req.Header.Set("Authorization", tc.token)
				w := httptest.NewRecorder()
				handler.ServeHTTP(w, req)
				if w.Code != tc.want || w.Header().Get("X-Request-ID") == "" {
					t.Fatalf("%s response = %d, want %d with request ID", method, w.Code, tc.want)
				}
				if tc.errorCode != "" {
					var body struct {
						Code    string `json:"code"`
						Message string `json:"message"`
					}
					if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || body.Code != tc.errorCode || body.Message == "" {
						t.Fatalf("expected JSON error %s, got %s", tc.errorCode, w.Body.String())
					}
				}
			}
			if (tc.key != "test" || tc.token == "") && client.called {
				t.Fatal("Identity called before service key and Bearer checks")
			}
		})
	}
}

// accessClientStub возвращает заданный результат проверки роли.
type accessClientStub struct {
	err    error
	userID int64
	called bool
}

// Authorize имитирует ответ Identity для текущего сценария.
func (s *accessClientStub) Authorize(_ context.Context, _ *identityv1.ValidateSessionRequest, _ ...grpc.CallOption) (*identityv1.OnlineConfAuthorization, error) {
	s.called = true
	return &identityv1.OnlineConfAuthorization{UserId: s.userID}, s.err
}
