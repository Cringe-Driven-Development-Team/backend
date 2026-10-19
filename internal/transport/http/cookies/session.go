// Package cookies определяет единую политику cookie сессии.
package cookies

import (
	"net/http"
	"time"
)

const AccessName = "access_token"
const RefreshName = "refresh_token"
const CSRFName = "__Host-csrf"

// Config задаёт существующие пути, сроки и HTTPS-режим.
type Config struct {
	AccessPath, RefreshPath string
	AccessTTL, CSRFTTL      time.Duration
	Secure                  bool
}
type Signer interface {
	Anonymous() string
	Signed(int64) string
}

// SessionCookies создаёт cookie при смене сессии; запись ответа остаётся в HTTP-адаптере.
type SessionCookies struct {
	Config Config
	Signer Signer
}

func CSRF(token string, ttl time.Duration) *http.Cookie {
	return &http.Cookie{Name: CSRFName, Value: token, Path: "/", Secure: true, SameSite: http.SameSiteLaxMode, MaxAge: int(ttl / time.Second)}
}
func (c SessionCookies) token(name, value, path string, maxAge int) *http.Cookie {
	return &http.Cookie{Name: name, Value: value, Path: path, MaxAge: maxAge, HttpOnly: true, Secure: c.Config.Secure, SameSite: http.SameSiteLaxMode}
}
func (c SessionCookies) Session(access, refresh string, userID int64, expiresAt time.Time) []*http.Cookie {
	return []*http.Cookie{c.token(AccessName, access, c.Config.AccessPath, int(c.Config.AccessTTL/time.Second)), c.token(RefreshName, refresh, c.Config.RefreshPath, int(time.Until(expiresAt).Seconds())), CSRF(c.Signer.Signed(userID), c.Config.CSRFTTL)}
}
func (c SessionCookies) Clear() []*http.Cookie {
	return []*http.Cookie{c.token(AccessName, "", c.Config.AccessPath, -1), c.token(RefreshName, "", c.Config.RefreshPath, -1), CSRF(c.Signer.Anonymous(), c.Config.CSRFTTL)}
}
