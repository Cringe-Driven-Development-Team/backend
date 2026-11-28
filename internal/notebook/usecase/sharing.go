package usecase

import (
	"context"
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

func ValidateSharing(mode domain.SharingMode, logins []string) error {
	switch mode {
	case domain.SharingModePrivate, domain.SharingModePublic:
		if len(logins) != 0 {
			return ErrInvalidSharing
		}
	case domain.SharingModeRestricted:
	default:
		return ErrInvalidSharing
	}
	for _, login := range logins {
		if !readerLogin.MatchString(login) {
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

// PutSharing разрешает логины до транзакции, сохраняя IDs при конкурентном переименовании.
func (uc *NotebookUsecase) PutSharing(ctx context.Context, id, ownerID int64, mode domain.SharingMode, readerLogins []string) (domain.SharingSettings, error) {
	if err := uc.repo.CheckOwner(ctx, id, ownerID); err != nil {
		return domain.SharingSettings{}, err
	}
	if err := ValidateSharing(mode, readerLogins); err != nil {
		return domain.SharingSettings{}, err
	}
	unique := map[string]bool{}
	logins := make([]string, 0, len(readerLogins))
	for _, login := range readerLogins {
		key := strings.ToLower(login)
		if !unique[key] {
			unique[key] = true
			logins = append(logins, key)
		}
	}
	ids := make([]int64, 0, len(logins))
	users := map[int64]domain.NotebookReader{}
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
				return domain.SharingSettings{}, ErrInvalidSharing
			}
			if strings.ToLower(resolution.Login) != logins[i] || resolution.User.ID <= 0 {
				return domain.SharingSettings{}, ErrUserDirectoryInconsistent
			}
			if resolution.User.ID != ownerID {
				ids = append(ids, resolution.User.ID)
				users[resolution.User.ID] = resolution.User
			}
		}
	}
	saved, err := uc.repo.PutSharing(ctx, id, ownerID, mode, ids)
	if err != nil {
		return domain.SharingSettings{}, err
	}
	result, ok := decorateSharing(saved, users)
	if !ok {
		return domain.SharingSettings{}, ErrUserDirectoryInconsistent
	}
	return result, nil
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
