package middleware

import (
	"bytes"
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"unicode/utf8"

	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/notebook/usecase"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/transport/http/api"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/transport/http/httperr"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/transport/http/principal"
)

// commentRoute распознаёт форму маршрута до декодирования и проверки ID.
func commentRoute(r *http.Request, prefix string) bool {
	path := r.URL.EscapedPath()
	if !strings.HasPrefix(path, prefix+"/notebooks/") {
		return false
	}
	p := strings.Split(strings.TrimPrefix(path, prefix+"/notebooks/"), "/")
	return (len(p) == 5 || len(p) == 6) && p[0] != "" && p[1] == "content" && p[2] == "cells" && p[3] != "" && p[4] == "comments" && (len(p) == 5 || p[5] != "")
}

// CellComments требует сессию до разбора записей и нормализует закрытое JSON-тело.
// prefix задаёт базовый путь API; тело ограничивается до декодирования.
func CellComments(prefix string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !commentRoute(r, prefix) {
				next.ServeHTTP(w, r)
				return
			}
			write := r.Method == http.MethodPost || r.Method == http.MethodPatch || r.Method == http.MethodDelete
			if _, ok := principal.UserIDFromContext(r.Context()); write && !ok {
				httperr.Write(w, 401, api.ErrorCodeUnauthorized, "authentication required")
				return
			}
			// URL.Query молча теряет повреждённые параметры: это могло повторить первую страницу.
			if _, err := url.ParseQuery(r.URL.RawQuery); err != nil {
				httperr.Write(w, 400, api.ErrorCodeValidationError, "invalid query parameters")
				return
			}
			if r.Method != http.MethodPost && r.Method != http.MethodPatch {
				next.ServeHTTP(w, r)
				return
			}
			if r.ContentLength > MaxRequestBodyBytes {
				httperr.Write(w, 413, api.ErrorCodeValidationError, "request body too large")
				return
			}
			media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
			if err != nil || media != "application/json" {
				httperr.Write(w, 400, api.ErrorCodeValidationError, "expected application/json")
				return
			}
			data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, MaxRequestBodyBytes))
			_ = r.Body.Close()
			if err != nil {
				httperr.RequestError(w, r, err)
				return
			}
			if !utf8.Valid(data) {
				httperr.Write(w, 400, api.ErrorCodeValidationError, "invalid comment")
				return
			}
			var fields map[string]json.RawMessage
			decoder := json.NewDecoder(bytes.NewReader(data))
			if err = decoder.Decode(&fields); err != nil || len(fields) != 1 {
				httperr.Write(w, 400, api.ErrorCodeValidationError, "invalid comment")
				return
			}
			// Map сохраняет точный регистр имени: struct decoder принимает также Text.
			var input *string
			raw, exists := fields["text"]
			if !exists || json.Unmarshal(raw, &input) != nil || input == nil {
				httperr.Write(w, 400, api.ErrorCodeValidationError, "invalid comment")
				return
			}
			var extra any
			if decoder.Decode(&extra) != io.EOF {
				httperr.Write(w, 400, api.ErrorCodeValidationError, "invalid comment")
				return
			}
			text, err := usecase.NormalizeCommentText(*input)
			if err != nil {
				httperr.Write(w, 400, api.ErrorCodeValidationError, "invalid comment")
				return
			}
			normalized, err := json.Marshal(struct {
				Text string `json:"text"`
			}{text})
			if err != nil {
				httperr.ResponseError(w, r, err)
				return
			}
			r.Body = io.NopCloser(bytes.NewReader(normalized))
			r.ContentLength = int64(len(normalized))
			r.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(normalized)), nil }
			next.ServeHTTP(w, r)
		})
	}
}
