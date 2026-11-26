package usecase

import (
	"context"

	D "github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/runtimesession/domain"
	F "github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/runtimesession/domain/workspacefile"
	"github.com/google/uuid"
)

// WorkspaceFilesPort передаёт типизированные команды к сервису окружений.
type WorkspaceFilesPort interface {
	Check(context.Context, F.Target) error
	Command(context.Context, F.Target, F.Command) (F.Result, error)
	Replay(context.Context, F.Target, uint64) (F.Replay, error)
	CloseSocket(context.Context, F.Target) error
	Upload(context.Context, F.Target, uuid.UUID, uint64, []byte) (F.Transfer, error)
	Download(context.Context, F.Target, uuid.UUID, *F.Range, string) (F.Download, error)
}

// WorkspaceFiles подтверждает владельца и состояние между HTTP и Environments.
type WorkspaceFiles struct {
	store Store
	port  WorkspaceFilesPort
}

// NewWorkspaceFiles подключает файловый порт к проверкам Workspace.
func NewWorkspaceFiles(store Store, port WorkspaceFilesPort) *WorkspaceFiles {
	return &WorkspaceFiles{store: store, port: port}
}

// authorize подтверждает пользователя по локальному владельцу и блокирует закрытые сессии.
func (s *WorkspaceFiles) authorize(ctx context.Context, owner int64, target F.Target) error {
	return authorizeFiles(ctx, s.store, owner, target.Session)
}

// Check проверяет готовность рабочей директории указанной сессии.
func (s *WorkspaceFiles) Check(ctx context.Context, owner int64, target F.Target) error {
	if err := s.authorize(ctx, owner, target); err != nil {
		return err
	}
	return s.port.Check(ctx, target)
}

// Command выполняет одну команду только внутри проверенного файлового поколения.
func (s *WorkspaceFiles) Command(ctx context.Context, owner int64, target F.Target, in F.Command) (F.Result, error) {
	if err := s.authorize(ctx, owner, target); err != nil {
		return F.Result{}, err
	}
	if in.SessionID != target.Session.String() || in.Generation != in.SessionID {
		return F.Result{}, D.ErrWorkspaceClosed
	}
	return s.port.Command(ctx, target, in)
}

// Replay получает ограниченную страницу текущей подписки.
func (s *WorkspaceFiles) Replay(ctx context.Context, owner int64, target F.Target, after uint64) (F.Replay, error) {
	if err := s.authorize(ctx, owner, target); err != nil {
		return F.Replay{}, err
	}
	return s.port.Replay(ctx, target, after)
}

// CloseSocket освобождает локальную подписку агента.
func (s *WorkspaceFiles) CloseSocket(ctx context.Context, owner int64, target F.Target) error {
	if err := s.authorize(ctx, owner, target); err != nil {
		return err
	}
	return s.port.CloseSocket(ctx, target)
}

// Upload передаёт ограниченный чанк с прежним transfer ID и offset.
func (s *WorkspaceFiles) Upload(ctx context.Context, owner int64, target F.Target, id uuid.UUID, offset uint64, payload []byte) (F.Transfer, error) {
	if err := s.authorize(ctx, owner, target); err != nil {
		return F.Transfer{}, err
	}
	if offset > F.MaxFileBytes || len(payload) == 0 || len(payload) > F.ChunkBytes || uint64(len(payload)) > F.MaxFileBytes-offset {
		return F.Transfer{}, D.ErrInvalid
	}
	return s.port.Upload(ctx, target, id, offset, payload)
}

// Download открывает подтверждённый поток, который вызывающий обязан закрывать.
func (s *WorkspaceFiles) Download(ctx context.Context, owner int64, target F.Target, id uuid.UUID, bounds *F.Range, etag string) (F.Download, error) {
	if err := s.authorize(ctx, owner, target); err != nil {
		return F.Download{}, err
	}
	return s.port.Download(ctx, target, id, bounds, etag)
}
