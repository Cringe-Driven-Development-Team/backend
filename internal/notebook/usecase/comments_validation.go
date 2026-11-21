package usecase

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

var commentCellID = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)

// NormalizeCommentText проверяет Unicode и возвращает текст без внешних пробелов.
// Ограничение 1–5000 измеряется в Unicode code points, не в байтах.
func NormalizeCommentText(text string) (string, error) {
	if !utf8.ValidString(text) || strings.ContainsRune(text, 0) {
		return "", ErrInvalidComment
	}
	text = strings.TrimSpace(text)
	if n := utf8.RuneCountInString(text); n < 1 || n > 5000 {
		return "", ErrInvalidComment
	}
	return text, nil
}

// ValidateCommentWall проверяет положительный ID блокнота и стабильный ID ячейки.
func ValidateCommentWall(notebookID int64, cellID string) error {
	if notebookID <= 0 || !commentCellID.MatchString(cellID) {
		return ErrInvalidComment
	}
	return nil
}
