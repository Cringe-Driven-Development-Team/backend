package delivery

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/observability"
	D "github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/runtimesession/domain"
	F "github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/runtimesession/domain/workspacefile"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/runtimesession/usecase"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/transport/http/api"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/transport/http/httperr"
	"github.com/google/uuid"
	"github.com/gorilla/mux"
)

// WorkspaceFileHandler переводит шесть HTTP маршрутов BFF в отдельные типизированные вызовы.
type WorkspaceFileHandler struct{ files *usecase.WorkspaceFiles }

// NewWorkspaceFileHandler подключает проверку владельца к файловому HTTP API.
func NewWorkspaceFileHandler(files *usecase.WorkspaceFiles) *WorkspaceFileHandler {
	return &WorkspaceFileHandler{files: files}
}

// ServeHTTP валидирует границу запроса до передачи ограниченного payload.
func (h *WorkspaceFileHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
	ctx := r.Context()
	id, err := uuid.Parse(mux.Vars(r)["id"])
	if err != nil || id == uuid.Nil {
		writeFileHTTPError(w, r, D.ErrInvalid)
		return
	}
	socket := r.Header.Get("X-Workspace-Socket")
	if socket != "" && (len(r.Header.Values("X-Workspace-Socket")) != 1 || !observability.ValidRequestID(socket)) {
		writeFileHTTPError(w, r, D.ErrInvalid)
		return
	}
	target := F.Target{Session: id, Socket: socket}
	user := owner(ctx)
	tail := mux.Vars(r)["file_path"]
	switch tail {
	case "check":
		if err = h.files.Check(ctx, user, target); err == nil {
			writeFileJSON(w, http.StatusOK, map[string]any{"workspace_generation": id, "ready": true})
		}
	case "command":
		err = h.command(w, r, user, target)
	case "events":
		raw := r.URL.Query().Get("after")
		var after uint64
		if raw != "" {
			after, err = strconv.ParseUint(raw, 10, 63)
		}
		if err != nil || socket == "" {
			err = D.ErrInvalid
			break
		}
		var page F.Replay
		if page, err = h.files.Replay(ctx, user, target, after); err == nil {
			writeFileJSON(w, http.StatusOK, page)
		}
	case "socket":
		if socket == "" {
			err = D.ErrInvalid
			break
		}
		if err = h.files.CloseSocket(ctx, user, target); err == nil {
			w.WriteHeader(http.StatusNoContent)
		}
	default:
		transferID, e := uuid.Parse(mux.Vars(r)["transfer_id"])
		if e != nil || transferID == uuid.Nil {
			err = D.ErrInvalid
			break
		}
		switch {
		case strings.HasSuffix(tail, "/chunks"):
			err = h.upload(w, r, user, target, transferID)
		case strings.HasSuffix(tail, "/download"):
			err = h.download(w, r, user, target, transferID)
		default:
			err = D.ErrNotFound
		}
	}
	if err != nil {
		writeFileHTTPError(w, r, err)
	}
}

func (h *WorkspaceFileHandler) command(w http.ResponseWriter, r *http.Request, user int64, target F.Target) error {
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	data, err := io.ReadAll(r.Body)
	if err != nil {
		return err
	}
	var in F.Command
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&in); err != nil {
		return D.ErrInvalid
	}
	// Разбор объекта также отвергает хвост после JSON и проверяет presence полей команды.
	var fields map[string]json.RawMessage
	if json.Unmarshal(data, &fields) != nil || !commandFields(in, fields) {
		return D.ErrInvalid
	}
	if target.Socket == "" && in.Kind != "upload_status" && in.Kind != "transfer_status" && in.Kind != "transfer_cancel" && in.Kind != "upload_commit" {
		return D.ErrInvalid
	}
	result, err := h.files.Command(r.Context(), user, target, in)
	if err != nil {
		return err
	}
	envelope := map[string]any{"protocol_version": 1, "type": "response", "request_id": in.RequestID}
	switch {
	case result.Error != nil:
		envelope["error"] = result.Error
	case result.Directory != nil:
		envelope["result"] = result.Directory
	case result.Transfer != nil:
		envelope["result"] = result.Transfer
	case result.Mutation != nil:
		envelope["result"] = result.Mutation
	case result.Watch != nil:
		envelope["result"] = result.Watch
	default:
		return D.ErrProtocol
	}
	writeFileJSON(w, http.StatusOK, envelope)
	return nil
}

// commandFields сохраняет строгость прежнего oneOf и валидацию параметров до RPC.
func commandFields(in F.Command, fields map[string]json.RawMessage) bool {
	if in.ProtocolVersion != 1 || !observability.ValidRequestID(in.RequestID) {
		return false
	}
	allowed := map[string][]string{
		"list": {"path", "limit", "cursor"}, "mkdir": {"path", "operation_id"},
		"rename": {"path", "operation_id", "name", "version"}, "delete": {"path", "operation_id", "version", "recursive"},
		"upload_begin": {"path", "operation_id", "size_bytes", "sha256"}, "download_begin": {"path", "operation_id"},
		"upload_status": {"transfer_id"}, "upload_commit": {"transfer_id"}, "transfer_status": {"transfer_id"}, "transfer_cancel": {"transfer_id"},
		"subscribe": {"paths", "after"}, "unsubscribe": {"paths"},
	}
	operation, ok := allowed[in.Kind]
	if !ok {
		return false
	}
	for name := range fields {
		if name == "protocol_version" || name == "type" || name == "request_id" || name == "session_id" || name == "workspace_generation" {
			continue
		}
		valid := false
		for _, key := range operation {
			if key == name {
				valid = true
				break
			}
		}
		if !valid {
			return false
		}
	}
	if raw, ok := fields["limit"]; ok && (in.Limit == 0 || string(raw) == "null") {
		return false
	}
	return in.Size == nil || *in.Size <= F.MaxFileBytes
}

