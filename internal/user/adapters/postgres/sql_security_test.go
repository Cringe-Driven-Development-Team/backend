package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/user/domain"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/user/usecase"
	"github.com/jackc/pgx/v5"
	"github.com/pashagolub/pgxmock/v5"
)

// Literal SQL and exact arguments protect the driver boundary against interpolation.
func TestUserRepositoryParameterizedQueries(t *testing.T) {
	for _, payload := range []string{"x' OR '1'='1' --", "x'); DROP TABLE users; --"} {
		t.Run(payload, func(t *testing.T) {
			db := newPool(t)
			r := NewUserRepository(db)
			ctx := context.Background()
			expires := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
			db.ExpectQuery(createUserSQL).WithArgs(payload, "hash").WillReturnRows(pgxmock.NewRows([]string{"id"}).AddRow(int64(17)))
			u, err := r.CreateUser(ctx, payload, "hash")
			if err != nil || u != (domain.User{ID: 17, Login: payload}) {
				t.Fatalf("user=%v err=%v", u, err)
			}
			db.ExpectQuery(byLoginSQL).WithArgs(payload).WillReturnRows(pgxmock.NewRows([]string{"id", "login", "password_hash"}).AddRow(int64(17), payload, "hash"))
			u, hash, err := r.UserByLogin(ctx, payload)
			if err != nil || u.ID != 17 || u.Login != payload || hash != "hash" {
				t.Fatalf("user=%v hash=%s err=%v", u, hash, err)
			}
			db.ExpectQuery(byIDSQL).WithArgs(int64(17)).WillReturnRows(pgxmock.NewRows([]string{"login"}).AddRow(payload))
			u, err = r.UserByID(ctx, 17)
			if err != nil || u.Login != payload || u.ID != 17 {
				t.Fatalf("user=%v err=%v", u, err)
			}
			db.ExpectExec(createSessionSQL).WithArgs(int64(17), payload, expires).WillReturnResult(pgxmock.NewResult("INSERT", 1))
			if err = r.CreateSession(ctx, 17, payload, expires); err != nil {
				t.Fatal(err)
			}
			for _, sql := range []string{deleteSessionSQL} {
				db.ExpectQuery(sql).WithArgs(payload).WillReturnRows(pgxmock.NewRows([]string{"user_id", "expires_at"}).AddRow(int64(17), expires))
				var id int64
				var expiry time.Time
				id, expiry, err = r.DeleteSession(ctx, payload)
				if err != nil || id != 17 || !expiry.Equal(expires) {
					t.Fatalf("id=%d expires=%v err=%v", id, expiry, err)
				}
			}
		})
	}
}

func TestUserRepositoryMissingRows(t *testing.T) {
	for _, name := range []string{"login", "id", "delete"} {
		t.Run(name, func(t *testing.T) {
			db := newPool(t)
			r := NewUserRepository(db)
			ctx := context.Background()
			var err, want error
			switch name {
			case "login":
				db.ExpectQuery(byLoginSQL).WithArgs("missing").WillReturnError(pgx.ErrNoRows)
				_, _, err = r.UserByLogin(ctx, "missing")
				want = usecase.ErrUserNotFound
			case "id":
				db.ExpectQuery(byIDSQL).WithArgs(int64(99)).WillReturnError(pgx.ErrNoRows)
				_, err = r.UserByID(ctx, 99)
				want = usecase.ErrUserNotFound
			case "delete":
				db.ExpectQuery(deleteSessionSQL).WithArgs("missing").WillReturnError(pgx.ErrNoRows)
				_, _, err = r.DeleteSession(ctx, "missing")
				want = usecase.ErrSessionNotFound
			}
			if !errors.Is(err, want) {
				t.Fatalf("err=%v want=%v", err, want)
			}
		})
	}
}
