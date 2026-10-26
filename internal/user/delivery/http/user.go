// Package delivery translates user usecases to the server-side HTTP contract.
package delivery

import (
	"context"
	"errors"
	"math"
	"time"

	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/transport/http/api"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/transport/http/principal"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/user/domain"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/user/usecase"
)

type Handler struct {
	uc usecase.Usecase
}

func NewHandler(uc usecase.Usecase) *Handler { return &Handler{uc: uc} }

func (h *Handler) RegisterUser(ctx context.Context, req api.RegisterUserRequestObject) (api.RegisterUserResponseObject, error) {
	u, sess, err := h.uc.Register(ctx, req.Body.Login, req.Body.Password)
	switch {
	case errors.Is(err, usecase.ErrLoginTaken):
		return api.RegisterUser409JSONResponse{Code: api.LoginTaken, Message: "login is already taken"}, nil
	case errors.Is(err, usecase.ErrPasswordTooLong):
		return api.RegisterUser400JSONResponse{Code: api.ValidationError, Message: err.Error()}, nil
	case err != nil:
		return nil, err
	}
	pair, err := toTokenPair(sess)
	if err != nil {
		return nil, err
	}
	return api.RegisterUser201JSONResponse{User: toAPIUser(u), Tokens: pair}, nil
}
func (h *Handler) LoginUser(ctx context.Context, req api.LoginUserRequestObject) (api.LoginUserResponseObject, error) {
	u, sess, err := h.uc.Login(ctx, req.Body.Login, req.Body.Password)
	if errors.Is(err, usecase.ErrInvalidCredentials) {
		return api.LoginUser401JSONResponse{Code: api.InvalidCredentials, Message: "invalid login or password"}, nil
	}
	if err != nil {
		return nil, err
	}
	pair, err := toTokenPair(sess)
	if err != nil {
		return nil, err
	}
	return api.LoginUser200JSONResponse{User: toAPIUser(u), Tokens: pair}, nil
}
func (h *Handler) RefreshToken(ctx context.Context, req api.RefreshTokenRequestObject) (api.RefreshTokenResponseObject, error) {
	sess, err := h.uc.Refresh(ctx, req.Body.RefreshToken)
	if errors.Is(err, usecase.ErrInvalidRefresh) {
		return api.RefreshToken401JSONResponse{Code: api.Unauthorized, Message: "refresh token is invalid or expired"}, nil
	}
	if err != nil {
		return nil, err
	}
	pair, err := toTokenPair(sess)
	if err != nil {
		return nil, err
	}
	return api.RefreshToken200JSONResponse(pair), nil
}
func (h *Handler) LogoutUser(ctx context.Context, req api.LogoutUserRequestObject) (api.LogoutUserResponseObject, error) {
	err := h.uc.Logout(ctx, req.Body.RefreshToken)
	if errors.Is(err, usecase.ErrInvalidRefresh) {
		return api.LogoutUser401JSONResponse{Code: api.Unauthorized, Message: "refresh token is invalid or expired"}, nil
	}
	if err != nil {
		return nil, err
	}
	return api.LogoutUser204Response{}, nil
}
func (h *Handler) GetCurrentUser(ctx context.Context, _ api.GetCurrentUserRequestObject) (api.GetCurrentUserResponseObject, error) {
	id, _ := principal.UserIDFromContext(ctx)
	u, err := h.uc.CurrentUser(ctx, id)
	if errors.Is(err, usecase.ErrUserNotFound) {
		return api.GetCurrentUser401JSONResponse{Code: api.Unauthorized, Message: "user no longer exists"}, nil
	}
	if err != nil {
		return nil, err
	}
	return api.GetCurrentUser200JSONResponse(toAPIUser(u)), nil
}
func toTokenPair(sess usecase.Session) (api.TokenPair, error) {
	now := time.Now()
	access := int64(math.Ceil(sess.AccessExpiresAt.Sub(now).Seconds()))
	refresh := int64(math.Ceil(sess.ExpiresAt.Sub(now).Seconds()))
	if access < 1 || refresh < 1 {
		return api.TokenPair{}, errors.New("issued session has invalid lifetime")
	}
	return api.TokenPair{AccessToken: sess.AccessToken, RefreshToken: sess.RefreshToken, AccessExpiresIn: access, RefreshExpiresIn: refresh}, nil
}
func toAPIUser(u domain.User) api.User { return api.User{Id: u.ID, Login: u.Login} }
