package onlineconf

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/transport/http/httperr"
	"github.com/jackc/pgx/v5"
)

// evaluate проверяет сохранённый флаг для указанного пользователя из одного снимка.
func (h keysHandler) evaluate(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	name := r.PathValue("name")
	scope, _, _ := strings.Cut(name, ".")
	query, err := url.ParseQuery(r.URL.RawQuery)
	values := query["user_id"]
	if err != nil || len(query) != 1 || len(values) != 1 || !validKeyName(name, scope) {
		writeKeyError(w, errValidation)
		return
	}

	rawID := values[0]
	userID, parseErr := strconv.ParseInt(rawID, 10, 64)
	if parseErr != nil || userID <= 0 || rawID[0] == '+' {
		writeKeyError(w, errValidation)
		return
	}

	users, err := h.byIDs(r.Context(), []int64{userID})
	if err != nil {
		writeKeyError(w, errDirectory)
		return
	}
	if _, exists := users[userID]; !exists {
		writeKeyError(w, pgx.ErrNoRows)
		return
	}

	view, err := h.readSnapshot(r.Context())
	entry, state := view.Lookup(name)
	if err != nil || state == Unavailable {
		writeSnapshotUnavailable(w)
		return
	}
	if state == Missing {
		writeKeyError(w, pgx.ErrNoRows)
		return
	}
	if entry.Type != "flag" {
		writeKeyError(w, errValidation)
		return
	}
	result, err := (keyRecord{Name: name, Type: entry.Type, Data: entry.Data}).evaluateFlag(userID)
	if err != nil {
		writeSnapshotUnavailable(w)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// writeSnapshotUnavailable сообщает, что расчёт по свежим данным невозможен.
func writeSnapshotUnavailable(w http.ResponseWriter) {
	httperr.Write(w, http.StatusServiceUnavailable, "onlineconf_unavailable", "onlineconf snapshot unavailable")
}
