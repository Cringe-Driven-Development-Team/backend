package usecase

import (
	"context"
	"errors"
	"time"
)

var (
	ErrInvalidSession = errors.New("session is invalid or expired")
	ErrVersionChanged = errors.New("session version has changed")
)

type SessionVersion uint64

type SessionClaims struct {
	UserID    int64
	SessionID string
	Version   SessionVersion
}

type RefreshSession struct {
	SessionClaims
	TokenHash string
	ExpiresAt time.Time
}

type NewSession struct {
	RefreshSession
	AccessExpiresAt time.Time
}

// SessionStore is the authoritative state of access and refresh authorization.
// Reads never initialize versions or extend TTL. Mutations must be atomic.
// A transport error on a mutation may mean an unknown outcome, not a rollback.
type SessionStore interface {
	CurrentVersion(context.Context, int64) (SessionVersion, error)
	Create(context.Context, NewSession) error
	Find(context.Context, string) (RefreshSession, error)
	ValidateAccess(context.Context, SessionClaims) error
	Rotate(context.Context, string, NewSession) error
	Revoke(context.Context, string, SessionClaims) error
	RevokeAll(context.Context, int64) error
}
