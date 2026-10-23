// Package httptransport собирает HTTP-конвейер приложения.
package httptransport

import (
	"fmt"
	"net/http"
	"time"

	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/transport/http/api"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/transport/http/httperr"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/transport/http/legacy"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/transport/http/middleware"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/transport/http/security"
	"github.com/gorilla/mux"
)

const BaseURL = "/api/v1"

type BFFOptions struct {
	Enabled     bool
	S2SRequired bool
	Keys        []string
}

func NewRouter(srv Server, tokens security.TokenParser, corsOrigins []string, csrf *security.CSRFSigner, csrfTTL time.Duration, refreshUsers security.RefreshUserResolver, appOrigin string, options ...BFFOptions) (http.Handler, error) {
	var bff BFFOptions
	if len(options) > 0 {
		bff = options[0]
	}
	spec, err := api.GetSpec()
	if !bff.Enabled {
		spec, err = legacy.GetSpec()
	}
	if err != nil {
		return nil, fmt.Errorf("load embedded spec: %w", err)
	}

	r := mux.NewRouter()
	r.Use(middleware.RecordRoute)
	r.NotFoundHandler = http.HandlerFunc(httperr.NotFound)
	r.MethodNotAllowedHandler = http.HandlerFunc(httperr.MethodNotAllowed)

	r.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		// Явно показываем линтеру и другим разработчикам, что игнорируем ошибку
		_, _ = w.Write([]byte(`{"status": "alive"}`))
	}).Methods(http.MethodGet)

	if !bff.Enabled {
		srv.RegisterLegacyRoutes(r, BaseURL, middleware.Validator(spec, BaseURL))
	}

	strict := api.NewStrictHandlerWithOptions(srv, nil, api.StrictHTTPServerOptions{
		RequestErrorHandlerFunc:  httperr.RequestError,
		ResponseErrorHandlerFunc: httperr.ResponseError,
	})
	api.HandlerWithOptions(strict, api.GorillaServerOptions{
		BaseURL:    BaseURL,
		BaseRouter: r,
		// Проверки S2S/Bearer или legacy CSRF/cookie выполняются до разбора параметров.
		Middlewares: []api.MiddlewareFunc{
			middleware.Validator(spec, BaseURL),
		},
		ErrorHandlerFunc: httperr.RequestError,
	})

	// Общие middleware оборачивают и ранние ответы CORS/CSRF, и ошибки маршрутизации.
	// Legacy: CORS → CSRF/Authenticate → Validator; BFF: S2S → Bearer → Validator.
	var handler http.Handler
	if bff.Enabled {
		handler = security.S2S(bff.S2SRequired, bff.Keys, BaseURL)(security.AuthenticateBearer(tokens)(r))
	} else {
		handler = middleware.CORS(corsOrigins)(security.Protect(tokens, csrf, csrfTTL, BaseURL, corsOrigins, refreshUsers, appOrigin)(r))
	}
	return middleware.RequestID(middleware.Logging(middleware.Recover(handler))), nil
}
