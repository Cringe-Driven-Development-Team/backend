package ipynb

import (
	"testing"

	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/notebook/domain"
	"github.com/google/go-cmp/cmp"
)

func TestIPYNBPreservesSource(t *testing.T) {
	for _, kind := range []domain.CellKind{domain.CellKindCode, domain.CellKindMarkdown} {
		for _, source := range []string{
			"if a < b:\n    print(\"<&>\")", `<script>alert(1)</script>`,
			`<img src=x onerror=alert(1)>`, `&lt;script&gt;`, `" onmouseover="alert(1)`, "Кириллица & кавычки ' \"",
		} {
			t.Run(string(kind)+"/"+source, func(t *testing.T) {
				want := []domain.Cell{{ID: "cell", Kind: kind, Source: source}}
				cells := want
				for range 2 {
					data, err := Build(cells)
					if err != nil {
						t.Fatal(err)
					}
					cells, err = Parse(data)
					if err != nil {
						t.Fatal(err)
					}
					if diff := cmp.Diff(want, cells); diff != "" {
						t.Fatalf("source changed (-want +got):\n%s", diff)
					}
				}
			})
		}
	}
	t.Run("array source", func(t *testing.T) {
		cells, err := Parse([]byte(`{"cells":[{"id":"cell","cell_type":"code","source":["if a < b:\n","    print(\"<&>\")"]}],"metadata":{},"nbformat":4,"nbformat_minor":5}`))
		if err != nil {
			t.Fatal(err)
		}
		if len(cells) != 1 || cells[0].Source != "if a < b:\n    print(\"<&>\")" {
			t.Fatalf("array source changed: %+v", cells)
		}
	})
}
