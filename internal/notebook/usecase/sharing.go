package usecase

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/notebook/domain"
)

var readerLogin = regexp.MustCompile(`^[A-Za-z0-9_]{3,32}$`)

// NormalizeName сохраняет общие правила создания, переименования и копирования.
func NormalizeName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", ErrEmptyName
	}
	if !utf8.ValidString(name) || utf8.RuneCountInString(name) > 128 || strings.ContainsRune(name, 0) {
		return "", ErrInvalidName
	}
	return name, nil
}

// ErrUnknownLogin обозначает отсутствующего адресата.
var ErrUnknownLogin = errors.New("unknown login")

// ValidateSharing проверяет весь список до разрешения логинов.
func ValidateSharing(readers []domain.NotebookReader) error {
	if len(readers) > 100 {
		return ErrInvalidSharing
	}
	for _, reader := range readers {
		if !readerLogin.MatchString(reader.Login) || (reader.Role != domain.RoleViewer && reader.Role != domain.RoleCommenter) {
			return ErrInvalidSharing
		}
	}
	return nil
}

// GetSharing читает IDs локально и актуальные логины через batch-порт владельца пользователей.
func (uc *NotebookUsecase) GetSharing(ctx context.Context, id, ownerID int64) (domain.SharingSettings, error) {
	for attempt := 0; attempt < 2; attempt++ {
		settings, err := uc.repo.GetSharing(ctx, id, ownerID)
		if err != nil {
			return domain.SharingSettings{}, err
		}
		if len(settings.Readers) == 0 {
			return settings, nil
		}
		if uc.users == nil {
			return domain.SharingSettings{}, ErrUserDirectoryUnavailable
		}
		ids := make([]int64, 0, len(settings.Readers))
		for _, r := range settings.Readers {
			ids = append(ids, r.ID)
		}
		users, err := uc.users.BatchGet(ctx, ids)
		if err != nil {
			return domain.SharingSettings{}, err
		}
		if result, ok := decorateSharing(settings, users); ok {
			return result, nil
		}
	}
	return domain.SharingSettings{}, ErrUserDirectoryInconsistent
}

// PutSharing разрешает логины до записи и сохраняет последнюю роль каждого адресата.
func (uc *NotebookUsecase) PutSharing(ctx context.Context, id, ownerID int64, link bool, readers []domain.NotebookReader) (domain.SharingSettings, error) {
	if err := uc.repo.CheckOwner(ctx, id, ownerID); err != nil {
		return domain.SharingSettings{}, err
	}
	if err := ValidateSharing(readers); err != nil {
		return domain.SharingSettings{}, err
	}
	logins := make([]string, 0, len(readers))
	resolvedUsers := make(map[string]domain.NotebookReader, len(readers))
	for _, reader := range readers {
		login := strings.ToLower(reader.Login)
		if _, exists := resolvedUsers[login]; !exists {
			logins = append(logins, login)
		}
		resolvedUsers[login] = domain.NotebookReader{}
	}
	users := make(map[int64]domain.NotebookReader, len(logins))
	if len(logins) > 0 {
		if uc.users == nil {
			return domain.SharingSettings{}, ErrUserDirectoryUnavailable
		}
		resolved, err := uc.users.ResolveLogins(ctx, logins)
		if err != nil {
			return domain.SharingSettings{}, err
		}
		if len(resolved) != len(logins) {
			return domain.SharingSettings{}, ErrUserDirectoryInconsistent
		}
		for i, resolution := range resolved {
			if !resolution.Found {
				return domain.SharingSettings{}, ErrUnknownLogin
			}
			if strings.ToLower(resolution.Login) != logins[i] || resolution.User.ID <= 0 {
				return domain.SharingSettings{}, ErrUserDirectoryInconsistent
			}
			resolvedUsers[logins[i]] = resolution.User
		}
	}
	for _, reader := range readers {
		user := resolvedUsers[strings.ToLower(reader.Login)]
		if user.ID != ownerID {
			user.Role = reader.Role
			users[user.ID] = user
		}
	}
	grants := make([]domain.NotebookReader, 0, len(users))
	for _, user := range users {
		grants = append(grants, user)
	}
	saved, err := uc.repo.PutSharing(ctx, id, ownerID, link, grants)
	if err != nil {
		return domain.SharingSettings{}, err
	}
	result, ok := decorateSharing(saved, users)
	if !ok {
		return domain.SharingSettings{}, ErrUserDirectoryInconsistent
	}
	return result, nil
}

// GetShared читает снимок по действующему токену для пользователя с сессией.
func (uc *NotebookUsecase) GetShared(ctx context.Context, token string, userID int64, includeCommentCounts bool) (domain.Notebook, error) {
	return uc.repo.GetShared(ctx, token, userID, includeCommentCounts)
}

func (uc *NotebookUsecase) Clone(ctx context.Context, id, userID int64, name string) (domain.Notebook, error) {
	if userID <= 0 {
		return domain.Notebook{}, ErrNotebookNotFound
	}
	name, err := NormalizeName(name)
	if err != nil {
		return domain.Notebook{}, err
	}
	n, err := uc.repo.Clone(ctx, id, userID, name)
	if err != nil {
		return domain.Notebook{}, err
	}
	n.Access = domain.AccessFor(n.OwnerID, userID)
	return n, nil
}

// CheckRead доступен сценариям комментариев без предоставления прав изменения.
func (uc *NotebookUsecase) CheckRead(ctx context.Context, id, userID int64) error {
	return uc.repo.CheckRead(ctx, id, userID)
}
