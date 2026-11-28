package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/notebook/domain"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/notebook/usecase"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/testutil/pgtest"
)

func TestSharingReadMatrixAndAtomicReplacement(t *testing.T) {
	repo, pool := testRepository(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `INSERT INTO identity.users(id,login,password_hash) OVERRIDING SYSTEM VALUE VALUES(2,'Reader','hash'),(3,'Other','hash')`); err != nil {
		t.Fatal(err)
	}
	uc := usecase.NewNotebookUsecase(repo, usecase.NoActiveSessions{}, nil, usecase.WithUsers(pgtest.UsersFixture{DB: repo.db}))
	n, err := uc.Create(ctx, 1, "matrix")
	if err != nil {
		t.Fatal(err)
	}
	grants := []domain.NotebookReader{{Login: "reader", Role: domain.RoleViewer}, {Login: "READER", Role: domain.RoleCommenter}, {Login: "owner", Role: domain.RoleViewer}}
	settings, err := uc.PutSharing(ctx, n.ID, 1, true, grants)
	if err != nil || settings.LinkToken == nil || len(settings.Readers) != 1 || settings.Readers[0].Role != domain.RoleCommenter {
		t.Fatalf("sharing: %+v %v", settings, err)
	}
	for _, user := range []int64{0, 1, 2, 3} {
		want := user == 1 || user == 2
		got, e := uc.Get(ctx, n.ID, user)
		if (e == nil) != want || (want && (!got.Access.CanClone || !got.Access.CanComment || got.Access.CanEdit != (user == 1))) {
			t.Fatalf("user %d: %+v %v", user, got.Access, e)
		}
		shared, e := uc.GetShared(ctx, *settings.LinkToken, user, true)
		if (e == nil) != (user > 0) || (user == 3 && shared.Access != (domain.NotebookAccess{})) {
			t.Fatalf("shared user %d: %+v %v", user, shared.Access, e)
		}
	}
	before, err := uc.PutSharing(ctx, n.ID, 1, true, []domain.NotebookReader{{Login: "Reader", Role: domain.RoleViewer}})
	if err != nil {
		t.Fatal(err)
	}
	repeated, err := uc.PutSharing(ctx, n.ID, 1, true, []domain.NotebookReader{{Login: "Reader", Role: domain.RoleCommenter}, {Login: "READER", Role: domain.RoleViewer}})
	if err != nil || !reflect.DeepEqual(before, repeated) {
		t.Fatalf("non-idempotent PUT: %+v %v", repeated, err)
	}
	if _, err = uc.PutSharing(ctx, n.ID, 1, false, []domain.NotebookReader{{Login: "Other", Role: domain.RoleViewer}, {Login: "missing", Role: domain.RoleViewer}}); !errors.Is(err, usecase.ErrUnknownLogin) {
		t.Fatal(err)
	}
	after, err := uc.GetSharing(ctx, n.ID, 1)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("partial replacement: %+v %v", after, err)
	}
	if _, err = pool.Exec(ctx, `UPDATE identity.users SET login='Renamed' WHERE id=2`); err != nil {
		t.Fatal(err)
	}
	after, err = uc.GetSharing(ctx, n.ID, 1)
	if err != nil || after.Readers[0].Login != "Renamed" {
		t.Fatalf("stale login: %+v %v", after, err)
	}
	if _, err = uc.Get(ctx, n.ID, 2); err != nil {
		t.Fatal("rename lost access", err)
	}
	if _, err = uc.PutSharing(ctx, n.ID, 1, false, nil); err != nil {
		t.Fatal(err)
	}
	if _, err = uc.Get(ctx, n.ID, 2); !errors.Is(err, usecase.ErrNotebookNotFound) {
		t.Fatal("revoked access allowed", err)
	}
}

