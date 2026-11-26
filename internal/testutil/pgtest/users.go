package pgtest

import (
	"context"
	"strings"

	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/notebook/domain"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/notebook/usecase"
	"github.com/jackc/pgx/v5"
)

// UsersFixture подменяет сетевой каталог только в интеграционных тестах.
type UsersFixture struct {
	DB interface {
		Query(context.Context, string, ...any) (pgx.Rows, error)
	}
}

// BatchGet читает тестовые профили одним запросом; production-репозиторий этого доступа не имеет.
func (u UsersFixture) BatchGet(ctx context.Context, ids []int64) (map[int64]domain.NotebookReader, error) {
	rows, err := u.DB.Query(ctx, `SELECT id,login FROM identity.users WHERE id=ANY($1)`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]domain.NotebookReader{}
	for rows.Next() {
		var user domain.NotebookReader
		if err := rows.Scan(&user.ID, &user.Login); err != nil {
			return nil, err
		}
		out[user.ID] = user
	}
	return out, rows.Err()
}

// ResolveLogins сохраняет явный not-found и порядок исходных селекторов.
func (u UsersFixture) ResolveLogins(ctx context.Context, logins []string) ([]usecase.UserResolution, error) {
	rows, err := u.DB.Query(ctx, `SELECT id,login FROM identity.users WHERE lower(login)=ANY($1)`, logins)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	users := map[string]domain.NotebookReader{}
	for rows.Next() {
		var user domain.NotebookReader
		if err := rows.Scan(&user.ID, &user.Login); err != nil {
			return nil, err
		}
		users[strings.ToLower(user.Login)] = user
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]usecase.UserResolution, len(logins))
	for i, login := range logins {
		user, found := users[strings.ToLower(login)]
		out[i] = usecase.UserResolution{Login: login, User: user, Found: found}
	}
	return out, nil
}
