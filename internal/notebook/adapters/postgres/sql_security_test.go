package postgres

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/notebook/domain"
	"github.com/google/go-cmp/cmp"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type capturedQuery struct {
	sql  string
	args []any
}

type notebookQueryCapture struct {
	DB
	queries []capturedQuery
}

func (db *notebookQueryCapture) QueryRow(_ context.Context, sql string, args ...any) pgx.Row { //nolint:ireturn // required by DB
	db.queries = append(db.queries, capturedQuery{sql, args})
	return captureRow{}
}

func (db *notebookQueryCapture) Query(_ context.Context, sql string, args ...any) (pgx.Rows, error) { //nolint:ireturn // required by DB
	db.queries = append(db.queries, capturedQuery{sql, args})
	return failedRows{}, nil
}

func (db *notebookQueryCapture) Begin(context.Context) (pgx.Tx, error) { //nolint:ireturn // required by DB
	return captureTx{db: db}, nil
}

type captureRow struct{}

func (captureRow) Scan(dest ...any) error {
	for _, d := range dest {
		switch v := d.(type) {
		case *int64:
			*v = 17
		case *int:
			*v = 1
		case *string:
			*v = "fixture"
		case *time.Time:
			*v = time.Date(2026, 10, 24, 0, 0, 0, 0, time.UTC)
		}
	}
	return nil
}

type captureTx struct {
	pgx.Tx
	db *notebookQueryCapture
}

func (tx captureTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row { //nolint:ireturn // required by pgx.Tx
	return tx.db.QueryRow(ctx, sql, args...)
}

func (tx captureTx) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	tx.db.queries = append(tx.db.queries, capturedQuery{sql, args})
	return pgconn.NewCommandTag("UPDATE 1"), nil
}

func (captureTx) Commit(context.Context) error   { return nil }
func (captureTx) Rollback(context.Context) error { return nil }

func TestNotebookRepositoryParameterizedQueries(t *testing.T) {
	ctx := context.Background()
	payload := "x'); DROP TABLE notebooks; --"
	for _, tc := range []struct {
		name string
		args [][]any
		call func(*NotebookRepository) error
	}{
		{"create", [][]any{{int64(23), payload, "notebooks/file.ipynb", 1}}, func(r *NotebookRepository) error {
			_, err := r.Create(ctx, domain.Notebook{OwnerID: 23, Name: payload, FileKey: "notebooks/file.ipynb", CellsCount: 1})
			return err
		}},
		{"list", [][]any{{int64(23)}}, func(r *NotebookRepository) error { _, err := r.List(ctx, 23); return err }},
		{"get", [][]any{{int64(17), int64(23)}}, func(r *NotebookRepository) error { _, err := r.Get(ctx, 17, 23); return err }},
		{"modify", [][]any{{int64(17), int64(23)}, {int64(17), 3}}, func(r *NotebookRepository) error {
			return r.Modify(ctx, 17, 23, func(domain.Notebook) (int, error) { return 3, nil })
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := &notebookQueryCapture{}
			if err := tc.call(NewNotebookRepository(db)); err != nil {
				t.Fatal(err)
			}
			if len(db.queries) != len(tc.args) {
				t.Fatalf("got %d queries, want %d", len(db.queries), len(tc.args))
			}
			for i, q := range db.queries {
				if diff := cmp.Diff(tc.args[i], q.args); diff != "" {
					t.Fatalf("query %d arguments (-want +got):\n%s", i, diff)
				}
				if strings.Contains(q.sql, payload) {
					t.Fatalf("name interpolated into SQL: %s", q.sql)
				}
				for j := range tc.args[i] {
					if !strings.Contains(q.sql, "$"+strconv.Itoa(j+1)) {
						t.Fatalf("argument %d has no placeholder: %s", j+1, q.sql)
					}
				}
				if (tc.name == "get" || tc.name == "modify") && i == 0 && !strings.Contains(q.sql, "id = $1 AND owner_id = $2") {
					t.Fatalf("owner restriction lost: %s", q.sql)
				}
				if tc.name == "list" && !strings.Contains(q.sql, "WHERE owner_id = $1") {
					t.Fatalf("list owner restriction lost: %s", q.sql)
				}
				if tc.name == "modify" && i == 0 && !strings.HasSuffix(q.sql, " FOR UPDATE") {
					t.Fatalf("row lock lost: %s", q.sql)
				}
			}
		})
	}
}
