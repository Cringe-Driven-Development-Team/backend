// Package pgtest создаёт disposable PostgreSQL базы с текущими service schemas для integration fixtures.
package pgtest

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/migration"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// NewDatabase использует только явно заданный test DSN и удаляет собственную DB в cleanup.
// configure меняет параметры новых соединений, не выдавая приложение migration credentials.
func NewDatabase(t testing.TB, env string, configure ...func(*pgxpool.Config)) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv(env)
	if dsn == "" {
		t.Skip("set disposable " + env)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	root, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	name := fmt.Sprintf("fixture_%d", time.Now().UnixNano())
	q := pgx.Identifier{name}.Sanitize()
	if _, err = root.Exec(ctx, "CREATE DATABASE "+q); err != nil {
		root.Close()
		t.Fatal(err)
	}
	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	parsed.Path = "/" + name
	cfg, err := pgxpool.ParseConfig(parsed.String())
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = "workspace,identity,environments"
	cfg.MaxConns = 8
	for _, apply := range configure {
		apply(cfg)
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		pool.Close()
		cleanup, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		_, _ = root.Exec(cleanup, "DROP DATABASE "+q+" WITH (FORCE)")
		root.Close()
	})
	for _, service := range []migration.Service{migration.Identity, migration.Workspace, migration.Environments} {
		if err = migration.Up(ctx, pool, service); err != nil {
			t.Fatal(err)
		}
	}
	return pool
}
