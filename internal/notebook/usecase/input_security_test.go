package usecase

import (
	"context"
	"testing"

	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/notebook/domain"
	"go.uber.org/mock/gomock"
)

func TestCreatePreservesTextName(t *testing.T) {
	for _, name := range []string{`<script>alert(1)</script>`, `A & B < C > D`, `&lt;script&gt;`, `" onmouseover="alert(1)`, "Анализ"} {
		t.Run(name, func(t *testing.T) {
			f := newNotebookFixture(t)
			ctx := context.Background()
			cells := []domain.Cell{{ID: "fixed-id", Kind: domain.CellKindCode}}
			want := domain.Notebook{OwnerID: owner, Name: name, FileKey: "notebooks/fixed-id.ipynb", CellsCount: 1}
			gomock.InOrder(f.codec.EXPECT().Build(cells).Return([]byte("file"), nil), f.files.EXPECT().Put(ctx, want.FileKey, []byte("file")).Return(nil), f.meta.EXPECT().Create(ctx, want).Return(want, nil))
			n, err := f.uc.Create(context.Background(), owner, "  "+name+"  ")
			if err != nil {
				t.Fatal(err)
			}
			if n.Name != name {
				t.Fatalf("text changed: returned %q, want %q", n.Name, name)
			}
		})
	}
}
