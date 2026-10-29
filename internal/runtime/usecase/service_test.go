package usecase

import (
	"context"
	"errors"
	"testing"

	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/runtime/domain"
	"github.com/google/uuid"
	"go.uber.org/mock/gomock"
)

func newSvc(t *testing.T, enabled bool) (*Service, *MockEnvironmentRepository) {
	ctrl := gomock.NewController(t)
	repo := NewMockEnvironmentRepository(ctrl)
	return NewService(repo, Settings{Enabled: enabled, MaxActive: 3, AgentVersion: "1.2.3", Profiles: map[string]bool{"default": true}}), repo
}

func TestRequestCreateNew(t *testing.T) {
	svc, repo := newSvc(t, true)
	id := uuid.New()
	repo.EXPECT().CreateWithJob(gomock.Any(), gomock.Any(), 3).DoAndReturn(
		func(_ context.Context, e domain.Environment, _ int) (domain.Environment, bool, error) {
			mustEq(t, id, e.SessionID)
			mustEq(t, domain.StateQueued, e.State)
			mustEq(t, domain.StateReady, e.DesiredState)
			if e.OperationID == uuid.Nil {
				t.Fatal("operation id is nil")
			}
			mustEq(t, "1.2.3", e.AgentVersion)
			mustEq(t, "default", e.Profile)
			return e, true, nil
		})
	env, err := svc.RequestCreate(context.Background(), CreateEnvironment{SessionID: id, Language: "python"})
	mustNoErr(t, err)
	mustEq(t, "python", env.Language)
	mustEq(t, "default", env.Profile)
}

func existing(state domain.State, lang string) domain.Environment {
	return domain.Environment{SessionID: uuid.New(), Language: lang, Profile: "default", State: state}
}

func TestRequestCreateSameParamsReturnsExisting(t *testing.T) {
	svc, repo := newSvc(t, true)
	ex := existing(domain.StateCreating, "r")
	repo.EXPECT().CreateWithJob(gomock.Any(), gomock.Any(), 3).Return(ex, false, nil)
	env, err := svc.RequestCreate(context.Background(), CreateEnvironment{SessionID: ex.SessionID, Language: "r", Profile: "default"})
	mustNoErr(t, err)
	mustEq(t, ex, env)
}

func TestRequestCreateDifferentParamsConflict(t *testing.T) {
	svc, repo := newSvc(t, true)
	ex := existing(domain.StateCreating, "r")
	repo.EXPECT().CreateWithJob(gomock.Any(), gomock.Any(), 3).Return(ex, false, nil)
	_, err := svc.RequestCreate(context.Background(), CreateEnvironment{SessionID: ex.SessionID, Language: "python"})
	mustIs(t, err, ErrConflict)
}

func TestRequestCreateFinishedReturnedAsIs(t *testing.T) {
	svc, repo := newSvc(t, true)
	ex := existing(domain.StateDeleted, "python")
	repo.EXPECT().CreateWithJob(gomock.Any(), gomock.Any(), 3).Return(ex, false, nil)
	env, err := svc.RequestCreate(context.Background(), CreateEnvironment{SessionID: ex.SessionID, Language: "python"})
	mustNoErr(t, err)
	mustEq(t, domain.StateDeleted, env.State)
}

func TestRequestCreateInvalidLanguage(t *testing.T) {
	svc, _ := newSvc(t, true)
	_, err := svc.RequestCreate(context.Background(), CreateEnvironment{SessionID: uuid.New(), Language: "julia"})
	mustIs(t, err, ErrInvalidLanguage)
}

func TestRequestCreateUnknownProfile(t *testing.T) {
	svc, _ := newSvc(t, true)
	_, err := svc.RequestCreate(context.Background(), CreateEnvironment{SessionID: uuid.New(), Language: "r", Profile: "big"})
	mustIs(t, err, ErrUnknownProfile)
}

func TestRequestCreateDisabled(t *testing.T) {
	svc, _ := newSvc(t, false)
	_, err := svc.RequestCreate(context.Background(), CreateEnvironment{SessionID: uuid.New(), Language: "r"})
	mustIs(t, err, ErrDisabled)
}

func TestRequestDeleteIdempotent(t *testing.T) {
	svc, repo := newSvc(t, true)
	id := uuid.New()
	repo.EXPECT().MarkForDelete(gomock.Any(), id, "why").Return(nil).Times(2)
	mustNoErr(t, svc.RequestDelete(context.Background(), id, "why"))
	mustNoErr(t, svc.RequestDelete(context.Background(), id, "why"))
}

func TestRequestDeleteNotFound(t *testing.T) {
	svc, repo := newSvc(t, true)
	id := uuid.New()
	repo.EXPECT().MarkForDelete(gomock.Any(), id, "x").Return(ErrNotFound)
	mustIs(t, svc.RequestDelete(context.Background(), id, "x"), ErrNotFound)
}

func mustNoErr(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func mustIs(t *testing.T, err, target error) {
	t.Helper()
	if !errors.Is(err, target) {
		t.Fatalf("got %v, want %v", err, target)
	}
}

func mustEq[T comparable](t *testing.T, want, got T) {
	t.Helper()
	if want != got {
		t.Fatalf("got %v, want %v", got, want)
	}
}
