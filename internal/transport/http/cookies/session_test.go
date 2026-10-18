package cookies

import (
	"testing"
	"time"
)

// TestCSRFCookiePolicy сохраняет проверку атрибутов, вынесенную из теста подписей.
func TestCSRFCookiePolicy(t *testing.T) {
	cookie := CSRF("signed-token", 30*24*time.Hour)
	if cookie.Name != "__Host-csrf" || cookie.Value != "signed-token" || cookie.Path != "/" || !cookie.Secure || cookie.HttpOnly || cookie.Domain != "" || cookie.MaxAge != 30*24*3600 {
		t.Fatalf("invalid CSRF cookie: %+v", cookie)
	}
}
