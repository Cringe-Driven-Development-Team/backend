package usecase

import (
	"context"
	"errors"
	"testing"
	"time"
)

// accessParserStub заменяет только криптографический адаптер в тесте сценария.
type accessParserStub struct {
	session ValidatedSession
	err     error
}

// ParseAccess возвращает заранее заданный результат проверки токена.
func (p accessParserStub) ParseAccess(string) (ValidatedSession, error) { return p.session, p.err }

// accessStoreStub отделяет ошибки отзыва сессии от сбоя внешнего Redis.
type accessStoreStub struct{ err error }

// ValidateAccess сохраняет отмену вызывающего контекста при проверке сессии.
func (s accessStoreStub) ValidateAccess(ctx context.Context, _ SessionClaims) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return s.err
}

// TestSessionValidator проверяет, что валидный JWT не обходит отзыв или отказ Redis.
func TestSessionValidator(t *testing.T) {
	expected := ValidatedSession{SessionClaims: SessionClaims{UserID: 42, SessionID: "session-42", Version: 7}, ExpiresAt: time.Now().Add(time.Hour)}
	unavailable := errors.New("storage unavailable")
	for _, tc := range []struct {
		name     string
		parseErr error
		storeErr error
		wantErr  error
	}{
		{name: "valid session"},
		{name: "invalid JWT", parseErr: errors.New("invalid signature"), wantErr: ErrInvalidSession},
		{name: "revoked session", storeErr: ErrInvalidSession, wantErr: ErrInvalidSession},
		{name: "Redis unavailable", storeErr: unavailable, wantErr: unavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			validator := NewSessionValidator(accessParserStub{session: expected, err: tc.parseErr}, accessStoreStub{err: tc.storeErr})
			got, err := validator.Verify(context.Background(), "test-access-token")
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) || got != (ValidatedSession{}) {
					t.Fatalf("Verify() = %+v, %v; want empty session, %v", got, err, tc.wantErr)
				}
				return
			}
			if err != nil || got != expected {
				t.Fatalf("Verify() = %+v, %v; want %+v, nil", got, err, expected)
			}
		})
	}
}

// TestSessionValidatorCancellation не превращает отмену запроса в успешную авторизацию.
func TestSessionValidatorCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	validator := NewSessionValidator(accessParserStub{}, accessStoreStub{})
	got, err := validator.Verify(ctx, "test-access-token")
	if !errors.Is(err, context.Canceled) || got != (ValidatedSession{}) {
		t.Fatalf("Verify() = %+v, %v; want empty session, context.Canceled", got, err)
	}
}
