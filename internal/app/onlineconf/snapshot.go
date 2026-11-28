package onlineconf

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"time"
)

const snapshotFreshness = 60 * time.Second

var errInvalidSnapshot = errors.New("invalid OnlineConf snapshot")

// ReadState отличает найденное значение, подтверждённое отсутствие и недоступность.
type ReadState string

const (
	Found       ReadState = "found"
	Missing     ReadState = "missing"
	Unavailable ReadState = "unavailable"
)

type snapshot struct {
	CheckedAtMS int64                    `json:"checked_at_ms"`
	Entries     map[string]SnapshotEntry `json:"entries"`
}

// SnapshotEntry содержит тип и данные одной настройки из продуктового снимка.
type SnapshotEntry struct {
	Scope string                     `json:"scope"`
	Type  string                     `json:"type"`
	Data  map[string]json.RawMessage `json:"data"`
}

// ProductView хранит одну подтверждённую версию для текущего продуктового запроса.
type ProductView struct {
	entries map[string]SnapshotEntry
}

// newProductView применяет границу свежести по времени Redis в миллисекундах.
func newProductView(value snapshot, nowMS int64) ProductView {
	if value.CheckedAtMS <= 0 || value.CheckedAtMS > nowMS || value.Entries == nil ||
		nowMS-value.CheckedAtMS > snapshotFreshness.Milliseconds() {
		return ProductView{}
	}
	return ProductView{entries: value.Entries}
}

// Lookup возвращает запись. Default допустим только при результате Missing.
func (v ProductView) Lookup(name string) (SnapshotEntry, ReadState) {
	if v.entries == nil {
		return SnapshotEntry{}, Unavailable
	}
	entry, ok := v.entries[name]
	if !ok {
		return SnapshotEntry{}, Missing
	}
	return entry, Found
}

// Flag вычисляет значение флага для пользователя по правилам #156.
func (v ProductView) Flag(name string, userID int64) (bool, ReadState) {
	entry, state := v.Lookup(name)
	if state != Found {
		return false, state
	}
	result, err := (keyRecord{Name: name, Type: entry.Type, Data: entry.Data}).flagEnabledFor(userID)
	if err != nil {
		return false, Unavailable
	}
	return result, Found
}

type snapshotUser struct {
	ID int64 `json:"id"`
}

// snapshotRules читает и валидирует полные правила, проецируя пользователей на их ID.
func (e SnapshotEntry) snapshotRules() (include, exclude []snapshotUser, ok bool) {
	var enabled bool
	var percent int
	if !readField(e.Data, "enabled", &enabled) ||
		!readField(e.Data, "rollout_percent", &percent) || percent < 0 || percent > 100 ||
		!readField(e.Data, "include_users", &include) || !readField(e.Data, "exclude_users", &exclude) {
		return nil, nil, false
	}
	included := make(map[int64]struct{}, len(include))
	for _, user := range include {
		if user.ID <= 0 {
			return nil, nil, false
		}
		included[user.ID] = struct{}{}
	}
	for _, user := range exclude {
		if _, overlap := included[user.ID]; overlap || user.ID <= 0 {
			return nil, nil, false
		}
	}
	return include, exclude, true
}

// validName проверяет принадлежность имени к scope.
func (e SnapshotEntry) validName(name string) bool {
	return slices.Contains(keyScopes(), e.Scope) && strings.HasPrefix(name, e.Scope+".") && keyName.MatchString(name)
}

// valid проверяет, что сериализованные данные снимка соответствуют типу настройки.
func (e SnapshotEntry) valid(name string) bool {
	if !e.validName(name) {
		return false
	}
	switch e.Type {
	case "config":
		return json.Valid(e.Data["value"])
	case "flag":
		_, _, ok := e.snapshotRules()
		return ok
	default:
		return false
	}
}

// readSnapshot читает согласованную версию всех ключей одним оператором PostgreSQL.
func (s keyStore) readSnapshot(ctx context.Context) (map[string]SnapshotEntry, error) {
	rows, err := s.pool.Query(ctx, "SELECT name, scope, type, data FROM onlineconf.keys")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	entries := make(map[string]SnapshotEntry)
	for rows.Next() {
		var name string
		var entry SnapshotEntry
		if err := rows.Scan(&name, &entry.Scope, &entry.Type, &entry.Data); err != nil {
			return nil, err
		}
		if !entry.validName(name) {
			return nil, errInvalidSnapshot
		}
		switch entry.Type {
		case "config":
			value := entry.Data["value"]
			if !json.Valid(value) {
				return nil, errInvalidSnapshot
			}
			entry.Data = map[string]json.RawMessage{"value": value}
		case "flag":
			include, exclude, ok := entry.snapshotRules()
			if !ok {
				return nil, errInvalidSnapshot
			}
			rawInclude, err := json.Marshal(include)
			if err != nil {
				return nil, err
			}
			rawExclude, err := json.Marshal(exclude)
			if err != nil {
				return nil, err
			}
			entry.Data = map[string]json.RawMessage{
				"enabled": entry.Data["enabled"], "rollout_percent": entry.Data["rollout_percent"],
				"include_users": rawInclude, "exclude_users": rawExclude,
			}
		default:
			return nil, errInvalidSnapshot
		}
		entries[name] = entry
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return entries, nil
}
