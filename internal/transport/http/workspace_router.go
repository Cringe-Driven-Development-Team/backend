package httptransport

import (
	"errors"
	"net/http"
	"strings"

	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/authn"
	notebookusecase "github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/notebook/usecase"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/transport/http/api"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/transport/http/httperr"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/transport/http/middleware"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/transport/http/security"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/transport/http/workspaceapi"
	"github.com/gorilla/mux"
)

// WorkspaceHandlers объединяет обработчики одной области владения.
type WorkspaceHandlers struct {
	*NotebookHandler
	*RuntimeHandler
	Files http.Handler
}

// NewWorkspaceRouter собирает HTTP API Workspace. HTTPS listener проверяет сертификат BFF до маршрутизации.
func NewWorkspaceRouter(handlers WorkspaceHandlers, verifier authn.SessionVerifier, bffKey string, observe middleware.HTTPObserver) (http.Handler, error) {
	if strings.TrimSpace(bffKey) == "" || verifier == nil || handlers.NotebookHandler == nil || handlers.RuntimeHandler == nil {
		return nil, errors.New("workspace handlers, session verifier and BFF key are required")
	}
	spec, err := workspaceapi.GetSpec()
	if err != nil {
		return nil, err
	}
	router := mux.NewRouter().UseEncodedPath()
	router.Use(middleware.RecordRoute, decodePathVariables)
	router.NotFoundHandler = http.HandlerFunc(httperr.NotFound)
	router.MethodNotAllowedHandler = http.HandlerFunc(httperr.MethodNotAllowed)
	files := handlers.Files
	if files == nil {
		files = http.HandlerFunc(handlers.Workspace)
	}
	workspaceFiles(router, files)
	router.HandleFunc(BaseURL+"/notebooks/{id}/export", handlers.NotebookHandler.ExportNotebook).Methods(http.MethodGet)
	strict := workspaceapi.NewStrictHandlerWithOptions(workspaceAdapter{server: handlers}, nil, workspaceapi.StrictHTTPServerOptions{
		RequestErrorHandlerFunc: httperr.RequestError, ResponseErrorHandlerFunc: httperr.ResponseError,
	})
	workspaceapi.HandlerWithOptions(strict, workspaceapi.GorillaServerOptions{
		BaseURL: BaseURL, BaseRouter: router,
		Middlewares:      []workspaceapi.MiddlewareFunc{middleware.Validator(spec, BaseURL)},
		ErrorHandlerFunc: httperr.RequestError,
	})
	bounded := workspaceRequestLimit(middleware.BodyLimit(router, notebookusecase.MaxSourceBytes, BaseURL+"/notebooks"))
	handler := security.S2S(bffKey, BaseURL)(security.AuthenticateWorkspace(verifier)(
		bulkNotebookTransfers(middleware.CellComments(BaseURL)(middleware.NotebookImport(BaseURL)(bounded))),
	))
	return middleware.RequestID(middleware.Observe(middleware.Recover(middleware.NotebookCachePolicy(BaseURL)(handler)), observe)), nil
}

// workspaceFiles регистрирует только разрешённые потоковые маршруты и сохраняет legacy tail для delivery.
func workspaceFiles(router *mux.Router, handler http.Handler) {
	const prefix = BaseURL + "/runtime-sessions/{id}/workspace/"
	for _, route := range []struct{ method, path string }{
		{http.MethodGet, "check"},
		{http.MethodPost, "command"},
		{http.MethodGet, "events"},
		{http.MethodDelete, "socket"},
		{http.MethodPost, "transfers/{transfer_id}/chunks"},
		{http.MethodGet, "transfers/{transfer_id}/download"},
	} {
		router.HandleFunc(prefix+route.path, func(w http.ResponseWriter, r *http.Request) {
			vars := mux.Vars(r)
			vars["file_path"] = strings.Replace(route.path, "{transfer_id}", vars["transfer_id"], 1)
			handler.ServeHTTP(w, r)
		}).Methods(route.method)
	}
}

// bulkNotebookTransfers ограничивает число временных снимков и больших импортов процесса.
func bulkNotebookTransfers(next http.Handler) http.Handler {
	limit := make(chan struct{}, 2)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		larger := r.Method == http.MethodPost && path == BaseURL+"/notebooks/import" ||
			r.Method == http.MethodGet && strings.HasPrefix(path, BaseURL+"/notebooks/") && strings.HasSuffix(path, "/export")
		if larger {
			select {
			case limit <- struct{}{}:
				defer func() { <-limit }()
			default:
				httperr.Write(w, http.StatusTooManyRequests, api.ErrorCodeRateLimited, "notebook transfer limit reached")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// workspaceRequestLimit ограничивает JSON runtime отдельно от chunk и multipart transport.
func workspaceRequestLimit(next http.Handler) http.Handler {
	const limit = 2 << 20
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, BaseURL+"/runtime-sessions/") && !strings.Contains(r.URL.Path, "/workspace/") && r.Body != nil {
			if r.ContentLength > limit {
				httperr.Write(w, http.StatusRequestEntityTooLarge, api.ErrorCodeValidationError, "request body too large")
				return
			}
			r.Body = http.MaxBytesReader(w, r.Body, limit)
		}
		next.ServeHTTP(w, r)
	})
}
