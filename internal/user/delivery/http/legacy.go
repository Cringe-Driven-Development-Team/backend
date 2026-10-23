package delivery

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/transport/http/api"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/transport/http/cookies"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/transport/http/httperr"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/transport/http/security"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/user/usecase"
	"github.com/gorilla/mux"
)

// LegacyHandler is a temporary browser adapter, sharing the BFF usecases.
// It is mounted only in legacy mode; none of its cookies enter the BFF contract.
type LegacyHandler struct {
	uc     usecase.Usecase
	cookie cookies.SessionCookies
	guard  *security.CSRFGuard
}

// NewHandler retains the legacy construction API during the cutover.
func NewHandler(uc usecase.Usecase, cfg cookies.Config, signer *security.CSRFSigner) *Handler {
	return &Handler{uc: uc, legacy: &LegacyHandler{uc: uc, cookie: cookies.SessionCookies{Config: cfg, Signer: signer}, guard: security.NewCSRFGuard(signer, NewRefreshResolver(uc))}}
}

// RegisterLegacyRoutes must precede generated routes in the mux. The caller
// supplies the frozen-contract validator; the outer pipeline still enforces CSRF.
func (h *Handler) RegisterLegacyRoutes(r *mux.Router, prefix string, validate func(http.Handler) http.Handler) {
	if h.legacy == nil {
		return
	}
	for _, operation := range []string{"register", "login", "refresh", "logout"} {
		handler := http.Handler(http.HandlerFunc(h.legacy.serveAuth))
		if validate != nil {
			handler = validate(handler)
		}
		if operation == "refresh" || operation == "logout" {
			next := handler
			handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// Match the former generated cookie binding: missing refresh is
				// authentication failure, before OpenAPI would classify it as 400.
				// The outer CSRF pipeline still runs before this route handler.
				cookie, err := r.Cookie("refresh_token")
				if err != nil || cookie.Value == "" {
					httperr.Write(w, http.StatusUnauthorized, api.Unauthorized, "refresh token is missing")
					return
				}
				next.ServeHTTP(w, r)
			})
		}
		r.Handle(prefix+"/auth/"+operation, handler).Methods(http.MethodPost)
	}
}
func (h *LegacyHandler) serveAuth(w http.ResponseWriter, r *http.Request) {
	operation := r.URL.Path[strings.LastIndexByte(r.URL.Path, '/')+1:]
	switch operation {
	case "register", "login":
		var body api.Credentials
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			httperr.RequestError(w, r, fmt.Errorf("can't decode JSON body: %w", err))
			return
		}
		var sess usecase.Session
		var user api.User
		var err error
		if operation == "register" {
			u, s, e := h.uc.Register(r.Context(), body.Login, body.Password)
			user, sess, err = toAPIUser(u), s, e
		} else {
			u, s, e := h.uc.Login(r.Context(), body.Login, body.Password)
			user, sess, err = toAPIUser(u), s, e
		}
		if err != nil {
			h.writeError(w, r, err)
			return
		}
		for _, c := range h.cookie.Session(sess.AccessToken, sess.RefreshToken, sess.UserID, sess.ExpiresAt) {
			http.SetCookie(w, c)
		}
		w.Header().Set("Content-Type", "application/json")
		status := http.StatusOK
		if operation == "register" {
			status = http.StatusCreated
		}
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(user)
	case "refresh", "logout":
		c, err := r.Cookie("refresh_token")
		if err != nil || c.Value == "" {
			httperr.Write(w, 401, api.Unauthorized, "refresh token is missing")
			return
		}
		var csrf *string
		if values, ok := r.Header[http.CanonicalHeaderKey(security.CSRFHeaderName)]; ok && len(values) > 0 {
			csrf = &values[0]
		}
		if err := h.guard.CheckRefresh(r.Context(), c.Value, csrf); err != nil {
			h.writeError(w, r, err)
			return
		}
		if operation == "refresh" {
			sess, err := h.uc.Refresh(r.Context(), c.Value)
			if err != nil {
				h.writeError(w, r, err)
				return
			}
			for _, cookie := range h.cookie.Session(sess.AccessToken, sess.RefreshToken, sess.UserID, sess.ExpiresAt) {
				http.SetCookie(w, cookie)
			}
		} else {
			if err := h.uc.Logout(r.Context(), c.Value); err != nil {
				h.writeError(w, r, err)
				return
			}
			for _, cookie := range h.cookie.Clear() {
				http.SetCookie(w, cookie)
			}
		}
		w.WriteHeader(http.StatusNoContent)
	}
}
func (h *LegacyHandler) writeError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, usecase.ErrLoginTaken):
		httperr.Write(w, 409, api.LoginTaken, "login is already taken")
	case errors.Is(err, usecase.ErrPasswordTooLong):
		httperr.Write(w, 400, api.ValidationError, err.Error())
	case errors.Is(err, usecase.ErrInvalidCredentials):
		httperr.Write(w, 401, api.InvalidCredentials, "invalid login or password")
	case errors.Is(err, usecase.ErrInvalidRefresh), errors.Is(err, security.ErrInvalidRefresh):
		httperr.Write(w, 401, api.Unauthorized, err.Error())
	case errors.Is(err, security.ErrCSRFInvalid):
		httperr.Write(w, 403, api.CsrfInvalid, err.Error())
	default:
		httperr.ResponseError(w, r, err)
	}
}
