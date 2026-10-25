package delivery

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/notebook/domain"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/transport/http/api"
)

type textUsecase struct {
	fakeUsecase
	gotName string
}

func (uc *textUsecase) Create(ctx context.Context, ownerID int64, name string) (domain.Notebook, error) {
	uc.gotName = name
	return uc.fakeUsecase.Create(ctx, ownerID, name)
}

func TestNotebookTextJSONRoundTrip(t *testing.T) {
	for _, text := range []string{
		`<script>alert(1)</script>`, `<img src=x onerror=alert(1)>`,
		`" onmouseover="alert(1)`, `A & B < C > D`, `&lt;script&gt;`, `Анализ данных`,
		"if a < b:\n    print(\"<&>\")",
	} {
		t.Run(text, func(t *testing.T) {
			now := time.Date(2026, 10, 24, 0, 0, 0, 0, time.UTC)
			uc := &textUsecase{fakeUsecase: fakeUsecase{
				notebook: domain.Notebook{ID: 1, Name: text, CreatedAt: now, UpdatedAt: now,
					Cells: []domain.Cell{{ID: "cell", Kind: domain.CellKindMarkdown, Source: text}}},
				list: []domain.NotebookSummary{{ID: 1, Name: text, UpdatedAt: now}},
			}}
			body, err := json.Marshal(api.CreateNotebookRequest{Name: text})
			if err != nil {
				t.Fatal(err)
			}
			for _, operation := range []struct {
				method, path, body string
				status             int
			}{
				{http.MethodPost, "/notebooks", string(body), http.StatusCreated},
				{http.MethodGet, "/notebooks", "", http.StatusOK},
				{http.MethodGet, "/notebooks/1", "", http.StatusOK},
			} {
				rec := do(t, uc, operation.method, operation.path, operation.body, true)
				if rec.Code != operation.status || rec.Header().Get("Content-Type") != "application/json" {
					t.Fatalf("%s %s: status=%d headers=%v body=%s", operation.method, operation.path, rec.Code, rec.Header(), rec.Body)
				}
				if operation.method == http.MethodGet && operation.path == "/notebooks" {
					var list []api.NotebookSummary
					if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
						t.Fatal(err)
					}
					if len(list) != 1 || list[0].Name != text {
						t.Fatalf("list changed text: %+v", list)
					}
					continue
				}
				var notebook api.Notebook
				if err := json.Unmarshal(rec.Body.Bytes(), &notebook); err != nil {
					t.Fatal(err)
				}
				if notebook.Name != text || len(notebook.Cells) != 1 || notebook.Cells[0].Source != text {
					t.Fatalf("response changed text: %+v", notebook)
				}
			}
			if uc.gotName != text {
				t.Fatalf("handler changed input: %q, want %q", uc.gotName, text)
			}
		})
	}
}

func TestValidationErrorWithHTMLIsJSON(t *testing.T) {
	rec := do(t, &fakeUsecase{}, http.MethodPost, "/notebooks/1/cells", `{"kind":"<img src=x onerror=alert(1)>"}`, true)
	var result api.Error
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusBadRequest || result.Code != api.ValidationError || rec.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("unsafe validation response: %d %v %s", rec.Code, rec.Header(), rec.Body)
	}
}
