package security

import (
	"net/http"
	"strings"
	"time"

	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/transport/http/api"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/transport/http/httperr"
)

// Protect собирает проверку исходного запроса, аутентификацию и восстановление cookie.
func Protect(tokens TokenParser, signer *CSRFSigner, ttl time.Duration, prefix string, allowedOrigins []string, refreshUsers RefreshUserResolver, appOrigin string) func(http.Handler) http.Handler {
	origins := make(map[string]bool, len(allowedOrigins)+1)
	origins[appOrigin] = true
	for _, origin := range allowedOrigins {
		origins[origin] = true
	}
	guard := NewCSRFGuard(signer, refreshUsers)
	guard.prefix, guard.origins = prefix, origins
	bootstrap := &CSRFBootstrap{tokens: tokens, signer: signer, ttl: ttl, origins: origins, refreshUsers: refreshUsers}
	return func(next http.Handler) http.Handler {
		authenticated := Authenticate(tokens)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !guard.checkAuthenticated(r) {
				csrfInvalid(w)
				return
			}
			next.ServeHTTP(w, r)
		}))
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if (r.URL.Path != prefix && !strings.HasPrefix(r.URL.Path, prefix+"/")) || r.Method == http.MethodOptions {
				next.ServeHTTP(w, r)
				return
			}
			original := r.CookiesNamed(CSRFCookieName)
			writer, finish, err := bootstrap.prepare(w, r, original)
			if err != nil {
				httperr.Internal(writer, r, err)
				return
			}
			defer finish()
			if !guard.checkRequest(r, original) {
				csrfInvalid(writer)
				return
			}
			authenticated.ServeHTTP(writer, r)
		})
	}
}
func csrfInvalid(w http.ResponseWriter) {
	httperr.Write(w, http.StatusForbidden, api.CsrfInvalid, "CSRF token is invalid")
}
