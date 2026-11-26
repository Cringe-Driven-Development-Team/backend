package httptransport

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	notebookcursor "github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/notebook/adapters/cursor"
	notebookdb "github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/notebook/adapters/postgres"
	notebookdelivery "github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/notebook/delivery/http"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/notebook/domain"
	notebookusecase "github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/notebook/usecase"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/testutil/pgtest"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/transport/http/api"
	usersecurity "github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/user/adapters/security"
	"github.com/jackc/pgx/v5/pgxpool"
)

type commentsHTTPFixture struct {
	pool     *pgxpool.Pool
	uc       *notebookusecase.NotebookUsecase
	notebook domain.Notebook
	request  func(method, path, body string, visitor int64, status int) *httptest.ResponseRecorder
	wall     string
}

func newCommentsHTTPFixture(t *testing.T) commentsHTTPFixture {
	t.Helper()
	ctx := t.Context()
	pool := pgtest.NewDatabase(t, "TEST_POSTGRES_DSN")
	if _, err := pool.Exec(ctx, `INSERT INTO users(id,login,password_hash) OVERRIDING SYSTEM VALUE VALUES(1,'owner','hash'),(2,'reader','hash'),(3,'other','hash')`); err != nil {
		t.Fatal(err)
	}
	uc := notebookusecase.NewNotebookUsecase(notebookdb.NewNotebookRepository(pool), notebookusecase.NoActiveSessions{}, nil, notebookusecase.WithUsers(pgtest.UsersFixture{DB: pool}), notebookusecase.WithCommentCursor(notebookcursor.NewCommentCodec([]byte("comments-key"))))
	n, err := uc.Create(ctx, 1, "comments")
	if err != nil {
		t.Fatal(err)
	}
	issuer := usersecurity.NewAccessToken([]byte("comments-http-key"), time.Hour)
	tokens := map[int64]string{}
	for _, id := range []int64{1, 2, 3} {
		tokens[id], err = issuer.Issue(testClaims(id))
		if err != nil {
			t.Fatal(err)
		}
	}
	router, err := NewRouter(Server{NotebookHandler: notebookdelivery.NewHandler(uc)}, issuer, allowSessions{}, "comments-bff")
	if err != nil {
		t.Fatal(err)
	}
	request := func(method, path, body string, visitor int64, status int) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, BaseURL+path, strings.NewReader(body))
		req.Header.Set("X-BFF-Key", "comments-bff")
		if visitor != 0 {
			req.Header.Set("Authorization", "Bearer "+tokens[visitor])
		}
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != status {
			t.Fatalf("%s %s visitor=%d: %d want %d: %s", method, path, visitor, rec.Code, status, rec.Body)
		}
		if rec.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("no-store missing")
		}
		return rec
	}
	return commentsHTTPFixture{pool: pool, uc: uc, notebook: n, request: request, wall: fmt.Sprintf("/notebooks/%d/content/cells/%s/comments", n.ID, n.Cells[0].ID)}
}

func decodeComment(t *testing.T, rec *httptest.ResponseRecorder) api.Comment {
	t.Helper()
	var c api.Comment
	if err := json.Unmarshal(rec.Body.Bytes(), &c); err != nil {
		t.Fatal(err)
	}
	return c
}

