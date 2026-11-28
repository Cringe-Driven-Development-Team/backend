package postgres

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/notebook/domain"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/notebook/usecase"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/testutil/pgtest"
)

func TestCommentRepositoryPermissions(t *testing.T) {
	repo, pool := testRepository(t)
	ctx := t.Context()
	if _, err := pool.Exec(ctx, `INSERT INTO identity.users(id,login,password_hash) OVERRIDING SYSTEM VALUE VALUES(2,'reader','hash'),(3,'other','hash')`); err != nil {
		t.Fatal(err)
	}
	uc := usecase.NewNotebookUsecase(repo, usecase.NoActiveSessions{}, nil, usecase.WithUsers(pgtest.UsersFixture{DB: repo.db}))
	n, err := uc.Create(ctx, 1, "comments")
	if err != nil {
		t.Fatal(err)
	}
	cell := n.Cells[0].ID
	if _, err = repo.PutSharing(ctx, n.ID, 1, domain.SharingModeRestricted, []int64{2}); err != nil {
		t.Fatal(err)
	}
	c, err := repo.CreateComment(ctx, n.ID, cell, 2, "reader text")
	if err != nil {
		t.Fatal(err)
	}
	if c.OwnerID != 1 || c.Comment.Author.ID != 2 || c.Comment.Author.Login != "" || !c.Comment.CreatedAt.Equal(c.Comment.UpdatedAt) {
		t.Fatalf("invalid creation: %+v", c)
	}
	for _, visitor := range []int64{0, 3} {
		if _, err = repo.ListComments(ctx, n.ID, cell, visitor, usecase.CommentQuery{Limit: 20}); !errors.Is(err, usecase.ErrNotebookNotFound) {
			t.Fatalf("closed read: %v", err)
		}
	}
	for _, visitor := range []int64{1, 3} {
		_, err = repo.UpdateComment(ctx, n.ID, cell, visitor, c.Comment.ID, "forbidden")
		want := usecase.ErrCommentForbidden
		if visitor == 3 {
			want = usecase.ErrNotebookNotFound
		}
		if !errors.Is(err, want) {
			t.Fatalf("foreign PATCH %d: %v", visitor, err)
		}
	}
	previous := c.Comment.UpdatedAt
	for range 2 {
		c, err = repo.UpdateComment(ctx, n.ID, cell, 2, c.Comment.ID, "reader text")
		if err != nil || !c.Comment.UpdatedAt.After(previous) || !c.Comment.UpdatedAt.After(c.Comment.CreatedAt) {
			t.Fatalf("PATCH date: %+v %v", c, err)
		}
		previous = c.Comment.UpdatedAt
	}
	if _, err = pool.Exec(ctx, `UPDATE identity.users SET login='renamed' WHERE id=2`); err != nil {
		t.Fatal(err)
	}
	page, err := repo.ListComments(ctx, n.ID, cell, 2, usecase.CommentQuery{Limit: 20})
	if err != nil || len(page.Items) != 1 || page.Items[0].Author.ID != 2 || page.Items[0].Author.Login != "" {
		t.Fatalf("author login: %+v %v", page, err)
	}
	if _, err = repo.UpdateComment(ctx, n.ID, "missing", 2, c.Comment.ID, "x"); !errors.Is(err, usecase.ErrCellNotFound) {
		t.Fatalf("wrong cell: %v", err)
	}
	second, err := uc.Create(ctx, 1, "second")
	if err != nil {
		t.Fatal(err)
	}
	if err = repo.DeleteComment(ctx, second.ID, second.Cells[0].ID, 1, c.Comment.ID); !errors.Is(err, usecase.ErrCommentNotFound) {
		t.Fatalf("wrong notebook: %v", err)
	}
	if _, err = repo.PutSharing(ctx, n.ID, 1, domain.SharingModePrivate, nil); err != nil {
		t.Fatal(err)
	}
	if _, err = repo.UpdateComment(ctx, n.ID, cell, 2, c.Comment.ID, "x"); !errors.Is(err, usecase.ErrNotebookNotFound) {
		t.Fatal("revoked author", err)
	}
	if _, err = repo.PutSharing(ctx, n.ID, 1, domain.SharingModePublic, nil); err != nil {
		t.Fatal(err)
	}
	if _, err = repo.ListComments(ctx, n.ID, cell, 0, usecase.CommentQuery{Limit: 20}); err != nil {
		t.Fatal(err)
	}
	if _, err = repo.CreateComment(ctx, n.ID, cell, 3, "public"); err != nil {
		t.Fatal(err)
	}
	if err = repo.DeleteComment(ctx, n.ID, cell, 3, c.Comment.ID); !errors.Is(err, usecase.ErrCommentForbidden) {
		t.Fatal("foreign delete", err)
	}
	if err = repo.DeleteComment(ctx, n.ID, cell, 1, c.Comment.ID); err != nil {
		t.Fatal(err)
	}
	if err = repo.DeleteComment(ctx, n.ID, cell, 1, c.Comment.ID); !errors.Is(err, usecase.ErrCommentNotFound) {
		t.Fatal("repeated delete", err)
	}
}

