// Package grpcdelivery реализует внутренний API управления окружениями.
package grpcdelivery

import (
	"bytes"
	"context"
	"errors"
	"math"

	pb "github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/gen/cellestial/environments/v1"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/runtime/domain"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/runtime/usecase"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/transport/environmentwire"
	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// ControlService отделяет protobuf от usecase управления окружениями.
type ControlService interface {
	Create(context.Context, usecase.Intent, string, string) (domain.Environment, error)
	Delete(context.Context, usecase.Intent, string) (domain.Environment, error)
	Get(context.Context, uuid.UUID) (domain.Environment, error)
	Operation(context.Context, uuid.UUID) (usecase.Intent, domain.Environment, error)
}

// Control принимает запросы Workspace после mTLS-авторизации метода.
type Control struct {
	pb.UnimplementedControlServer
	service ControlService
}

// NewControl связывает RPC с атомарным хранилищем намерений.
func NewControl(service ControlService) *Control { return &Control{service: service} }

// RequestCreate подтверждает постановку подготовки в очередь или возвращает результат прежнего запроса.
func (s *Control) RequestCreate(ctx context.Context, request *pb.CreateRequest) (*pb.Environment, error) {
	if len(request.GetLanguage()) > 16 || len(request.GetProfile()) > 64 {
		return nil, invalidIntent()
	}
	i, err := intent(usecase.JobCreate, request.GetSessionId(), request.GetOperationId(), request.GetFingerprint(), request.GetFence(), request.GetLanguage(), request.GetProfile())
	if err != nil {
		return nil, err
	}
	env, err := s.service.Create(ctx, i, request.GetLanguage(), request.GetProfile())
	return environment(ctx, env, err)
}

// RequestDelete подтверждает durable намерение. Подтверждение очистки сообщает текущее состояние.
func (s *Control) RequestDelete(ctx context.Context, request *pb.DeleteRequest) (*pb.Environment, error) {
	reason := request.GetReasonCode()
	switch reason {
	case "requested", "idle_timeout", "kernel_died", "agent_unreachable", "protocol_error", "environment_failed", "environment_lost", "capacity_exceeded", "startup_timeout":
	default:
		return nil, invalidIntent()
	}
	i, err := intent(usecase.JobDelete, request.GetSessionId(), request.GetOperationId(), request.GetFingerprint(), request.GetFence(), reason)
	if err != nil {
		return nil, err
	}
	env, err := s.service.Delete(ctx, i, reason)
	return environment(ctx, env, err)
}

// GetEnvironment возвращает состояние без endpoint, облачных ID и credentials.
func (s *Control) GetEnvironment(ctx context.Context, request *pb.EnvironmentRequest) (*pb.Environment, error) {
	id, err := parseID(request.GetSessionId())
	if err != nil {
		return nil, err
	}
	env, err := s.service.Get(ctx, id)
	return environment(ctx, env, err)
}

// GetOperation восстанавливает состояние принятого намерения после потери ответа.
func (s *Control) GetOperation(ctx context.Context, request *pb.OperationRequest) (*pb.Operation, error) {
	id, err := parseID(request.GetOperationId())
	if err != nil {
		return nil, err
	}
	i, env, err := s.service.Operation(ctx, id)
	view, err := environment(ctx, env, err)
	if err != nil {
		return nil, err
	}
	done := view.CleanupConfirmed
	if i.Kind == usecase.JobCreate {
		done = done || env.State == domain.StateReady
	}
	return &pb.Operation{OperationId: id.String(), SessionId: view.SessionId, State: view.State,
		Done: done, CleanupConfirmed: view.CleanupConfirmed, ErrorCode: view.ReasonCode}, nil
}

func parseID(raw string) (uuid.UUID, error) {
	id, err := uuid.Parse(raw)
	if err != nil || id == uuid.Nil || id.String() != raw {
		return uuid.Nil, invalidIntent()
	}
	return id, nil
}

func intent(kind usecase.JobKind, session, operation string, fingerprint []byte, fence uint64, fields ...string) (usecase.Intent, error) {
	sid, err := parseID(session)
	if err != nil {
		return usecase.Intent{}, err
	}
	oid, err := parseID(operation)
	if err != nil || fence == 0 || fence > math.MaxInt64 || !bytes.Equal(fingerprint, environmentwire.Fingerprint(string(kind), sid, fields...)) {
		return usecase.Intent{}, invalidIntent()
	}
	return usecase.Intent{OperationID: oid, SessionID: sid, Kind: kind, Fingerprint: fingerprint, Fence: int64(fence)}, nil
}

func invalidIntent() error { return status.Error(codes.InvalidArgument, "invalid environment intent") }

func environment(ctx context.Context, env domain.Environment, err error) (*pb.Environment, error) {
	if err != nil {
		switch {
		case ctx.Err() != nil:
			return nil, status.FromContextError(ctx.Err()).Err()
		case errors.Is(err, usecase.ErrNotFound):
			return nil, status.Error(codes.NotFound, "environment not found")
		case errors.Is(err, usecase.ErrConflict):
			return nil, status.Error(codes.AlreadyExists, "operation conflict")
		case errors.Is(err, usecase.ErrStale):
			return nil, status.Error(codes.Aborted, "stale environment fence")
		case errors.Is(err, usecase.ErrCapacity):
			return nil, status.Error(codes.ResourceExhausted, "environment capacity exhausted")
		case errors.Is(err, usecase.ErrInvalidSession), errors.Is(err, usecase.ErrInvalidLanguage), errors.Is(err, usecase.ErrUnknownProfile):
			return nil, invalidIntent()
		default:
			return nil, status.Error(codes.Unavailable, "environment storage unavailable")
		}
	}
	reason := ""
	if env.LastError != "" {
		reason = "environment_failed"
	}
	return &pb.Environment{SessionId: env.SessionID.String(), State: string(env.State), ReasonCode: reason, CleanupConfirmed: env.CleanupConfirmed()}, nil
}
