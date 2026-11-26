// Package rpcerror отображает безопасные внутренние ошибки на внешний HTTP-контракт.
package rpcerror

import (
	"context"
	"encoding/json"
	"errors"

	commonv1 "github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/gen/cellestial/common/v1"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/observability"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// CallKind различает отказ пользователя и ошибку служебных полномочий зависимости.
type CallKind string

const (
	CallBusiness    CallKind = "business"
	CallUserSession CallKind = "user_session"
	CallDependency  CallKind = "dependency"
	CallRuntime     CallKind = "runtime"
	CallFiles       CallKind = "files"
)

// ErrorBody содержит прежний бизнес-код и correlation без сырого exception/metadata.
type ErrorBody struct {
	Code      string          `json:"code"`
	Message   string          `json:"message"`
	RequestID string          `json:"request_id,omitempty"`
	Details   json.RawMessage `json:"details,omitempty"`
}

// errorMapping закрепляет допустимую комбинацию gRPC status, бизнес-кода и HTTP status.
type errorMapping struct {
	rpc           codes.Code
	http          int
	code, message string
}

// businessMappings возвращает конечный реестр разрешённых доменных ошибок.
func businessMappings() []errorMapping {
	return []errorMapping{
		{codes.FailedPrecondition, 409, "notebook_conflict", "notebook revision conflict"},
		{codes.Aborted, 409, "notebook_conflict", "notebook revision conflict"},
		{codes.FailedPrecondition, 409, "notebook_in_use", "notebook is in use"},
		{codes.AlreadyExists, 409, "active_session_exists", "active runtime session exists"},
		{codes.FailedPrecondition, 503, "runtime_not_ready", "runtime not ready"},
		{codes.FailedPrecondition, 410, "workspace_closed", "workspace closed"},
		{codes.ResourceExhausted, 413, "payload_too_large", "payload too large"},
		{codes.InvalidArgument, 400, "invalid_path", "invalid path"},
		{codes.AlreadyExists, 409, "conflict", "operation conflict"},
		{codes.ResourceExhausted, 429, "quota_exceeded", "quota exceeded"},
		{codes.Aborted, 409, "file_changed", "file changed"},
		{codes.FailedPrecondition, 409, "resync_required", "workspace resynchronization required"},
		{codes.Unknown, 503, "operation_outcome_unknown", "operation outcome unknown"},
	}
}

// fallback отображает только status, не используя произвольное сообщение зависимости.
func fallback(code codes.Code) errorMapping {
	switch code {
	case codes.InvalidArgument:
		return errorMapping{code, 400, "validation_error", "invalid request"}
	case codes.Unauthenticated:
		return errorMapping{code, 401, "unauthorized", "authentication required"}
	case codes.PermissionDenied:
		return errorMapping{code, 403, "forbidden", "access forbidden"}
	case codes.NotFound:
		return errorMapping{code, 404, "not_found", "resource not found"}
	case codes.AlreadyExists, codes.FailedPrecondition, codes.Aborted:
		return errorMapping{code, 409, "conflict", "operation conflict"}
	case codes.ResourceExhausted:
		return errorMapping{code, 429, "rate_limited", "resource limit reached"}
	case codes.Unavailable, codes.Canceled:
		return errorMapping{code, 503, "dependency_unavailable", "dependency unavailable"}
	case codes.DeadlineExceeded:
		return errorMapping{code, 504, "deadline_exceeded", "operation deadline exceeded"}
	default:
		return errorMapping{code, 500, "internal", "internal server error"}
	}
}

// HTTP использует correlation входного context и только allowlisted бизнес-details.
func HTTP(ctx context.Context, err error, kind CallKind) (int, ErrorBody) {
	code := status.Code(err)
	mapping := fallback(code)
	if (code == codes.Unauthenticated || code == codes.PermissionDenied) &&
		(kind == CallDependency || kind == CallRuntime || kind == CallFiles) {
		mapping = fallback(codes.Unavailable)
	} else {
		for _, detail := range status.Convert(err).Details() {
			d, ok := detail.(*commonv1.ErrorDetail)
			if !ok {
				continue
			}
			for _, allowed := range businessMappings() {
				if allowed.rpc == code && allowed.code == d.Code {
					mapping = allowed
				}
			}
		}
	}
	switch {
	case kind == CallRuntime && mapping.code == "dependency_unavailable":
		mapping.code, mapping.message = "runtime_unavailable", "Runtime unavailable"
	case kind == CallRuntime && mapping.code == "rate_limited":
		mapping.code, mapping.message = "queue_full", "Runtime queue is full"
	case kind == CallFiles && mapping.code == "dependency_unavailable":
		mapping.code, mapping.message = "runtime_not_ready", "runtime unavailable"
	}
	body := ErrorBody{Code: mapping.code, Message: mapping.message, RequestID: observability.RequestID(ctx)}
	for _, detail := range status.Convert(err).Details() {
		if d, ok := detail.(*commonv1.ErrorDetail); ok && d.Code == body.Code {
			body.Details = safeDetails(d.Code, d.DetailsJson)
		}
	}
	return mapping.http, body
}

// boundaryError хранит публичный ответ, сохраняя доменную причину для errors.Is.
type boundaryError struct {
	cause error
	code  int
	body  ErrorBody
}

// Error возвращает безопасное описание отказа.
func (e *boundaryError) Error() string { return e.body.Message }

// Unwrap сохраняет семантику повтора и отказа для usecase.
func (e *boundaryError) Unwrap() error { return e.cause }

// Preserve сохраняет отображение удалённого статуса и выбрасывает сырой текст RPC.
func Preserve(remote error, kind CallKind, cause error) error {
	if remote == nil || errors.Is(cause, context.Canceled) || errors.Is(cause, context.DeadlineExceeded) {
		return cause
	}
	if _, ok := status.FromError(remote); !ok {
		return cause
	}
	code, body := HTTP(context.Background(), remote, kind)
	return &boundaryError{cause: cause, code: code, body: body}
}

// Response читает безопасный ответ, подготовленный адаптером.
func Response(err error) (int, ErrorBody, bool) {
	var e *boundaryError
	if !errors.As(err, &e) {
		return 0, ErrorBody{}, false
	}
	return e.code, e.body, true
}

// Error создаёт gRPC status со стабильным business code, исключая сырой текст ошибки.
func Error(code codes.Code, business string, details json.RawMessage) error {
	mapping := fallback(code)
	for _, allowed := range businessMappings() {
		if allowed.rpc == code && allowed.code == business {
			mapping = allowed
			break
		}
	}
	s := status.New(code, mapping.message)
	with, err := s.WithDetails(&commonv1.ErrorDetail{Code: mapping.code, Message: mapping.message, DetailsJson: safeDetails(mapping.code, details)})
	if err != nil {
		return s.Err()
	}
	return with.Err()
}

// safeDetails допускает лишь публичный active_session DTO; остальные непрозрачные details отбрасываются.
func safeDetails(code string, data []byte) json.RawMessage {
	if code != "active_session_exists" || len(data) == 0 || len(data) > 16<<10 {
		return nil
	}
	var envelope struct {
		ActiveSession json.RawMessage `json:"active_session"`
	}
	if json.Unmarshal(data, &envelope) != nil || len(envelope.ActiveSession) == 0 {
		return nil
	}
	var session struct {
		ID                string `json:"id"`
		NotebookID        int64  `json:"notebook_id"`
		Language          string `json:"language"`
		State             string `json:"state"`
		Stage             string `json:"stage"`
		Reason            string `json:"reason,omitempty"`
		CreatedAt         string `json:"created_at"`
		LastActivityAt    string `json:"last_activity_at"`
		Generation        string `json:"generation,omitempty"`
		Cursor            int64  `json:"cursor"`
		CleanupPending    bool   `json:"cleanup_pending"`
		JournalIncomplete bool   `json:"journal_incomplete"`
	}
	if json.Unmarshal(envelope.ActiveSession, &session) != nil {
		return nil
	}
	body, err := json.Marshal(struct {
		ActiveSession any `json:"active_session"`
	}{session})
	if err != nil {
		return nil
	}
	return body
}
