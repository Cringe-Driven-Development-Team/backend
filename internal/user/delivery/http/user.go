// Package delivery содержит HTTP-ручки пользователя. Они переводят ошибки
// usecase в ответы контракта и выдают токены в HttpOnly-cookie.
package delivery

import (
	"context"
	"errors"

	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/transport/http/api"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/transport/http/cookies"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/transport/http/principal"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/transport/http/security"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/user/domain"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/user/usecase"
)

type Handler struct {
	uc     usecase.Usecase
	cookie cookies.SessionCookies
	guard  *security.CSRFGuard
}

func NewHandler(uc usecase.Usecase, config cookies.Config, csrf *security.CSRFSigner) *Handler {
	return &Handler{uc: uc, cookie: cookies.SessionCookies{Config: config, Signer: csrf}, guard: security.NewCSRFGuard(csrf, NewRefreshResolver(uc))}
}

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
	cookies := h.sessionCookies(sess)
	return registerResponse{api.RegisterUser201JSONResponse{Body: toAPIUser(u)}, cookies}, nil
}

func (h *Handler) LoginUser(ctx context.Context, req api.LoginUserRequestObject) (api.LoginUserResponseObject, error) {
	u, sess, err := h.uc.Login(ctx, req.Body.Login, req.Body.Password)
	switch {
	case errors.Is(err, usecase.ErrInvalidCredentials):
		return api.LoginUser401JSONResponse{Code: api.InvalidCredentials, Message: "invalid login or password"}, nil
	case err != nil:
		return nil, err
	}
	cookies := h.sessionCookies(sess)
	return loginResponse{api.LoginUser200JSONResponse{Body: toAPIUser(u)}, cookies}, nil
}

func (h *Handler) RefreshToken(ctx context.Context, req api.RefreshTokenRequestObject) (api.RefreshTokenResponseObject, error) {
	if err := h.guard.CheckRefresh(ctx, req.Params.RefreshToken, req.Params.XCSRFToken); err != nil {
		switch {
		case errors.Is(err, usecase.ErrInvalidRefresh), errors.Is(err, security.ErrInvalidRefresh):
			return api.RefreshToken401JSONResponse{Code: api.Unauthorized, Message: err.Error()}, nil
		case errors.Is(err, security.ErrCSRFInvalid):
			return api.RefreshToken403JSONResponse{ForbiddenJSONResponse: api.ForbiddenJSONResponse{Code: api.CsrfInvalid, Message: err.Error()}}, nil
		default:
			return nil, err
		}
	}
	sess, err := h.uc.Refresh(ctx, req.Params.RefreshToken)
	switch {
	case errors.Is(err, usecase.ErrInvalidRefresh), errors.Is(err, security.ErrInvalidRefresh):
		return api.RefreshToken401JSONResponse{Code: api.Unauthorized, Message: err.Error()}, nil
	case err != nil:
		return nil, err
	}
	cookies := h.sessionCookies(sess)
	return refreshResponse{api.RefreshToken204Response{}, cookies}, nil
}

func (h *Handler) LogoutUser(ctx context.Context, req api.LogoutUserRequestObject) (api.LogoutUserResponseObject, error) {
	if err := h.guard.CheckRefresh(ctx, req.Params.RefreshToken, req.Params.XCSRFToken); err != nil {
		switch {
		case errors.Is(err, usecase.ErrInvalidRefresh), errors.Is(err, security.ErrInvalidRefresh):
			return api.LogoutUser401JSONResponse{Code: api.Unauthorized, Message: err.Error()}, nil
		case errors.Is(err, security.ErrCSRFInvalid):
			return api.LogoutUser403JSONResponse{ForbiddenJSONResponse: api.ForbiddenJSONResponse{Code: api.CsrfInvalid, Message: err.Error()}}, nil
		default:
			return nil, err
		}
	}
	err := h.uc.Logout(ctx, req.Params.RefreshToken)
	switch {
	case errors.Is(err, usecase.ErrInvalidRefresh), errors.Is(err, security.ErrInvalidRefresh):
		return api.LogoutUser401JSONResponse{Code: api.Unauthorized, Message: err.Error()}, nil
	case err != nil:
		return nil, err
	}
	cookies := responseCookies(h.cookie.Clear())
	return logoutResponse{api.LogoutUser204Response{}, cookies}, nil
}

func (h *Handler) GetCurrentUser(ctx context.Context, _ api.GetCurrentUserRequestObject) (api.GetCurrentUserResponseObject, error) {
	// Без пользователя в контексте сюда не попасть: Validator отвечает 401 раньше.
	id, _ := principal.UserIDFromContext(ctx)
	u, err := h.uc.CurrentUser(ctx, id)
	switch {
	case errors.Is(err, usecase.ErrUserNotFound):
		// Токен ещё жив, а пользователя уже удалили.
		return api.GetCurrentUser401JSONResponse{Code: api.Unauthorized, Message: "user no longer exists"}, nil
	case err != nil:
		return nil, err
	}
	return api.GetCurrentUser200JSONResponse(toAPIUser(u)), nil
}

func (h *Handler) sessionCookies(sess usecase.Session) responseCookies {
	return responseCookies(h.cookie.Session(sess.AccessToken, sess.RefreshToken, sess.UserID, sess.ExpiresAt))
}

func toAPIUser(u domain.User) api.User {
	return api.User{Id: u.ID, Login: u.Login}
}
