package grpcdelivery

import (
	"context"
	"errors"

	pb "github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/gen/cellestial/identity/v1"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/user/usecase"
	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// UserDeletionService отделяет gRPC от сценария физического удаления.
type UserDeletionService interface {
	Begin(context.Context, int64, uuid.UUID) (usecase.Deletion, error)
	Get(context.Context, uuid.UUID) (usecase.Deletion, error)
}

// UserAdministration имеет отдельные полномочия и не принимает пользовательские Bearer.
type UserAdministration struct {
	pb.UnimplementedUserAdministrationServer
	service UserDeletionService
}

// NewUserAdministration подключает сервис длительных операций Identity.
func NewUserAdministration(service UserDeletionService) *UserAdministration {
	return &UserAdministration{service: service}
}

// RequestUserDeletion ставит аккаунт в удаление при явном operation ID.
func (s *UserAdministration) RequestUserDeletion(ctx context.Context, in *pb.RequestUserDeletionRequest) (*pb.UserDeletion, error) {
	operation, err := uuid.Parse(in.GetOperationId())
	if err != nil || operation == uuid.Nil || in.GetUserId() <= 0 {
		return nil, status.Error(codes.InvalidArgument, "invalid deletion request")
	}
	d, err := s.service.Begin(ctx, in.GetUserId(), operation)
	return deletionResponse(d, err)
}

// GetUserDeletion читает текущий этап без повторного выполнения операции.
func (s *UserAdministration) GetUserDeletion(ctx context.Context, in *pb.GetUserDeletionRequest) (*pb.UserDeletion, error) {
	operation, err := uuid.Parse(in.GetOperationId())
	if err != nil || operation == uuid.Nil {
		return nil, status.Error(codes.InvalidArgument, "invalid deletion request")
	}
	d, err := s.service.Get(ctx, operation)
	return deletionResponse(d, err)
}

func deletionResponse(d usecase.Deletion, err error) (*pb.UserDeletion, error) {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return nil, status.FromContextError(context.DeadlineExceeded).Err()
	case errors.Is(err, context.Canceled):
		return nil, status.FromContextError(context.Canceled).Err()
	case errors.Is(err, usecase.ErrUserNotFound):
		return nil, status.Error(codes.NotFound, "user or deletion operation not found")
	case errors.Is(err, usecase.ErrDeletionConflict):
		return nil, status.Error(codes.AlreadyExists, "deletion operation conflict")
	case err != nil:
		return nil, status.Error(codes.Unavailable, "identity deletion unavailable")
	}
	return &pb.UserDeletion{OperationId: d.OperationID.String(), UserId: d.UserID, Stage: d.Stage, Done: d.Stage == "done", ErrorCode: d.ErrorCode}, nil
}
