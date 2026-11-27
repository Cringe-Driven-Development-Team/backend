// Package httperr отдаёт ошибки в формате Error из контракта.
package httperr

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/authn"
	notebookdomain "github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/notebook/domain"
	notebookusecase "github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/notebook/usecase"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/observability"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/transport/http/api"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/transport/rpcerror"
)

// Write отправляет через w JSON-ошибку с HTTP status, кодом code и сообщением message.
func Write(w http.ResponseWriter, status int, code api.ErrorCode, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	body := struct {
		api.Error
		RequestID string `json:"request_id,omitempty"`
	}{Error: api.Error{Code: code, Message: message}, RequestID: w.Header().Get("X-Request-ID")}
	_ = json.NewEncoder(w).Encode(body)
}

// Internal пишет причину в лог, а клиенту отдаёт 500 без подробностей.
func Internal(w http.ResponseWriter, r *http.Request, err error) {
	slog.ErrorContext(r.Context(), "http.internal_error",
		slog.String("error_code", observability.ErrorCode(err)),
	)
	Write(w, http.StatusInternalServerError, api.ErrorCodeInternal, "internal server error")
}

// RequestError отвечает, когда сгенерированный код не смог разобрать запрос.
func RequestError(w http.ResponseWriter, _ *http.Request, err error) {
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		Write(w, http.StatusRequestEntityTooLarge, api.ErrorCodeValidationError, "request body too large")
		return
	}
	Write(w, http.StatusBadRequest, api.ErrorCodeValidationError, err.Error())
}

// ResponseError отвечает на ошибки, которые вернули хендлеры.
func ResponseError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, context.DeadlineExceeded) {
		Write(w, http.StatusGatewayTimeout, "deadline_exceeded", "operation deadline exceeded")
		return
	}
	if WriteRPC(w, err) {
		return
	}
	if errors.Is(err, authn.ErrInvalidSession) {
		Write(w, http.StatusUnauthorized, api.ErrorCodeUnauthorized, "authentication required")
		return
	}
	if errors.Is(err, notebookusecase.ErrUserDirectoryUnavailable) {
		Write(w, http.StatusServiceUnavailable, "dependency_unavailable", "user directory unavailable")
		return
	}
	if errors.Is(err, notebookdomain.ErrTooLarge) {
		Write(w, http.StatusRequestEntityTooLarge, api.ErrorCodeValidationError, "notebook size limit exceeded")
		return
	}

	Internal(w, r, err)
}

// WriteRPC отдаёт безопасную ошибку удалённого вызова, если адаптер сохранил её статус.
func WriteRPC(w http.ResponseWriter, err error) bool {
	code, body, ok := rpcerror.Response(err)
	if !ok {
		return false
	}
	w.Header().Set("Content-Type", "application/json")
	body.RequestID = w.Header().Get("X-Request-ID")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(body)
	return true
}

// NotFound отвечает на запрос к неизвестному маршруту.
func NotFound(w http.ResponseWriter, _ *http.Request) {
	Write(w, http.StatusNotFound, api.ErrorCodeNotFound, "no such endpoint")
}

// MethodNotAllowed отвечает на неизвестный для маршрута метод.
func MethodNotAllowed(w http.ResponseWriter, _ *http.Request) {
	Write(w, http.StatusMethodNotAllowed, api.ErrorCodeValidationError, "method not allowed")
}
