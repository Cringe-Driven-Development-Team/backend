package postgres

import (
	"context"
	"errors"

	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/observability"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/user/usecase"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Deletions хранит намерения Identity с отдельной версией lease.
type Deletions struct {
	db interface {
		Querier
		Begin(context.Context) (pgx.Tx, error)
	}
}

// NewDeletions подключает хранилище с ограниченной ролью Identity.
func NewDeletions(db interface {
	Querier
	Begin(context.Context) (pgx.Tx, error)
}) *Deletions {
	return &Deletions{db: db}
}

const deletionColumns = `operation_id,user_id,stage,error_code,avatar_key,correlation_id,version`

func readDeletion(row pgx.Row) (usecase.Deletion, error) {
	var d usecase.Deletion
	err := row.Scan(&d.OperationID, &d.UserID, &d.Stage, &d.ErrorCode, &d.AvatarKey, &d.CorrelationID, &d.Version)
	if errors.Is(err, pgx.ErrNoRows) {
		err = usecase.ErrUserNotFound
	}
	return d, err
}

// Begin сериализует смену состояния с login, TOTP и обновлением аватара.
func (r *Deletions) Begin(ctx context.Context, uid int64, id uuid.UUID) (usecase.Deletion, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return usecase.Deletion{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	selectExisting := func() (usecase.Deletion, error) {
		d, readErr := readDeletion(tx.QueryRow(ctx, `SELECT `+deletionColumns+` FROM identity.user_deletions WHERE operation_id=$1 OR user_id=$2 ORDER BY operation_id LIMIT 1`, id, uid))
		if readErr == nil && (d.OperationID != id || d.UserID != uid) {
			return usecase.Deletion{}, usecase.ErrDeletionConflict
		}
		return d, readErr
	}
	d, err := selectExisting()
	if !errors.Is(err, usecase.ErrUserNotFound) {
		return d, err
	}
	var avatar string
	if err = tx.QueryRow(ctx, `SELECT COALESCE(avatar_key,'') FROM identity.users WHERE id=$1 FOR UPDATE`, uid).Scan(&avatar); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return selectExisting()
		}
		return d, err
	}
	correlation := observability.RequestID(ctx)
	if !observability.ValidRequestID(correlation) {
		correlation = uuid.NewString()
	}
	tag, err := tx.Exec(ctx, `INSERT INTO identity.user_deletions(operation_id,user_id,stage,avatar_key,correlation_id)
		VALUES($1,$2,'revoking',$3,$4) ON CONFLICT DO NOTHING`, id, uid, avatar, correlation)
	if err != nil {
		return d, err
	}
	if tag.RowsAffected() == 0 {
		return selectExisting()
	}
	if _, err = tx.Exec(ctx, `UPDATE identity.users SET deleting=true WHERE id=$1`, uid); err != nil {
		return d, err
	}
	d, err = selectExisting()
	if err != nil {
		return d, err
	}
	return d, tx.Commit(ctx)
}

// Get читает публично безопасное состояние, хранимое до и после удаления.
func (r *Deletions) Get(ctx context.Context, id uuid.UUID) (usecase.Deletion, error) {
	return readDeletion(r.db.QueryRow(ctx, `SELECT `+deletionColumns+` FROM identity.user_deletions WHERE operation_id=$1`, id))
}

// Claim выдаёт одному worker следующее действие с коротким lease.
func (r *Deletions) Claim(ctx context.Context) (usecase.Deletion, error) {
	return readDeletion(r.db.QueryRow(ctx, `UPDATE identity.user_deletions SET version=version+1,lease_until=now()+interval '30 seconds'
		WHERE operation_id=(SELECT operation_id FROM identity.user_deletions WHERE stage<>'done'
			AND lease_until<now() AND next_run_at<=now() ORDER BY next_run_at FOR UPDATE SKIP LOCKED LIMIT 1)
		RETURNING `+deletionColumns))
}

// Advance сохраняет результат и в последнем шаге удаляет Identity-профиль под тем же lease.
func (r *Deletions) Advance(ctx context.Context, d usecase.Deletion, next, code string) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	tag, err := tx.Exec(ctx, `UPDATE identity.user_deletions SET stage=$3,error_code=$4,
		avatar_key=CASE WHEN $3='done' THEN '' ELSE avatar_key END,lease_until='-infinity',
		next_run_at=now()+interval '1 second'
		WHERE operation_id=$1 AND version=$2 AND lease_until>now()`, d.OperationID, d.Version, next, code)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return usecase.ErrDeletionConflict
	}
	if next == "done" && d.Stage == "avatar" {
		if _, err = tx.Exec(ctx, `DELETE FROM identity.users WHERE id=$1 AND deleting`, d.UserID); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// ActiveAccount отвергает новый или ранее выданный доступ к удаляемому аккаунту.
func (r *Deletions) ActiveAccount(ctx context.Context, id int64) error {
	var active bool
	err := r.db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM identity.users WHERE id=$1 AND NOT deleting)`, id).Scan(&active)
	if err != nil {
		return err
	}
	if !active {
		return usecase.ErrInvalidSession
	}
	return nil
}
