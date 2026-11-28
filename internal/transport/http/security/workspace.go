package security

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/authn"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/transport/http/api"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/transport/http/httperr"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/transport/http/principal"
)

// AuthenticateWorkspace проверяет Bearer в Identity после S2S.
func AuthenticateWorkspace(verifier authn.SessionVerifier) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			values := r.Header.Values("Authorization")
			token := workspaceBearer(values)
			if token == "" {
				httperr.Write(w, http.StatusUnauthorized, api.ErrorCodeUnauthorized, "authentication required")
				return
			}
			p, err := verifier.Verify(r.Context(), token)
			switch {
			case httperr.WriteRPC(w, err):
				return
			case errors.Is(err, authn.ErrInvalidSession):
				httperr.Write(w, http.StatusUnauthorized, api.ErrorCodeUnauthorized, "authentication required")
			case errors.Is(err, context.DeadlineExceeded):
				httperr.Write(w, http.StatusGatewayTimeout, "deadline_exceeded", "operation deadline exceeded")
			case err != nil || p.Anonymous || p.UserID <= 0:
				httperr.Write(w, http.StatusServiceUnavailable, "dependency_unavailable", "session dependency unavailable")
			default:
				ctx := authn.WithPrincipal(r.Context(), p)
				next.ServeHTTP(w, r.WithContext(principal.WithUserID(ctx, p.UserID)))
			}
		})
	}
}

// workspaceBearer принимает ровно один Bearer без встроенных пробельных символов.
func workspaceBearer(values []string) string {
	if len(values) != 1 {
		return ""
	}
	scheme, token, ok := strings.Cut(values[0], " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") || strings.ContainsAny(token, " \t\r\n,") || len(token) > 8192 {
		return ""
	}
	return token
}
