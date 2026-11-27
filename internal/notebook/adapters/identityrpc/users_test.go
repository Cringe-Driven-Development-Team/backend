package identityrpc

import (
	"context"
	"errors"
	"strings"
	"testing"

	identityv1 "github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/gen/cellestial/identity/v1"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/notebook/usecase"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// usersFake возвращает явные результаты и фиксирует лимит одного RPC.
type usersFake struct {
	identityv1.UsersClient
	calls, largest int
	corrupt        bool
	err            error
}

// BatchResolveUsers намеренно меняет порядок результата, сохраняя исходный селектор.
func (f *usersFake) BatchResolveUsers(_ context.Context, req *identityv1.BatchResolveUsersRequest, _ ...grpc.CallOption) (*identityv1.UsersResponse, error) {
	response := &identityv1.UsersResponse{}
	for i := len(req.Logins) - 1; i >= 0; i-- {
		login := req.Logins[i]
		result := &identityv1.UserResolution{RequestedLogin: login, Outcome: &identityv1.UserResolution_NotFound{NotFound: true}}
		if login != "missing" {
			result.Outcome = &identityv1.UserResolution_User{User: &identityv1.User{Id: 2, Login: strings.ToUpper(login)}}
		}
		response.Results = append(response.Results, result)
	}
	if f.corrupt && len(response.Results) > 0 {
		response.Results[0].Outcome = nil
	}
	return response, f.err
}

// TestResolvePreservesSelectors проверяет регистр, дубликаты, not-found и неполный oneof.
func TestResolvePreservesSelectors(t *testing.T) {
	fake := &usersFake{}
	users := NewUsers(fake)
	got, err := users.ResolveLogins(t.Context(), []string{"Reader", "missing", "READER"})
	if err != nil || len(got) != 3 || !got[0].Found || got[1].Found || !got[2].Found || got[0].Login != "Reader" || got[2].Login != "READER" || got[0].User.Login != "READER" {
		t.Fatalf("selectors: %+v %v", got, err)
	}
	fake.corrupt = true
	if _, err = users.ResolveLogins(t.Context(), []string{"reader"}); !errors.Is(err, usecase.ErrUserDirectoryInconsistent) {
		t.Fatal("invalid oneof accepted", err)
	}
}

func (f *usersFake) BatchGetUsers(_ context.Context, req *identityv1.BatchGetUsersRequest, _ ...grpc.CallOption) (*identityv1.UsersResponse, error) {
	f.calls++
	if len(req.UserIds) > f.largest {
		f.largest = len(req.UserIds)
	}
	if f.err != nil {
		return nil, f.err
	}
	out := &identityv1.UsersResponse{}
	for _, id := range req.UserIds {
		row := &identityv1.UserResolution{RequestedId: id, Outcome: &identityv1.UserResolution_NotFound{NotFound: true}}
		if id%2 == 0 {
			row.Outcome = &identityv1.UserResolution_User{User: &identityv1.User{Id: id, Login: "reader"}}
		}
		out.Results = append(out.Results, row)
	}
	if f.corrupt {
		out.Results = nil
	}
	return out, nil
}

// TestBatchDirectoryLimits проверяет дедупликацию и границу 100 селекторов на RPC.
func TestBatchDirectoryLimits(t *testing.T) {
	fake := &usersFake{}
	users := NewUsers(fake)
	ids := make([]int64, 0, 251)
	for i := int64(1); i <= 250; i++ {
		ids = append(ids, i)
	}
	ids = append(ids, 2)
	got, err := users.BatchGet(t.Context(), ids)
	if err != nil || len(got) != 125 || fake.calls != 3 || fake.largest != 100 {
		t.Fatalf("batch %d %d %d %v", len(got), fake.calls, fake.largest, err)
	}
	fake.corrupt = true
	if _, err = users.BatchGet(t.Context(), []int64{2}); !errors.Is(err, usecase.ErrUserDirectoryInconsistent) {
		t.Fatal("missing outcome hidden", err)
	}
	fake.corrupt = false
	fake.err = status.Error(codes.Unavailable, "private endpoint")
	if _, err = users.BatchGet(t.Context(), []int64{2}); !errors.Is(err, usecase.ErrUserDirectoryUnavailable) || err.Error() == fake.err.Error() {
		t.Fatal("unsafe dependency error", err)
	}
}
