package security

import (
	"context"
	"crypto/hmac"
	"errors"
	"net/http"

	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/transport/http/cookies"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/transport/http/principal"
)

const CSRFCookieName = cookies.CSRFName
const CSRFHeaderName = "X-CSRF-Token"

var ErrCSRFInvalid = errors.New("CSRF token is invalid")
var ErrInvalidRefresh = errors.New("refresh token is invalid or expired")

// RefreshIdentity отличает отсутствующую сессию от сбоя хранилища.
type RefreshIdentity struct {
	UserID int64
	Valid  bool
}
type RefreshUserResolver interface {
	ResolveRefresh(context.Context, string) (RefreshIdentity, error)
}
type CSRFGuard struct {
	signer       *CSRFSigner
	refreshUsers RefreshUserResolver
	prefix       string
	origins      map[string]bool
}

func NewCSRFGuard(signer *CSRFSigner, resolver RefreshUserResolver) *CSRFGuard {
	return &CSRFGuard{signer: signer, refreshUsers: resolver}
}

// CheckRefresh проверяет подпись для владельца refresh-сессии до её изменения.
func (g *CSRFGuard) CheckRefresh(ctx context.Context, refresh string, token *string) error {
	identity, err := g.refreshUsers.ResolveRefresh(ctx, refresh)
	if err != nil {
		return err
	}
	if !identity.Valid {
		return ErrInvalidRefresh
	}
	if token == nil || !g.signer.Valid(*token, identity.UserID) {
		return ErrCSRFInvalid
	}
	return nil
}
func (g *CSRFGuard) exempt(path string) bool {
	return path == g.prefix+"/auth/login" || path == g.prefix+"/auth/register" || path == g.prefix+"/auth/refresh"
}
func (g *CSRFGuard) checkRequest(r *http.Request, csrfCookies []*http.Cookie) bool {
	if !changingMethod(r.Method) {
		return true
	}
	if g.exempt(r.URL.Path) {
		if values := r.Header.Values("Origin"); len(values) != 0 && (len(values) != 1 || !g.origins[values[0]]) {
			return false
		}
	}
	values := r.Header.Values(CSRFHeaderName)
	return len(csrfCookies) == 1 && len(values) == 1 && values[0] != "" && csrfCookies[0].Value != "" && hmac.Equal([]byte(values[0]), []byte(csrfCookies[0].Value))
}
func (g *CSRFGuard) checkAuthenticated(r *http.Request) bool {
	userID, ok := principal.UserIDFromContext(r.Context())
	return !changingMethod(r.Method) || !ok || g.exempt(r.URL.Path) || g.signer.Valid(r.Header.Get(CSRFHeaderName), userID)
}
func changingMethod(method string) bool {
	return method == http.MethodPost || method == http.MethodPut || method == http.MethodPatch || method == http.MethodDelete
}
