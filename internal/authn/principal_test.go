package authn

import (
	"context"
	"testing"
	"time"
)

// TestPrincipalContext отличает подтверждённого пользователя от явного гостя и отсутствия контекста.
func TestPrincipalContext(t *testing.T) {
	if _, ok := FromContext(context.Background()); ok {
		t.Fatal("implicit identity")
	}
	p := Principal{UserID: 42, SessionID: "session", Version: 3, ExpiresAt: time.Now()}
	got, ok := FromContext(WithPrincipal(context.Background(), p))
	if !ok || got != p {
		t.Fatal("principal lost")
	}
	guest, ok := FromContext(WithPrincipal(context.Background(), Principal{Anonymous: true}))
	if !ok || !guest.Anonymous || guest.UserID != 0 {
		t.Fatal("anonymous context lost")
	}
}
