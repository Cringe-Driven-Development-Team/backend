package usecase

import (
	"context"
	"time"
)

// ValidatedSession содержит подтверждённые данные и срок действия access-токена.
type ValidatedSession struct {
	SessionClaims
	ExpiresAt time.Time
}

// AccessTokenParser проверяет подпись, срок и обязательные поля access-токена.
type AccessTokenParser interface {
	ParseAccess(string) (ValidatedSession, error)
}

// AccessSessions проверяет актуальность сессии в основном хранилище авторизации.
type AccessSessions interface {
	ValidateAccess(context.Context, SessionClaims) error
}

// SessionValidator объединяет проверку JWT и отзыва сессии без привязки к транспорту.
type SessionValidator struct {
	tokens   AccessTokenParser
	sessions AccessSessions
}

// NewSessionValidator связывает проверку токенов tokens и хранилище sessions.
func NewSessionValidator(tokens AccessTokenParser, sessions AccessSessions) *SessionValidator {
	return &SessionValidator{tokens: tokens, sessions: sessions}
}

// Verify возвращает сессию только после проверки токена и актуального состояния хранилища.
// Ошибка хранилища сохраняется: недоступность Redis не означает неверный пароль или токен.
func (v *SessionValidator) Verify(ctx context.Context, token string) (ValidatedSession, error) {
	if err := ctx.Err(); err != nil {
		return ValidatedSession{}, err
	}
	session, err := v.tokens.ParseAccess(token)
	if err != nil {
		return ValidatedSession{}, ErrInvalidSession
	}
	if err = v.sessions.ValidateAccess(ctx, session.SessionClaims); err != nil {
		return ValidatedSession{}, err
	}
	return session, nil
}

// ActiveAccounts проверяет актуальность аккаунта при физическом удалении.
type ActiveAccounts interface {
	ActiveAccount(context.Context, int64) error
}

// WithActiveAccounts запрещает refresh для закрытого аккаунта.
func WithActiveAccounts(accounts ActiveAccounts) func(*UserUsecase) {
	return func(u *UserUsecase) { u.active = accounts }
}

// GuardedAccessSessions объединяет проверку Redis и durable состояние Identity.
type GuardedAccessSessions struct {
	Sessions AccessSessions
	Accounts ActiveAccounts
}

// ValidateAccess подтверждает сессию лишь для активного аккаунта.
func (s GuardedAccessSessions) ValidateAccess(ctx context.Context, claims SessionClaims) error {
	if err := s.Sessions.ValidateAccess(ctx, claims); err != nil {
		return err
	}
	return s.Accounts.ActiveAccount(ctx, claims.UserID)
}
