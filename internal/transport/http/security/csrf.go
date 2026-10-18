// Package security реализует аутентификацию и защиту HTTP-запросов.
package security

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"strconv"
	"strings"
)

// CSRFSigner выпускает токены и проверяет их привязку к пользователю.
type CSRFSigner struct {
	secret []byte
}

func NewCSRFSigner(secret []byte) *CSRFSigner {
	return &CSRFSigner{secret: append([]byte(nil), secret...)}
}

func (c *CSRFSigner) Anonymous() string {
	return base64.RawURLEncoding.EncodeToString(csrfNonce())
}

// IsAnonymousCSRF распознаёт только корректный неподписанный токен.
func IsAnonymousCSRF(token string) bool {
	nonce, err := base64.RawURLEncoding.Strict().DecodeString(token)
	return err == nil && len(nonce) == 32 && base64.RawURLEncoding.EncodeToString(nonce) == token
}

func (c *CSRFSigner) Signed(userID int64) string {
	nonce := csrfNonce()
	return base64.RawURLEncoding.EncodeToString(nonce) + "." + base64.RawURLEncoding.EncodeToString(c.signature(userID, nonce))
}

func (c *CSRFSigner) Valid(token string, userID int64) bool {
	parts := strings.Split(token, ".")
	if len(parts) != 2 || userID <= 0 {
		return false
	}
	nonce, err := base64.RawURLEncoding.Strict().DecodeString(parts[0])
	if err != nil || len(nonce) != 32 {
		return false
	}
	signature, err := base64.RawURLEncoding.Strict().DecodeString(parts[1])
	return err == nil && len(signature) == sha256.Size && hmac.Equal(signature, c.signature(userID, nonce))
}

func (c *CSRFSigner) signature(userID int64, nonce []byte) []byte {
	mac := hmac.New(sha256.New, c.secret)
	_, _ = mac.Write([]byte(strconv.FormatInt(userID, 10) + "."))
	_, _ = mac.Write(nonce)
	return mac.Sum(nil)
}

func csrfNonce() []byte {
	nonce := make([]byte, 32)
	_, _ = rand.Read(nonce)
	return nonce
}
