package security

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestSessionClaimsAreMandatoryAndExact(t *testing.T) {
	issuer := NewAccessToken([]byte("secret"), time.Minute)
	for _, version := range []any{nil, "", float64(1), "0", "01", "-1", "18446744073709551616", "1.2"} {
		c := jwt.MapClaims{"sub": "7", "exp": time.Now().Add(time.Minute).Unix(), "sid": "session", "session_version": version}
		token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, c).SignedString([]byte("secret"))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := issuer.Parse(token); err == nil {
			t.Fatalf("accepted version %v", version)
		}
	}
	for _, field := range []string{"sid", "session_version", "sub", "exp"} {
		c := jwt.MapClaims{"sub": "7", "exp": time.Now().Add(time.Minute).Unix(), "sid": "session", "session_version": "9007199254740993"}
		delete(c, field)
		token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, c).SignedString([]byte("secret"))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := issuer.Parse(token); err == nil {
			t.Fatalf("accepted missing %s", field)
		}
	}
	c := testClaims(7)
	token, _, err := issuer.IssueAccess(c)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := issuer.Parse(token)
	if err != nil || parsed != c {
		t.Fatalf("claims=%+v err=%v", parsed, err)
	}
}
