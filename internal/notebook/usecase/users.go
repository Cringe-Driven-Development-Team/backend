package usecase

import (
	"context"
	"errors"
	"sort"
	"strings"

	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/authn"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/notebook/domain"
)

// ErrUserDirectoryUnavailable не позволяет представить отказ Identity как пустые профили.
var ErrUserDirectoryUnavailable = errors.New("user directory unavailable")

// ErrUserDirectoryInconsistent обозначает оставшуюся после одного перечитывания битую ссылку.
var ErrUserDirectoryInconsistent = errors.New("user directory inconsistent")

// UserResolution связывает исходный login с явным результатом поиска стабильного ID.
type UserResolution struct {
	Login string
	User  domain.NotebookReader
	Found bool
}

// Users задаёт batch-порт Identity; usecase не знает gRPC или таблиц пользователей.
type Users interface {
	BatchGet(context.Context, []int64) (map[int64]domain.NotebookReader, error)
	ResolveLogins(context.Context, []string) ([]UserResolution, error)
}

// WithUsers подключает только публичный каталог, не авторизационные secrets Identity.
func WithUsers(users Users) Option { return func(uc *NotebookUsecase) { uc.users = users } }

// author подтверждает профиль до локальной записи; сетевой вызов не выполняется под notebook lock.
func (uc *NotebookUsecase) author(ctx context.Context, id int64) (domain.CommentAuthor, error) {
	if id <= 0 {
		return domain.CommentAuthor{}, ErrCommentForbidden
	}
	if uc.users == nil {
		return domain.CommentAuthor{}, ErrUserDirectoryUnavailable
	}
	users, err := uc.users.BatchGet(ctx, []int64{id})
	if err != nil {
		return domain.CommentAuthor{}, err
	}
	user, ok := users[id]
	if !ok || user.ID != id || user.Login == "" {
		return domain.CommentAuthor{}, authn.ErrInvalidSession
	}
	return domain.CommentAuthor{ID: id, Login: user.Login, AvatarURL: user.AvatarURL}, nil
}

// decorateComments дополняет страницу одним batch; false означает конкурентно удалённого автора.
func (uc *NotebookUsecase) decorateComments(ctx context.Context, items []domain.Comment) (bool, error) {
	if len(items) == 0 {
		return true, nil
	}
	if uc.users == nil {
		return false, ErrUserDirectoryUnavailable
	}
	ids := make([]int64, 0, len(items))
	seen := map[int64]bool{}
	for _, c := range items {
		if c.Author.ID <= 0 {
			return false, nil
		}
		if !seen[c.Author.ID] {
			seen[c.Author.ID] = true
			ids = append(ids, c.Author.ID)
		}
	}
	users, err := uc.users.BatchGet(ctx, ids)
	if err != nil {
		return false, err
	}
	for i := range items {
		user, ok := users[items[i].Author.ID]
		if !ok || user.ID != items[i].Author.ID || user.Login == "" {
			return false, nil
		}
		items[i].Author.Login = user.Login
		items[i].Author.AvatarURL = user.AvatarURL
	}
	return true, nil
}

// decorateSharing устанавливает актуальные логины и прежнюю сортировку без SQL JOIN users.
func decorateSharing(settings domain.SharingSettings, users map[int64]domain.NotebookReader) (domain.SharingSettings, bool) {
	for i := range settings.Readers {
		user, ok := users[settings.Readers[i].ID]
		if !ok || user.Login == "" {
			return domain.SharingSettings{}, false
		}
		settings.Readers[i] = user
	}
	sort.Slice(settings.Readers, func(i, j int) bool {
		a, b := strings.ToLower(settings.Readers[i].Login), strings.ToLower(settings.Readers[j].Login)
		if a == b {
			return settings.Readers[i].ID < settings.Readers[j].ID
		}
		return a < b
	})
	return settings, true
}
