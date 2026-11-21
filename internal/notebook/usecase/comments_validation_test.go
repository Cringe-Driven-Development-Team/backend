package usecase

import (
	"errors"
	"strings"
	"testing"
)

func TestNormalizeCommentText(t *testing.T) {
	for _, tc := range []struct {
		input, want string
		valid       bool
	}{
		{"  я😀\n x  ", "я😀\n x", true}, {"x", "x", true}, {strings.Repeat("😀", 5000), strings.Repeat("😀", 5000), true},
		{" <script>\n & </script> ", "<script>\n & </script>", true}, {strings.Repeat("a", 5001), "", false},
		{"", "", false}, {" \n\t ", "", false}, {"a\x00b", "", false}, {string([]byte{0xff}), "", false},
	} {
		got, err := NormalizeCommentText(tc.input)
		if tc.valid {
			if err != nil || got != tc.want {
				t.Fatalf("normalization: %q %v", got, err)
			}
		} else if !errors.Is(err, ErrInvalidComment) {
			t.Fatalf("accepted invalid string: %v", err)
		}
	}
}

func TestValidateCommentWall(t *testing.T) {
	for _, cell := range []string{"", "a/b", strings.Repeat("x", 65), "a\x00b"} {
		if ValidateCommentWall(1, cell) == nil {
			t.Fatalf("accepted %q", cell)
		}
	}
	if ValidateCommentWall(0, "cell-1") == nil || ValidateCommentWall(-1, "cell-1") == nil || ValidateCommentWall(1, "cell-1_") != nil {
		t.Fatal("invalid wall validation")
	}
}
