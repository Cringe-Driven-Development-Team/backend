package usecase

import (
	"context"

	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/notebook/domain"
)

// WithCommentCursor подключает подпись страниц, не меняя остальные операции блокнота.
func WithCommentCursor(codec CommentCursor) Option {
	return func(uc *NotebookUsecase) { uc.commentCursor = codec }
}

// commentForVisitor вычисляет разрешения после проверки текущего доступа в репозитории.
func commentForVisitor(comment domain.Comment, ownerID, visitorID int64) domain.Comment {
	comment.CanEdit = visitorID > 0 && visitorID == comment.Author.ID
	comment.CanDelete = visitorID > 0 && (visitorID == comment.Author.ID || visitorID == ownerID)
	return comment
}

// ListComments возвращает страницу новых сообщений перед более старыми.
// Отсутствующий курсор означает начало; userID=0 обозначает анонимного посетителя.
func (uc *NotebookUsecase) ListComments(ctx context.Context, notebookID int64, cellID string, visitorID int64, input CommentListInput) (CommentPage, error) {
	if err := ValidateCommentWall(notebookID, cellID); err != nil {
		return CommentPage{}, err
	}
	limit := 20
	if input.Limit != nil {
		limit = *input.Limit
	}
	if limit < 1 || limit > 100 {
		return CommentPage{}, ErrInvalidCommentList
	}
	if uc.commentCursor == nil {
		return CommentPage{}, ErrCommentCursorUnavailable
	}
	query := CommentQuery{Limit: limit}
	if input.Cursor != nil {
		p, err := uc.commentCursor.Decode(*input.Cursor, notebookID, cellID)
		if err != nil {
			return CommentPage{}, err
		}
		query.After = &p
	}
	rows, err := uc.repo.ListComments(ctx, notebookID, cellID, visitorID, query)
	if err != nil {
		return CommentPage{}, err
	}
	page := CommentPage{Items: rows.Items}
	if len(page.Items) > limit {
		page.Items = page.Items[:limit]
		last := page.Items[len(page.Items)-1]
		token, e := uc.commentCursor.Encode(notebookID, cellID, CommentPosition{CreatedAt: last.CreatedAt, ID: last.ID})
		if e != nil {
			return CommentPage{}, e
		}
		page.NextCursor = &token
	}
	if page.Items == nil {
		page.Items = []domain.Comment{}
	}
	for i, c := range page.Items {
		page.Items[i] = commentForVisitor(c, rows.OwnerID, visitorID)
	}
	return page, nil
}

// CreateComment создаёт сообщение текущего пользователя без изменения документа.
func (uc *NotebookUsecase) CreateComment(ctx context.Context, notebookID int64, cellID string, visitorID int64, text string) (domain.Comment, error) {
	if err := ValidateCommentWall(notebookID, cellID); err != nil {
		return domain.Comment{}, err
	}
	text, err := NormalizeCommentText(text)
	if err != nil {
		return domain.Comment{}, err
	}
	record, err := uc.repo.CreateComment(ctx, notebookID, cellID, visitorID, text)
	if err != nil {
		return domain.Comment{}, err
	}
	return commentForVisitor(record.Comment, record.OwnerID, visitorID), nil
}

// UpdateComment изменяет только текст собственного сообщения с текущим доступом.
func (uc *NotebookUsecase) UpdateComment(ctx context.Context, notebookID int64, cellID string, visitorID, commentID int64, text string) (domain.Comment, error) {
	if err := ValidateCommentWall(notebookID, cellID); err != nil {
		return domain.Comment{}, err
	}
	if commentID <= 0 {
		return domain.Comment{}, ErrInvalidComment
	}
	text, err := NormalizeCommentText(text)
	if err != nil {
		return domain.Comment{}, err
	}
	record, err := uc.repo.UpdateComment(ctx, notebookID, cellID, visitorID, commentID, text)
	if err != nil {
		return domain.Comment{}, err
	}
	return commentForVisitor(record.Comment, record.OwnerID, visitorID), nil
}

// DeleteComment удаляет сообщение; авторство и модерацию проверяет транзакция.
func (uc *NotebookUsecase) DeleteComment(ctx context.Context, notebookID int64, cellID string, visitorID, commentID int64) error {
	if err := ValidateCommentWall(notebookID, cellID); err != nil {
		return err
	}
	if commentID <= 0 {
		return ErrInvalidComment
	}
	return uc.repo.DeleteComment(ctx, notebookID, cellID, visitorID, commentID)
}
