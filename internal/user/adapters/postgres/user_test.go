package postgres

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/observability"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/user/usecase"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestCreateUserLogsSQLStateWithoutSensitiveInputs(t *testing.T) {
	var out bytes.Buffer
	var level slog.LevelVar
	level.Set(slog.LevelDebug)
	old := slog.Default()
	slog.SetDefault(slog.New(observability.NewHandler(slog.NewJSONHandler(&out, &slog.HandlerOptions{Level: &level}))))
	t.Cleanup(func() { slog.SetDefault(old) })
	db := newPool(t)
	db.ExpectQuery(createUserSQL).WithArgs("SECRET-login", "SECRET-hash").WillReturnError(&pgconn.PgError{Code: "23505", Detail: "SECRET-detail"})
	repo := NewUserRepository(db)
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
	db := newPool(t)
	failure := errors.New("SECRET-db")
	db.ExpectQuery(createUserSQL).WithArgs("SECRET-login", "SECRET-hash").WillReturnError(failure)
	db.ExpectQuery(byLoginSQL).WithArgs("SECRET-login").WillReturnError(failure)
	db.ExpectQuery(byIDSQL).WithArgs(int64(1)).WillReturnError(failure)
	repo := NewUserRepository(db)
	for _, tc := range []struct {
		operation string
		call      func() error
	}{
		{"user.create", func() error { _, err := repo.CreateUser(ctx, "SECRET-login", "SECRET-hash"); return err }},
		{"user.by_login", func() error { _, _, err := repo.UserByLogin(ctx, "SECRET-login"); return err }},
		{"user.by_id", func() error { _, err := repo.UserByID(ctx, 1); return err }},
	} {
		out.Reset()
		if err := tc.call(); !errors.Is(err, failure) {
			t.Fatalf("%s unexpectedly succeeded", tc.operation)
		}
		log := out.String()
		if strings.Count(log, `"operation":"`+tc.operation+`"`) != 1 || !strings.Contains(log, `"outcome":"error"`) || !strings.Contains(log, `"request_id":"req-db"`) || strings.Contains(log, "SECRET") {
			t.Fatalf("%s log: %s", tc.operation, log)
		}
	}
}
