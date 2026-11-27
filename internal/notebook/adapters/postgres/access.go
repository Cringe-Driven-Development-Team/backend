package postgres

import (
	"context"
	"errors"

	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/notebook/domain"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/notebook/usecase"
	"github.com/jackc/pgx/v5"
)

const readableCondition = `(owner_id=$2 OR sharing_mode='public' OR (sharing_mode='restricted' AND $2>0 AND EXISTS (SELECT 1 FROM notebook_readers nr WHERE nr.notebook_id=notebooks.id AND nr.user_id=$2)))`
const selectReadableNotebook = `SELECT id,owner_id,name,revision,cells_count,preview_code,size_bytes,created_at,updated_at,sharing_mode FROM notebooks WHERE id=$1 AND ` + readableCondition
const selectNotebookForClone = `SELECT id,owner_id,name,revision,cells_count,preview_code,size_bytes,created_at,updated_at,sharing_mode FROM notebooks WHERE id=$1 FOR UPDATE`

// readQuerier позволяет проверять доступ через пул или уже открытую транзакцию.
type readQuerier interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

// checkRead использует единую политику доступа, не открывая отдельную транзакцию.
func checkRead(ctx context.Context, q readQuerier, id, userID int64) error {
	var found int64
	err := q.QueryRow(ctx, `SELECT id FROM notebooks WHERE id=$1 AND `+readableCondition, id, userID).Scan(&found)
	if errors.Is(err, pgx.ErrNoRows) {
		return usecase.ErrNotebookNotFound
	}
	return err
}

func (r *NotebookRepository) CheckRead(ctx context.Context, id, userID int64) error {
	return checkRead(ctx, r.db, id, userID)
}

// GetReadable читает доступный пользователю снимок, при необходимости со счётчиками комментариев.
func (r *NotebookRepository) GetReadable(ctx context.Context, id, userID int64, includeCommentCounts bool) (domain.Notebook, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return domain.Notebook{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err = tx.Exec(ctx, `SET TRANSACTION ISOLATION LEVEL REPEATABLE READ READ ONLY`); err != nil {
		return domain.Notebook{}, err
	}
	n, err := scanNotebook(tx.QueryRow(ctx, selectReadableNotebook, id, userID))
	if err != nil {
		return domain.Notebook{}, err
	}
	n, err = readContent(ctx, tx, n)
	if err != nil {
		return domain.Notebook{}, err
	}
	if includeCommentCounts {
		if err = readCommentCounts(ctx, tx, &n); err != nil {
			return domain.Notebook{}, err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return domain.Notebook{}, err
	}
	n.Access = domain.AccessFor(n.OwnerID, userID)
	return n, nil
}

// Clone блокирует исходник до проверки грантов. Отдельный запрос после ожидания
// блокировки видит уже завершившийся отзыв доступа в READ COMMITTED.
func (r *NotebookRepository) Clone(ctx context.Context, id, userID int64, name string) (domain.Notebook, error) {
	if userID <= 0 {
		return domain.Notebook{}, usecase.ErrNotebookNotFound
	}
	name, err := usecase.NormalizeName(name)
	if err != nil {
		return domain.Notebook{}, err
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return domain.Notebook{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	n, err := scanNotebook(tx.QueryRow(ctx, selectNotebookForClone, id))
	if err != nil {
		return domain.Notebook{}, err
	}
	var allowed bool
	err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM notebooks WHERE id=$1 AND `+readableCondition+`)`, id, userID).Scan(&allowed)
	if err != nil {
		return domain.Notebook{}, err
	}
	if !allowed {
		return domain.Notebook{}, usecase.ErrNotebookNotFound
	}
	n, err = readContent(ctx, tx, n)
	if err != nil {
		return domain.Notebook{}, err
	}
	n.ID = 0
	n.OwnerID = userID
	n.Name = name
	if err = prepare(&n); err != nil {
		return domain.Notebook{}, err
	}
	n, err = createNotebookTx(ctx, tx, n)
	if err != nil {
		return domain.Notebook{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return domain.Notebook{}, err
	}
	n.Access = domain.AccessFor(n.OwnerID, userID)
	return n, nil
}