func TestCellCommentsPreserveNotebook(t *testing.T) {
	repo, pool, n, c := commentNotebook(t)
	ctx := t.Context()
	uc := usecase.NewNotebookUsecase(repo, usecase.NoActiveSessions{}, nil, usecase.WithUsers(pgtest.UsersFixture{DB: repo.db}))
	src, err := repo.ExecutionSource(ctx, n.ID, 1, c.CellID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = repo.SaveResult(ctx, n.ID, 1, usecase.CellResult{CellID: c.CellID, SourceVersion: src.SourceVersion, Outputs: json.RawMessage(`[{"output_type":"stream","name":"stdout","text":"kept"}]`)}); err != nil {
		t.Fatal(err)
	}
	snapshot := func() string {
		t.Helper()
		var value string
		snapshotErr := pool.QueryRow(ctx, `SELECT jsonb_build_object('notebook',to_jsonb(n),'cells',(SELECT jsonb_agg(to_jsonb(c) ORDER BY cell_id) FROM workspace.notebook_cells c WHERE notebook_id=n.id),'data',(SELECT jsonb_agg(to_jsonb(d) ORDER BY cell_id) FROM workspace.notebook_cell_data d WHERE notebook_id=n.id),'sessions',(SELECT jsonb_agg(to_jsonb(s) ORDER BY id) FROM workspace.runtime_sessions s WHERE notebook_id=n.id))::text FROM workspace.notebooks n WHERE id=$1`, n.ID).Scan(&value)
		if snapshotErr != nil {
			t.Fatal(snapshotErr)
		}
		return value
	}
	before := snapshot()
	added, err := repo.CreateComment(ctx, n.ID, c.CellID, 2, "temporary")
	if err != nil {
		t.Fatal(err)
	}
	if snapshot() != before {
		t.Fatal("POST changed content, metadata or runtime")
	}
	if _, err = repo.UpdateComment(ctx, n.ID, c.CellID, 2, added.Comment.ID, "updated"); err != nil {
		t.Fatal(err)
	}
	if snapshot() != before {
		t.Fatal("PATCH changed content, metadata or runtime")
	}
	if err = repo.DeleteComment(ctx, n.ID, c.CellID, 2, added.Comment.ID); err != nil {
		t.Fatal(err)
	}
	if snapshot() != before {
		t.Fatal("DELETE changed content, metadata or runtime")
	}
	extra, err := uc.AddCell(ctx, n.ID, 1, n.Revision, domain.CellKindMarkdown, "neighbor", nil)
	if err != nil {
		t.Fatal(err)
	}
	moved, err := uc.MoveCell(ctx, n.ID, 1, extra.Hash, c.CellID, 1)
	if err != nil {
		t.Fatal(err)
	}
	edited, err := uc.EditCell(ctx, n.ID, 1, moved, c.CellID, "changed source")
	if err != nil {
		t.Fatal(err)
	}
	page, err := repo.ListComments(ctx, n.ID, c.CellID, 2, usecase.CommentQuery{Limit: 20})
	if err != nil || len(page.Items) != 1 || page.Items[0].ID != c.ID {
		t.Fatal(page, err)
	}
	clone, err := repo.Clone(ctx, n.ID, 2, "copy")
	if err != nil {
		t.Fatal(err)
	}
	for _, cell := range clone.Cells {
		page, err = repo.ListComments(ctx, clone.ID, cell.ID, 2, usecase.CommentQuery{Limit: 20})
		if err != nil || len(page.Items) != 0 {
			t.Fatal("clone discussion", page, err)
		}
	}
	if _, err = uc.RemoveCell(ctx, n.ID, 1, edited.Hash, c.CellID); err != nil {
		t.Fatal(err)
	}
	var count int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM workspace.cell_comments WHERE id=$1`, c.ID).Scan(&count); err != nil || count != 0 {
		t.Fatal("deleted cell comments", count, err)
	}
}

func TestCellCommentsCascade(t *testing.T) {
	repo, pool, n, c := commentNotebook(t)
	ctx := t.Context()
	uc := usecase.NewNotebookUsecase(repo, usecase.NoActiveSessions{}, nil, usecase.WithUsers(pgtest.UsersFixture{DB: repo.db}))
	other, err := uc.Create(ctx, 1, "neighbor")
	if err != nil {
		t.Fatal(err)
	}
	own, err := repo.CreateComment(ctx, other.ID, other.Cells[0].ID, 1, "keep")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `DELETE FROM identity.users WHERE id=2`); err != nil {
		t.Fatal(err)
	}
	var count int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM workspace.cell_comments WHERE id=$1`, c.ID).Scan(&count); err != nil || count != 1 {
		t.Fatal("Identity delete must not cascade across service ownership", count, err)
	}
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM workspace.cell_comments WHERE id=$1`, own.Comment.ID).Scan(&count); err != nil || count != 1 {
		t.Fatal("neighbor lost", count, err)
	}
	if _, err = repo.CreateComment(ctx, n.ID, n.Cells[0].ID, 1, "delete with notebook"); err != nil {
		t.Fatal(err)
	}
	if err = repo.DeleteNotebook(ctx, n.ID, 1); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM workspace.cell_comments WHERE notebook_id=$1`, n.ID).Scan(&count); err != nil || count != 0 {
		t.Fatal("notebook cascade", count, err)
	}
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM workspace.cell_comments WHERE id=$1`, own.Comment.ID).Scan(&count); err != nil || count != 1 {
		t.Fatal("neighbor changed", count, err)
	}
}

func TestCommentRepositoryDatesAndPaging(t *testing.T) {
	repo, pool := testRepository(t)
	ctx := t.Context()
	uc := usecase.NewNotebookUsecase(repo, usecase.NoActiveSessions{}, nil, usecase.WithUsers(pgtest.UsersFixture{DB: repo.db}))
	n, err := uc.Create(ctx, 1, "paging")
	if err != nil {
		t.Fatal(err)
	}
	cell := n.Cells[0].ID
	for range 45 {
		if _, err = repo.CreateComment(ctx, n.ID, cell, 1, "entry"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = pool.Exec(ctx, `UPDATE workspace.cell_comments SET created_at='2026-11-22T00:00:00Z',updated_at='2026-11-22T00:00:00Z' WHERE notebook_id=$1`, n.ID); err != nil {
		t.Fatal(err)
	}
	var after *usecase.CommentPosition
	ids := map[int64]bool{}
	previous := int64(1 << 62)
	for {
		page, e := repo.ListComments(ctx, n.ID, cell, 1, usecase.CommentQuery{Limit: 20, After: after})
		if e != nil {
			t.Fatal(e)
		}
		if len(page.Items) > 21 {
			t.Fatal("unbounded page")
		}
		items := page.Items
		if len(items) > 20 {
			items = items[:20]
		}
		for _, c := range items {
			if c.ID >= previous || ids[c.ID] {
				t.Fatal("duplicate or wrong order")
			}
			previous = c.ID
			ids[c.ID] = true
		}
		if len(page.Items) <= 20 {
			break
		}
		last := items[len(items)-1]
		after = &usecase.CommentPosition{CreatedAt: last.CreatedAt, ID: last.ID}
	}
	if len(ids) != 45 {
		t.Fatalf("count %d", len(ids))
	}
	got, err := repo.Get(ctx, n.ID, 1)
	if err != nil || got.Revision != n.Revision || !got.UpdatedAt.Equal(n.UpdatedAt) {
		t.Fatal("comments changed notebook", err)
	}
}
