package postgres

import (
	"context"
	"errors"

	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/notebook/domain"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/notebook/usecase"
	"github.com/jackc/pgx/v5"
)

const commentColumns = `c.id,c.notebook_id,c.cell_id,c.author_id,u.login,c.text,c.created_at,c.updated_at`

// lockCommentWall сериализует операции с изменением доступа и удалением ячейки.
// Проверка грантов отдельным запросом после ожидания видит свежий READ COMMITTED snapshot.
func lockCommentWall(ctx context.Context, tx pgx.Tx, notebookID int64, cellID string, visitorID int64) (int64, error) {
	var ownerID int64
	err := tx.QueryRow(ctx, `SELECT owner_id FROM notebooks WHERE id=$1 FOR UPDATE`, notebookID).Scan(&ownerID)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, usecase.ErrNotebookNotFound
	}
	if err != nil {
		return 0, err
	}
	if err = checkRead(ctx, tx, notebookID, visitorID); err != nil {
		return 0, err
	}
	var cell string
	err = tx.QueryRow(ctx, `SELECT cell_id FROM notebook_cells WHERE notebook_id=$1 AND cell_id=$2`, notebookID, cellID).Scan(&cell)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, usecase.ErrCellNotFound
	}
	return ownerID, err
}

// withCommentWall держит блокировку до успешного commit, откатывая любой ранний выход.
func (r *NotebookRepository) withCommentWall(ctx context.Context, notebookID int64, cellID string, visitorID int64, operation func(pgx.Tx, int64) error) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	ownerID, err := lockCommentWall(ctx, tx, notebookID, cellID, visitorID)
	if err != nil {
		return err
	}
	if err = operation(tx, ownerID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// scanComment читает только публичные данные автора и сообщения.
func scanComment(row pgx.Row) (domain.Comment, error) {
	var c domain.Comment
	err := row.Scan(&c.ID, &c.NotebookID, &c.CellID, &c.Author.ID, &c.Author.Login, &c.Text, &c.CreatedAt, &c.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return c, usecase.ErrCommentNotFound
	}
	c.CreatedAt = c.CreatedAt.UTC()
	c.UpdatedAt = c.UpdatedAt.UTC()
	return c, err
}

// readComment проверяет полную привязку и блокирует найденное сообщение.
func readComment(ctx context.Context, tx pgx.Tx, notebookID int64, cellID string, commentID int64) (domain.Comment, error) {
	return scanComment(tx.QueryRow(ctx, `SELECT `+commentColumns+` FROM cell_comments c JOIN users u ON u.id=c.author_id WHERE c.notebook_id=$1 AND c.cell_id=$2 AND c.id=$3 FOR UPDATE OF c`, notebookID, cellID, commentID))
}

// ListComments читает не более limit+1 сообщений одной стены с актуальными логинами.
func (r *NotebookRepository) ListComments(ctx context.Context, notebookID int64, cellID string, visitorID int64, query usecase.CommentQuery) (usecase.CommentRows, error) {
	if query.Limit < 1 || query.Limit > 100 {
		return usecase.CommentRows{}, usecase.ErrInvalidCommentList
	}
	result := usecase.CommentRows{Items: []domain.Comment{}}
	err := r.withCommentWall(ctx, notebookID, cellID, visitorID, func(tx pgx.Tx, ownerID int64) error {
		result.OwnerID = ownerID
		sql := `SELECT ` + commentColumns + ` FROM cell_comments c JOIN users u ON u.id=c.author_id WHERE c.notebook_id=$1 AND c.cell_id=$2`
		args := []any{notebookID, cellID}
		if query.After != nil {
			sql += ` AND (c.created_at,c.id)<($3,$4)`
			args = append(args, query.After.CreatedAt, query.After.ID)
		}
		if query.After == nil {
			sql += ` ORDER BY c.created_at DESC,c.id DESC LIMIT $3`
		} else {
			sql += ` ORDER BY c.created_at DESC,c.id DESC LIMIT $5`
		}
		args = append(args, query.Limit+1)
		rows, err := tx.Query(ctx, sql, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			c, e := scanComment(rows)
			if e != nil {
				return e
			}
			result.Items = append(result.Items, c)
		}
		return rows.Err()
	})
	if err != nil {
		return usecase.CommentRows{}, err
	}
	return result, nil
}

// CreateComment присваивает авторство из сессии и одинаковые начальные даты БД.
func (r *NotebookRepository) CreateComment(ctx context.Context, notebookID int64, cellID string, visitorID int64, text string) (usecase.CommentRecord, error) {
	if visitorID <= 0 {
		return usecase.CommentRecord{}, usecase.ErrCommentForbidden
	}
	var result usecase.CommentRecord
	err := r.withCommentWall(ctx, notebookID, cellID, visitorID, func(tx pgx.Tx, ownerID int64) error {
		result.OwnerID = ownerID
		var id int64
		err := tx.QueryRow(ctx, `INSERT INTO cell_comments(notebook_id,cell_id,author_id,text,created_at,updated_at) VALUES($1,$2,$3,$4,statement_timestamp(),statement_timestamp()) RETURNING id`, notebookID, cellID, visitorID, text).Scan(&id)
		if err != nil {
			return err
		}
		result.Comment, err = readComment(ctx, tx, notebookID, cellID, id)
		return err
	})
	if err != nil {
		return usecase.CommentRecord{}, err
	}
	return result, nil
}

// UpdateComment атомарно меняет собственный текст и строго увеличивает дату.
func (r *NotebookRepository) UpdateComment(ctx context.Context, notebookID int64, cellID string, visitorID, commentID int64, text string) (usecase.CommentRecord, error) {
	if visitorID <= 0 {
		return usecase.CommentRecord{}, usecase.ErrCommentForbidden
	}
	var result usecase.CommentRecord
	err := r.withCommentWall(ctx, notebookID, cellID, visitorID, func(tx pgx.Tx, ownerID int64) error {
		c, err := readComment(ctx, tx, notebookID, cellID, commentID)
		if err != nil {
			return err
		}
		if c.Author.ID != visitorID {
			return usecase.ErrCommentForbidden
		}
		if _, err = tx.Exec(ctx, `UPDATE cell_comments SET text=$4,updated_at=GREATEST(clock_timestamp(),updated_at+INTERVAL '1 microsecond') WHERE notebook_id=$1 AND cell_id=$2 AND id=$3`, notebookID, cellID, commentID, text); err != nil {
			return err
		}
		result.OwnerID = ownerID
		result.Comment, err = readComment(ctx, tx, notebookID, cellID, commentID)
		return err
	})
	if err != nil {
		return usecase.CommentRecord{}, err
	}
	return result, nil
}

// DeleteComment физически удаляет сообщение автора или по действию владельца.
func (r *NotebookRepository) DeleteComment(ctx context.Context, notebookID int64, cellID string, visitorID, commentID int64) error {
	if visitorID <= 0 {
		return usecase.ErrCommentForbidden
	}
	return r.withCommentWall(ctx, notebookID, cellID, visitorID, func(tx pgx.Tx, ownerID int64) error {
		c, err := readComment(ctx, tx, notebookID, cellID, commentID)
		if err != nil {
			return err
		}
		if c.Author.ID != visitorID && ownerID != visitorID {
			return usecase.ErrCommentForbidden
		}
		_, err = tx.Exec(ctx, `DELETE FROM cell_comments WHERE notebook_id=$1 AND cell_id=$2 AND id=$3`, notebookID, cellID, commentID)
		return err
	})
}
