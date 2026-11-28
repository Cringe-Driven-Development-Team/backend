package postgres

import (
	"context"
	"errors"

	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/notebook/domain"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/notebook/usecase"
	"github.com/jackc/pgx/v5"
)

const readableCondition = `($2::bigint>0 AND (owner_id=$2 OR EXISTS (SELECT 1 FROM workspace.notebook_readers nr WHERE nr.notebook_id=notebooks.id AND nr.user_id=$2)))`
const selectReadableNotebook = `SELECT id,owner_id,name,revision,cells_count,preview_code,size_bytes,created_at,updated_at,link_token FROM workspace.notebooks WHERE id=$1 AND ` + readableCondition
const selectNotebookForClone = `SELECT id,owner_id,name,revision,cells_count,preview_code,size_bytes,created_at,updated_at,link_token FROM workspace.notebooks WHERE id=$1 FOR UPDATE`

// readQuerier позволяет проверять доступ через пул или уже открытую транзакцию.
type readQuerier interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

// checkRead использует единую политику доступа, не открывая отдельную транзакцию.
func checkRead(ctx context.Context, q readQuerier, id, userID int64) error {
	var found int64
	err := q.QueryRow(ctx, `SELECT id FROM workspace.notebooks WHERE id=$1 AND `+readableCondition, id, userID).Scan(&found)
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
	return r.readNotebookSnapshot(ctx, userID, includeCommentCounts, selectReadableNotebook, id, userID)
}

// GetShared проверяет токен и читает документ в одном снимке.
func (r *NotebookRepository) GetShared(ctx context.Context, token string, userID int64, includeCommentCounts bool) (domain.Notebook, error) {
	if userID <= 0 || token == "" {
		return domain.Notebook{}, usecase.ErrNotebookNotFound
	}
	const query = `SELECT id,owner_id,name,revision,cells_count,preview_code,size_bytes,created_at,updated_at,link_token FROM workspace.notebooks WHERE link_token=$1`
	return r.readNotebookSnapshot(ctx, userID, includeCommentCounts, query, token)
}

func (r *NotebookRepository) readNotebookSnapshot(ctx context.Context, userID int64, includeCommentCounts bool, query string, args ...any) (domain.Notebook, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return domain.Notebook{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err = tx.Exec(ctx, `SET TRANSACTION ISOLATION LEVEL REPEATABLE READ READ ONLY`); err != nil {
		return domain.Notebook{}, err
	}
	n, err := scanNotebook(tx.QueryRow(ctx, query, args...))
	if err != nil {
		return domain.Notebook{}, err
	}
	n, err = readContent(ctx, tx, n)
	if err != nil {
		return domain.Notebook{}, err
	}
	n.Access, err = notebookAccess(ctx, tx, n.ID, n.OwnerID, userID)
	if err != nil {
		return domain.Notebook{}, err
	}
	if includeCommentCounts && n.Access.CanClone {
		if err = readCommentCounts(ctx, tx, &n); err != nil {
			return domain.Notebook{}, err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return domain.Notebook{}, err
	}
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
	if err = lockActor(ctx, tx, userID); err != nil {
		return domain.Notebook{}, err
	}

	n, err := scanNotebook(tx.QueryRow(ctx, selectNotebookForClone, id))
	if err != nil {
		return domain.Notebook{}, err
	}
	var allowed bool
	err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM workspace.notebooks WHERE id=$1 AND `+readableCondition+`)`, id, userID).Scan(&allowed)
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

// notebookAccess читает роль в снимке вызывающей транзакции.
func notebookAccess(ctx context.Context, q readQuerier, id, ownerID, userID int64) (domain.NotebookAccess, error) {
	var role domain.ShareRole
	if userID > 0 && userID != ownerID {
		err := q.QueryRow(ctx, `SELECT role FROM workspace.notebook_readers WHERE notebook_id=$1 AND user_id=$2`, id, userID).Scan(&role)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return domain.NotebookAccess{}, err
		}
	}
	return domain.AccessForRole(ownerID, userID, role), nil
}
