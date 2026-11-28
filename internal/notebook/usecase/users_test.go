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

// directoryFake фиксирует количество batch вызовов вместо N+1 чтений авторов.
type directoryFake struct {
	avatarURL string
	calls     int
	missing   bool
	err       error
}

func (d *directoryFake) BatchGet(_ context.Context, ids []int64) (map[int64]domain.NotebookReader, error) {
	d.calls++
	out := map[int64]domain.NotebookReader{}
	for _, id := range ids {
		if !d.missing {
			out[id] = domain.NotebookReader{ID: id, Login: "current_login", AvatarURL: d.avatarURL}
		}
	}
	return out, d.err
}
func (d *directoryFake) ResolveLogins(context.Context, []string) ([]usecase.UserResolution, error) {
	return nil, d.err
}

// TestCommentAuthorsUseOneBatch проверяет актуальные логины и отсутствие N+1 SQL/HTTP вызовов.
func TestCommentAuthorsUseOneBatch(t *testing.T) {
	rows := make([]domain.Comment, 100)
	for i := range rows {
		rows[i] = domain.Comment{ID: int64(100 - i), Author: domain.CommentAuthor{ID: 42}, CreatedAt: time.Now()}
	}
	repo := &commentRepo{rows: usecase.CommentRows{OwnerID: 1, Items: rows}}
	users := &directoryFake{avatarURL: "https://cdn.example.com/first.png"}
	uc := usecase.NewNotebookUsecase(repo, nil, nil, usecase.WithUsers(users), usecase.WithCommentCursor(cursor.NewCommentCodec([]byte("key"))))
	limit := 100
	page, err := uc.ListComments(t.Context(), 7, "cell", 0, usecase.CommentListInput{Limit: &limit})
	if err != nil || len(page.Items) != 100 || users.calls != 1 {
		t.Fatalf("batch lookup: %v %d", err, users.calls)
	}
	for _, comment := range page.Items {
		if comment.Author.Login != "current_login" || comment.Author.AvatarURL != users.avatarURL {
			t.Fatal("missing current author login")
		}
	}
	for _, url := range []string{"https://cdn.example.com/second.png", ""} {
		users.avatarURL = url
		page, err = uc.ListComments(t.Context(), 7, "cell", 0, usecase.CommentListInput{Limit: &limit})
		if err != nil || len(page.Items) != 100 || page.Items[0].Author.AvatarURL != url {
			t.Fatalf("updated author avatar %q: %v", url, err)
		}
	}
	users.err = usecase.ErrUserDirectoryUnavailable
	if _, err = uc.ListComments(t.Context(), 7, "cell", 0, usecase.CommentListInput{}); !errors.Is(err, usecase.ErrUserDirectoryUnavailable) {
		t.Fatal("dependency outage hidden", err)
	}
	users.err = nil
	users.missing = true
	before := repo.calls
	if _, err = uc.ListComments(t.Context(), 7, "cell", 0, usecase.CommentListInput{}); !errors.Is(err, usecase.ErrUserDirectoryInconsistent) || repo.calls-before != 2 {
		t.Fatalf("missing author fallback: %v calls=%d", err, repo.calls-before)
	}
}
