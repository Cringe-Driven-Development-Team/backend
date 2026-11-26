package usecase_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/notebook/adapters/cursor"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/notebook/domain"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/notebook/usecase"
)

type commentRepo struct {
	usecase.Repository
	rows  usecase.CommentRows
	query usecase.CommentQuery
	calls int
	text  string
	err   error
}

func (r *commentRepo) ListComments(_ context.Context, _ int64, _ string, _ int64, q usecase.CommentQuery) (usecase.CommentRows, error) {
	r.query = q
	r.calls++
	return r.rows, r.err
}
func (r *commentRepo) CreateComment(_ context.Context, id int64, cell string, visitor int64, text string) (usecase.CommentRecord, error) {
	r.calls++
	r.text = text
	return usecase.CommentRecord{OwnerID: 1, Comment: domain.Comment{NotebookID: id, CellID: cell, Author: domain.CommentAuthor{ID: visitor}, Text: text}}, r.err
}
func (r *commentRepo) UpdateComment(ctx context.Context, id int64, cell string, visitor, _ int64, text string) (usecase.CommentRecord, error) {
	return r.CreateComment(ctx, id, cell, visitor, text)
}
func (r *commentRepo) DeleteComment(context.Context, int64, string, int64, int64) error {
	r.calls++
	return r.err
}

func TestCommentUsecaseList(t *testing.T) {
	stamp := time.Date(2026, 11, 22, 12, 0, 0, 123456000, time.UTC)
	repo := &commentRepo{rows: usecase.CommentRows{OwnerID: 1, Items: []domain.Comment{{ID: 3, CreatedAt: stamp, Author: domain.CommentAuthor{ID: 2}}, {ID: 2, CreatedAt: stamp, Author: domain.CommentAuthor{ID: 3}}, {ID: 1, CreatedAt: stamp, Author: domain.CommentAuthor{ID: 4}}}}}
	uc := usecase.NewNotebookUsecase(repo, nil, nil, usecase.WithUsers(&directoryFake{}), usecase.WithCommentCursor(cursor.NewCommentCodec([]byte("key"))))
	page, err := uc.ListComments(t.Context(), 7, "cell", 2, usecase.CommentListInput{Limit: ptr(2)})
	if err != nil || len(page.Items) != 2 || page.NextCursor == nil || !page.Items[0].CanEdit || page.Items[1].CanDelete {
		t.Fatalf("page %+v %v", page, err)
	}
	_, err = uc.ListComments(t.Context(), 7, "cell", 1, usecase.CommentListInput{Cursor: page.NextCursor})
	if err != nil || repo.query.Limit != 20 || repo.query.After.ID != 2 {
		t.Fatal(repo.query, err)
	}
	before := repo.calls
	for _, in := range []usecase.CommentListInput{{Limit: ptr(0)}, {Limit: ptr(101)}, {Cursor: ptr("")}, {Cursor: ptr("bad")}} {
		if _, err = uc.ListComments(t.Context(), 7, "cell", 1, in); !errors.Is(err, usecase.ErrInvalidCommentList) {
			t.Fatal(err)
		}
	}
	if repo.calls != before {
		t.Fatal("invalid input reached repository")
	}
	repo.rows = usecase.CommentRows{OwnerID: 1}
	page, err = uc.ListComments(t.Context(), 7, "cell", 0, usecase.CommentListInput{})
	if err != nil || page.Items == nil || page.NextCursor != nil {
		t.Fatal(page, err)
	}
	missing := usecase.NewNotebookUsecase(repo, nil, nil)
	if _, err = missing.ListComments(t.Context(), 7, "cell", 1, usecase.CommentListInput{}); !errors.Is(err, usecase.ErrCommentCursorUnavailable) {
		t.Fatal(err)
	}
}

func TestCommentUsecaseWrites(t *testing.T) {
	repo := &commentRepo{}
	uc := usecase.NewNotebookUsecase(repo, nil, nil, usecase.WithUsers(&directoryFake{}))
	c, err := uc.CreateComment(t.Context(), 1, "cell", 2, "  hello\n world  ")
	if err != nil || repo.text != "hello\n world" || !c.CanEdit || !c.CanDelete {
		t.Fatal(c, err)
	}
	if _, err = uc.UpdateComment(t.Context(), 1, "cell", 2, 3, " trimmed "); err != nil || repo.text != "trimmed" {
		t.Fatal(err)
	}
	before := repo.calls
	if _, err = uc.UpdateComment(t.Context(), 1, "cell", 2, 0, "x"); err == nil {
		t.Fatal("zero comment ID")
	}
	if _, err = uc.CreateComment(t.Context(), 1, "cell", 2, " \n "); err == nil {
		t.Fatal("blank")
	}
	if err = uc.DeleteComment(t.Context(), 1, "cell", 2, -1); err == nil {
		t.Fatal("negative ID")
	}
	if repo.calls != before {
		t.Fatal("invalid write reached repository")
	}
}

func TestCommentUsecasePermissions(t *testing.T) {
	for _, tc := range []struct {
		visitor      int64
		edit, delete bool
	}{{1, false, true}, {2, true, true}, {3, false, false}, {0, false, false}} {
		repo := &commentRepo{rows: usecase.CommentRows{OwnerID: 1, Items: []domain.Comment{{ID: 1, Author: domain.CommentAuthor{ID: 2}}}}}
		uc := usecase.NewNotebookUsecase(repo, nil, nil, usecase.WithUsers(&directoryFake{}), usecase.WithCommentCursor(cursor.NewCommentCodec([]byte("key"))))
		page, err := uc.ListComments(t.Context(), 1, "cell", tc.visitor, usecase.CommentListInput{Limit: ptr(1)})
		if err != nil || len(page.Items) != 1 || page.NextCursor != nil || page.Items[0].CanEdit != tc.edit || page.Items[0].CanDelete != tc.delete {
			t.Fatalf("visitor %d: %+v %v", tc.visitor, page, err)
		}
	}
}
