package postgres

import (
	"os"
	"strings"
	"testing"

	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/notebook/usecase"
)

func TestCellCommentsMigration(t *testing.T) {
	repo, pool := testRepository(t)
	ctx := t.Context()
	uc := usecase.NewNotebookUsecase(repo, usecase.NoActiveSessions{}, nil)
	n, err := uc.Create(ctx, 1, "before migration")
	if err != nil {
		t.Fatal(err)
	}
	var count int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM cell_comments`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("empty table: %d %v", count, err)
	}
	sql, err := os.ReadFile("../../../../migrations/20261122120000_add_cell_comments.sql")
	if err != nil {
		t.Fatal(err)
	}
	up, down, ok := strings.Cut(string(sql), "-- +goose Down")
	if !ok {
		t.Fatal("missing Down")
	}
	if _, err = pool.Exec(ctx, down); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, up); err != nil {
		t.Fatal(err)
	}
	got, err := repo.Get(ctx, n.ID, 1)
	if err != nil || got.Revision != n.Revision || !got.UpdatedAt.Equal(n.UpdatedAt) || got.SharingMode != n.SharingMode {
		t.Fatalf("migration changed notebook: %+v %v", got, err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO cell_comments(notebook_id,cell_id,author_id,text,created_at,updated_at) VALUES($1,$2,1,'ok',now(),now())`, n.ID, n.Cells[0].ID); err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`INSERT INTO cell_comments(notebook_id,cell_id,author_id,text,created_at,updated_at) VALUES($1,'missing',1,'ok',now(),now())`,
		`INSERT INTO cell_comments(notebook_id,cell_id,author_id,text,created_at,updated_at) VALUES($1,$2,999,'ok',now(),now())`,
	} {
		args := []any{n.ID}
		if strings.Contains(statement, "$2") {
			args = append(args, n.Cells[0].ID)
		}
		if _, err = pool.Exec(ctx, statement, args...); err == nil {
			t.Fatal("invalid FK accepted")
		}
	}
	var indexes int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM pg_indexes WHERE schemaname=current_schema() AND tablename='cell_comments'`).Scan(&indexes); err != nil || indexes != 3 {
		t.Fatalf("indexes=%d %v", indexes, err)
	}
}