func TestCellCommentsLifecycle(t *testing.T) {
	f := newCommentsHTTPFixture(t)
	ctx := t.Context()
	n := f.notebook
	owner := decodeComment(t, f.request("POST", f.wall, `{"text":"  private  "}`, 1, 201))
	if owner.Text != "private" || !owner.CanEdit || !owner.CanDelete {
		t.Fatal(owner)
	}
	f.request("POST", f.wall, `{"text":"no"}`, 0, 401)
	f.request("GET", f.wall, "", 2, 404)
	if _, err := f.uc.PutSharing(ctx, n.ID, 1, domain.SharingModeRestricted, []string{"reader"}); err != nil {
		t.Fatal(err)
	}
	c := decodeComment(t, f.request("POST", f.wall, `{"text":"reader"}`, 2, 201))
	path := fmt.Sprintf("%s/%d", f.wall, c.Id)
	f.request("PATCH", path, `{"text":"owner may not edit"}`, 1, 403)
	if _, err := f.uc.PutSharing(ctx, n.ID, 1, domain.SharingModePublic, nil); err != nil {
		t.Fatal(err)
	}
	f.request("POST", f.wall, `{"text":"public"}`, 3, 201)
	f.request("GET", f.wall, "", 0, 200)
	f.request("PATCH", path, `{"text":"not mine"}`, 3, 403)
	f.request("DELETE", path, "", 3, 403)
	if _, err := f.pool.Exec(ctx, `UPDATE users SET login='newreader' WHERE id=2`); err != nil {
		t.Fatal(err)
	}
	c = decodeComment(t, f.request("PATCH", path, `{"text":"still mine"}`, 2, 200))
	if c.Author.Login != "newreader" || c.Author.Id != 2 || !c.CanEdit {
		t.Fatal(c)
	}
	if _, err := f.uc.PutSharing(ctx, n.ID, 1, domain.SharingModePrivate, nil); err != nil {
		t.Fatal(err)
	}
	f.request("GET", f.wall, "", 2, 404)
	f.request("PATCH", path, `{"text":"revoked"}`, 2, 404)
	f.request("DELETE", path, "", 2, 404)
	if _, err := f.uc.PutSharing(ctx, n.ID, 1, domain.SharingModeRestricted, []string{"newreader"}); err != nil {
		t.Fatal(err)
	}
	f.request("PATCH", path, `{"text":"restored"}`, 2, 200)
	f.request("DELETE", path, "", 1, 204)
	f.request("DELETE", path, "", 1, 404)
}

func TestCellCommentsTimestampJSON(t *testing.T) {
	f := newCommentsHTTPFixture(t)
	created := decodeComment(t, f.request("POST", f.wall, `{"text":"same"}`, 1, 201))
	path := fmt.Sprintf("%s/%d", f.wall, created.Id)
	if !created.CreatedAt.Equal(created.UpdatedAt) {
		t.Fatal("unequal creation dates")
	}
	previous := created.UpdatedAt
	for range 2 {
		rec := f.request("PATCH", path, `{"text":"same"}`, 1, 200)
		c := decodeComment(t, rec)
		if !c.UpdatedAt.After(previous) || !c.CreatedAt.Equal(created.CreatedAt) {
			t.Fatal(c)
		}
		previous = c.UpdatedAt
	}
	rec := f.request("GET", f.wall, "", 1, 200)
	var page api.CommentPage
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || !page.Items[0].UpdatedAt.Equal(previous) || page.Items[0].Text != "same" {
		t.Fatal(page)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rec.Body.String(), "Z\"") {
		t.Fatal("dates not UTC")
	}
}

