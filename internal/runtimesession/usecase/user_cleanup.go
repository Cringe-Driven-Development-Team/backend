package usecase

import (
	"context"

	"github.com/google/uuid"
)

// UserCleanup — состояние локальной очистки Workspace без профиля Identity.
type UserCleanup struct {
	OperationID uuid.UUID
	UserID      int64
	Done        bool
}

// UserCleanupStore сохраняет барьер и атомарно завершает удаление зависимостей.
type UserCleanupStore interface {
	BeginUserCleanup(context.Context, int64, uuid.UUID) (UserCleanup, error)
	GetUserCleanup(context.Context, uuid.UUID) (UserCleanup, error)
}
