// Package onlineconf собирает отдельный HTTP-сервис OnlineConf.
package onlineconf

import (
	"net/http"
	"strings"

	identityv1 "github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/gen/cellestial/identity/v1"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/transport/http/httperr"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/transport/http/middleware"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/transport/http/principal"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/transport/http/security"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// newRouter закрывает всю административную ветку проверкой сессии и роли в Identity.
func newRouter(identity identityv1.OnlineConfAccessClient, admin http.Handler, bffKey string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	protected := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		values := r.Header.Values("Authorization")
		if len(values) != 1 {
			unauthenticated(w)
			return
		}
		scheme, token, found := strings.Cut(values[0], " ")
		if !found || !strings.EqualFold(scheme, "Bearer") || token == "" || len(token) > 8192 || strings.ContainsAny(token, " \t,") {
			unauthenticated(w)
			return
		}

		authorization, err := identity.Authorize(r.Context(), &identityv1.ValidateSessionRequest{AccessToken: token})
		switch status.Code(err) {
		case codes.OK:
			userID := authorization.GetUserId()
			if userID <= 0 {
				httperr.Write(w, http.StatusServiceUnavailable, "dependency_unavailable", "authorization unavailable")
				return
			}
			admin.ServeHTTP(w, r.WithContext(principal.WithUserID(r.Context(), userID)))
		case codes.Unauthenticated, codes.InvalidArgument:
			unauthenticated(w)
		case codes.PermissionDenied:
			httperr.Write(w, http.StatusForbidden, "forbidden", "onlineconf_admin role required")
		default:
			httperr.Write(w, http.StatusServiceUnavailable, "dependency_unavailable", "authorization unavailable")
		}
	})
	requireAccess := security.S2S(bffKey, "/admin")(protected)
	mux.Handle("/admin", requireAccess)
	mux.Handle("/admin/", requireAccess)
	return middleware.RequestID(middleware.Logging(middleware.Recover(mux)))
}

// unauthenticated сообщает клиенту требуемую схему аутентификации.
func unauthenticated(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", "Bearer")
	httperr.Write(w, http.StatusUnauthorized, "unauthorized", "authentication required")
}
