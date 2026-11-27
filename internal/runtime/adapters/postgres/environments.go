// Package postgres хранит окружения и задания рантайма в PostgreSQL.
package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/runtime/domain"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/runtime/usecase"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// DB описывает нужное от *pgxpool.Pool: запросы и транзакции.
type DB interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Begin(ctx context.Context) (pgx.Tx, error)
}

// Repository реализует usecase.EnvironmentRepository, usecase.PoolRepository и usecase.JobQueue.
type Repository struct{ db DB }

var (
	_ usecase.EnvironmentRepository = (*Repository)(nil)
	_ usecase.JobQueue              = (*Repository)(nil)
	_ usecase.PoolRepository        = (*Repository)(nil)
)

// NewRepository принимает пул соединений.
func NewRepository(db DB) *Repository { return &Repository{db: db} }

const (
	capacityLockSQL = `SELECT pg_advisory_xact_lock(hashtext('runtime_capacity'))`
	envColumns      = `session_id, operation_id, language, profile, state, desired_state,
		COALESCE(server_id, ''), COALESCE(volume_id, ''), COALESCE(port_id, ''), COALESCE(private_ip, ''),
		COALESCE(endpoint, ''), agent_version, COALESCE(last_error, ''), COALESCE(cleanup_result, ''),
		COALESCE(pool_id, '00000000-0000-0000-0000-000000000000'), version, created_at, updated_at`
	selectEnvSQL   = `SELECT ` + envColumns + ` FROM environments.runtime_environments WHERE session_id = $1`
	countActiveSQL = `SELECT count(*) FROM environments.runtime_environments WHERE state NOT IN ('DELETED','FAILED')`
	insertEnvSQL   = `INSERT INTO environments.runtime_environments (session_id, operation_id, language, profile, state, desired_state, agent_version)
		VALUES ($1, $2, $3, $4, $5, $6, $7) ON CONFLICT (session_id) DO NOTHING
		RETURNING version, created_at, updated_at`
	insertJobSQL  = `INSERT INTO environments.runtime_jobs (session_id, kind) VALUES ($1, $2)`
	lockEnvSQL    = `SELECT state FROM environments.runtime_environments WHERE session_id = $1 FOR UPDATE`
	markDeleteSQL = `UPDATE environments.runtime_environments SET desired_state = 'DELETED', delete_reason = $2,
		version = version + 1, updated_at = now()
		WHERE session_id = $1`
	convertJobSQL = `UPDATE environments.runtime_jobs SET kind = 'delete' WHERE session_id = $1 AND status IN ('queued','running')`
	saveSQL       = `UPDATE environments.runtime_environments e SET state = $3,
		desired_state = CASE WHEN e.desired_state = 'DELETED' THEN 'DELETED' ELSE $4 END, server_id = NULLIF($5, ''),
		volume_id = NULLIF($6, ''), port_id = NULLIF($7, ''), private_ip = NULLIF($8, ''), endpoint = NULLIF($9, ''),
		agent_version = $10, last_error = NULLIF($11, ''), cleanup_result = NULLIF($12, ''),
		pool_id = NULLIF($14, '00000000-0000-0000-0000-000000000000'::uuid),
		version = e.version + 1, updated_at = now()
		WHERE e.session_id = $1 AND e.version = $2 AND EXISTS (
			SELECT 1 FROM environments.runtime_jobs j WHERE j.session_id = e.session_id AND j.status = 'running' AND j.lease_owner = $13)
		RETURNING e.version, e.updated_at`
	saveSecretsSQL = `UPDATE environments.runtime_environments e SET token_sealed = $2, host_private_key_sealed = $3, host_public_key = $4,
		updated_at = now()
		WHERE e.session_id = $1 AND EXISTS (
			SELECT 1 FROM environments.runtime_jobs j WHERE j.session_id = e.session_id AND j.status = 'running' AND j.lease_owner = $5)`
	loadSecretsSQL = `SELECT token_sealed, host_private_key_sealed, COALESCE(host_public_key, '')
		FROM environments.runtime_environments WHERE session_id = $1`
	knownSQL       = `SELECT session_id, state FROM environments.runtime_environments WHERE session_id = ANY($1)`
	listByStateSQL = `SELECT ` + envColumns + ` FROM environments.runtime_environments WHERE state = $1 ORDER BY updated_at`
)

