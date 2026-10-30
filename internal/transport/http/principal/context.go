// Package principal передаёт пользователя через контекст HTTP-запроса.
package principal

import (
	"context"

	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/user/usecase"
)

type userIDKey struct{}

func WithUserID(ctx context.Context, id int64) context.Context {
	return context.WithValue(ctx, userIDKey{}, id)
}

func UserIDFromContext(ctx context.Context) (int64, bool) {
	id, ok := ctx.Value(userIDKey{}).(int64)
	return id, ok
}

type sessionKey struct{}

func WithSession(ctx context.Context, claims usecase.SessionClaims) context.Context {
	return context.WithValue(WithUserID(ctx, claims.UserID), sessionKey{}, claims)
}
func SessionFromContext(ctx context.Context) (usecase.SessionClaims, bool) {
	claims, ok := ctx.Value(sessionKey{}).(usecase.SessionClaims)
	return claims, ok
}