func TestCellCommentsCursorTraversalHTTP(t *testing.T) {
	f := newCommentsHTTPFixture(t)
	ctx := t.Context()
	n := f.notebook
	for range 45 {
		if _, err := f.uc.CreateComment(ctx, n.ID, n.Cells[0].ID, 1, "entry"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := f.pool.Exec(ctx, `UPDATE cell_comments SET created_at='2020-01-01T00:00:00Z',updated_at='2020-01-01T00:00:00Z'`); err != nil {
		t.Fatal(err)
	}
	var first api.CommentPage
	rec := f.request("GET", f.wall, "", 1, 200)
	if err := json.Unmarshal(rec.Body.Bytes(), &first); err != nil {
		t.Fatal(err)
	}
	if len(first.Items) != 20 || first.NextCursor == nil {
		t.Fatal(first)
	}
	firstCursor := *first.NextCursor
	firstID := first.Items[0].Id
	boundary := first.Items[len(first.Items)-1]
	seen := map[int64]bool{}
	page := first
	for {
		for _, c := range page.Items {
			if seen[c.Id] {
				t.Fatal("duplicate")
			}
			seen[c.Id] = true
		}
		if page.NextCursor == nil {
			break
		}
		rec = f.request("GET", f.wall+"?cursor="+*page.NextCursor, "", 1, 200)
		if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
			t.Fatal(err)
		}
	}
	if len(seen) != 45 || len(page.Items) != 5 {
		t.Fatalf("count %d last %d", len(seen), len(page.Items))
	}
	// Три полные страницы по 15: у последней полный размер, но курсора уже нет.
	fullPath := f.wall + "?limit=15"
	for number := 0; number < 3; number++ {
		var full api.CommentPage
		if err := json.Unmarshal(f.request("GET", fullPath, "", 1, 200).Body.Bytes(), &full); err != nil {
			t.Fatal(err)
		}
		if len(full.Items) != 15 || (full.NextCursor == nil) != (number == 2) {
			t.Fatalf("full page %d: %+v", number, full)
		}
		if full.NextCursor != nil {
			fullPath = f.wall + "?limit=15&cursor=" + *full.NextCursor
		}
	}
	for _, size := range []int{1, 100} {
		var edge api.CommentPage
		if err := json.Unmarshal(f.request("GET", fmt.Sprintf("%s?limit=%d", f.wall, size), "", 1, 200).Body.Bytes(), &edge); err != nil {
			t.Fatal(err)
		}
		want := size
		if want > 45 {
			want = 45
		}
		if len(edge.Items) != want || (edge.NextCursor == nil) != (size == 100) {
			t.Fatalf("limit=%d: %+v", size, edge)
		}
	}
	if _, err := f.uc.PutSharing(ctx, n.ID, 1, domain.SharingModePublic, nil); err != nil {
		t.Fatal(err)
	}
	guestPath := f.wall
	guestCount := 0
	for {
		var guest api.CommentPage
		if err := json.Unmarshal(f.request("GET", guestPath, "", 0, 200).Body.Bytes(), &guest); err != nil {
			t.Fatal(err)
		}
		for _, c := range guest.Items {
			if c.CanEdit || c.CanDelete {
				t.Fatal("anonymous permissions")
			}
			guestCount++
		}
		if guest.NextCursor == nil {
			break
		}
		guestPath = f.wall + "?cursor=" + *guest.NextCursor
	}
	if guestCount != 45 {
		t.Fatalf("anonymous pages: %d", guestCount)
	}
	if _, err := f.uc.PutSharing(ctx, n.ID, 1, domain.SharingModeRestricted, []string{"reader"}); err != nil {
		t.Fatal(err)
	}
	var readerPage api.CommentPage
	if err := json.Unmarshal(f.request("GET", f.wall, "", 2, 200).Body.Bytes(), &readerPage); err != nil {
		t.Fatal(err)
	}
	if readerPage.NextCursor == nil {
		t.Fatal("reader cursor missing")
	}
	if _, err := f.uc.PutSharing(ctx, n.ID, 1, domain.SharingModeRestricted, []string{}); err != nil {
		t.Fatal(err)
	}
	f.request("GET", f.wall+"?cursor="+*readerPage.NextCursor, "", 2, 404)
	f.request("DELETE", fmt.Sprintf("%s/%d", f.wall, boundary.Id), "", 1, 204)
	inserted := decodeComment(t, f.request("POST", f.wall, `{"text":"new"}`, 1, 201))
	f.request("PATCH", fmt.Sprintf("%s/%d", f.wall, firstID), `{"text":"edited older message"}`, 1, 200)
	var latest api.CommentPage
	if err := json.Unmarshal(f.request("GET", f.wall, "", 1, 200).Body.Bytes(), &latest); err != nil {
		t.Fatal(err)
	}
	if len(latest.Items) == 0 || latest.Items[0].Id != inserted.Id {
		t.Fatal("new message missing or PATCH changed wall order")
	}
	rec = f.request("GET", f.wall+"?cursor="+firstCursor, "", 1, 200)
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 20 || page.Items[0].Id >= boundary.Id {
		t.Fatal("cursor lost its boundary")
	}
	f.request("GET", f.wall+"?cursor=broken", "", 1, 400)
	other, err := f.uc.AddCell(ctx, n.ID, 1, n.Revision, domain.CellKindMarkdown, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	otherWall := fmt.Sprintf("/notebooks/%d/content/cells/%s/comments", n.ID, other.Cell.ID)
	f.request("GET", otherWall+"?cursor="+firstCursor, "", 1, 400)
	f.request("PATCH", fmt.Sprintf("%s/%d", otherWall, first.Items[0].Id), `{"text":"wrong wall"}`, 1, 404)
	for _, q := range []string{"?limit=", "?limit=0", "?limit=101", "?cursor="} {
		f.request("GET", f.wall+q, "", 1, 400)
	}
	f.request("GET", otherWall, "", 1, 200)
}
