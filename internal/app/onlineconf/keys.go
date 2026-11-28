package onlineconf

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	identityv1 "github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/gen/cellestial/identity/v1"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/transport/http/httperr"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/transport/http/principal"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// keyScopes возвращает полный набор допустимых областей.
func keyScopes() []string {
	return []string{"frontend", "bff", "identity", "workspace"}
}

//nolint:gochecknoglobals // Регулярное выражение компилируется один раз для всех запросов.
var keyName = regexp.MustCompile(`^[a-zA-Z0-9_]+(\.[a-zA-Z0-9_]+)+$`)
var errValidation = errors.New("invalid key or user fields")
var errDirectory = errors.New("user directory unavailable")

// keysRepository задаёт операции хранения для административных обработчиков.
type keysRepository interface {
	list(context.Context, string, string, string) ([]map[string]any, int64, int64, error)
	create(context.Context, keyRecord) (keyRecord, error)
	get(context.Context, string) (keyRecord, error)
	update(context.Context, keyRecord) (keyRecord, error)
	delete(context.Context, string) error
}

// keysHandler связывает HTTP-контракт, каталог Identity и хранилище ключей.
type keysHandler struct {
	store keysRepository
	users identityv1.UsersClient
}

// newKeysHandler регистрирует маршруты внутри защищённой ветки.
func newKeysHandler(store keysRepository, users identityv1.UsersClient) *http.ServeMux {
	h := keysHandler{store: store, users: users}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /admin/access", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("GET /admin/keys", h.list)
	mux.HandleFunc("POST /admin/keys", h.create)
	mux.HandleFunc("GET /admin/keys/{name}", h.get)
	mux.HandleFunc("PUT /admin/keys/{name}", h.update)
	mux.HandleFunc("DELETE /admin/keys/{name}", h.delete)
	mux.HandleFunc("GET /admin/users/resolve", h.resolve)
	return mux
}

// validText проверяет текстовые поля перед передачей в PostgreSQL.
func validText(value string) bool {
	return utf8.ValidString(value) && !strings.ContainsRune(value, 0)
}

// writeJSON отправляет успешный ответ с JSON-телом.
func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

// writeKeyError преобразует ошибки в стабильные HTTP-коды.
func writeKeyError(w http.ResponseWriter, err error) {
	var pgErr *pgconn.PgError
	switch {
	case errors.Is(err, errValidation):
		httperr.Write(w, 400, "validation_error", "Некорректные поля запроса")
	case errors.Is(err, pgx.ErrNoRows):
		httperr.Write(w, 404, "not_found", "Запись не найдена")
	case errors.Is(err, errDirectory):
		httperr.Write(w, 503, "dependency_unavailable", "Каталог пользователей недоступен")
	case errors.As(err, &pgErr) && pgErr.Code == "23505":
		httperr.Write(w, 409, "key_name_taken", "Ключ уже существует")
	default:
		httperr.Write(w, 500, "internal_error", "Не удалось выполнить операцию")
	}
}

// readFields разбирает один JSON-объект, сохраняя исходное представление каждого значения.
func readFields(w http.ResponseWriter, r *http.Request) (map[string]json.RawMessage, error) {
	var fields map[string]json.RawMessage
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	if err := decoder.Decode(&fields); err != nil || fields == nil || decoder.Decode(new(any)) != io.EOF {
		return nil, errValidation
	}
	return fields, nil
}

// readField извлекает обязательное поле конкретного типа.
func readField[T any](fields map[string]json.RawMessage, name string, dst *T) bool {
	raw, ok := fields[name]
	return ok && string(raw) != "null" && json.Unmarshal(raw, dst) == nil
}

// list возвращает отфильтрованные строки и общие счётчики.
func (h keysHandler) list(w http.ResponseWriter, r *http.Request) {
	q, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		writeKeyError(w, errValidation)
		return
	}
	scope, kind := q.Get("scope"), q.Get("type")
	search := strings.TrimSpace(q.Get("search"))
	if !validText(search) || (q.Has("scope") && !slices.Contains(keyScopes(), scope)) || (q.Has("type") && kind != "config" && kind != "flag") {
		writeKeyError(w, errValidation)
		return
	}
	items, total, scopeCount, err := h.store.list(r.Context(), search, scope, kind)
	if err != nil {
		writeKeyError(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"items": items, "total_count": total, "scope_count": scopeCount, "scopes": keyScopes()})
}

// create сохраняет метаданные и начальное значение ключа.
func (h keysHandler) create(w http.ResponseWriter, r *http.Request) {
	fields, err := readFields(w, r)
	var k keyRecord
	if err != nil || !readField(fields, "name", &k.Name) || !readField(fields, "scope", &k.Scope) || !readField(fields, "type", &k.Type) ||
		!slices.Contains(keyScopes(), k.Scope) || !strings.HasPrefix(k.Name, k.Scope+".") || !keyName.MatchString(k.Name) {
		writeKeyError(w, errValidation)
		return
	}
	for field := range fields {
		switch field {
		case "name", "scope", "type", "description", "format":
		default:
			writeKeyError(w, errValidation)
			return
		}
	}
	if _, ok := fields["description"]; ok && (!readField(fields, "description", &k.Description) || !validText(k.Description)) {
		writeKeyError(w, errValidation)
		return
	}
	switch k.Type {
	case "config":
		var format string
		if !readField(fields, "format", &format) || format != "json" {
			writeKeyError(w, errValidation)
			return
		}
		k.Data = map[string]json.RawMessage{"format": json.RawMessage(`"json"`), "value": json.RawMessage("null")}
	case "flag":
		if _, ok := fields["format"]; ok {
			writeKeyError(w, errValidation)
			return
		}
		k.Data = map[string]json.RawMessage{"enabled": json.RawMessage("false"), "rollout_percent": json.RawMessage("0"),
			"include_users": json.RawMessage("[]"), "exclude_users": json.RawMessage("[]")}
	default:
		writeKeyError(w, errValidation)
		return
	}
	k.CreatedBy, err = h.actor(r.Context())
	if err == nil {
		k, err = h.store.create(r.Context(), k)
	}
	writeCard(w, 201, k, err)
}

