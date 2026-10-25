package postgres

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Проверяем границу с драйвером: интерполяция ввода в SQL должна сломать тест.
type queryCapture struct {
	Querier
	sql  string
	args []any
}

func (db *queryCapture) QueryRow(_ context.Context, sql string, args ...any) pgx.Row { //nolint:ireturn // required by Querier
	db.sql, db.args = sql, args
	return failureRow{err: pgx.ErrNoRows}
}

func (db *queryCapture) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	db.sql, db.args = sql, args
	return pgconn.NewCommandTag("INSERT 0 1"), nil
}

func TestUserRepositoryParameterizedQueries(t *testing.T) {
	ctx := context.Background()
	expires := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	for _, payload := range []string{"x' OR '1'='1' --", "x'); DROP TABLE users; --"} {
		for _, tc := range []struct {
			name string
			args []any
			call func(*UserRepository)
		}{
			{"create", []any{payload, "hash"}, func(r *UserRepository) { _, _ = r.CreateUser(ctx, payload, "hash") }},
			{"by_login", []any{payload}, func(r *UserRepository) { _, _, _ = r.UserByLogin(ctx, payload) }},
			{"by_id", []any{int64(17)}, func(r *UserRepository) { _, _ = r.UserByID(ctx, 17) }},
			{"create_session", []any{int64(17), payload, expires}, func(r *UserRepository) { _ = r.CreateSession(ctx, 17, payload, expires) }},
			{"delete_session", []any{payload}, func(r *UserRepository) { _, _, _ = r.DeleteSession(ctx, payload) }},
			{"session_by_hash", []any{payload}, func(r *UserRepository) { _, _, _ = r.SessionByTokenHash(ctx, payload) }},
		} {
			t.Run(tc.name+"/"+payload, func(t *testing.T) {
				db := &queryCapture{}
				tc.call(NewUserRepository(db))
				if diff := cmp.Diff(tc.args, db.args); diff != "" {
					t.Fatalf("SQL arguments (-want +got):\n%s", diff)
				}
				if strings.Contains(db.sql, payload) {
					t.Fatalf("input interpolated into SQL: %s", db.sql)
				}
				for i := range tc.args {
					if !strings.Contains(db.sql, "$"+strconv.Itoa(i+1)) {
						t.Fatalf("argument %d has no placeholder: %s", i+1, db.sql)
					}
				}
				if tc.name == "by_login" && !strings.Contains(db.sql, "lower(login) = lower($1)") {
					t.Fatalf("case-insensitive login lookup lost: %s", db.sql)
				}
			})
		}
	}
}
