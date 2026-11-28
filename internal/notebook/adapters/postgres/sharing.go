package postgres

import (
	"context"
	"errors"
	"slices"

	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/authn"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/notebook/domain"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/notebook/usecase"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/storage/workspaceguard"
	"github.com/jackc/pgx/v5"
)

// CheckOwner проверяет сокрытие чужого блокнота до валидации или сетевого разрешения логинов.
func (r *NotebookRepository) CheckOwner(ctx context.Context, id, ownerID int64) error {
	var found int64
	err := r.db.QueryRow(ctx, "SELECT id FROM workspace.notebooks WHERE id=$1 AND owner_id=$2", id, ownerID).Scan(&found)
	if errors.Is(err, pgx.ErrNoRows) {
		return usecase.ErrNotebookNotFound
	}
	return err
}

// PutSharing принимает уже разрешённые stable IDs и меняет ACL одной локальной транзакцией.
func (r *NotebookRepository) PutSharing(ctx context.Context, id, ownerID int64, mode domain.SharingMode, readerIDs []int64) (domain.SharingSettings, error) {
	if ownerID <= 0 {
		return domain.SharingSettings{}, usecase.ErrNotebookNotFound
	}
	ids := slices.Clone(readerIDs)
	slices.Sort(ids)
	ids = slices.Compact(ids)
	for _, userID := range ids {
		if userID <= 0 {
			return domain.SharingSettings{}, usecase.ErrInvalidSharing
		}
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return domain.SharingSettings{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	guards, err := workspaceguard.Lock(ctx, tx, append(slices.Clone(ids), ownerID)...)
	if err != nil {
		return domain.SharingSettings{}, err
	}
	if guards[ownerID].Deleting {
		return domain.SharingSettings{}, authn.ErrInvalidSession
	}
	if _, err = scanNotebook(tx.QueryRow(ctx, selectNotebook+` FOR UPDATE`, id, ownerID)); err != nil {
		return domain.SharingSettings{}, err
	}
	if err = usecase.ValidateSharing(mode, nil); err != nil {
		return domain.SharingSettings{}, err
	}
	if mode != domain.SharingModeRestricted && len(ids) > 0 {
		return domain.SharingSettings{}, usecase.ErrInvalidSharing
	}
	for _, userID := range ids {
		if guards[userID].Deleting {
			return domain.SharingSettings{}, usecase.ErrInvalidSharing
		}
	}
	if _, err = tx.Exec(ctx, `UPDATE workspace.notebooks SET sharing_mode=$2 WHERE id=$1`, id, mode); err != nil {
		return domain.SharingSettings{}, err
	}
	if _, err = tx.Exec(ctx, `DELETE FROM workspace.notebook_readers WHERE notebook_id=$1`, id); err != nil {
		return domain.SharingSettings{}, err
	}
	readers := make([]domain.NotebookReader, 0, len(ids))
	for _, userID := range ids {
		if userID == ownerID {
			continue
		}
		if _, err = tx.Exec(ctx, `INSERT INTO workspace.notebook_readers(notebook_id,user_id) VALUES($1,$2)`, id, userID); err != nil {
			return domain.SharingSettings{}, err
		}
		readers = append(readers, domain.NotebookReader{ID: userID})
	}
	if err = tx.Commit(ctx); err != nil {
		return domain.SharingSettings{}, err
	}
	return domain.SharingSettings{Mode: mode, Readers: readers}, nil
}

// GetSharing читает ID адресатов вместе с mode в одном snapshot, без JOIN Identity.
func (r *NotebookRepository) GetSharing(ctx context.Context, id, ownerID int64) (domain.SharingSettings, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return domain.SharingSettings{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err = tx.Exec(ctx, `SET TRANSACTION ISOLATION LEVEL REPEATABLE READ READ ONLY`); err != nil {
		return domain.SharingSettings{}, err
	}
	n, err := scanNotebook(tx.QueryRow(ctx, selectNotebook, id, ownerID))
	if err != nil {
		return domain.SharingSettings{}, err
	}
	readers, err := readReaders(ctx, tx, id)
	if err != nil {
		return domain.SharingSettings{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return domain.SharingSettings{}, err
	}
	return domain.SharingSettings{Mode: n.SharingMode, Readers: readers}, nil
}

// readReaders возвращает только принадлежащие Workspace ссылки на пользователей.
func readReaders(ctx context.Context, tx pgx.Tx, id int64) ([]domain.NotebookReader, error) {
	rows, err := tx.Query(ctx, `SELECT user_id FROM workspace.notebook_readers WHERE notebook_id=$1 ORDER BY user_id`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	readers := make([]domain.NotebookReader, 0)
	for rows.Next() {
		var r domain.NotebookReader
		if err = rows.Scan(&r.ID); err != nil {
			return nil, err
		}
		readers = append(readers, r)
	}
	return readers, rows.Err()
}
