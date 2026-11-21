package middleware

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/transport/http/principal"
)

func TestCellCommentsBodyValidation(t *testing.T) {
	valid, _ := json.Marshal(map[string]string{"text": " \u2003" + strings.Repeat("😀", 5000) + "\u2003 "})
	cases := []struct {
		name, body string
		status     int
	}{
		{"valid", `{"text":"  hello\n world  "}`, 200}, {"max trimmed", string(valid), 200}, {"html", `{"text":"<b>& hello</b>"}`, 200},
		{"missing", `{}`, 400}, {"null", `{"text":null}`, 400}, {"type", `{"text":1}`, 400}, {"empty", `{"text":" \n "}`, 400},
		{"nul", `{"text":"a\u0000b"}`, 400}, {"utf8", "{\"text\":\"" + string([]byte{0xff}) + "\"}", 400},
		{"unknown", `{"text":"ok","author_id":1}`, 400}, {"parent", `{"text":"ok","parent_id":1}`, 400},
		{"case-sensitive field", `{"Text":"ok"}`, 400},
		{"trailing", `{"text":"ok"} {"text":"second"}`, 400}, {"null body", `null`, 400}, {"too long", `{"text":"` + strings.Repeat("x", 5001) + `"}`, 400},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/api/v1/notebooks/1/content/cells/cell-1/comments", strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			req = req.WithContext(principal.WithUserID(req.Context(), 1))
			next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				data, err := io.ReadAll(r.Body)
				if err != nil {
					t.Fatal(err)
				}
				var body struct {
					Text string `json:"text"`
				}
				if err = json.Unmarshal(data, &body); err != nil {
					t.Fatal(err)
				}
				if body.Text != strings.TrimSpace(body.Text) {
					t.Fatal("not trimmed")
				}
				w.WriteHeader(200)
			})
			rec := httptest.NewRecorder()
			CellComments("/api/v1")(next).ServeHTTP(rec, req)
			if rec.Code != tc.status {
				t.Fatalf("status=%d want=%d %s", rec.Code, tc.status, rec.Body)
			}
		})
	}
}

func TestCellCommentsBodyLimit(t *testing.T) {
	for _, authenticated := range []bool{false, true} {
		for _, chunked := range []bool{false, true} {
			req := httptest.NewRequest("POST", "/api/v1/notebooks/no/content/cells/cell-1/comments", strings.NewReader(strings.Repeat("x", MaxRequestBodyBytes+1)))
			req.Header.Set("Content-Type", "application/json")
			if authenticated {
				req = req.WithContext(principal.WithUserID(req.Context(), 1))
			}
			if chunked {
				req.ContentLength = -1
			}
			rec := httptest.NewRecorder()
			CellComments("/api/v1")(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("oversized body reached next") })).ServeHTTP(rec, req)
			want := 401
			if authenticated {
				want = 413
			}
			if rec.Code != want {
				t.Fatalf("got %d want %d", rec.Code, want)
			}
		}
	}
}
