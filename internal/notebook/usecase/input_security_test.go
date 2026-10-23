package usecase

import (
	"context"
	"testing"
)

func TestCreatePreservesTextName(t *testing.T) {
	for _, name := range []string{`<script>alert(1)</script>`, `A & B < C > D`, `&lt;script&gt;`, `" onmouseover="alert(1)`, "Анализ"} {
		t.Run(name, func(t *testing.T) {
			uc, meta, _ := newTestUsecase()
			n, err := uc.Create(context.Background(), owner, "  "+name+"  ")
			if err != nil {
				t.Fatal(err)
			}
			stored, err := meta.Get(context.Background(), n.ID, owner)
			if err != nil {
				t.Fatal(err)
			}
			if n.Name != name || stored.Name != name {
				t.Fatalf("text changed: returned %q, stored %q, want %q", n.Name, stored.Name, name)
			}
		})
	}
}
