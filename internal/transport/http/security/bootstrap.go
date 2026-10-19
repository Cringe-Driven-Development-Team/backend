package security

import (
	"net/http"
	"time"

	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/transport/http/cookies"
)

// CSRFBootstrap восстанавливает cookie, не меняя проверяемый исходный запрос.
type CSRFBootstrap struct {
	tokens       TokenParser
	signer       *CSRFSigner
	ttl          time.Duration
	origins      map[string]bool
	refreshUsers RefreshUserResolver
}

func (b *CSRFBootstrap) prepare(w http.ResponseWriter, r *http.Request, original []*http.Cookie) (http.ResponseWriter, func(), error) {
	if len(original) != 0 && (len(original) != 1 || !IsAnonymousCSRF(original[0].Value)) {
		return w, func() {}, nil
	}
	origin := r.Header.Values("Origin")
	trusted := len(origin) == 0 || (len(origin) == 1 && b.origins[origin[0]])
	var userID int64
	if trusted {
		if access, err := r.Cookie(cookies.AccessName); err == nil {
			if id, parseErr := b.tokens.Parse(access.Value); parseErr == nil {
				userID = id
			}
		}
		if userID <= 0 && b.refreshUsers != nil {
			if refresh, err := r.Cookie(cookies.RefreshName); err == nil && refresh.Value != "" {
				identity, lookupErr := b.refreshUsers.ResolveRefresh(r.Context(), refresh.Value)
				if lookupErr != nil {
					if len(original) == 0 {
						http.SetCookie(w, cookies.CSRF(b.signer.Anonymous(), b.ttl))
					}
					return w, func() {}, lookupErr
				}
				if identity.Valid {
					userID = identity.UserID
				}
			}
		}
	}
	if userID > 0 || len(original) == 0 {
		token := b.signer.Anonymous()
		if userID > 0 {
			token = b.signer.Signed(userID)
		}
		writer := &csrfBootstrapWriter{ResponseWriter: w, cookie: cookies.CSRF(token, b.ttl)}
		return writer, writer.ensureCookie, nil
	}
	return w, func() {}, nil
}

// csrfBootstrapWriter не дублирует CSRF-cookie, если обработчик сменил сессию.
type csrfBootstrapWriter struct {
	http.ResponseWriter
	cookie  *http.Cookie
	checked bool
}

func (w *csrfBootstrapWriter) ensureCookie() {
	if w.checked {
		return
	}
	w.checked = true
	w.Header().Set("Cache-Control", "no-store")
	response := &http.Response{Header: w.Header()}
	for _, cookie := range response.Cookies() {
		if cookie.Name == CSRFCookieName {
			return
		}
	}
	http.SetCookie(w.ResponseWriter, w.cookie)
}

func (w *csrfBootstrapWriter) WriteHeader(status int) {
	w.ensureCookie()
	w.ResponseWriter.WriteHeader(status)
}

func (w *csrfBootstrapWriter) Write(body []byte) (int, error) {
	w.ensureCookie()
	return w.ResponseWriter.Write(body)
}

func (w *csrfBootstrapWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
