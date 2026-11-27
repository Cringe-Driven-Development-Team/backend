package grpcdelivery

import (
	"context"
	"errors"
	"strings"

	identityv1 "github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/gen/cellestial/identity/v1"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/user/domain"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/user/usecase"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Directory определяет прикладные batch-операции без зависимости от PostgreSQL.
type Directory interface {
	ByIDs(context.Context, []int64) ([]domain.UserSuggestion, error)
	ByLogins(context.Context, []string) ([]domain.UserSuggestion, error)
}

// UsersServer выдаёт только публичные поля доверенному Workspace.
type UsersServer struct {
	identityv1.UnimplementedUsersServer
	directory Directory
}

// NewUsersServer связывает транспорт с прикладным каталогом пользователей.
func NewUsersServer(d Directory) *UsersServer { return &UsersServer{directory: d} }

// BatchGetUsers проверяет ID и возвращает найденных пользователей одним batch.
func (s *UsersServer) BatchGetUsers(ctx context.Context, r *identityv1.BatchGetUsersRequest) (*identityv1.UsersResponse, error) {
	users, err := s.directory.ByIDs(ctx, r.GetUserIds())
	return directoryResponse(ctx, users, err, r.GetUserIds(), nil)
}

// BatchResolveUsers разрешает логины без учёта регистра.
func (s *UsersServer) BatchResolveUsers(ctx context.Context, r *identityv1.BatchResolveUsersRequest) (*identityv1.UsersResponse, error) {
	users, err := s.directory.ByLogins(ctx, r.GetLogins())
	return directoryResponse(ctx, users, err, nil, r.GetLogins())
}

// directoryResponse скрывает ошибки БД и не превращает отказ зависимости в пустой успех.
func directoryResponse(ctx context.Context, users []domain.UserSuggestion, err error, ids []int64, logins []string) (*identityv1.UsersResponse, error) {
	if err != nil {
		switch {
		case ctx.Err() != nil:
			return nil, status.FromContextError(ctx.Err()).Err()
		case errors.Is(err, usecase.ErrInvalidUserBatch):
			return nil, status.Error(codes.InvalidArgument, "invalid user batch")
		default:
			return nil, status.Error(codes.Unavailable, "user directory unavailable")
		}
	}
	out := &identityv1.UsersResponse{Users: make([]*identityv1.User, 0, len(users))}
	for _, u := range users {
		out.Users = append(out.Users, &identityv1.User{Id: u.ID, Login: u.Login})
	}
	byID := make(map[int64]*identityv1.User, len(out.Users))
	byLogin := make(map[string]*identityv1.User, len(out.Users))
	for _, u := range out.Users {
		byID[u.Id] = u
		byLogin[strings.ToLower(u.Login)] = u
	}
	for _, id := range ids {
		resolution := &identityv1.UserResolution{RequestedId: id, Outcome: &identityv1.UserResolution_NotFound{NotFound: true}}
		if user, ok := byID[id]; ok {
			resolution.Outcome = &identityv1.UserResolution_User{User: user}
		}
		out.Results = append(out.Results, resolution)
	}
	for _, login := range logins {
		resolution := &identityv1.UserResolution{RequestedLogin: login, Outcome: &identityv1.UserResolution_NotFound{NotFound: true}}
		if user, ok := byLogin[strings.ToLower(login)]; ok {
			resolution.Outcome = &identityv1.UserResolution_User{User: user}
		}
		out.Results = append(out.Results, resolution)
	}
	return out, nil
}