func TestCloneCompleteIndependentDocument(t *testing.T) {
	repo, pool := testRepository(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `INSERT INTO identity.users(id,login,password_hash) OVERRIDING SYSTEM VALUE VALUES(2,'reader','hash')`); err != nil {
		t.Fatal(err)
	}
	count := int64(9)
	src, err := repo.Create(ctx, domain.Notebook{OwnerID: 1, Name: "source", Document: domain.Document{NBFormat: 4, NBFormatMinor: 5, Metadata: json.RawMessage(`{"custom":"header"}`), Extra: map[string]json.RawMessage{"extra_field": json.RawMessage(`{"keep":true}`)}}, Cells: []domain.Cell{
		{ID: "code", Kind: domain.CellKindCode, Source: "print(1)", Metadata: json.RawMessage(`{"custom":1}`), Extra: map[string]json.RawMessage{"x": json.RawMessage(`true`)}, Outputs: json.RawMessage(`[{"output_type":"stream","name":"stdout","text":"saved"}]`), ExecutionCount: &count, HasExecutionCount: true, OutputTruncated: true, HasRuntimeMetadata: true},
		{ID: "markdown", Kind: domain.CellKindMarkdown, Source: "![a](attachment:a.png)", Metadata: json.RawMessage(`{}`), Attachments: json.RawMessage(`{"a.png":{"image/png":"YWJj"}}`)},
		{ID: "raw", Kind: "raw", Source: "raw text", Metadata: json.RawMessage(`{"format":"text/plain"}`)},
	}})
	if err != nil {
		t.Fatal(err)
	}
	uc := usecase.NewNotebookUsecase(repo, usecase.NoActiveSessions{}, nil, usecase.WithUsers(pgtest.UsersFixture{DB: repo.db}))
	if _, err = uc.PutSharing(ctx, src.ID, 1, false, []domain.NotebookReader{{Login: "reader", Role: domain.RoleViewer}}); err != nil {
		t.Fatal(err)
	}
	clone, err := uc.Clone(ctx, src.ID, 2, "  копия 😀  ")
	if err != nil {
		t.Fatal(err)
	}
	if clone.ID == src.ID || clone.OwnerID != 2 || clone.Name != "копия 😀" || clone.Revision == src.Revision || clone.LinkToken != nil {
		t.Fatalf("clone identity: %+v", clone)
	}
	got, err := repo.Get(ctx, clone.ID, 2)
	if err != nil {
		t.Fatal(err)
	}
	original, err := repo.Get(ctx, src.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Document, original.Document) || len(got.Cells) != len(original.Cells) {
		t.Fatal("document not preserved")
	}
	for i, c := range got.Cells {
		old := original.Cells[i]
		if c.SourceVersion == old.SourceVersion {
			t.Fatal("source version reused")
		}
		c.ID = old.ID
		c.SourceVersion = old.SourceVersion
		if !reflect.DeepEqual(c, old) {
			t.Fatalf("cell %d differs:\ngot %+v\nwant %+v", i, c, old)
		}
	}
	settings, err := uc.GetSharing(ctx, clone.ID, 2)
	if err != nil || len(settings.Readers) != 0 {
		t.Fatal(settings, err)
	}
	page, err := uc.List(ctx, 2, usecase.ListInput{})
	if err != nil || len(page.Items) != 1 || page.Items[0].ID != clone.ID {
		t.Fatalf("copy missing from own list: %+v %v", page, err)
	}
	if _, err = uc.EditCell(ctx, clone.ID, 2, clone.Revision, clone.Cells[0].ID, "independent"); err != nil {
		t.Fatal(err)
	}
	unchanged, err := repo.Get(ctx, src.ID, 1)
	if err != nil || unchanged.Cells[0].Source != original.Cells[0].Source {
		t.Fatal("editing copy changed source", err)
	}
	if err = repo.DeleteNotebook(ctx, src.ID, 1); err != nil {
		t.Fatal(err)
	}
	if _, err = uc.Get(ctx, clone.ID, 2); err != nil {
		t.Fatal("original deletion broke copy", err)
	}
	if _, err = uc.Clone(ctx, clone.ID, 0, "anonymous"); err == nil {
		t.Fatal("anonymous clone allowed")
	}
}

func TestSharingMigrationPreservesExistingNotebook(t *testing.T) {
	repo, pool := testRepository(t)
	ctx := t.Context()
	uc := usecase.NewNotebookUsecase(repo, usecase.NoActiveSessions{}, nil, usecase.WithUsers(pgtest.UsersFixture{DB: repo.db}))
	before, err := uc.Create(ctx, 1, "existing")
	if err != nil {
		t.Fatal(err)
	}
	// Временно возвращаем прежнюю структуру и добавляем адресного читателя.
	sql, err := os.ReadFile("../../../../migrations/workspace/00005_notebook_access.sql")
	if err != nil {
		t.Fatal(err)
	}
	up, down, _ := strings.Cut(string(sql), "-- +goose Down")
	if _, err = pool.Exec(ctx, down); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `UPDATE workspace.notebooks SET sharing_mode='restricted' WHERE id=$1`, before.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO workspace.notebook_readers(notebook_id,user_id) VALUES($1,2)`, before.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, up); err != nil {
		t.Fatal(err)
	}
	grant, err := repo.GetSharing(ctx, before.ID, 1)
	if err != nil || grant.LinkToken != nil || len(grant.Readers) != 1 || grant.Readers[0].Role != domain.RoleViewer {
		t.Fatalf("migrated grant: %+v %v", grant, err)
	}
	after, err := repo.Get(ctx, before.ID, 1)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("existing notebook changed: %+v %v", after, err)
	}
}

func TestCloneEmptyNotebook(t *testing.T) {
	repo, _ := testRepository(t)
	ctx := context.Background()
	src, err := repo.Create(ctx, domain.Notebook{OwnerID: 1, Name: "empty", Document: domain.Document{NBFormat: 4, NBFormatMinor: 5}})
	if err != nil {
		t.Fatal(err)
	}
	uc := usecase.NewNotebookUsecase(repo, usecase.NoActiveSessions{}, nil, usecase.WithUsers(pgtest.UsersFixture{DB: repo.db}))
	clone, err := uc.Clone(ctx, src.ID, 1, "empty copy")
	if err != nil || len(clone.Cells) != 0 {
		t.Fatal(clone, err)
	}
}
