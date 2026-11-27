package domain

import (
	"context"

	"github.com/google/uuid"
)

type fenceKey struct{}

type workFence struct {
	session uuid.UUID
	version uint64
}

// WithFence связывает владение фоновой задачей только с указанной сессией.
func WithFence(ctx context.Context, session uuid.UUID, version int64) context.Context {
	if version <= 0 {
		return ctx
	}
	return context.WithValue(ctx, fenceKey{}, workFence{session: session, version: uint64(version)})
}

// Fence возвращает epoch фоновой работы. Ноль обозначает вызов вне lease worker.
func Fence(ctx context.Context, session uuid.UUID) uint64 {
	if value, ok := ctx.Value(fenceKey{}).(workFence); ok && value.session == session {
		return value.version
	}
	return 0
}
