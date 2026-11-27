package postgres

import (
	"context"
	"errors"

	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/observability"
	D "github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/runtimesession/domain"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/runtimesession/usecase"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/storage/workspaceguard"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

var _ usecase.UserCleanupStore = (*Store)(nil)

// BeginUserCleanup ставит барьер до попытки удалить зависимости. Повтор продолжает тот же запрос.
func (s *Store) BeginUserCleanup(ctx context.Context, owner int64, id uuid.UUID) (usecase.UserCleanup, error) {
	if owner <= 0 || id == uuid.Nil {
		return usecase.UserCleanup{}, D.ErrInvalid
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return usecase.UserCleanup{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	guards, err := workspaceguard.Lock(ctx, tx, owner)
	if err != nil {
		return usecase.UserCleanup{}, err
	}
	guard := guards[owner]
	cleanup := usecase.UserCleanup{OperationID: id, UserID: owner}
	if _, err = tx.Exec(ctx, `INSERT INTO workspace.user_cleanup(operation_id,user_id) VALUES($1,$2) ON CONFLICT DO NOTHING`, id, owner); err != nil {
		return cleanup, err
	}
	if err = tx.QueryRow(ctx, `SELECT done FROM workspace.user_cleanup WHERE operation_id=$1 AND user_id=$2`, id, owner).Scan(&cleanup.Done); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			err = D.ErrConflict
		}
		return cleanup, err
	}
	if cleanup.Done {
		return cleanup, tx.Commit(ctx)
	}
	if _, err = tx.Exec(ctx, `UPDATE workspace.user_guards SET deleting=true WHERE user_id=$1`, owner); err != nil {
		return cleanup, err
	}
	if guard.ActiveSessionID != nil {
		if err = stopForUserCleanup(ctx, tx, *guard.ActiveSessionID, owner); err != nil {
			return cleanup, err
		}
		return cleanup, tx.Commit(ctx)
	}
	// Проекции и журналы ссылаются на исполнения; runtime-строки удаляются прежде блокнота.
	for _, statement := range []string{
		`DELETE FROM workspace.runtime_projections WHERE session_id IN (SELECT id FROM workspace.runtime_sessions WHERE owner_id=$1)`,
		`DELETE FROM workspace.runtime_events WHERE session_id IN (SELECT id FROM workspace.runtime_sessions WHERE owner_id=$1)`,
		`DELETE FROM workspace.runtime_batches WHERE session_id IN (SELECT id FROM workspace.runtime_sessions WHERE owner_id=$1)`,
		`DELETE FROM workspace.runtime_executions WHERE session_id IN (SELECT id FROM workspace.runtime_sessions WHERE owner_id=$1)`,
		`DELETE FROM workspace.runtime_sessions WHERE owner_id=$1`,
		`DELETE FROM workspace.cell_comments WHERE author_id=$1`,
		`DELETE FROM workspace.notebook_readers WHERE user_id=$1`,
		`DELETE FROM workspace.notebooks WHERE owner_id=$1`,
		`UPDATE workspace.user_cleanup SET done=true WHERE user_id=$1`,
	} {
		if _, err = tx.Exec(ctx, statement, owner); err != nil {
			return cleanup, err
		}
	}
	cleanup.Done = true
	return cleanup, tx.Commit(ctx)
}

// stopForUserCleanup отменяет прежнее владение сессией и оставляет слот до фактической очистки.
func stopForUserCleanup(ctx context.Context, tx pgx.Tx, id uuid.UUID, owner int64) error {
	session, err := readSession(tx.QueryRow(ctx, `SELECT data FROM workspace.runtime_sessions WHERE id=$1 AND owner_id=$2 FOR UPDATE`, id, owner))
	if err != nil {
		return err
	}
	if session.State == D.Stopping || !session.State.Active() {
		return nil
	}
	session.State = D.Stopping
	session.Stage = "stopping"
	session.Reason = "requested"
	if correlation := observability.RequestID(ctx); observability.ValidRequestID(correlation) {
		session.CorrelationID = correlation
	}
	data, err := encodeSession(session)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE workspace.runtime_sessions SET state=$2,data=$3,next_check=now(),lease_until=now(),lease_version=lease_version+1 WHERE id=$1`, id, session.State, data)
	return err
}

// GetUserCleanup читает подтверждение без изменений.
func (s *Store) GetUserCleanup(ctx context.Context, id uuid.UUID) (usecase.UserCleanup, error) {
	result := usecase.UserCleanup{OperationID: id}
	err := s.db.QueryRow(ctx, `SELECT user_id,done FROM workspace.user_cleanup WHERE operation_id=$1`, id).Scan(&result.UserID, &result.Done)
	if errors.Is(err, pgx.ErrNoRows) {
		err = D.ErrNotFound
	}
	return result, err
}
