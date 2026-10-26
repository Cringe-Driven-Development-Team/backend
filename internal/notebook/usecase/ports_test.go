package usecase

import (
	"context"
	"errors"
	"testing"

	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/notebook/domain"
)

type failedCodec struct{ err error }

func (c failedCodec) Parse([]byte) ([]domain.Cell, error)           { return nil, c.err }
func (c failedCodec) Build([]domain.Cell) ([]byte, error)           { return nil, c.err }
func (c failedCodec) Rewrite([]byte, []domain.Cell) ([]byte, error) { return nil, c.err }
func TestCodecFailureDoesNotCreateNotebook(t *testing.T) {
	_, meta, files := newTestUsecase()
	failure := errors.New("encode failed")
	uc := NewNotebookUsecase(meta, files, failedCodec{failure})
	_, err := uc.Create(context.Background(), owner, "name")
	if !errors.Is(err, failure) {
		t.Fatalf("Create error = %v", err)
	}
	if len(meta.notebooks) != 0 || len(files.data) != 0 {
		t.Fatal("codec failure wrote notebook")
	}
}
