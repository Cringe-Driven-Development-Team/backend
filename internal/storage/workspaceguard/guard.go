// Package workspaceguard сериализует локальные пользовательские барьеры Workspace без обращения к Identity.
package workspaceguard

import (
	"context"
	"errors"
	"slices"

	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/authn"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Guard содержит tombstone и единственный runtime-слот, без копии профиля пользователя.
type Guard struct {
	UserID          int64
	Deleting        bool
	ActiveSessionID *uuid.UUID
}

// Lock создаёт недостающие guards и блокирует их в возрастающем порядке до notebook/session locks.
func Lock(ctx context.Context, tx pgx.Tx, ids ...int64) (map[int64]Guard, error) {
	ordered := slices.Clone(ids)
	slices.Sort(ordered)
	ordered = slices.Compact(ordered)
	result := make(map[int64]Guard, len(ordered))
	for _, id := range ordered {
		if id <= 0 {
			return nil, errors.New("invalid Workspace user guard")
		}
		if _, err := tx.Exec(ctx, `INSERT INTO workspace.user_guards(user_id) VALUES($1) ON CONFLICT(user_id) DO NOTHING`, id); err != nil {
			return nil, err
		}
		g := Guard{UserID: id}
		if err := tx.QueryRow(ctx, `SELECT deleting,active_session_id FROM workspace.user_guards WHERE user_id=$1 FOR UPDATE`, id).Scan(&g.Deleting, &g.ActiveSessionID); err != nil {
			return nil, err
		}
		result[id] = g
	}
	return result, nil
}

// Actor проверяет локальный tombstone после ранее успешной сетевой авторизации.
func Actor(ctx context.Context, tx pgx.Tx, id int64) (Guard, error) {
	guards, err := Lock(ctx, tx, id)
	if err != nil {
		return Guard{}, err
	}
	g := guards[id]
	if g.Deleting {
		return Guard{}, authn.ErrInvalidSession
	}
	return g, nil
}
