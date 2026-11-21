package httptransport

import (
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/notebook/domain"
	notebookusecase "github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/notebook/usecase"
	"go.uber.org/mock/gomock"
)

func TestCellCommentsHTTPContract(t *testing.T) {
	f := newRouterFixture(t)
	f.notebooks.EXPECT().ListComments(gomock.Any(), int64(42), "cell-1", int64(0), notebookusecase.CommentListInput{}).Return(notebookusecase.CommentPage{}, nil)
	req := httptest.NewRequest("GET", "/api/v1/notebooks/42/content/cells/cell-1/comments", nil)
	req.Header.Set("X-BFF-Key", "test-key")
	rec := httptest.NewRecorder()
	f.handler.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("missing no-store")
	}
	if rec.Body.String() != "{\"items\":[],\"next_cursor\":null}\n" {
		t.Fatalf("body %s", rec.Body)
	}
}

func TestCellCommentsMalformedQuery(t *testing.T) {
	for _, query := range []string{"?cursor=%ZZ", "?limit=2;3", "?cursor=ok%", "?limit=%GG"} {
		f := newRouterFixture(t)
		req := httptest.NewRequest("GET", "/api/v1/notebooks/42/content/cells/cell/comments"+query, nil)
		req.Header.Set("X-BFF-Key", "test-key")
		// Некорректный query не должен превращаться в обычный запрос первой страницы.
		f.notebooks.EXPECT().ListComments(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(notebookusecase.CommentPage{}, nil).AnyTimes()
		rec := httptest.NewRecorder()
		f.handler.ServeHTTP(rec, req)
		if rec.Code != 400 {
			t.Fatalf("%s returned %d: %s", query, rec.Code, rec.Body)
		}
	}
}

func TestCellCommentsAuthenticationOrder(t *testing.T) {
	for _, tc := range []struct {
		name, method, path, body, auth string
		bff                            bool
		want                           int
	}{
		{"s2s first", "POST", "/api/v1/notebooks/no/content/cells/cell/comments", "{", "bad", false, 403},
		{"session before body", "POST", "/api/v1/notebooks/no/content/cells/cell/comments", "{", "", true, 401},
		{"delete session before id", "DELETE", "/api/v1/notebooks/no/content/cells/cell/comments/no", "", "", true, 401},
		{"bad provided token", "GET", "/api/v1/notebooks/42/content/cells/cell/comments", "", "Bearer bad", true, 401},
		{"empty auth header", "GET", "/api/v1/notebooks/42/content/cells/cell/comments", "", "empty", true, 401},
		{"duplicate auth", "GET", "/api/v1/notebooks/42/content/cells/cell/comments", "", "duplicate", true, 401},
		{"valid auth invalid id", "POST", "/api/v1/notebooks/no/content/cells/cell/comments", `{"text":"ok"}`, "valid", true, 400},
		{"empty limit", "GET", "/api/v1/notebooks/42/content/cells/cell/comments?limit=", "", "", true, 400},
		{"empty cursor", "GET", "/api/v1/notebooks/42/content/cells/cell/comments?cursor=", "", "", true, 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newRouterFixture(t)
			req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			if tc.bff {
				req.Header.Set("X-BFF-Key", "test-key")
			}
			switch tc.auth {
			case "valid":
				req.Header.Set("Authorization", "Bearer "+f.access)
			case "duplicate":
				req.Header.Add("Authorization", "Bearer "+f.access)
				req.Header.Add("Authorization", "Bearer "+f.access)
			case "empty":
				req.Header["Authorization"] = []string{""}
			default:
				if tc.auth != "" {
					req.Header.Set("Authorization", tc.auth)
				}
			}
			rec := httptest.NewRecorder()
			f.handler.ServeHTTP(rec, req)
			if rec.Code != tc.want {
				t.Fatalf("got %d want %d: %s", rec.Code, tc.want, rec.Body)
			}
			if rec.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("missing no-store")
			}
		})
	}
}

func TestCellCommentsActionErrors(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want int
		code string
	}{
		{notebookusecase.ErrCommentForbidden, 403, "forbidden"}, {notebookusecase.ErrCommentNotFound, 404, "not_found"}, {notebookusecase.ErrCellNotFound, 404, "not_found"}, {notebookusecase.ErrNotebookNotFound, 404, "not_found"}, {notebookusecase.ErrInvalidComment, 400, "validation_error"}, {errors.New("secret SQL password"), 500, "internal"},
	} {
		for _, method := range []string{"PATCH", "DELETE"} {
			t.Run(method+tc.code, func(t *testing.T) {
				f := newRouterFixture(t)
				if method == "PATCH" {
					f.notebooks.EXPECT().UpdateComment(gomock.Any(), int64(42), "cell", int64(1), int64(9), "ok").Return(domain.Comment{}, tc.err)
				} else {
					f.notebooks.EXPECT().DeleteComment(gomock.Any(), int64(42), "cell", int64(1), int64(9)).Return(tc.err)
				}
				req := httptest.NewRequest(method, "/api/v1/notebooks/42/content/cells/cell/comments/9", strings.NewReader(`{"text":"ok"}`))
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("X-BFF-Key", "test-key")
				req.Header.Set("Authorization", "Bearer "+f.access)
				rec := httptest.NewRecorder()
				f.handler.ServeHTTP(rec, req)
				var got struct {
					Code string `json:"code"`
				}
				if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
					t.Fatal(err)
				}
				if rec.Code != tc.want || got.Code != tc.code || strings.Contains(rec.Body.String(), "secret SQL") {
					t.Fatalf("%d %s", rec.Code, rec.Body)
				}
				if rec.Header().Get("Cache-Control") != "no-store" {
					t.Fatal("missing no-store")
				}
			})
		}
	}
}