func (h *WorkspaceFileHandler) upload(w http.ResponseWriter, r *http.Request, user int64, target F.Target, id uuid.UUID) error {
	raw := r.URL.Query()["offset"]
	if len(raw) != 1 {
		return D.ErrInvalid
	}
	offset, err := strconv.ParseUint(raw[0], 10, 64)
	if err != nil || offset > F.MaxFileBytes {
		return D.ErrInvalid
	}
	r.Body = http.MaxBytesReader(w, r.Body, F.ChunkBytes)
	data, err := io.ReadAll(r.Body)
	if err != nil {
		return err
	}
	item, err := h.files.Upload(r.Context(), user, target, id, offset, data)
	if err != nil {
		return err
	}
	writeFileJSON(w, http.StatusOK, item)
	return nil
}

func (h *WorkspaceFileHandler) download(w http.ResponseWriter, r *http.Request, user int64, target F.Target, id uuid.UUID) error {
	download, err := h.files.Download(r.Context(), user, target, id, requestRange(r.Header.Values("Range")), r.Header.Get("If-Range"))
	if err != nil {
		return err
	}
	defer download.Body.Close()
	w.Header().Set("Accept-Ranges", "bytes")
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Length", strconv.FormatUint(download.Length, 10))
	w.Header().Set("ETag", `"`+download.Version+`"`)
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": download.Filename}))
	status := http.StatusOK
	if download.Partial {
		status = http.StatusPartialContent
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", download.Offset, download.Offset+download.Length-1, download.Total))
	}
	w.WriteHeader(status)
	controller := http.NewResponseController(w)
	buf := make([]byte, 64<<10)
	for {
		if err = controller.SetWriteDeadline(time.Now().Add(time.Minute)); err != nil && !errors.Is(err, http.ErrNotSupported) {
			return nil
		}
		n, readErr := download.Body.Read(buf)
		if n > 0 {
			if _, err = w.Write(buf[:n]); err != nil {
				return nil
			}
			if err = controller.Flush(); err != nil && !errors.Is(err, http.ErrNotSupported) {
				return nil
			}
		}
		if readErr != nil {
			return nil
		}
	}
}

// requestRange передаёт форму диапазона отдельно от ответа и размера файла.
func requestRange(values []string) *F.Range {
	if len(values) == 0 {
		return nil
	}
	out := &F.Range{}
	if len(values) != 1 || !strings.HasPrefix(values[0], "bytes=") {
		return out
	}
	left, right, ok := strings.Cut(strings.TrimPrefix(values[0], "bytes="), "-")
	if !ok {
		return out
	}
	parse := func(raw string) *uint64 {
		value, err := strconv.ParseUint(raw, 10, 64)
		if err != nil || strings.HasPrefix(raw, "+") {
			return nil
		}
		return &value
	}
	if left == "" {
		out.Suffix = parse(right)
		return out
	}
	out.Start = parse(left)
	if right != "" {
		out.End = parse(right)
		if out.End == nil {
			return &F.Range{}
		}
	}
	return out
}

// writeFileJSON сохраняет media type файлового контракта и явный status.
func writeFileJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

// writeFileHTTPError сохраняет бизнес-коды и Content-Range на отказе до начала потока.
func writeFileHTTPError(w http.ResponseWriter, r *http.Request, err error) {
	if httperr.WriteRPC(w, err) {
		return
	}
	var rangeErr *F.InvalidRange
	var fileErr *F.Error
	var limit *http.MaxBytesError
	switch {
	case errors.As(err, &rangeErr):
		w.Header().Set("Content-Range", fmt.Sprintf("bytes */%d", rangeErr.Total))
		w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
	case errors.As(err, &limit):
		w.WriteHeader(http.StatusRequestEntityTooLarge)
	case errors.As(err, &fileErr):
		status := http.StatusConflict
		switch fileErr.Code {
		case "invalid_path":
			status = http.StatusBadRequest
		case "workspace_closed":
			status = http.StatusGone
		case "runtime_not_ready":
			status = http.StatusServiceUnavailable
		case "rate_limited":
			status = http.StatusTooManyRequests
		case "deadline_exceeded":
			status = http.StatusGatewayTimeout
		}
		writeFileJSON(w, status, map[string]any{"error": fileErr})
	case errors.Is(err, D.ErrWorkspaceClosed):
		httperr.Write(w, http.StatusGone, "workspace_closed", "workspace closed")
	case errors.Is(err, D.ErrInvalid):
		httperr.Write(w, http.StatusBadRequest, api.ErrorCodeValidationError, "invalid file request")
	case errors.Is(err, D.ErrNotFound):
		httperr.Write(w, http.StatusNotFound, api.ErrorCodeNotFound, "runtime session not found")
	case errors.Is(err, context.DeadlineExceeded):
		httperr.Write(w, http.StatusGatewayTimeout, "deadline_exceeded", "operation deadline exceeded")
	case errors.Is(err, D.ErrUnavailable):
		httperr.Write(w, http.StatusServiceUnavailable, "runtime_not_ready", "runtime unavailable")
	default:
		httperr.ResponseError(w, r, err)
	}
}