// get возвращает полную карточку ключа.
func (h keysHandler) get(w http.ResponseWriter, r *http.Request) {
	k, err := h.store.get(r.Context(), r.PathValue("name"))
	writeCard(w, 200, k, err)
}

// writeCard отправляет карточку или ошибку операции.
func writeCard(w http.ResponseWriter, status int, k keyRecord, err error) {
	if err != nil {
		writeKeyError(w, err)
		return
	}
	writeJSON(w, status, k.card())
}

// update проверяет полный набор полей и заменяет данные одной записью.
func (h keysHandler) update(w http.ResponseWriter, r *http.Request) {
	fields, err := readFields(w, r)
	k := keyRecord{Name: r.PathValue("name")}
	var include, exclude []int64
	if err != nil || !readField(fields, "description", &k.Description) || !validText(k.Description) {
		writeKeyError(w, errValidation)
		return
	}
	if value, ok := fields["value"]; ok && len(fields) == 2 {
		k.Type = "config"
		k.Data = map[string]json.RawMessage{"format": json.RawMessage(`"json"`), "value": value}
	} else {
		var enabled bool
		var percent int
		if len(fields) != 5 || !readField(fields, "enabled", &enabled) || !readField(fields, "rollout_percent", &percent) ||
			percent < 0 || percent > 100 || !readField(fields, "include_user_ids", &include) ||
			!readField(fields, "exclude_user_ids", &exclude) || !validLists(include, exclude) {
			writeKeyError(w, errValidation)
			return
		}
		k.Type = "flag"
		k.Data = map[string]json.RawMessage{"enabled": fields["enabled"], "rollout_percent": fields["rollout_percent"]}
	}
	current, err := h.store.get(r.Context(), k.Name)
	if err != nil {
		writeKeyError(w, err)
		return
	}
	if current.Type != k.Type {
		writeKeyError(w, errValidation)
		return
	}
	id, _ := principal.UserIDFromContext(r.Context())
	ids := append([]int64{id}, include...)
	ids = append(ids, exclude...)
	users, err := h.byIDs(r.Context(), ids)
	if err != nil {
		writeKeyError(w, err)
		return
	}
	actor, ok := users[id]
	if !ok {
		writeKeyError(w, errDirectory)
		return
	}
	k.UpdatedBy = actor
	if k.Type == "flag" {
		k.Data["include_users"], err = userList(include, users)
		if err == nil {
			k.Data["exclude_users"], err = userList(exclude, users)
		}
		if err != nil {
			writeKeyError(w, err)
			return
		}
	}
	k, err = h.store.update(r.Context(), k)
	writeCard(w, 200, k, err)
}

// validLists проверяет положительность ID и отсутствие пересечения списков.
func validLists(include, exclude []int64) bool {
	included := make(map[int64]bool, len(include))
	for _, id := range include {
		if id <= 0 {
			return false
		}
		included[id] = true
	}
	for _, id := range exclude {
		if id <= 0 || included[id] {
			return false
		}
	}
	return true
}

// userList собирает чипы в порядке запроса без повторяющихся ID.
func userList(ids []int64, users map[int64]userRef) (json.RawMessage, error) {
	out := make([]userRef, 0, len(ids))
	seen := make(map[int64]bool, len(ids))
	for _, id := range ids {
		user, ok := users[id]
		if !ok {
			return nil, errValidation
		}
		if !seen[id] {
			out = append(out, user)
			seen[id] = true
		}
	}
	return json.Marshal(out)
}

// delete удаляет ключ и возвращает пустой ответ.
func (h keysHandler) delete(w http.ResponseWriter, r *http.Request) {
	if err := h.store.delete(r.Context(), r.PathValue("name")); err != nil {
		writeKeyError(w, err)
		return
	}
	w.WriteHeader(204)
}

// resolve ищет пользователя ровно по одному ID или логину.
func (h keysHandler) resolve(w http.ResponseWriter, r *http.Request) {
	q, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil || len(q) != 1 {
		writeKeyError(w, errValidation)
		return
	}
	if values := q["id"]; len(values) == 1 {
		id, err := strconv.ParseInt(values[0], 10, 64)
		if err != nil || id <= 0 {
			writeKeyError(w, errValidation)
			return
		}
		users, err := h.byIDs(r.Context(), []int64{id})
		if err != nil {
			writeKeyError(w, err)
			return
		}
		if user, ok := users[id]; ok {
			writeJSON(w, 200, user)
			return
		}
	} else if values := q["login"]; len(values) == 1 && strings.TrimSpace(values[0]) != "" {
		resp, err := h.users.BatchResolveUsers(r.Context(), &identityv1.BatchResolveUsersRequest{Logins: values})
		if err != nil {
			writeKeyError(w, directoryError(err))
			return
		}
		for _, user := range resp.GetUsers() {
			if strings.EqualFold(user.GetLogin(), values[0]) {
				writeJSON(w, 200, userRef{ID: user.GetId(), Login: user.GetLogin()})
				return
			}
		}
	} else {
		writeKeyError(w, errValidation)
		return
	}
	writeKeyError(w, pgx.ErrNoRows)
}
