package onlineconf

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	identityv1 "github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/gen/cellestial/identity/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// TestAdminAuthorization проверяет общий барьер для чтения и записи.
func TestAdminAuthorization(t *testing.T) {
	for _, tc := range []struct {
		name, token string
		code        codes.Code
		want        int
	}{
		{"bff key only", "", codes.OK, 401},
		{"invalid session", "Bearer revoked", codes.Unauthenticated, 401},
		{"missing role", "Bearer regular", codes.PermissionDenied, 403},
		{"administrator", "Bearer admin", codes.OK, 204},
		{"identity unavailable", "Bearer admin", codes.Unavailable, 503},
	} {
		t.Run(tc.name, func(t *testing.T) {
			admin := http.NewServeMux()
			for _, method := range []string{"GET", "POST"} {
				admin.HandleFunc(method+" /admin/access", func(w http.ResponseWriter, _ *http.Request) {
					w.WriteHeader(http.StatusNoContent)
				})
			}
			client := accessClientStub{err: status.Error(tc.code, "test")}
			handler := newRouter(client, admin)
			for _, method := range []string{"GET", "POST"} {
				req := httptest.NewRequest(method, "/admin/access", nil)
				req.Header.Set("X-BFF-API-Key", "test")
				req.Header.Set("Authorization", tc.token)
				w := httptest.NewRecorder()
				handler.ServeHTTP(w, req)
				if w.Code != tc.want || w.Header().Get("X-Request-ID") == "" {
					t.Fatalf("%s response = %d, want %d with request ID", method, w.Code, tc.want)
				}
			}
		})
	}
}

// accessClientStub возвращает заданный результат проверки роли.
type accessClientStub struct{ err error }

// Authorize имитирует ответ Identity для текущего сценария.
func (s accessClientStub) Authorize(_ context.Context, _ *identityv1.ValidateSessionRequest, _ ...grpc.CallOption) (*identityv1.OnlineConfAuthorization, error) {
	return &identityv1.OnlineConfAuthorization{}, s.err
}
