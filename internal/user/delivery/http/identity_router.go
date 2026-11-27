package delivery

import (
	"errors"
	"net/http"
	"strings"

	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/transport/http/api"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/transport/http/httperr"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/transport/http/identityapi"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/transport/http/middleware"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/transport/http/security"
	"github.com/gorilla/mux"
)

// NewIdentityRouter обслуживает только Identity OpenAPI и проверяет S2S до тела запроса.
func NewIdentityRouter(h *Handler, tokens security.TokenParser, sessions security.AccessSessions, bffKey string) (http.Handler, error) {
	if strings.TrimSpace(bffKey) == "" {
		return nil, errors.New("BFF_API_KEY is required")
	}
	spec, err := identityapi.GetSpec()
	if err != nil {
		return nil, err
	}
	r := mux.NewRouter()
	r.Use(middleware.RecordRoute)
	r.NotFoundHandler = http.HandlerFunc(httperr.NotFound)
	r.MethodNotAllowedHandler = http.HandlerFunc(httperr.MethodNotAllowed)
	strict := identityapi.NewStrictHandlerWithOptions(identityAdapter{handler: h}, nil, identityapi.StrictHTTPServerOptions{RequestErrorHandlerFunc: httperr.RequestError, ResponseErrorHandlerFunc: httperr.ResponseError})
	identityapi.HandlerWithOptions(strict, identityapi.GorillaServerOptions{BaseURL: "/api/v1", BaseRouter: r, Middlewares: []identityapi.MiddlewareFunc{middleware.Validator(spec, "/api/v1")}, ErrorHandlerFunc: httperr.RequestError})
	bounded := http.HandlerFunc(func(w http.ResponseWriter, rq *http.Request) {
		if rq.URL.Path != "/api/v1/users/me/avatar" {
			if rq.ContentLength > middleware.MaxRequestBodyBytes {
				httperr.Write(w, http.StatusRequestEntityTooLarge, api.ErrorCodeValidationError, "request body too large")
				return
			}
			rq.Body = http.MaxBytesReader(w, rq.Body, middleware.MaxRequestBodyBytes)
		}
		r.ServeHTTP(w, rq)
	})
	handler := security.S2S(bffKey, "/api/v1")(security.AuthenticateBearer(tokens, sessions)(middleware.AvatarUpload("/api/v1")(bounded)))
	return middleware.RequestID(middleware.Logging(middleware.Recover(handler))), nil
}
