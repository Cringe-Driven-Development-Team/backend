// Package identityrpc реализует каталог пользователей через внутренний API Identity.
package identityrpc

import (
	"context"
	"errors"
	"strings"

	identityv1 "github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/gen/cellestial/identity/v1"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/notebook/domain"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/notebook/usecase"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/transport/rpcerror"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Users переводит protobuf в доменные DTO; соединением и его mTLS владеет application.
type Users struct{ client identityv1.UsersClient }

// NewUsers принимает клиент с общими interceptors, deadlines и ограниченными read-retry.
func NewUsers(client identityv1.UsersClient) *Users { return &Users{client: client} }

var _ usecase.Users = (*Users)(nil)

// directoryError скрывает транспортные подробности, сохраняя отмену входного запроса.
func directoryError(ctx context.Context, callErr error) error {
	if err := ctx.Err(); err != nil {
		return errors.Join(usecase.ErrUserDirectoryUnavailable, err)
	}
	if status.Code(callErr) == codes.DeadlineExceeded {
		return errors.Join(usecase.ErrUserDirectoryUnavailable, context.DeadlineExceeded)
	}
	return rpcerror.Preserve(callErr, rpcerror.CallDependency, usecase.ErrUserDirectoryUnavailable)
}

// BatchGet дедуплицирует IDs и читает не более 100 за RPC без N+1 запросов.
func (u *Users) BatchGet(ctx context.Context, ids []int64) (map[int64]domain.NotebookReader, error) {
	unique := make([]int64, 0, len(ids))
	seen := map[int64]bool{}
	for _, id := range ids {
		if id <= 0 {
			return nil, usecase.ErrUserDirectoryInconsistent
		}
		if !seen[id] {
			seen[id] = true
			unique = append(unique, id)
		}
	}
	out := map[int64]domain.NotebookReader{}
	for start := 0; start < len(unique); start += 100 {
		end := min(start+100, len(unique))
		batch := unique[start:end]
		response, err := u.client.BatchGetUsers(ctx, &identityv1.BatchGetUsersRequest{UserIds: batch})
		if err != nil {
			return nil, directoryError(ctx, err)
		}
		if response == nil || len(response.Results) != len(batch) {
			return nil, usecase.ErrUserDirectoryInconsistent
		}
		requested := map[int64]bool{}
		for _, id := range batch {
			requested[id] = true
		}
		for _, result := range response.Results {
			if result == nil || !requested[result.RequestedId] || result.RequestedLogin != "" {
				return nil, usecase.ErrUserDirectoryInconsistent
			}
			delete(requested, result.RequestedId)
			user, found, valid := resolution(result)
			if !valid || (found && user.ID != result.RequestedId) {
				return nil, usecase.ErrUserDirectoryInconsistent
			}
			if found {
				out[user.ID] = user
			}
		}
	}
	return out, nil
}

// ResolveLogins сохраняет порядок селекторов и явный not-found независимо от порядка RPC-ответа.
func (u *Users) ResolveLogins(ctx context.Context, logins []string) ([]usecase.UserResolution, error) {
	unique := make([]string, 0, len(logins))
	seen := map[string]bool{}
	for _, login := range logins {
		key := strings.ToLower(login)
		if key == "" {
			return nil, usecase.ErrUserDirectoryInconsistent
		}
		if !seen[key] {
			seen[key] = true
			unique = append(unique, key)
		}
	}
	resolved := map[string]usecase.UserResolution{}
	for start := 0; start < len(unique); start += 100 {
		end := min(start+100, len(unique))
		batch := unique[start:end]
		response, err := u.client.BatchResolveUsers(ctx, &identityv1.BatchResolveUsersRequest{Logins: batch})
		if err != nil {
			return nil, directoryError(ctx, err)
		}
		if response == nil || len(response.Results) != len(batch) {
			return nil, usecase.ErrUserDirectoryInconsistent
		}
		requested := map[string]bool{}
		for _, login := range batch {
			requested[login] = true
		}
		for _, result := range response.Results {
			if result == nil {
				return nil, usecase.ErrUserDirectoryInconsistent
			}
			key := strings.ToLower(result.RequestedLogin)
			if !requested[key] || result.RequestedId != 0 {
				return nil, usecase.ErrUserDirectoryInconsistent
			}
			delete(requested, key)
			user, found, valid := resolution(result)
			if !valid || (found && strings.ToLower(user.Login) != key) {
				return nil, usecase.ErrUserDirectoryInconsistent
			}
			resolved[key] = usecase.UserResolution{User: user, Found: found}
		}
	}
	out := make([]usecase.UserResolution, len(logins))
	for i, login := range logins {
		out[i] = resolved[strings.ToLower(login)]
		out[i].Login = login
	}
	return out, nil
}

// resolution не принимает пустой oneof или false not-found за успешный профиль.
func resolution(result *identityv1.UserResolution) (domain.NotebookReader, bool, bool) {
	switch value := result.Outcome.(type) {
	case *identityv1.UserResolution_User:
		if value.User != nil && value.User.Id > 0 && value.User.Login != "" {
			return domain.NotebookReader{ID: value.User.Id, Login: value.User.Login}, true, true
		}
	case *identityv1.UserResolution_NotFound:
		return domain.NotebookReader{}, false, value.NotFound
	}
	return domain.NotebookReader{}, false, false
}
