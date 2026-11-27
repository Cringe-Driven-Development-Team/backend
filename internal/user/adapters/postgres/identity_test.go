package postgres

import (
	"context"
	"testing"

	"github.com/pashagolub/pgxmock/v5"
)

// TestIdentityRepositoryQualifiedSQL проверяет, что отдельный Identity не использует public.users.
func TestIdentityRepositoryQualifiedSQL(t *testing.T) {
	db := newPool(t)
	r := NewIdentityUserRepository(db)
	db.ExpectQuery(`INSERT INTO identity.users (login, password_hash) VALUES ($1, $2) RETURNING id`).WithArgs("reader", "hash").WillReturnRows(pgxmock.NewRows([]string{"id"}).AddRow(int64(42)))
	u, err := r.CreateUser(context.Background(), "reader", "hash")
	if err != nil || u.ID != 42 {
		t.Fatalf("user=%v, error=%v", u, err)
	}
}
