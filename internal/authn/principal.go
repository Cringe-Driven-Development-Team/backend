// Package authn содержит подтверждённую пользовательскую идентичность без транспорта и хранилищ.
package authn

import (
	"context"
	"errors"
	"time"
)

// ErrInvalidSession означает отказ проверки пользователя; сбой сервиса не равен этому отказу.
var ErrInvalidSession = errors.New("invalid user session")

// ErrSessionUnavailable означает, что проверку сессии нельзя завершить.
var ErrSessionUnavailable = errors.New("session dependency unavailable")

// Principal содержит только проверенные claims Identity или явный anonymous-контекст.
type Principal struct {
	UserID    int64
	SessionID string
	Version   uint64
	ExpiresAt time.Time
	Anonymous bool
}

// SessionVerifier задаёт порт проверки сессии для потребителей Identity.
type SessionVerifier interface {
	Verify(context.Context, string) (Principal, error)
}

// principalKey отделяет подтверждённые claims от произвольных контекстных значений.
type principalKey struct{}

// WithPrincipal переносит principal во вложенные операции с тем же сроком context.
func WithPrincipal(ctx context.Context, p Principal) context.Context {
	return context.WithValue(ctx, principalKey{}, p)
}

// FromContext возвращает false, если подтверждённый/anonymous субъект ещё не задан.
func FromContext(ctx context.Context) (Principal, bool) {
	p, ok := ctx.Value(principalKey{}).(Principal)
	return p, ok
}
