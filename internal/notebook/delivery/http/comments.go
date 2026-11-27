package delivery

import (
	"context"
	"errors"

	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/notebook/domain"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/notebook/usecase"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/transport/http/api"
)

// toAPIComment возвращает только публичные поля с точными UTC-датами.
func toAPIComment(c domain.Comment) api.Comment {
	var avatarURL *string
	if c.Author.AvatarURL != "" {
		avatarURL = &c.Author.AvatarURL
	}
	return api.Comment{Id: c.ID, NotebookId: c.NotebookID, CellId: c.CellID, Author: api.CommentAuthor{Id: c.Author.ID, Login: c.Author.Login, AvatarUrl: avatarURL}, Text: c.Text, CreatedAt: c.CreatedAt.UTC(), UpdatedAt: c.UpdatedAt.UTC(), CanEdit: c.CanEdit, CanDelete: c.CanDelete}
}

// commentError преобразует прикладной отказ в безопасную контрактную ошибку.
func commentError(err error) (int, api.CommentError) {
	switch {
	case errors.Is(err, usecase.ErrInvalidComment), errors.Is(err, usecase.ErrInvalidCommentList):
		return 400, api.CommentError{Code: api.CommentErrorCodeValidationError, Message: "invalid comment request"}
	case errors.Is(err, usecase.ErrNotebookNotFound), errors.Is(err, usecase.ErrCellNotFound), errors.Is(err, usecase.ErrCommentNotFound):
		return 404, api.CommentError{Code: api.CommentErrorCodeNotFound, Message: "not found"}
	case errors.Is(err, usecase.ErrCommentForbidden):
		return 403, api.CommentError{Code: api.CommentErrorCodeForbidden, Message: "comment action forbidden"}
	default:
		return 0, api.CommentError{}
	}
}

// ListCellComments выполняет операцию стены для текущего посетителя.
func (h *Handler) ListCellComments(ctx context.Context, req api.ListCellCommentsRequestObject) (api.ListCellCommentsResponseObject, error) {
	page, err := h.uc.ListComments(ctx, req.Id, req.CellId, userID(ctx), usecase.CommentListInput{Limit: req.Params.Limit, Cursor: req.Params.Cursor})
	if err != nil {
		status, body := commentError(err)
		switch status {
		case 400:
			return api.ListCellComments400JSONResponse{Body: body, Headers: api.ListCellComments400ResponseHeaders{CacheControl: noStore()}}, nil
		case 403:
			return api.ListCellComments403JSONResponse{Body: body, Headers: api.ListCellComments403ResponseHeaders{CacheControl: noStore()}}, nil
		case 404:
			return api.ListCellComments404JSONResponse{Body: body, Headers: api.ListCellComments404ResponseHeaders{CacheControl: noStore()}}, nil
		default:
			return nil, err
		}
	}
	items := make([]api.Comment, 0, len(page.Items))
	for _, c := range page.Items {
		items = append(items, toAPIComment(c))
	}
	return api.ListCellComments200JSONResponse{Body: api.CommentPage{Items: items, NextCursor: page.NextCursor}, Headers: api.ListCellComments200ResponseHeaders{CacheControl: noStore()}}, nil
}

// CreateCellComment выполняет операцию стены для текущего посетителя.
func (h *Handler) CreateCellComment(ctx context.Context, req api.CreateCellCommentRequestObject) (api.CreateCellCommentResponseObject, error) {
	comment, err := h.uc.CreateComment(ctx, req.Id, req.CellId, userID(ctx), req.Body.Text)
	if err != nil {
		status, body := commentError(err)
		switch status {
		case 400:
			return api.CreateCellComment400JSONResponse{Body: body, Headers: api.CreateCellComment400ResponseHeaders{CacheControl: noStore()}}, nil
		case 403:
			return api.CreateCellComment403JSONResponse{Body: body, Headers: api.CreateCellComment403ResponseHeaders{CacheControl: noStore()}}, nil
		case 404:
			return api.CreateCellComment404JSONResponse{Body: body, Headers: api.CreateCellComment404ResponseHeaders{CacheControl: noStore()}}, nil
		default:
			return nil, err
		}
	}
	return api.CreateCellComment201JSONResponse{Body: toAPIComment(comment), Headers: api.CreateCellComment201ResponseHeaders{CacheControl: noStore()}}, nil
}

// UpdateCellComment выполняет операцию стены для текущего посетителя.
func (h *Handler) UpdateCellComment(ctx context.Context, req api.UpdateCellCommentRequestObject) (api.UpdateCellCommentResponseObject, error) {
	comment, err := h.uc.UpdateComment(ctx, req.Id, req.CellId, userID(ctx), req.CommentId, req.Body.Text)
	if err != nil {
		status, body := commentError(err)
		switch status {
		case 400:
			return api.UpdateCellComment400JSONResponse{Body: body, Headers: api.UpdateCellComment400ResponseHeaders{CacheControl: noStore()}}, nil
		case 403:
			return api.UpdateCellComment403JSONResponse{Body: body, Headers: api.UpdateCellComment403ResponseHeaders{CacheControl: noStore()}}, nil
		case 404:
			return api.UpdateCellComment404JSONResponse{Body: body, Headers: api.UpdateCellComment404ResponseHeaders{CacheControl: noStore()}}, nil
		default:
			return nil, err
		}
	}
	return api.UpdateCellComment200JSONResponse{Body: toAPIComment(comment), Headers: api.UpdateCellComment200ResponseHeaders{CacheControl: noStore()}}, nil
}

// DeleteCellComment выполняет операцию стены для текущего посетителя.
func (h *Handler) DeleteCellComment(ctx context.Context, req api.DeleteCellCommentRequestObject) (api.DeleteCellCommentResponseObject, error) {
	err := h.uc.DeleteComment(ctx, req.Id, req.CellId, userID(ctx), req.CommentId)
	if err != nil {
		status, body := commentError(err)
		switch status {
		case 400:
			return api.DeleteCellComment400JSONResponse{Body: body, Headers: api.DeleteCellComment400ResponseHeaders{CacheControl: noStore()}}, nil
		case 403:
			return api.DeleteCellComment403JSONResponse{Body: body, Headers: api.DeleteCellComment403ResponseHeaders{CacheControl: noStore()}}, nil
		case 404:
			return api.DeleteCellComment404JSONResponse{Body: body, Headers: api.DeleteCellComment404ResponseHeaders{CacheControl: noStore()}}, nil
		default:
			return nil, err
		}
	}
	return api.DeleteCellComment204Response{Headers: api.DeleteCellComment204ResponseHeaders{CacheControl: noStore()}}, nil
}
