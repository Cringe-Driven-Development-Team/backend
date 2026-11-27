// Package identityrpc проверяет пользовательские сессии через Identity.
package identityrpc

import (
	"context"
	"time"

	identityv1 "github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/gen/cellestial/identity/v1"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/authn"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/transport/rpcerror"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Session проверяет Bearer через доверенное mTLS-соединение приложения.
type Session struct{ client identityv1.SessionClient }

// NewSession принимает клиент Identity с общими транспортными ограничениями.
func NewSession(client identityv1.SessionClient) *Session { return &Session{client: client} }

var _ authn.SessionVerifier = (*Session)(nil)

// Verify передаёт только токен и возвращает проверенного пользователя или безопасную ошибку.
func (s *Session) Verify(ctx context.Context, token string) (authn.Principal, error) {
	if len(token) == 0 || len(token) > 8192 {
		return authn.Principal{}, authn.ErrInvalidSession
	}
	p, err := s.client.ValidateSession(ctx, &identityv1.ValidateSessionRequest{AccessToken: token})
	if err != nil {
		if ctx.Err() != nil {
			return authn.Principal{}, ctx.Err()
		}
		switch status.Code(err) {
		case codes.Unauthenticated:
			return authn.Principal{}, rpcerror.Preserve(err, rpcerror.CallUserSession, authn.ErrInvalidSession)
		case codes.DeadlineExceeded:
			return authn.Principal{}, context.DeadlineExceeded
		default:
			return authn.Principal{}, rpcerror.Preserve(err, rpcerror.CallDependency, authn.ErrSessionUnavailable)
		}
	}
	if p == nil || p.UserId <= 0 || p.SessionId == "" || p.ExpiresAt == nil || p.ExpiresAt.CheckValid() != nil {
		return authn.Principal{}, authn.ErrSessionUnavailable
	}
	expires := p.ExpiresAt.AsTime()
	if !time.Now().Before(expires) {
		return authn.Principal{}, authn.ErrInvalidSession
	}
	return authn.Principal{UserID: p.UserId, SessionID: p.SessionId, Version: p.SessionVersion, ExpiresAt: expires}, nil
}
