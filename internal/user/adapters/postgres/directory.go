package postgres

import (
	"context"
	"strings"

	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/user/domain"
)

// BatchUsers читает публичные поля одним запросом, без JOIN и чтения auth-данных.
func (r *UserRepository) BatchUsers(ctx context.Context, ids []int64, logins []string) ([]domain.UserSuggestion, error) {
	folded := make([]string, len(logins))
	for i, v := range logins {
		folded[i] = strings.ToLower(v)
	}
	rows, err := r.db.Query(ctx, `SELECT id,login,COALESCE(avatar_key, '') FROM `+r.table+` WHERE id=ANY($1::bigint[]) OR (lower(login)=ANY($2::text[])`+r.active+`) ORDER BY id`, ids, folded)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	users := make([]domain.UserSuggestion, 0)
	for rows.Next() {
		var u domain.UserSuggestion
		if err := rows.Scan(&u.ID, &u.Login, &u.AvatarKey); err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	return users, rows.Err()
}
