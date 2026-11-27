package postgres

import (
	"context"

	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/notebook/usecase"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/storage/workspaceguard"
	"github.com/jackc/pgx/v5"
)

// lockActor ставит постоянный пользовательский барьер перед notebook lock, не читая Identity.
func lockActor(ctx context.Context, tx pgx.Tx, userID int64) error {
	if userID <= 0 {
		return usecase.ErrNotebookNotFound
	}
	_, err := workspaceguard.Actor(ctx, tx, userID)
	return err
}
