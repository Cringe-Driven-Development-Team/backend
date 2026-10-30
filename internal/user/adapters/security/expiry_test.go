package security

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestIssuedAccessExpiryMatchesJWT(t *testing.T) {
	issuer := NewAccessToken([]byte("test-secret"), time.Minute)
	token, expiresAt, err := issuer.IssueAccess(testClaims(42))
	if err != nil {
		t.Fatal(err)
	}
	var claims jwt.RegisteredClaims
	_, err = jwt.ParseWithClaims(token, &claims, func(*jwt.Token) (any, error) { return []byte("test-secret"), nil })
	if err != nil {
		t.Fatal(err)
	}
	if !expiresAt.Equal(claims.ExpiresAt.Time) {
		t.Fatal("expiry metadata differs from signed JWT")
	}
	if time.Until(expiresAt) < 58*time.Second || time.Until(expiresAt) > time.Minute {
		t.Fatal("unexpected access lifetime")
	}
}
