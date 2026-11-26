package usecase

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/observability"
	"github.com/google/uuid"
)

// ErrDeletionConflict означает переиспользование operation ID с другим пользователем.
var ErrDeletionConflict = errors.New("user deletion conflict")

// Deletion хранит долговечное состояние удаления. AvatarKey и CorrelationID не выдаются через RPC.
type Deletion struct {
	OperationID              uuid.UUID
	UserID                   int64
	Stage, ErrorCode         string
	AvatarKey, CorrelationID string
	Version                  int64
}

// DeletionStore закрепляет переходы за lease и завершает удаление аккаунта одной транзакцией.
type DeletionStore interface {
	Begin(context.Context, int64, uuid.UUID) (Deletion, error)
	Get(context.Context, uuid.UUID) (Deletion, error)
	Claim(context.Context) (Deletion, error)
	Advance(context.Context, Deletion, string, string) error
}

// WorkspaceCleanup подтверждает, что пользователь больше не владеет ресурсами Workspace.
type WorkspaceCleanup interface {
	Cleanup(context.Context, int64, uuid.UUID) (bool, error)
}

// DeletionService координирует отзыв сессий, удаление зависимостей и очистку аватара.
type DeletionService struct {
	store    DeletionStore
	sessions interface {
		RevokeAll(context.Context, int64) error
	}
	workspace WorkspaceCleanup
	avatars   interface {
		Delete(context.Context, string) error
	}
}

// NewDeletionService подключает только порты владельцев данных и хранилище состояния.
func NewDeletionService(store DeletionStore, sessions interface {
	RevokeAll(context.Context, int64) error
},
	workspace WorkspaceCleanup, avatars interface {
		Delete(context.Context, string) error
	}) *DeletionService {
	return &DeletionService{store: store, sessions: sessions, workspace: workspace, avatars: avatars}
}

// Begin закрывает создание новых сессий и сохраняет намерение.
func (s *DeletionService) Begin(ctx context.Context, user int64, operation uuid.UUID) (Deletion, error) {
	if user <= 0 || operation == uuid.Nil {
		return Deletion{}, ErrValidation
	}
	return s.store.Begin(ctx, user, operation)
}

// Get читает ранее сохранённое намерение удаления.
func (s *DeletionService) Get(ctx context.Context, operation uuid.UUID) (Deletion, error) {
	if operation == uuid.Nil {
		return Deletion{}, ErrValidation
	}
	return s.store.Get(ctx, operation)
}

// Run продолжает durable операции, используя сохранённую корреляцию независимо от исходного запроса.
func (s *DeletionService) Run(ctx context.Context) error {
	timer := time.NewTicker(time.Second)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-timer.C:
			attempt, cancel := context.WithTimeout(ctx, 20*time.Second)
			s.step(attempt)
			cancel()
		}
	}
}

func (s *DeletionService) step(ctx context.Context) {
	job, err := s.store.Claim(ctx)
	if errors.Is(err, ErrUserNotFound) {
		return
	}
	if err != nil {
		slog.WarnContext(ctx, "identity.deletion.claim_failed", "error_code", observability.ErrorCode(err))
		return
	}
	ctx = observability.WithRequestID(ctx, job.CorrelationID)
	next, code := job.Stage, ""
	switch job.Stage {
	case "revoking":
		if err = s.sessions.RevokeAll(ctx, job.UserID); err == nil {
			next = "workspace"
		} else {
			code = "session_store_unavailable"
		}
	case "workspace":
		var done bool
		done, err = s.workspace.Cleanup(ctx, job.UserID, job.OperationID)
		if err != nil {
			code = "workspace_unavailable"
		} else if done {
			next = "avatar"
		}
	case "avatar":
		if job.AvatarKey != "" {
			err = s.avatars.Delete(ctx, job.AvatarKey)
		}
		if err == nil {
			next = "done"
		} else {
			code = "avatar_store_unavailable"
		}
	}
	if err = s.store.Advance(ctx, job, next, code); err != nil || code != "" {
		slog.WarnContext(ctx, "identity.deletion.deferred", "stage", job.Stage, "operation_id", job.OperationID, "error_code", code)
	}
}
