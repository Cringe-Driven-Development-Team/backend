package cursor

import (
	"encoding/base64"
	"strings"
	"testing"
	"time"

	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/notebook/usecase"
)

func TestCommentCursorRoundTrip(t *testing.T) {
	c := NewCommentCodec([]byte("secret"))
	p := usecase.CommentPosition{ID: 9007199254740993, CreatedAt: time.Date(2026, 11, 22, 12, 0, 0, 123456000, time.UTC)}
	token, err := c.Encode(7, "cell-1", p)
	if err != nil {
		t.Fatal(err)
	}
	got, err := c.Decode(token, 7, "cell-1")
	if err != nil || got.ID != p.ID || !got.CreatedAt.Equal(p.CreatedAt) {
		t.Fatalf("round trip: %+v %v", got, err)
	}
	for _, tc := range []struct {
		token string
		book  int64
		cell  string
	}{{token, 8, "cell-1"}, {token, 7, "cell-2"}, {"", 7, "cell-1"}, {strings.Repeat("a", 2049), 7, "cell-1"}, {token + "x", 7, "cell-1"}} {
		if _, err = c.Decode(tc.token, tc.book, tc.cell); err == nil {
			t.Fatal("accepted invalid cursor")
		}
	}
	if _, err = NewCommentCodec([]byte("other")).Decode(token, 7, "cell-1"); err == nil {
		t.Fatal("accepted different key")
	}
	old, err := New([]byte("secret")).Encode(7, "", usecase.ListPosition{ID: 1, UpdatedAt: p.CreatedAt})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = c.Decode(old, 7, "cell-1"); err == nil {
		t.Fatal("accepted notebook cursor")
	}
}

func TestCommentCursorRejectsSignedInvalidPayload(t *testing.T) {
	c := NewCommentCodec([]byte("secret"))
	for _, body := range []string{`null`, `{}`, `{"v":2,"notebook_id":7,"cell_id":"cell-1","id":1,"created_at":"2026-11-22T12:00:00Z"}`, `{"v":1,"notebook_id":7,"cell_id":"cell-1","id":9223372036854775808,"created_at":"2026-11-22T12:00:00Z"}`, `{"v":1,"notebook_id":7,"cell_id":"cell-1","id":1,"created_at":"0001-01-01T00:00:00Z"}`} {
		token := base64.RawURLEncoding.EncodeToString([]byte(body)) + "." + base64.RawURLEncoding.EncodeToString(c.signature([]byte(body)))
		if _, err := c.Decode(token, 7, "cell-1"); err == nil {
			t.Fatalf("accepted %s", body)
		}
	}
}

func TestCommentCursorRejectsNonCanonicalBoundary(t *testing.T) {
	c := NewCommentCodec([]byte("secret"))
	stamp := time.Date(2026, 11, 22, 12, 0, 0, 123456000, time.UTC)
	token, err := c.Encode(7, "cell", usecase.CommentPosition{CreatedAt: stamp, ID: 1})
	if err != nil {
		t.Fatal(err)
	}
	for _, space := range []string{"\r", "\n"} {
		if _, err = c.Decode(token[:5]+space+token[5:], 7, "cell"); err == nil {
			t.Error("accepted newline in base64url")
		}
	}
	if _, err = c.Encode(7, "cell", usecase.CommentPosition{CreatedAt: stamp.Add(time.Nanosecond), ID: 1}); err == nil {
		t.Error("accepted sub-microsecond boundary")
	}
	body := []byte(`{"v":1,"notebook_id":7,"cell_id":"cell","created_at":"2026-11-22T12:00:00.123456789Z","id":1}`)
	token = base64.RawURLEncoding.EncodeToString(body) + "." + base64.RawURLEncoding.EncodeToString(c.signature(body))
	if _, err = c.Decode(token, 7, "cell"); err == nil {
		t.Error("accepted signed sub-microsecond boundary")
	}
}
