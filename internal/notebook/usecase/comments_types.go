package usecase

import (
	"context"
	"errors"
	"time"

	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/notebook/domain"
)

var (
	ErrInvalidComment           = errors.New("invalid comment")
	ErrInvalidCommentList       = errors.New("invalid comment page parameters")
	ErrCommentNotFound          = errors.New("comment not found")
	ErrCommentForbidden         = errors.New("comment action forbidden")
	ErrCommentCursorUnavailable = errors.New("comment cursor is not configured")
)

// CommentListInput задаёт размер страницы и необязательный курсор.
type CommentListInput struct {
	Limit  *int
	Cursor *string
}

// CommentPosition задаёт устойчивую границу сортировки стены.
type CommentPosition struct {
	CreatedAt time.Time
	ID        int64
}

// CommentQuery содержит проверенные параметры чтения из БД.
type CommentQuery struct {
	Limit int
	After *CommentPosition
}

// CommentRows возвращает страницу и владельца из одной транзакции.
type CommentRows struct {
	OwnerID int64
	Items   []domain.Comment
}

// CommentRecord возвращает записанное сообщение и владельца блокнота.
type CommentRecord struct {
	OwnerID int64
	Comment domain.Comment
}

// CommentPage содержит сообщения и курсор следующей страницы.
type CommentPage struct {
	Items      []domain.Comment
	NextCursor *string
}

// CommentCursor подписывает позицию и проверяет привязку к стене.
type CommentCursor interface {
	Encode(notebookID int64, cellID string, position CommentPosition) (string, error)
	Decode(token string, notebookID int64, cellID string) (CommentPosition, error)
}

// CommentsRepository атомарно проверяет доступ и выполняет операции стены.
type CommentsRepository interface {
	ListComments(ctx context.Context, notebookID int64, cellID string, visitorID int64, query CommentQuery) (CommentRows, error)
	CreateComment(ctx context.Context, notebookID int64, cellID string, visitorID int64, text string) (CommentRecord, error)
	UpdateComment(ctx context.Context, notebookID int64, cellID string, visitorID, commentID int64, text string) (CommentRecord, error)
	DeleteComment(ctx context.Context, notebookID int64, cellID string, visitorID, commentID int64) error
}

// Comments задаёт прикладные операции стены доступной ячейки.
type Comments interface {
	ListComments(ctx context.Context, notebookID int64, cellID string, visitorID int64, input CommentListInput) (CommentPage, error)
	CreateComment(ctx context.Context, notebookID int64, cellID string, visitorID int64, text string) (domain.Comment, error)
	UpdateComment(ctx context.Context, notebookID int64, cellID string, visitorID, commentID int64, text string) (domain.Comment, error)
	DeleteComment(ctx context.Context, notebookID int64, cellID string, visitorID, commentID int64) error
}
