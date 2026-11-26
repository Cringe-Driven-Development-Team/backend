package grpcdelivery

import (
	"context"
	"errors"
	"io"

	agentv1 "github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/gen/cellestial/agent/v1"
	pb "github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/gen/cellestial/environments/v1"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/runtime/domain"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/runtime/usecase"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// AgentDialer подключается только к разрешённому хранилищем endpoint.
type AgentDialer interface {
	Dial(usecase.AgentTarget) (*grpc.ClientConn, error)
}

// SessionAgent передаёт команды только агенту, назначенному данной сессии.
type SessionAgent struct {
	pb.UnimplementedSessionAgentServer
	targets usecase.AgentTargets
	dialer  AgentDialer
}

// NewSessionAgent принимает локальное хранилище назначений и настроенный mTLS transport.
func NewSessionAgent(targets usecase.AgentTargets, dialer AgentDialer) *SessionAgent {
	return &SessionAgent{targets: targets, dialer: dialer}
}

// connect разрешает сессию с заменой Workspace fence на собственный монотонный fence.
func (s *SessionAgent) connect(ctx context.Context, input *agentv1.Target) (*grpc.ClientConn, *agentv1.Target, error) {
	id, err := parseID(input.GetSessionId())
	if err != nil {
		return nil, nil, err
	}
	target, err := s.targets.ResolveAgent(ctx, id, input.GetFence())
	if err != nil {
		_, safe := environment(ctx, domain.Environment{}, err)
		return nil, nil, safe
	}
	conn, err := s.dialer.Dial(target)
	if err != nil {
		return nil, nil, status.Error(codes.Unavailable, "agent connection unavailable")
	}
	return conn, &agentv1.Target{SessionId: id.String(), Generation: input.GetGeneration(), Fence: target.Fence, OperationId: input.GetOperationId()}, nil
}

//nolint:ireturn // Тип T выводится из конкретного protobuf-ответа каждого метода.
func unaryTarget[T any](ctx context.Context, s *SessionAgent, t *agentv1.Target, call func(agentv1.ControlClient, *agentv1.Target) (T, error)) (T, error) {
	conn, target, err := s.connect(ctx, t)
	if err != nil {
		var out T
		return out, err
	}
	defer conn.Close()
	out, err := call(agentv1.NewControlClient(conn), target)
	return out, agentError(ctx, err)
}

func streamTarget[T any](ctx context.Context, s *SessionAgent, t *agentv1.Target, send func(*T) error, call func(agentv1.ControlClient, *agentv1.Target) (grpc.ServerStreamingClient[T], error)) error {
	conn, target, err := s.connect(ctx, t)
	if err != nil {
		return err
	}
	defer conn.Close()
	stream, err := call(agentv1.NewControlClient(conn), target)
	if err != nil {
		return agentError(ctx, err)
	}
	return agentError(ctx, forward(stream.Recv, send))
}

// forward читает следующий frame лишь после отправки предыдущего downstream.
func forward[T any](recv func() (*T, error), send func(*T) error) error {
	for {
		frame, err := recv()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		if err = send(frame); err != nil {
			return err
		}
	}
}

// agentError сохраняет ожидаемые отказ и неоднозначность, не передавая текст из удалённого процесса.
func agentError(ctx context.Context, err error) error {
	if err == nil {
		return nil
	}
	if ctx.Err() != nil {
		return status.FromContextError(ctx.Err()).Err()
	}
	code := status.Code(err)
	switch code {
	case codes.InvalidArgument, codes.Unauthenticated, codes.PermissionDenied, codes.NotFound, codes.AlreadyExists,
		codes.FailedPrecondition, codes.Aborted, codes.ResourceExhausted, codes.DeadlineExceeded, codes.Canceled, codes.Internal:
	default:
		code = codes.Unavailable
	}
	return status.Error(code, "agent operation failed")
}

// Health читает состояние назначенного kernel.
func (s *SessionAgent) Health(ctx context.Context, input *agentv1.Target) (*agentv1.HealthResponse, error) {
	return unaryTarget(ctx, s, input, func(client agentv1.ControlClient, target *agentv1.Target) (*agentv1.HealthResponse, error) {
		return client.Health(ctx, target)
	})
}

// Activity передаёт пользовательскую активность.
func (s *SessionAgent) Activity(ctx context.Context, input *agentv1.Target) (*agentv1.HealthResponse, error) {
	return unaryTarget(ctx, s, input, func(client agentv1.ControlClient, target *agentv1.Target) (*agentv1.HealthResponse, error) {
		return client.Activity(ctx, target)
	})
}

// Drain прекращает приём команд.
func (s *SessionAgent) Drain(ctx context.Context, input *agentv1.Target) (*agentv1.HealthResponse, error) {
	return unaryTarget(ctx, s, input, func(client agentv1.ControlClient, target *agentv1.Target) (*agentv1.HealthResponse, error) {
		return client.Drain(ctx, target)
	})
}

// Stop запрашивает остановку kernel.
func (s *SessionAgent) Stop(ctx context.Context, input *agentv1.Target) (*agentv1.HealthResponse, error) {
	return unaryTarget(ctx, s, input, func(client agentv1.ControlClient, target *agentv1.Target) (*agentv1.HealthResponse, error) {
		return client.Stop(ctx, target)
	})
}

// Interrupt передаёт один запрос прерывания.
func (s *SessionAgent) Interrupt(ctx context.Context, input *agentv1.Target) (*agentv1.ControlResponse, error) {
	return unaryTarget(ctx, s, input, func(client agentv1.ControlClient, target *agentv1.Target) (*agentv1.ControlResponse, error) {
		return client.Interrupt(ctx, target)
	})
}

// Restart передаёт один запрос перезапуска.
func (s *SessionAgent) Restart(ctx context.Context, input *agentv1.Target) (*agentv1.ControlResponse, error) {
	return unaryTarget(ctx, s, input, func(client agentv1.ControlClient, target *agentv1.Target) (*agentv1.ControlResponse, error) {
		return client.Restart(ctx, target)
	})
}

// StopExecutions сохраняет неизменные идентификаторы команды и операции.
func (s *SessionAgent) StopExecutions(ctx context.Context, input *agentv1.StopExecutionsRequest) (*agentv1.ControlResponse, error) {
	return unaryTarget(ctx, s, input.GetTarget(), func(client agentv1.ControlClient, target *agentv1.Target) (*agentv1.ControlResponse, error) {
		return client.StopExecutions(ctx, &agentv1.StopExecutionsRequest{Target: target, ExecutionIds: input.GetExecutionIds()})
	})
}

// Execute передаёт код и неизменный execution ID.
func (s *SessionAgent) Execute(input *agentv1.ExecuteRequest, stream grpc.ServerStreamingServer[agentv1.ResultFrame]) error {
	ctx := stream.Context()
	return streamTarget(ctx, s, input.GetTarget(), stream.Send, func(client agentv1.ControlClient, target *agentv1.Target) (grpc.ServerStreamingClient[agentv1.ResultFrame], error) {
		return client.Execute(ctx, &agentv1.ExecuteRequest{Target: target, ExecutionId: input.GetExecutionId(), Code: input.GetCode(), TimeoutSeconds: input.GetTimeoutSeconds()})
	})
}

// GetExecution передаёт поток результата сохранённой команды.
func (s *SessionAgent) GetExecution(input *agentv1.ExecutionRequest, stream grpc.ServerStreamingServer[agentv1.ResultFrame]) error {
	ctx := stream.Context()
	return streamTarget(ctx, s, input.GetTarget(), stream.Send, func(client agentv1.ControlClient, target *agentv1.Target) (grpc.ServerStreamingClient[agentv1.ResultFrame], error) {
		return client.GetExecution(ctx, &agentv1.ExecutionRequest{Target: target, ExecutionId: input.GetExecutionId()})
	})
}

// InspectVariables передаёт инспекцию в очередь агента.
func (s *SessionAgent) InspectVariables(input *agentv1.ExecutionRequest, stream grpc.ServerStreamingServer[agentv1.ResultFrame]) error {
	ctx := stream.Context()
	return streamTarget(ctx, s, input.GetTarget(), stream.Send, func(client agentv1.ControlClient, target *agentv1.Target) (grpc.ServerStreamingClient[agentv1.ResultFrame], error) {
		return client.InspectVariables(ctx, &agentv1.ExecutionRequest{Target: target, ExecutionId: input.GetExecutionId()})
	})
}

// ReadEvents передаёт ограниченную страницу журнала.
func (s *SessionAgent) ReadEvents(input *agentv1.EventsRequest, stream grpc.ServerStreamingServer[agentv1.EventFrame]) error {
	ctx := stream.Context()
	return streamTarget(ctx, s, input.GetTarget(), stream.Send, func(client agentv1.ControlClient, target *agentv1.Target) (grpc.ServerStreamingClient[agentv1.EventFrame], error) {
		return client.ReadEvents(ctx, &agentv1.EventsRequest{Target: target, After: input.GetAfter(), MaxEvents: input.GetMaxEvents(), MaxBytes: input.GetMaxBytes()})
	})
}
