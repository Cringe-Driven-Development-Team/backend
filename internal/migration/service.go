// Package migration применяет независимые service histories отдельной командой владельца.
package migration

import (
	"context"
	"errors"
	"io/fs"
	"strconv"
	"strings"

	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/migrations"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)

// Service ограничивает имя schema известными владельцами, без SQL из окружения.
type Service string

const (
	Identity     Service = "identity"
	Workspace    Service = "workspace"
	Environments Service = "environments"
)

// State сообщает текущую и ожидаемую версии без неявной миграции схемы.
type State struct {
	Version  int64 `json:"version"`
	Expected int64 `json:"expected"`
	Pending  bool  `json:"pending"`
}

// source проверяет Service до любых SQL-идентификаторов и вычисляет ожидаемую version.
func source(s Service) (fs.FS, int64, error) {
	files, err := migrations.ServiceSource(string(s))
	if err != nil {
		return nil, 0, err
	}
	names, err := fs.Glob(files, "*.sql")
	if err != nil {
		return nil, 0, err
	}
	var expected int64
	for _, name := range names {
		n, _, ok := strings.Cut(name, "_")
		if !ok {
			return nil, 0, errors.New("invalid migration filename")
		}
		version, e := strconv.ParseInt(n, 10, 64)
		if e != nil {
			return nil, 0, errors.New("invalid migration version")
		}
		expected = max(expected, version)
	}
	if expected == 0 {
		return nil, 0, errors.New("empty service migration source")
	}
	return files, expected, nil
}

// Up вызывается только migration CLI от роли владельца; API использует CheckCompatible.
func Up(ctx context.Context, pool *pgxpool.Pool, s Service) error {
	files, _, err := source(s)
	if err != nil {
		return err
	}
	if _, err = pool.Exec(ctx, "CREATE SCHEMA IF NOT EXISTS "+string(s)); err != nil {
		return err
	}
	db := stdlib.OpenDBFromPool(pool)
	defer db.Close()
	provider, err := goose.NewProvider(goose.DialectPostgres, db, files, goose.WithTableName(string(s)+".goose_db_version"))
	if err != nil {
		return err
	}
	_, err = provider.Up(ctx)
	return err
}

// Status читает history, не создавая schema/table при её отсутствии.
func Status(ctx context.Context, pool *pgxpool.Pool, s Service) (State, error) {
	_, expected, err := source(s)
	if err != nil {
		return State{}, err
	}
	state := State{Expected: expected}
	err = pool.QueryRow(ctx, "SELECT COALESCE(max(version_id),0) FROM "+string(s)+".goose_db_version WHERE is_applied").Scan(&state.Version)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && (pgErr.Code == "42P01" || pgErr.Code == "3F000") {
		err = nil
	}
	if err != nil {
		return State{}, errors.New("cannot read service migration history")
	}
	state.Pending = state.Version != expected
	return state, nil
}

// CheckCompatible отклоняет неподготовленную schema и неподходящую app-role без DDL.
func CheckCompatible(ctx context.Context, pool *pgxpool.Pool, s Service) error {
	state, err := Status(ctx, pool, s)
	if err != nil {
		return err
	}
	if state.Pending {
		return errors.New("incompatible service schema version")
	}
	return checkRole(ctx, pool, s)
}
