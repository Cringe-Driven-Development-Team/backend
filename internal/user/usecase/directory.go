package usecase

import (
	"context"
	"errors"

	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/user/domain"
)

// ErrInvalidUserBatch сообщает о недопустимом количестве или формате селекторов.
var ErrInvalidUserBatch = errors.New("invalid user batch")

// DirectoryStore возвращает только публичные идентификаторы и логины одним запросом.
type DirectoryStore interface {
	BatchUsers(context.Context, []int64, []string) ([]domain.UserSuggestion, error)
}

// Directory разрешает ссылки Workspace на пользователей без передачи credentials.
type Directory struct{ store DirectoryStore }

// NewDirectory связывает batch usecase с хранилищем владельца пользователей.
func NewDirectory(store DirectoryStore) *Directory { return &Directory{store: store} }

// ByIDs возвращает найденных пользователей; отсутствующие ID пропускаются явно по контракту.
func (d *Directory) ByIDs(ctx context.Context, ids []int64) ([]domain.UserSuggestion, error) {
	if len(ids) == 0 || len(ids) > 100 {
		return nil, ErrInvalidUserBatch
	}
	for _, id := range ids {
		if id <= 0 {
			return nil, ErrInvalidUserBatch
		}
	}
	return d.store.BatchUsers(ctx, ids, nil)
}

// ByLogins разрешает логины без учёта регистра, по тем же ограничениям, что HTTP API.
func (d *Directory) ByLogins(ctx context.Context, logins []string) ([]domain.UserSuggestion, error) {
	if len(logins) == 0 || len(logins) > 100 {
		return nil, ErrInvalidUserBatch
	}
	for _, login := range logins {
		if !profileLogin.MatchString(login) {
			return nil, ErrInvalidUserBatch
		}
	}
	return d.store.BatchUsers(ctx, nil, logins)
}
