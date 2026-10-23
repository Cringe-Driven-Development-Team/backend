package usecase

import "time"

// Session получает клиент после входа или refresh.
type Session struct {
	UserID          int64
	AccessExpiresAt time.Time
	AccessToken     string
	RefreshToken    string
	ExpiresAt       time.Time // Refresh-session expiry.
}
