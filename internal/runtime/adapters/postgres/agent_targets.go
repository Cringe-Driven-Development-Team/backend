package postgres

import (
	"context"
	"errors"
	"math"

	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/runtime/domain"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/runtime/usecase"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// ResolveAgent проверяет назначение и монотонность под локальной блокировкой Environments.
func (r *Repository) ResolveAgent(ctx context.Context, id uuid.UUID, workspaceFence uint64) (usecase.AgentTarget, error) {
	if id == uuid.Nil || workspaceFence > math.MaxInt64 {
		return usecase.AgentTarget{}, usecase.ErrInvalidSession
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return usecase.AgentTarget{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	target := usecase.AgentTarget{SessionID: id}
	var applied, agentFence int64
	var state, desired string
	err = tx.QueryRow(ctx, `SELECT COALESCE(private_ip,''),COALESCE(pool_id,session_id), state, desired_state,
		workspace_fence,agent_fence FROM environments.runtime_environments WHERE session_id=$1 FOR UPDATE`, id).
		Scan(&target.PrivateIP, &target.HostID, &state, &desired, &applied, &agentFence)
	if errors.Is(err, pgx.ErrNoRows) {
		return usecase.AgentTarget{}, usecase.ErrNotFound
	}
	if err != nil {
		return usecase.AgentTarget{}, err
	}
	if desired == string(domain.StateDeleted) || state != string(domain.StateReady) || target.PrivateIP == "" {
		return usecase.AgentTarget{}, usecase.ErrNotFound
	}
	epoch := int64(workspaceFence)
	if epoch != 0 && epoch < applied {
		return usecase.AgentTarget{}, usecase.ErrStale
	}
	// Интерактивный запрос без lease использует уже зарегистрированную версию.
	if epoch > applied {
		if err = tx.QueryRow(ctx, `UPDATE environments.runtime_environments SET
			workspace_fence=$2,agent_fence=agent_fence+1 WHERE session_id=$1 RETURNING agent_fence`, id, epoch).Scan(&agentFence); err != nil {
			return usecase.AgentTarget{}, err
		}
	}
	target.Fence = uint64(agentFence)
	return target, commit(ctx, tx)
}

// AgentJobFence возвращает версию текущего задания provisioning или cleanup без полномочий Workspace.
func (r *Repository) AgentJobFence(ctx context.Context, id uuid.UUID) (uint64, error) {
	var fence int64
	err := r.db.QueryRow(ctx, `SELECT agent_fence FROM environments.runtime_environments WHERE session_id=$1`, id).Scan(&fence)
	return uint64(fence), err
}
