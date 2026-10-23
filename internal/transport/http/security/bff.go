package security

import (
	"crypto/sha256"
	"crypto/subtle"
	"net/http"
	"strings"

	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/transport/http/api"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/transport/http/httperr"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/transport/http/principal"
)

// S2S verifies the calling service before any user authentication or body parsing.
// Only API routes are gated; the internal GET /health remains available.
func S2S(required bool, keys []string, prefix string) func(http.Handler) http.Handler {
	hashes := make([][sha256.Size]byte, 0, len(keys))
	for _, key := range keys {
		if key != "" {
			hashes = append(hashes, sha256.Sum256([]byte(key)))
		}
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !required || (r.URL.Path != prefix && !strings.HasPrefix(r.URL.Path, prefix+"/")) {
				next.ServeHTTP(w, r)
				return
			}
			values := r.Header.Values("X-BFF-Key")
			matched := 0
			if len(values) == 1 && values[0] != "" {
				hash := sha256.Sum256([]byte(values[0]))
				for _, allowed := range hashes {
					matched |= subtle.ConstantTimeCompare(hash[:], allowed[:])
				}
			}
			if matched != 1 {
				httperr.Write(w, http.StatusForbidden, api.S2sForbidden, "service access forbidden")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// AuthenticateBearer never consults browser cookies. Validator enforces which
// operations require an authenticated principal.
func AuthenticateBearer(tokens TokenParser) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			values := r.Header.Values("Authorization")
			if len(values) == 1 {
				scheme, token, ok := strings.Cut(values[0], " ")
				if ok && strings.EqualFold(scheme, "Bearer") && token != "" && !strings.ContainsAny(token, " \t\r\n") {
					if id, err := tokens.Parse(token); err == nil {
						r = r.WithContext(principal.WithUserID(r.Context(), id))
					}
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}
