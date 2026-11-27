package security

import (
	"testing"
	"time"

	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/user/usecase"
)

// TestParseAccessPreservesExpiry проверяет срок и claims, передаваемые потребителю Identity.
func TestParseAccessPreservesExpiry(t *testing.T) {
	issuer := NewAccessToken([]byte("test-validation-secret"), time.Minute)
	claims := usecase.SessionClaims{UserID: 42, SessionID: "session-42", Version: 7}
	token, expires, err := issuer.IssueAccess(claims)
	if err != nil {
		t.Fatal(err)
	}
	got, err := issuer.ParseAccess(token)
	if err != nil || got.SessionClaims != claims || !got.ExpiresAt.Equal(expires) {
		t.Fatalf("ParseAccess() = %+v, %v; want claims and issuer expiry", got, err)
	}
	if _, err = NewAccessToken([]byte("other-secret"), time.Minute).ParseAccess(token); err == nil {
		t.Fatal("accepted token signed by another key")
	}
}
