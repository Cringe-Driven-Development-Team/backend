package usecase

import (
	"context"

	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/user/domain"
)

type UserStore interface {
	CreateUser(ctx context.Context, login, passwordHash string) (domain.User, error)
	// UserByLogin ищет без учёта регистра, как и уникальный индекс.
	UserByLogin(ctx context.Context, login string) (u domain.User, passwordHash string, err error)
	UserByID(ctx context.Context, id int64) (domain.User, error)
}

// PasswordHasher отделяет сценарии от алгоритма хранения паролей.
type PasswordHasher interface {
	Hash(password string) (string, error)
	// Compare отличает несовпадение (false, nil) от сбоя проверки.
	Compare(hash, password string) (matched bool, err error)
}

// RefreshTokens выпускает случайный refresh-токен и его хеш для хранилища.
type RefreshTokens interface {
	New() (token, hash string)
	Hash(token string) string
}
