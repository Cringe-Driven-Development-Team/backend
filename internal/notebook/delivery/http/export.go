package delivery

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io"
	"mime"
	"net/http"
	"os"
	"strconv"

	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/notebook/usecase"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/transport/http/api"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/transport/http/httperr"
	"github.com/gorilla/mux"
)

// ExportNotebook передаёт сохранённый snapshot без удержания SQL-транзакции при сетевой записи.
func (h *Handler) ExportNotebook(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(mux.Vars(r)["id"], 10, 64)
	if err != nil || id <= 0 {
		httperr.Write(w, http.StatusBadRequest, api.ErrorCodeValidationError, "invalid notebook ID")
		return
	}
	svc, ok := h.uc.(interface {
		Export(context.Context, int64, int64, io.Writer) (string, string, error)
	})
	if !ok {
		httperr.Write(w, http.StatusServiceUnavailable, "dependency_unavailable", "notebook export unavailable")
		return
	}
	file, err := os.CreateTemp("", "cellestial-ipynb-export-*")
	if err != nil {
		httperr.Internal(w, r, err)
		return
	}
	defer func() { _ = file.Close(); _ = os.Remove(file.Name()) }()
	sum := sha256.New()
	name, revision, err := svc.Export(r.Context(), id, userID(r.Context()), io.MultiWriter(file, sum))
	switch {
	case errors.Is(err, usecase.ErrNotebookNotFound):
		httperr.Write(w, http.StatusNotFound, api.ErrorCodeNotFound, "notebook not found")
		return
	case err != nil:
		httperr.ResponseError(w, r, err)
		return
	}
	size, err := file.Seek(0, io.SeekCurrent)
	if err == nil {
		_, err = file.Seek(0, io.SeekStart)
	}
	if err != nil {
		httperr.Internal(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("Content-Type", "application/x-ipynb+json")
	w.Header().Set("Content-Length", strconv.FormatInt(size, 10))
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": name + ".ipynb"}))
	w.Header().Set("ETag", *quotedETag(revision))
	w.Header().Set("Content-Digest", "sha-256=:"+base64.StdEncoding.EncodeToString(sum.Sum(nil))+":")
	w.WriteHeader(http.StatusOK)
	_, _ = io.CopyBuffer(w, io.LimitReader(file, size), make([]byte, 64<<10))
}
