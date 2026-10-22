package postgres

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/notebook/domain"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/observability"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type failureRow struct{ err error }

func (r failureRow) Scan(...any) error { return r.err }

type failureDB struct {
	err  error
	rows pgx.Rows
	tx   pgx.Tx
}

func (db failureDB) QueryRow(context.Context, string, ...any) pgx.Row { //nolint:ireturn // required by DB
	return failureRow{err: db.err}
}
func (db failureDB) Query(context.Context, string, ...any) (pgx.Rows, error) { //nolint:ireturn // required by DB
	if db.rows != nil {
		return db.rows, nil
	}
	return nil, db.err
}
func (db failureDB) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, db.err
}
func (db failureDB) Begin(context.Context) (pgx.Tx, error) { //nolint:ireturn // required by DB
	if db.tx != nil {
		return db.tx, nil
	}
	return nil, db.err
}

type failedRows struct {
	pgx.Rows
	err error
}

func (r failedRows) Next() bool { return false }
func (r failedRows) Err() error { return r.err }
func (r failedRows) Close()     {}

type successRow struct{}

func (successRow) Scan(dest ...any) error {
	*dest[0].(*int64) = 1
	*dest[1].(*int64) = 2
	*dest[2].(*string) = "SECRET-name"
	*dest[3].(*string) = "SECRET-key"
	*dest[4].(*int) = 1
	*dest[5].(*time.Time) = time.Now()
	*dest[6].(*time.Time) = time.Now()
	return nil
}

type failedCommitTx struct {
	pgx.Tx
	err error
}

func (tx failedCommitTx) QueryRow(context.Context, string, ...any) pgx.Row { //nolint:ireturn // required by pgx.Tx
	return successRow{}
}
func (tx failedCommitTx) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, nil
}
func (tx failedCommitTx) Commit(context.Context) error   { return tx.err }
func (tx failedCommitTx) Rollback(context.Context) error { return nil }

func TestGetLogsScanFailureWithoutErrorText(t *testing.T) {
	var out bytes.Buffer
	var level slog.LevelVar
	level.Set(slog.LevelDebug)
	old := slog.Default()
	slog.SetDefault(slog.New(observability.NewHandler(slog.NewJSONHandler(&out, &slog.HandlerOptions{Level: &level}))))
	t.Cleanup(func() { slog.SetDefault(old) })
	repo := NewNotebookRepository(failureDB{err: errors.New("SECRET-row")})
	_, err := repo.Get(observability.WithRequestID(context.Background(), "req-db"), 1, 2)
	if err == nil {
		t.Fatal("expected scan error")
	}
	log := out.String()
	if !strings.Contains(log, `"operation":"notebook.get"`) || !strings.Contains(log, `"outcome":"error"`) || !strings.Contains(log, `"request_id":"req-db"`) || strings.Contains(log, "SECRET") {
		t.Fatalf("unsafe log: %s", log)
	}
}

func TestNotebookRepositoryLogsFailedOperationsOnce(t *testing.T) {
	var out bytes.Buffer
	var level slog.LevelVar
	level.Set(slog.LevelDebug)
	old := slog.Default()
	slog.SetDefault(slog.New(observability.NewHandler(slog.NewJSONHandler(&out, &slog.HandlerOptions{Level: &level}))))
	t.Cleanup(func() { slog.SetDefault(old) })
	ctx := observability.WithRequestID(context.Background(), "req-db")
	repo := NewNotebookRepository(failureDB{err: errors.New("SECRET-db")})
	for _, tc := range []struct {
		operation string
		call      func() error
	}{
		{"notebook.create", func() error { _, err := repo.Create(ctx, domain.Notebook{Name: "SECRET-name"}); return err }},
		{"notebook.list", func() error { _, err := repo.List(ctx, 2); return err }},
		{"notebook.get", func() error { _, err := repo.Get(ctx, 1, 2); return err }},
		{"notebook.modify", func() error { return repo.Modify(ctx, 1, 2, func(domain.Notebook) (int, error) { return 2, nil }) }},
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

func TestNotebookListLogsCollectRowsError(t *testing.T) {
	var out bytes.Buffer
	var level slog.LevelVar
	level.Set(slog.LevelDebug)
	old := slog.Default()
	slog.SetDefault(slog.New(observability.NewHandler(slog.NewJSONHandler(&out, &slog.HandlerOptions{Level: &level}))))
	t.Cleanup(func() { slog.SetDefault(old) })
	source := errors.New("SECRET-rows")
	repo := NewNotebookRepository(failureDB{rows: failedRows{err: source}})
	_, err := repo.List(context.Background(), 2)
	if !errors.Is(err, source) || strings.Count(out.String(), `"operation":"notebook.list"`) != 1 || !strings.Contains(out.String(), `"outcome":"error"`) || strings.Contains(out.String(), "SECRET") {
		t.Fatalf("err=%v log=%s", err, out.String())
	}
}

func TestNotebookModifyLogsCommitError(t *testing.T) {
	var out bytes.Buffer
	var level slog.LevelVar
	level.Set(slog.LevelDebug)
	old := slog.Default()
	slog.SetDefault(slog.New(observability.NewHandler(slog.NewJSONHandler(&out, &slog.HandlerOptions{Level: &level}))))
	t.Cleanup(func() { slog.SetDefault(old) })
	source := errors.New("SECRET-commit")
	repo := NewNotebookRepository(failureDB{tx: failedCommitTx{err: source}})
	err := repo.Modify(context.Background(), 1, 2, func(domain.Notebook) (int, error) { return 2, nil })
	if !errors.Is(err, source) || strings.Count(out.String(), `"operation":"notebook.modify"`) != 1 || !strings.Contains(out.String(), `"outcome":"error"`) || strings.Contains(out.String(), "SECRET") {
		t.Fatalf("err=%v log=%s", err, out.String())
	}
}
