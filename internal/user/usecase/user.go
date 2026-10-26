package usecase

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/observability"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/user/domain"
)

type UserUsecase struct {
	users      UserStore
	sessions   SessionStore
	tokens     TokenIssuer
	passwords  PasswordHasher
	refresh    RefreshTokens
	refreshTTL time.Duration
	// now подменяется в тестах, чтобы проверить истёкший токен.
	now func() time.Time
}

func NewUserUsecase(users UserStore, sessions SessionStore, tokens TokenIssuer, passwords PasswordHasher, refresh RefreshTokens, refreshTTL time.Duration) *UserUsecase {
	return &UserUsecase{users: users, sessions: sessions, tokens: tokens, passwords: passwords, refresh: refresh, refreshTTL: refreshTTL, now: time.Now}
}

func (uc *UserUsecase) Register(ctx context.Context, login, password string) (domain.User, Session, error) {
	if len(password) > 72 {
		return domain.User{}, Session{}, ErrPasswordTooLong
	}
	hash, err := uc.passwords.Hash(password)
	if err != nil {
		return domain.User{}, Session{}, observability.WithCode(fmt.Errorf("hash password: %w", err), "password_hash_failed")
	}
	u, err := uc.users.CreateUser(ctx, login, string(hash))
	if errors.Is(err, ErrLoginTaken) {
		return domain.User{}, Session{}, ErrLoginTaken
	}
	if err != nil {
		return domain.User{}, Session{}, err
	}
	sess, err := uc.startSession(ctx, u.ID)
	if err == nil {
		slog.InfoContext(ctx, "user.register", slog.String("outcome", "success"), slog.Int64("user_id", u.ID))
	}
	return u, sess, err
}

func (uc *UserUsecase) Login(ctx context.Context, login, password string) (domain.User, Session, error) {
	u, hash, err := uc.users.UserByLogin(ctx, login)
	if errors.Is(err, ErrUserNotFound) {
		return domain.User{}, Session{}, ErrInvalidCredentials
	}
	if err != nil {
		return domain.User{}, Session{}, err
	}
	matched, err := uc.passwords.Compare(hash, password)
	if err != nil {
		return domain.User{}, Session{}, observability.WithCode(fmt.Errorf("compare password: %w", err), "password_compare_failed")
	}
	if !matched {
		return domain.User{}, Session{}, ErrInvalidCredentials
	}
	sess, err := uc.startSession(ctx, u.ID)
	if err == nil {
		slog.InfoContext(ctx, "user.login", slog.String("outcome", "success"), slog.Int64("user_id", u.ID))
	}
	return u, sess, err
}

func (uc *UserUsecase) Refresh(ctx context.Context, refreshToken string) (Session, error) {
	userID, expiresAt, err := uc.sessions.DeleteSession(ctx, uc.refresh.Hash(refreshToken))
	if errors.Is(err, ErrSessionNotFound) {
		return Session{}, ErrInvalidRefresh
	}
	if err != nil {
		return Session{}, err
	}
	if !uc.now().Before(expiresAt) {
		return Session{}, ErrInvalidRefresh
	}
	sess, err := uc.startSession(ctx, userID)
	if err == nil {
		slog.InfoContext(ctx, "session.refresh", slog.String("outcome", "success"), slog.Int64("user_id", userID))
	}
	return sess, err
}

func (uc *UserUsecase) Logout(ctx context.Context, refreshToken string) error {
	_, _, err := uc.sessions.DeleteSession(ctx, uc.refresh.Hash(refreshToken))
	if errors.Is(err, ErrSessionNotFound) {
		return ErrInvalidRefresh
	}
	if err == nil {
		slog.InfoContext(ctx, "session.logout", slog.String("outcome", "success"))
	}
	return err
}

func (uc *UserUsecase) CurrentUser(ctx context.Context, id int64) (domain.User, error) {
	u, err := uc.users.UserByID(ctx, id)
	if errors.Is(err, ErrUserNotFound) {
		return domain.User{}, ErrUserNotFound
	}
	return u, err
}

func (uc *UserUsecase) startSession(ctx context.Context, userID int64) (Session, error) {
	access, accessExpiresAt, err := uc.tokens.IssueAccess(userID)
	if err != nil {
		return Session{}, err
	}
	refresh, hash := uc.refresh.New()
	expiresAt := uc.now().Add(uc.refreshTTL)
	if err := uc.sessions.CreateSession(ctx, userID, hash, expiresAt); err != nil {
		return Session{}, err
	}
	return Session{UserID: userID, AccessToken: access, AccessExpiresAt: accessExpiresAt, RefreshToken: refresh, ExpiresAt: expiresAt}, nil
}