// scanEnv восстанавливает окружение из строки PostgreSQL.
func scanEnv(row pgx.Row) (domain.Environment, error) {
	var e domain.Environment
	var state, desired string
	err := row.Scan(&e.SessionID, &e.OperationID, &e.Language, &e.Profile, &state, &desired,
		&e.Resources.ServerID, &e.Resources.VolumeID, &e.Resources.PortID, &e.Resources.PrivateIP,
		&e.Endpoint, &e.AgentVersion, &e.LastError, &e.CleanupResult, &e.PoolID, &e.Version, &e.CreatedAt, &e.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Environment{}, usecase.ErrNotFound
	}
	if err != nil {
		return domain.Environment{}, fmt.Errorf("select environment: %w", err)
	}
	e.State, e.DesiredState = domain.State(state), domain.State(desired)
	return e, nil
}

// CreateWithJob создаёт окружение и задание create в одной транзакции под advisory-lock лимита.
func (r *Repository) CreateWithJob(ctx context.Context, env domain.Environment, maxActive int) (domain.Environment, bool, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return domain.Environment{}, false, fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err = tx.Exec(ctx, capacityLockSQL); err != nil {
		return domain.Environment{}, false, fmt.Errorf("capacity lock: %w", err)
	}
	result, created, err := createWithJob(ctx, tx, env, maxActive)
	if err != nil || !created {
		return result, created, err
	}
	return result, true, commit(ctx, tx)
}

// createWithJob сохраняет запись и задание в транзакции вызывающего.
func createWithJob(ctx context.Context, tx pgx.Tx, env domain.Environment, maxActive int) (domain.Environment, bool, error) {
	existing, err := scanEnv(tx.QueryRow(ctx, selectEnvSQL, env.SessionID))
	switch {
	case err == nil:
		return existing, false, nil
	case !errors.Is(err, usecase.ErrNotFound):
		return domain.Environment{}, false, err
	}
	var active int
	if err = tx.QueryRow(ctx, countActiveSQL).Scan(&active); err != nil {
		return domain.Environment{}, false, fmt.Errorf("count active: %w", err)
	}
	if active >= maxActive {
		return domain.Environment{}, false, usecase.ErrCapacity
	}
	err = tx.QueryRow(ctx, insertEnvSQL, env.SessionID, env.OperationID, env.Language, env.Profile,
		string(env.State), string(env.DesiredState), env.AgentVersion).Scan(&env.Version, &env.CreatedAt, &env.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		existing, err = scanEnv(tx.QueryRow(ctx, selectEnvSQL, env.SessionID))
		return existing, false, err
	}
	if err != nil {
		return domain.Environment{}, false, fmt.Errorf("insert environment: %w", err)
	}
	if _, err = tx.Exec(ctx, insertJobSQL, env.SessionID, string(usecase.JobCreate)); err != nil {
		return domain.Environment{}, false, fmt.Errorf("insert job: %w", err)
	}
	return env, true, nil
}

// Get возвращает окружение или ErrNotFound.
func (r *Repository) Get(ctx context.Context, id uuid.UUID) (domain.Environment, error) {
	return scanEnv(r.db.QueryRow(ctx, selectEnvSQL, id))
}

// MarkForDelete переводит окружение к удалению и гарантирует задание delete.
func (r *Repository) MarkForDelete(ctx context.Context, id uuid.UUID, reason string) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err = markForDelete(ctx, tx, id, reason, false); err != nil {
		return err
	}
	return commit(ctx, tx)
}

