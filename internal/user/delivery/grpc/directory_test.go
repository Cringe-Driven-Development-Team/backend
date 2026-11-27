package grpcdelivery

import (
	"context"
	"testing"

	identityv1 "github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/gen/cellestial/identity/v1"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/user/domain"
	"google.golang.org/protobuf/proto"
)

// directoryFixture возвращает найденную запись без фиктивных DTO отсутствующего пользователя.
type directoryFixture struct{}

func (directoryFixture) ByIDs(context.Context, []int64) ([]domain.UserSuggestion, error) {
	return []domain.UserSuggestion{{ID: 42, Login: "Reader", AvatarKey: "avatars/42.png"}}, nil
}
func (directoryFixture) ByLogins(context.Context, []string) ([]domain.UserSuggestion, error) {
	return []domain.UserSuggestion{{ID: 42, Login: "Reader"}}, nil
}

// TestBatchUsersExplicitMissing сохраняет порядок селекторов и различимость отсутствия/найденного ID.
func TestBatchUsersExplicitMissing(t *testing.T) {
	server := NewUsersServer(directoryFixture{}, "https://cdn.example.com/")
	byID, err := server.BatchGetUsers(context.Background(), &identityv1.BatchGetUsersRequest{UserIds: []int64{42, 99, 42}})
	if err != nil || len(byID.GetResults()) != 3 {
		t.Fatalf("missing ID resolutions: %v", err)
	}
	wire, err := proto.Marshal(byID)
	if err != nil {
		t.Fatal(err)
	}
	byID = &identityv1.UsersResponse{}
	if err := proto.Unmarshal(wire, byID); err != nil {
		t.Fatal(err)
	}
	if byID.Results[0].GetUser().GetAvatarUrl() != "https://cdn.example.com/avatars/42.png" || byID.Results[0].GetUser().GetId() != 42 || !byID.Results[1].GetNotFound() || byID.Results[1].RequestedId != 99 || byID.Results[2].GetUser().GetId() != 42 {
		t.Fatal("incorrect ID outcomes")
	}
	byLogin, err := server.BatchResolveUsers(context.Background(), &identityv1.BatchResolveUsersRequest{Logins: []string{"READER", "missing"}})
	if err != nil || len(byLogin.GetResults()) != 2 {
		t.Fatalf("missing login resolutions: %v", err)
	}
	if byLogin.Results[0].GetUser().GetId() != 42 || !byLogin.Results[1].GetNotFound() || byLogin.Results[1].RequestedLogin != "missing" {
		t.Fatal("incorrect login outcomes")
	}
}
