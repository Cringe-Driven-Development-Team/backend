package grpcdelivery

import (
	"context"

	identityv1 "github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/gen/cellestial/identity/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// OnlineConfRoles читает актуальное назначение роли по подтверждённому ID пользователя.
type OnlineConfRoles interface {
	HasOnlineConfAdmin(context.Context, int64) (bool, error)
}

// OnlineConfAccessServer проверяет сессию и назначение роли для каждого вызова.
type OnlineConfAccessServer struct {
	identityv1.UnimplementedOnlineConfAccessServer
	sessions *SessionServer
	roles    OnlineConfRoles
}

// NewOnlineConfAccessServer связывает проверку сессии с хранилищем ролей Identity.
func NewOnlineConfAccessServer(sessions *SessionServer, roles OnlineConfRoles) *OnlineConfAccessServer {
	return &OnlineConfAccessServer{sessions: sessions, roles: roles}
}

// Authorize подтверждает доступ владельца токена или возвращает безопасную gRPC-ошибку.
func (s *OnlineConfAccessServer) Authorize(ctx context.Context, req *identityv1.ValidateSessionRequest) (*identityv1.OnlineConfAuthorization, error) {
	session, err := s.sessions.ValidateSession(ctx, req)
	if err != nil {
		return nil, err
	}
	granted, err := s.roles.HasOnlineConfAdmin(ctx, session.GetUserId())
	if err != nil {
		return nil, status.Error(codes.Unavailable, "role dependency unavailable")
	}
	if !granted {
		return nil, status.Error(codes.PermissionDenied, "onlineconf_admin role required")
	}
	return &identityv1.OnlineConfAuthorization{UserId: session.GetUserId()}, nil
}
