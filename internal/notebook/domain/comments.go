package domain

import "time"

// CommentAuthor содержит стабильный ID автора и его текущий логин.
type CommentAuthor struct {
	ID    int64
	Login string
}

// Comment хранит сообщение стены ячейки и права текущего посетителя.
type Comment struct {
	ID, NotebookID       int64
	CellID               string
	Author               CommentAuthor
	Text                 string
	CreatedAt, UpdatedAt time.Time
	CanEdit, CanDelete   bool
}
