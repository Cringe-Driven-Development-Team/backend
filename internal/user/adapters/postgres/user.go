// Package postgres хранит пользователей в PostgreSQL.
package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/observability"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/user/domain"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/user/usecase"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Querier описывает то общее, что есть у *pgxpool.Pool и pgx.Tx.
type Querier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

const uniqueViolation = "23505"

type UserRepository struct {
	db Querier
}

func NewUserRepository(db Querier) *UserRepository {
	return &UserRepository{db: db}
}

func (r *UserRepository) CreateUser(ctx context.Context, login, passwordHash string) (domain.User, error) {
	started := time.Now()
	u := domain.User{Login: login}
	err := r.db.QueryRow(ctx,
		`INSERT INTO users (login, password_hash) VALUES ($1, $2) RETURNING id`,
		login, passwordHash,
	).Scan(&u.ID)
	observability.LogStorage(ctx, "user.create", started, err)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation {
		return domain.User{}, usecase.ErrLoginTaken
	}
	if err != nil {
		return domain.User{}, fmt.Errorf("insert user: %w", err)
	}
	return u, nil
}

func (r *UserRepository) UserByLogin(ctx context.Context, login string) (u domain.User, passwordHash string, err error) {
	started := time.Now()
	err = r.db.QueryRow(ctx,
		`SELECT id, login, password_hash FROM users WHERE lower(login) = lower($1)`,
		login,
	).Scan(&u.ID, &u.Login, &passwordHash)
	observability.LogStorage(ctx, "user.by_login", started, err)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.User{}, "", usecase.ErrUserNotFound
	}
	if err != nil {
		return domain.User{}, "", fmt.Errorf("select user by login: %w", err)
	}
	return u, passwordHash, nil
}

func (r *UserRepository) UserByID(ctx context.Context, id int64) (domain.User, error) {
	started := time.Now()
	u := domain.User{ID: id}
	err := r.db.QueryRow(ctx, `SELECT login FROM users WHERE id = $1`, id).Scan(&u.Login)
	observability.LogStorage(ctx, "user.by_id", started, err)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.User{}, usecase.ErrUserNotFound
	}
	if err != nil {
		return domain.User{}, fmt.Errorf("select user by id: %w", err)
	}
	return u, nil
}