// markForDelete может запланировать подтверждение очистки также для старой терминальной строки.
func markForDelete(ctx context.Context, tx pgx.Tx, id uuid.UUID, reason string, verifyTerminal bool) error {
	var state string
	if err := tx.QueryRow(ctx, lockEnvSQL, id).Scan(&state); errors.Is(err, pgx.ErrNoRows) {
		return usecase.ErrNotFound
	} else if err != nil {
		return fmt.Errorf("lock environment: %w", err)
	}
	if domain.State(state).Finished() {
		if !verifyTerminal {
			return nil
		}
		env, err := scanEnv(tx.QueryRow(ctx, selectEnvSQL, id))
		if err != nil {
			return err
		}
		if env.CleanupConfirmed() {
			return nil
		}
		if _, err = tx.Exec(ctx, `UPDATE environments.runtime_environments SET state='CLEANUP_PENDING' WHERE session_id=$1`, id); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(ctx, markDeleteSQL, id, reason); err != nil {
		return fmt.Errorf("mark delete: %w", err)
	}
	tag, err := tx.Exec(ctx, convertJobSQL, id)
	if err != nil {
		return fmt.Errorf("convert job: %w", err)
	}
	if tag.RowsAffected() == 0 {
		if _, err = tx.Exec(ctx, insertJobSQL, id, string(usecase.JobDelete)); err != nil {
			return fmt.Errorf("insert delete job: %w", err)
		}
	}
	return nil
}

// commit завершает транзакцию provisioning и сохраняет исходную ошибку.
func commit(ctx context.Context, tx pgx.Tx) error {
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

// Save обновляет окружение при совпадении версии и владении заданием.
func (r *Repository) Save(ctx context.Context, env domain.Environment, owner string) (domain.Environment, error) {
	err := r.db.QueryRow(ctx, saveSQL, env.SessionID, env.Version, string(env.State), string(env.DesiredState),
		env.Resources.ServerID, env.Resources.VolumeID, env.Resources.PortID, env.Resources.PrivateIP,
		env.Endpoint, env.AgentVersion, env.LastError, env.CleanupResult, owner, env.PoolID,
	).Scan(&env.Version, &env.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Environment{}, usecase.ErrStale
	}
	if err != nil {
		return domain.Environment{}, fmt.Errorf("save environment: %w", err)
	}
	return env, nil
}

// SaveSecrets сохраняет зашифрованные секреты владельцем задания.
func (r *Repository) SaveSecrets(ctx context.Context, id uuid.UUID, s usecase.SealedSecrets, owner string) error {
	tag, err := r.db.Exec(ctx, saveSecretsSQL, id, s.Token, s.HostPrivateKey, s.HostPublicKey, owner)
	if err != nil {
		return fmt.Errorf("save secrets: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return usecase.ErrStale
	}
	return nil
}

// LoadSecrets читает зашифрованные секреты.
func (r *Repository) LoadSecrets(ctx context.Context, id uuid.UUID) (usecase.SealedSecrets, error) {
	var s usecase.SealedSecrets
	err := r.db.QueryRow(ctx, loadSecretsSQL, id).Scan(&s.Token, &s.HostPrivateKey, &s.HostPublicKey)
	if errors.Is(err, pgx.ErrNoRows) {
		return usecase.SealedSecrets{}, usecase.ErrNotFound
	}
	if err != nil {
		return usecase.SealedSecrets{}, fmt.Errorf("load secrets: %w", err)
	}
	return s, nil
}

// KnownSessions возвращает состояния известных сессий из ids.
func (r *Repository) KnownSessions(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]domain.State, error) {
	rows, err := r.db.Query(ctx, knownSQL, ids)
	if err != nil {
		return nil, fmt.Errorf("select known sessions: %w", err)
	}
	defer rows.Close()
	out := make(map[uuid.UUID]domain.State, len(ids))
	for rows.Next() {
		var id uuid.UUID
		var st string
		if err = rows.Scan(&id, &st); err != nil {
			return nil, fmt.Errorf("scan known session: %w", err)
		}
		out[id] = domain.State(st)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate known sessions: %w", err)
	}
	return out, nil
}

// ListByState возвращает окружения в состоянии state, старые изменения первыми.
func (r *Repository) ListByState(ctx context.Context, state domain.State) ([]domain.Environment, error) {
	rows, err := r.db.Query(ctx, listByStateSQL, string(state))
	if err != nil {
		return nil, fmt.Errorf("select environments by state: %w", err)
	}
	defer rows.Close()
	var out []domain.Environment
	for rows.Next() {
		e, serr := scanEnv(rows)
		if serr != nil {
			return nil, serr
		}
		out = append(out, e)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate environments by state: %w", err)
	}
	return out, nil
}
