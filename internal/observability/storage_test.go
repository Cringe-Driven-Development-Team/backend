package observability

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestLogStorageTreatsMissingRowAsExpectedOutcome(t *testing.T) {
	var out bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(NewHandler(slog.NewJSONHandler(&out, &slog.HandlerOptions{Level: slog.LevelDebug}))))
	t.Cleanup(func() { slog.SetDefault(old) })

	LogStorage(WithRequestID(context.Background(), "req-missing"), "user.by_login", time.Now(), pgx.ErrNoRows)
	log := out.String()
	if !strings.Contains(log, `"outcome":"not_found"`) || !strings.Contains(log, `"error_code":"not_found"`) || strings.Contains(log, `"error_code":"internal_error"`) {
		t.Fatalf("missing row should not look like a database failure: %s", log)
	}
}

func TestLogStorageShowsSQLStateWithoutDetail(t *testing.T) {
	var out bytes.Buffer
	var level slog.LevelVar
	level.Set(slog.LevelDebug)
	old := slog.Default()
	slog.SetDefault(slog.New(NewHandler(slog.NewJSONHandler(&out, &slog.HandlerOptions{Level: &level}))))
	t.Cleanup(func() { slog.SetDefault(old) })
	ctx := WithRequestID(context.Background(), "req-db")
	LogStorage(ctx, "user.create", time.Now().Add(-time.Millisecond), &pgconn.PgError{Code: "23505", Detail: "SECRET-data"})
	log := out.String()
	if !strings.Contains(log, `"operation":"user.create"`) || !strings.Contains(log, `"outcome":"error"`) || !strings.Contains(log, `"sqlstate":"23505"`) || !strings.Contains(log, `"request_id":"req-db"`) || !strings.Contains(log, `"duration":`) || strings.Contains(log, "SECRET") {
		t.Fatalf("unsafe storage log: %s", log)
	}
	out.Reset()
	LogStorage(ctx, "user.get", time.Now(), errors.New("SECRET-unknown"))
	if strings.Contains(out.String(), "SECRET") || !strings.Contains(out.String(), `"error_code":"internal_error"`) {
		t.Fatalf("unsafe fallback: %s", out.String())
	}
}
