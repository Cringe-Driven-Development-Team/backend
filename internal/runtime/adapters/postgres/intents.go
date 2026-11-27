package postgres

import (
	"bytes"
	"context"
	"errors"

	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/observability"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/runtime/domain"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/runtime/usecase"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

var _ usecase.IntentRepository = (*Repository)(nil)

// ApplyIntent сериализует provisioning, delete-before-create и повтор запроса под одним DB lock.
func (r *Repository) ApplyIntent(ctx context.Context, intent usecase.Intent, initial domain.Environment, maxActive int, reason string) (domain.Environment, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return domain.Environment{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err = tx.Exec(ctx, capacityLockSQL); err != nil {
		return domain.Environment{}, err
	}
	prior, err := readIntent(ctx, tx, intent.OperationID)
	if err == nil {
		if prior.SessionID != intent.SessionID || prior.Kind != intent.Kind || !bytes.Equal(prior.Fingerprint, intent.Fingerprint) {
			return domain.Environment{}, usecase.ErrConflict
		}
		// Повтор может читать актуальное состояние после закрытия старой lease.
		return scanEnv(tx.QueryRow(ctx, selectEnvSQL, intent.SessionID))
	}
	if !errors.Is(err, usecase.ErrNotFound) {
		return domain.Environment{}, err
	}
	var fence int64
	err = tx.QueryRow(ctx, `SELECT workspace_fence FROM environments.runtime_environments WHERE session_id=$1 FOR UPDATE`, intent.SessionID).Scan(&fence)
	exists := err == nil
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return domain.Environment{}, err
	}
	if fence > intent.Fence {
		return domain.Environment{}, usecase.ErrStale
	}
	switch intent.Kind {
	case usecase.JobCreate:
		if err = applyCreate(ctx, tx, initial, maxActive); err != nil {
			return domain.Environment{}, err
		}
	case usecase.JobDelete:
		if err = applyDelete(ctx, tx, initial, reason, exists); err != nil {
			return domain.Environment{}, err
		}
	default:
		return domain.Environment{}, usecase.ErrInvalidSession
	}
	if _, err = tx.Exec(ctx, `UPDATE environments.runtime_environments SET workspace_fence=$2, agent_fence=agent_fence+CASE WHEN $3 THEN 1 ELSE 0 END WHERE session_id=$1`, intent.SessionID, intent.Fence, intent.Kind == usecase.JobDelete); err != nil {
		return domain.Environment{}, err
	}
	id := observability.RequestID(ctx)
	if !observability.ValidRequestID(id) {
		id = uuid.NewString()
	}
	_, err = tx.Exec(ctx, `INSERT INTO environments.runtime_intents(operation_id,session_id,kind,fingerprint,correlation_id)
		VALUES($1,$2,$3,$4,$5)`, intent.OperationID, intent.SessionID, string(intent.Kind), intent.Fingerprint, id)
	if err != nil {
		return domain.Environment{}, err
	}
	_, err = tx.Exec(ctx, `UPDATE environments.runtime_jobs SET correlation_id=$3
		WHERE session_id=$1 AND kind=$2 AND status IN ('queued','running') AND ($2='delete' OR correlation_id IS NULL)`,
		intent.SessionID, string(intent.Kind), id)
	if err != nil {
		return domain.Environment{}, err
	}
	result, err := scanEnv(tx.QueryRow(ctx, selectEnvSQL, intent.SessionID))
	if err != nil {
		return domain.Environment{}, err
	}
	return result, commit(ctx, tx)
}

// applyCreate оставляет любое уже записанное намерение удаления в силе.
func applyCreate(ctx context.Context, tx pgx.Tx, initial domain.Environment, maxActive int) error {
	env, created, err := createWithJob(ctx, tx, initial, maxActive)
	if err != nil || created || env.DesiredState == domain.StateDeleted {
		return err
	}
	if env.Language != initial.Language || env.Profile != initial.Profile {
		return usecase.ErrConflict
	}
	return nil
}

// applyDelete ставит реальную инвентаризацию облака и на отсутствующую строку.
func applyDelete(ctx context.Context, tx pgx.Tx, initial domain.Environment, reason string, exists bool) error {
	if !exists {
		err := tx.QueryRow(ctx, insertEnvSQL, initial.SessionID, initial.OperationID, initial.Language,
			initial.Profile, string(initial.State), string(initial.DesiredState), initial.AgentVersion).
			Scan(&initial.Version, &initial.CreatedAt, &initial.UpdatedAt)
		if err != nil {
			return err
		}
	}
	return markForDelete(ctx, tx, initial.SessionID, reason, true)
}

type intentReader interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func readIntent(ctx context.Context, db intentReader, id uuid.UUID) (usecase.Intent, error) {
	i := usecase.Intent{OperationID: id}
	var kind string
	err := db.QueryRow(ctx, `SELECT session_id,kind,fingerprint FROM environments.runtime_intents WHERE operation_id=$1`, id).
		Scan(&i.SessionID, &kind, &i.Fingerprint)
	if errors.Is(err, pgx.ErrNoRows) {
		return usecase.Intent{}, usecase.ErrNotFound
	}
	i.Kind = usecase.JobKind(kind)
	return i, err
}

// ReadIntent читает принятую операцию по стабильному ID, не повторяя её.
func (r *Repository) ReadIntent(ctx context.Context, id uuid.UUID) (usecase.Intent, domain.Environment, error) {
	intent, err := readIntent(ctx, r.db, id)
	if err != nil {
		return usecase.Intent{}, domain.Environment{}, err
	}
	env, err := r.Get(ctx, intent.SessionID)
	return intent, env, err
}
