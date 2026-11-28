package onlineconf

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// userRef хранит ID и логин, подтверждённые Identity при записи.
type userRef struct {
	ID    int64  `json:"id"`
	Login string `json:"login"`
}

// keyRecord содержит метаданные и JSON-поля одного варианта ключа.
type keyRecord struct {
	Name, Scope, Type, Description string
	Data                           map[string]json.RawMessage
	CreatedAt, UpdatedAt           time.Time
	CreatedBy, UpdatedBy           userRef
}

// card возвращает полную карточку с датами в UTC.
func (k keyRecord) card() map[string]any {
	out := k.item()
	out["description"] = k.Description
	out["created_at"] = k.CreatedAt.UTC()
	out["created_by"] = k.CreatedBy
	out["updated_by"] = k.UpdatedBy
	for field, value := range k.Data {
		out[field] = value
	}
	return out
}

// item возвращает поля строки списка.
func (k keyRecord) item() map[string]any {
	out := map[string]any{"name": k.Name, "scope": k.Scope, "type": k.Type, "updated_at": k.UpdatedAt.UTC()}
	if k.Type == "config" {
		out["value"] = k.Data["value"]
	} else {
		out["enabled"] = k.Data["enabled"]
		out["rollout_percent"] = k.Data["rollout_percent"]
	}
	return out
}

// keyStore выполняет атомарные операции над ключами PostgreSQL.
type keyStore struct{ pool *pgxpool.Pool }

const keyColumns = "name, scope, type, description, data, created_at, updated_at, created_by, updated_by"

// scanKey читает запись с полным набором метаданных.
func scanKey(row pgx.Row) (keyRecord, error) {
	var k keyRecord
	err := row.Scan(&k.Name, &k.Scope, &k.Type, &k.Description, &k.Data, &k.CreatedAt, &k.UpdatedAt, &k.CreatedBy, &k.UpdatedBy)
	return k, err
}

// list читает общие счётчики и отфильтрованные строки из одного снимка.
func (s keyStore) list(ctx context.Context, search, scope, kind string) ([]map[string]any, int64, int64, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, 0, 0, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var total, scopes int64
	if err = tx.QueryRow(ctx, "SELECT count(*), count(DISTINCT scope) FROM onlineconf.keys").Scan(&total, &scopes); err != nil {
		return nil, 0, 0, err
	}
	rows, err := tx.Query(ctx, "SELECT "+keyColumns+` FROM onlineconf.keys
 WHERE strpos(lower(name), lower($1)) > 0 AND ($2 = '' OR scope = $2) AND ($3 = '' OR type = $3)
 ORDER BY updated_at DESC, name COLLATE "C"`, search, scope, kind)
	if err != nil {
		return nil, 0, 0, err
	}
	defer rows.Close()
	items := make([]map[string]any, 0)
	for rows.Next() {
		k, err := scanKey(rows)
		if err != nil {
			return nil, 0, 0, err
		}
		items = append(items, k.item())
	}
	if err := rows.Err(); err != nil {
		return nil, 0, 0, err
	}
	return items, total, scopes, tx.Commit(ctx)
}

// create создаёт ключ и возвращает сохранённую карточку.
func (s keyStore) create(ctx context.Context, k keyRecord) (keyRecord, error) {
	return scanKey(s.pool.QueryRow(ctx, `INSERT INTO onlineconf.keys
 (name,scope,type,description,data,created_by,updated_by)
 VALUES ($1,$2,$3,$4,$5,$6,$6) RETURNING `+keyColumns, k.Name, k.Scope, k.Type, k.Description, k.Data, k.CreatedBy))
}

// get читает карточку по имени.
func (s keyStore) get(ctx context.Context, name string) (keyRecord, error) {
	return scanKey(s.pool.QueryRow(ctx, "SELECT "+keyColumns+" FROM onlineconf.keys WHERE name = $1", name))
}

// update заменяет редактируемые поля и возвращает карточку.
func (s keyStore) update(ctx context.Context, k keyRecord) (keyRecord, error) {
	return scanKey(s.pool.QueryRow(ctx, `UPDATE onlineconf.keys SET description = $3, data = $4,
 updated_by = $5, updated_at = clock_timestamp() WHERE name = $1 AND type = $2 RETURNING `+keyColumns,
		k.Name, k.Type, k.Description, k.Data, k.UpdatedBy))
}

// delete удаляет ключ по полному имени.
func (s keyStore) delete(ctx context.Context, name string) error {
	tag, err := s.pool.Exec(ctx, "DELETE FROM onlineconf.keys WHERE name = $1", name)
	if err == nil && tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return err
}
