// Package grpcdelivery обслуживает внутренние RPC сервиса Identity.
package grpcdelivery

import (
	"context"
	"errors"

	identityv1 "github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/gen/cellestial/identity/v1"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/user/usecase"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// SessionVerifier отделяет транспорт от проверки JWT и отзыва сессии.
type SessionVerifier interface {
	Verify(context.Context, string) (usecase.ValidatedSession, error)
}

// SessionServer реализует внутреннюю проверку сессии без передачи секретов потребителю.
type SessionServer struct {
	identityv1.UnimplementedSessionServer
	validator SessionVerifier
}

// NewSessionServer связывает RPC с прикладным validator.
func NewSessionServer(validator SessionVerifier) *SessionServer {
	return &SessionServer{validator: validator}
}

// ValidateSession возвращает подтверждённый субъект или безопасную транспортную ошибку.
func (s *SessionServer) ValidateSession(ctx context.Context, req *identityv1.ValidateSessionRequest) (*identityv1.ValidateSessionResponse, error) {
	if req == nil || req.AccessToken == "" || len(req.AccessToken) > 8192 {
		return nil, status.Error(codes.InvalidArgument, "invalid access token input")
	}
	session, err := s.validator.Verify(ctx, req.AccessToken)
	if err != nil {
		switch {
		case ctx.Err() != nil:
			return nil, status.FromContextError(ctx.Err()).Err()
		case errors.Is(err, usecase.ErrInvalidSession):
			return nil, status.Error(codes.Unauthenticated, "authentication required")
		default:
			return nil, status.Error(codes.Unavailable, "session dependency unavailable")
		}
	}
	return &identityv1.ValidateSessionResponse{
		UserId: session.UserID, SessionId: session.SessionID, SessionVersion: uint64(session.Version),
		ExpiresAt: timestamppb.New(session.ExpiresAt),
	}, nil
}
