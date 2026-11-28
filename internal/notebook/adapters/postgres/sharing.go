package postgres

import (
	"context"
	"crypto/rand"
	"encoding/base64"
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
func (r *NotebookRepository) PutSharing(ctx context.Context, id, ownerID int64, link bool, readers []domain.NotebookReader) (domain.SharingSettings, error) {
	if ownerID <= 0 {
		return domain.SharingSettings{}, usecase.ErrNotebookNotFound
	}
	ids := make([]int64, 0, len(readers))
	roles := make(map[int64]domain.ShareRole, len(readers))
	for _, reader := range readers {
		if reader.ID != ownerID {
			ids = append(ids, reader.ID)
			roles[reader.ID] = reader.Role
		}
	}
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
	n, err := scanNotebook(tx.QueryRow(ctx, selectNotebook+` FOR UPDATE`, id, ownerID))
	if err != nil {
		return domain.SharingSettings{}, err
	}
	for _, userID := range ids {
		if guards[userID].Deleting {
			return domain.SharingSettings{}, usecase.ErrUnknownLogin
		}
	}
	token := n.LinkToken
	if !link {
		token = nil
	} else if token == nil {
		var random [32]byte
		_, _ = rand.Read(random[:])
		value := base64.RawURLEncoding.EncodeToString(random[:])
		token = &value
	}
	if _, err = tx.Exec(ctx, `UPDATE workspace.notebooks SET link_token=$2 WHERE id=$1`, id, token); err != nil {
		return domain.SharingSettings{}, err
	}
	if _, err = tx.Exec(ctx, `DELETE FROM workspace.notebook_readers WHERE notebook_id=$1`, id); err != nil {
		return domain.SharingSettings{}, err
	}
	saved := make([]domain.NotebookReader, 0, len(ids))
	for _, userID := range ids {
		if _, err = tx.Exec(ctx, `INSERT INTO workspace.notebook_readers(notebook_id,user_id,role) VALUES($1,$2,$3)`, id, userID, roles[userID]); err != nil {
			return domain.SharingSettings{}, err
		}
		saved = append(saved, domain.NotebookReader{ID: userID, Role: roles[userID]})
	}
	if err = tx.Commit(ctx); err != nil {
		return domain.SharingSettings{}, err
	}
	return domain.SharingSettings{LinkToken: token, Readers: saved}, nil
}

// GetSharing читает ID адресатов вместе с токеном в одном snapshot, без JOIN Identity.
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
	return domain.SharingSettings{LinkToken: n.LinkToken, Readers: readers}, nil
}

// readReaders возвращает только принадлежащие Workspace ссылки на пользователей.
func readReaders(ctx context.Context, tx pgx.Tx, id int64) ([]domain.NotebookReader, error) {
	rows, err := tx.Query(ctx, `SELECT user_id,role FROM workspace.notebook_readers WHERE notebook_id=$1 ORDER BY user_id`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	readers := make([]domain.NotebookReader, 0)
	for rows.Next() {
		var r domain.NotebookReader
		if err = rows.Scan(&r.ID, &r.Role); err != nil {
			return nil, err
		}
		readers = append(readers, r)
	}
	return readers, rows.Err()
}
