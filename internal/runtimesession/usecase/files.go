package usecase

import (
	"context"

	D "github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/runtimesession/domain"
	"github.com/google/uuid"
)

// FileAgent передаёт файлы подготовленному агенту с ограниченной буферизацией.
type FileAgent interface {
	Files(context.Context, uuid.UUID, FileRequest) (FileResponse, error)
}

// Files проверяет владельца и готовность runtime перед каждой файловой командой или частью.
func (s *Service) Files(ctx context.Context, owner int64, id uuid.UUID, req FileRequest) (FileResponse, error) {
	if err := authorizeFiles(ctx, s.store, owner, id); err != nil {
		return FileResponse{}, err
	}
	agent, ok := s.agent.(FileAgent)
	if !ok {
		return FileResponse{}, D.ErrUnavailable
	}
	return agent.Files(ctx, id, req)
}

// authorizeFiles проверяет локальное владение и готовность до любого сетевого обращения.
func authorizeFiles(ctx context.Context, store Store, owner int64, id uuid.UUID) error {
	if owner <= 0 {
		return D.ErrNotFound
	}
	session, err := store.Owned(ctx, owner, id)
	if err != nil {
		return err
	}
	if session.State != D.Ready {
		if session.State == D.Stopping || session.State == D.Stopped || session.State == D.Failed {
			return D.ErrWorkspaceClosed
		}
		return D.ErrUnavailable
	}
	return nil
}
