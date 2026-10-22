package postgres

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/observability"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/user/usecase"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type failureRow struct{ err error }

func (r failureRow) Scan(...any) error { return r.err }

type failureDB struct{ err error }

func (db failureDB) QueryRow(context.Context, string, ...any) pgx.Row { //nolint:ireturn // required by Querier
	return failureRow(db)
}
func (db failureDB) Query(context.Context, string, ...any) (pgx.Rows, error) { //nolint:ireturn // required by Querier
	return nil, db.err
}
func (db failureDB) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, db.err
}

func TestCreateUserLogsSQLStateWithoutSensitiveInputs(t *testing.T) {
	var out bytes.Buffer
	var level slog.LevelVar
	level.Set(slog.LevelDebug)
	old := slog.Default()
	slog.SetDefault(slog.New(observability.NewHandler(slog.NewJSONHandler(&out, &slog.HandlerOptions{Level: &level}))))
	t.Cleanup(func() { slog.SetDefault(old) })
	repo := NewUserRepository(failureDB{&pgconn.PgError{Code: "23505", Detail: "SECRET-detail"}})
	_, err := repo.CreateUser(observability.WithRequestID(context.Background(), "req-db"), "SECRET-login", "SECRET-hash")
	if !errors.Is(err, usecase.ErrLoginTaken) {
		t.Fatalf("error=%v", err)
	}
	log := out.String()
	if !strings.Contains(log, `"sqlstate":"23505"`) || !strings.Contains(log, `"operation":"user.create"`) || !strings.Contains(log, `"request_id":"req-db"`) || strings.Contains(log, "SECRET") {
		t.Fatalf("unsafe log: %s", log)
	}
}

func TestUserRepositoryLogsEachFailedOperationOnce(t *testing.T) {
	var out bytes.Buffer
	var level slog.LevelVar
	level.Set(slog.LevelDebug)
	old := slog.Default()
	slog.SetDefault(slog.New(observability.NewHandler(slog.NewJSONHandler(&out, &slog.HandlerOptions{Level: &level}))))
	t.Cleanup(func() { slog.SetDefault(old) })
	ctx := observability.WithRequestID(context.Background(), "req-db")
	repo := NewUserRepository(failureDB{errors.New("SECRET-db")})
	for _, tc := range []struct {
		operation string
		call      func() error
	}{
		{"user.create", func() error { _, err := repo.CreateUser(ctx, "SECRET-login", "SECRET-hash"); return err }},
		{"user.by_login", func() error { _, _, err := repo.UserByLogin(ctx, "SECRET-login"); return err }},
		{"user.by_id", func() error { _, err := repo.UserByID(ctx, 1); return err }},
		{"session.create", func() error { return repo.CreateSession(ctx, 1, "SECRET-hash", time.Now()) }},
		{"session.delete", func() error { _, _, err := repo.DeleteSession(ctx, "SECRET-hash"); return err }},
		{"session.by_token", func() error { _, _, err := repo.SessionByTokenHash(ctx, "SECRET-hash"); return err }},
	} {
		out.Reset()
		if err := tc.call(); err == nil {
			t.Fatalf("%s unexpectedly succeeded", tc.operation)
		}
		log := out.String()
		if strings.Count(log, `"operation":"`+tc.operation+`"`) != 1 || !strings.Contains(log, `"outcome":"error"`) || !strings.Contains(log, `"request_id":"req-db"`) || strings.Contains(log, "SECRET") {
			t.Fatalf("%s log: %s", tc.operation, log)
		}
	}
}
