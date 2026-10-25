package usecase

import (
	"context"
	"errors"
	"testing"

	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/notebook/domain"
)

func TestCodecFailureDoesNotCreateNotebook(t *testing.T) {
	f := newNotebookFixture(t)
	failure := errors.New("encode failed")
	f.codec.EXPECT().Build([]domain.Cell{{ID: "fixed-id", Kind: domain.CellKindCode}}).Return(nil, failure)
	_, err := f.uc.Create(context.Background(), owner, "name")
	if !errors.Is(err, failure) {
		t.Fatal(err)
	}
}
