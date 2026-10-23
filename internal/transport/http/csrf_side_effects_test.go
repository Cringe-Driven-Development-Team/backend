package httptransport

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/notebook/adapters/ipynb"
	notebookdelivery "github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/notebook/delivery/http"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/notebook/domain"
	notebookusecase "github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/notebook/usecase"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/transport/http/cookies"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/transport/http/middleware"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/transport/http/security"
	usersecurity "github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/user/adapters/security"
	userdelivery "github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/user/delivery/http"
)

type csrfMetadata struct {
	notebookusecase.Metadata
	writes int
}

func (m *csrfMetadata) Create(_ context.Context, n domain.Notebook) (domain.Notebook, error) {
	m.writes++
	n.ID = 1
	return n, nil
}

func (m *csrfMetadata) Modify(_ context.Context, id, ownerID int64, fn func(domain.Notebook) (int, error)) error {
	m.writes++
	_, err := fn(domain.Notebook{ID: id, OwnerID: ownerID, FileKey: "fixture.ipynb"})
	return err
}

type csrfFiles struct {
	data   []byte
	reads  int
	writes int
}

func (f *csrfFiles) Get(context.Context, string) ([]byte, error) {
	f.reads++
	return f.data, nil
}

func (f *csrfFiles) Put(_ context.Context, _ string, data []byte) error {
	f.writes++
	f.data = data
	return nil
}

func TestCSRFRejectionDoesNotTouchNotebookStorage(t *testing.T) {
	for _, op := range []struct {
		method, path string
		status       int
	}{
		{http.MethodPost, BaseURL + "/notebooks", http.StatusCreated},
		{http.MethodPost, BaseURL + "/notebooks/1/cells", http.StatusCreated},
		{http.MethodDelete, BaseURL + "/notebooks/1/cells/0", http.StatusNoContent},
	} {
		for _, invalid := range []string{"missing", "mismatch", "other user", "bad signature", "duplicate header", "duplicate cookie"} {
			t.Run(op.method+op.path+"/"+invalid, func(t *testing.T) {
				_, signer, users, access, _ := csrfRouter(t)
				meta := &csrfMetadata{}
				data, err := ipynb.Build([]domain.Cell{{ID: "cell", Kind: domain.CellKindCode}})
				if err != nil {
					t.Fatal(err)
				}
				files := &csrfFiles{data: data}
				uc := notebookusecase.NewNotebookUsecase(meta, files, ipynb.Codec{})
				handler, err := NewRouter(Server{
					UserHandler:     userdelivery.NewHandler(users, cookies.Config{CSRFTTL: time.Hour}, signer),
					NotebookHandler: notebookdelivery.NewHandler(uc),
				}, usersecurity.NewAccessToken([]byte("jwt-test-secret"), time.Hour), nil, signer, time.Hour, userdelivery.NewRefreshResolver(users), middleware.DefaultAppOrigin)
				if err != nil {
					t.Fatal(err)
				}
				token := signer.Signed(1)
				switch invalid {
				case "other user":
					token = signer.Signed(2)
				case "bad signature":
					token = "broken.signature"
				}
				req := csrfRequest(op.method, op.path, token, access, "")
				switch invalid {
				case "missing":
					req.Header.Del(security.CSRFHeaderName)
				case "mismatch":
					req.Header.Set(security.CSRFHeaderName, signer.Signed(1))
				case "duplicate header":
					req.Header.Add(security.CSRFHeaderName, token)
				case "duplicate cookie":
					req.AddCookie(&http.Cookie{Name: security.CSRFCookieName, Value: token})
				}
				assertCSRFError(t, serveCSRF(handler, req))
				if meta.writes != 0 || files.reads != 0 || files.writes != 0 || string(files.data) != string(data) || users.mutations != 0 {
					t.Fatalf("rejected request reached storage: metadata=%d files read=%d write=%d sessions=%d", meta.writes, files.reads, files.writes, users.mutations)
				}
				// Положительный контроль: настоящие usecase и codec доходят до тех же портов.
				rec := serveCSRF(handler, csrfRequest(op.method, op.path, signer.Signed(1), access, ""))
				if rec.Code != op.status || meta.writes != 1 || files.writes != 1 {
					t.Fatalf("valid request failed: status=%d metadata=%d writes=%d body=%s", rec.Code, meta.writes, files.writes, rec.Body)
				}
			})
		}
	}
}
