// Package grpcdelivery обслуживает закрытый RPC вход Workspace.
package grpcdelivery

import (
	"context"
	"errors"

	pb "github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/gen/cellestial/workspace/v1"
	D "github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/runtimesession/domain"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/runtimesession/usecase"
	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// UserCleanup принимает только сервисные запросы Identity после mTLS.
type UserCleanup struct {
	pb.UnimplementedUserCleanupServer
	store usecase.UserCleanupStore
}

// NewUserCleanup связывает RPC с локальным барьером Workspace.
func NewUserCleanup(store usecase.UserCleanupStore) *UserCleanup { return &UserCleanup{store: store} }

// BeginUserCleanup ставит tombstone и продолжает удаление зависимостей по operation ID.
func (s *UserCleanup) BeginUserCleanup(ctx context.Context, in *pb.BeginUserCleanupRequest) (*pb.Cleanup, error) {
	id, err := uuid.Parse(in.GetOperationId())
	if err != nil || id == uuid.Nil || in.GetUserId() <= 0 {
		return nil, status.Error(codes.InvalidArgument, "invalid cleanup intent")
	}
	job, err := s.store.BeginUserCleanup(ctx, in.GetUserId(), id)
	return cleanupResponse(job, err)
}

// GetUserCleanup возвращает сохранённое подтверждение по operation ID.
func (s *UserCleanup) GetUserCleanup(ctx context.Context, in *pb.GetUserCleanupRequest) (*pb.Cleanup, error) {
	id, err := uuid.Parse(in.GetOperationId())
	if err != nil || id == uuid.Nil {
		return nil, status.Error(codes.InvalidArgument, "invalid cleanup intent")
	}
	job, err := s.store.GetUserCleanup(ctx, id)
	return cleanupResponse(job, err)
}

func cleanupResponse(job usecase.UserCleanup, err error) (*pb.Cleanup, error) {
	switch {
	case errors.Is(err, D.ErrNotFound):
		return nil, status.Error(codes.NotFound, "cleanup operation not found")
	case errors.Is(err, D.ErrConflict):
		return nil, status.Error(codes.AlreadyExists, "cleanup operation conflict")
	case err != nil:
		return nil, status.Error(codes.Unavailable, "workspace cleanup unavailable")
	}
	stage := "waiting_runtime"
	if job.Done {
		stage = "done"
	}
	return &pb.Cleanup{OperationId: job.OperationID.String(), UserId: job.UserID, Stage: stage, Done: job.Done}, nil
}
