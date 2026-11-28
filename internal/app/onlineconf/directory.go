package onlineconf

import (
	"context"
	"slices"

	identityv1 "github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/gen/cellestial/identity/v1"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/transport/http/principal"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// directoryError отличает неверный запрос от отказа Identity.
func directoryError(err error) error {
	if status.Code(err) == codes.InvalidArgument {
		return errValidation
	}
	return errDirectory
}

// actor читает подтверждённого сессией пользователя.
func (h keysHandler) actor(ctx context.Context) (userRef, error) {
	id, _ := principal.UserIDFromContext(ctx)
	users, err := h.byIDs(ctx, []int64{id})
	if err != nil {
		return userRef{}, err
	}
	user, ok := users[id]
	if !ok {
		return userRef{}, errDirectory
	}
	return user, nil
}

// byIDs читает каталог порциями до 100 ID, что соответствует ограничению Identity.
func (h keysHandler) byIDs(ctx context.Context, ids []int64) (map[int64]userRef, error) {
	slices.Sort(ids)
	ids = slices.Compact(ids)
	out := make(map[int64]userRef, len(ids))
	for batch := range slices.Chunk(ids, 100) {
		response, err := h.users.BatchGetUsers(ctx, &identityv1.BatchGetUsersRequest{UserIds: batch})
		if err != nil {
			return nil, directoryError(err)
		}
		for _, user := range response.GetUsers() {
			out[user.GetId()] = userRef{ID: user.GetId(), Login: user.GetLogin()}
		}
	}
	return out, nil
}
