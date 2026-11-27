package environmentsrpc

import (
	"context"

	agentv1 "github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/gen/cellestial/agent/v1"
	pb "github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/gen/cellestial/environments/v1"
	D "github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/runtimesession/domain"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/runtimesession/usecase"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/transport/agentwire"
	"github.com/google/uuid"
	"google.golang.org/grpc"
)

// Agent вызывает агент через Environments, не зная его адреса и сертификата.
type Agent struct{ client pb.SessionAgentClient }

// NewAgent использует управляемое приложением mTLS-соединение с Environments.
func NewAgent(client pb.SessionAgentClient) *Agent { return &Agent{client: client} }

var _ usecase.SessionAgent = (*Agent)(nil)

// target включает только относящийся к этой сессии epoch фоновой работы.
func target(ctx context.Context, id uuid.UUID) *agentv1.Target {
	return &agentv1.Target{SessionId: id.String(), Fence: D.Fence(ctx, id)}
}

// Health читает подтверждённое назначение, состояние kernel и очередь.
func (a *Agent) Health(ctx context.Context, id uuid.UUID) (D.Health, error) {
	h, err := a.client.Health(ctx, target(ctx, id))
	if err != nil {
		return D.Health{}, runtimeError(ctx, err)
	}
	if h == nil || id == uuid.Nil || h.SessionId != id.String() || h.Language != "python" || h.State == "" {
		return D.Health{}, D.ErrProtocol
	}
	return D.Health{
		SessionID: id, Language: h.Language, State: h.State, Reason: h.Reason,
		Generation: h.Generation, Ready: h.Ready, Draining: h.Draining,
		ActiveExecutionID: h.ActiveExecutionId, Queued: int(h.Queued),
	}, nil
}

// Execute передаёт неизменный execution ID, выбирая команду исполнения или инспекции.
func (a *Agent) Execute(ctx context.Context, id uuid.UUID, cmd D.Command) (D.AgentExecution, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var stream agentwire.ResultReader
	var err error
	if cmd.Kind == "inspect" {
		if err = (D.InspectRequest{ExecutionID: cmd.ExecutionID}).Validate(); err != nil {
			return D.AgentExecution{}, err
		}
		stream, err = a.client.InspectVariables(ctx, &agentv1.ExecutionRequest{Target: target(ctx, id), ExecutionId: cmd.ExecutionID})
	} else {
		if _, err = D.CommandPayload(cmd); err != nil {
			return D.AgentExecution{}, err
		}
		stream, err = a.client.Execute(ctx, &agentv1.ExecuteRequest{
			Target: target(ctx, id), ExecutionId: cmd.ExecutionID, Code: cmd.Code, TimeoutSeconds: cmd.TimeoutSeconds,
		})
	}
	return readExecution(ctx, stream, cmd.ExecutionID, err)
}

// Execution читает целый сохранённый результат с проверкой хеша и завершения потока.
func (a *Agent) Execution(ctx context.Context, id uuid.UUID, executionID string) (D.AgentExecution, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	stream, err := a.client.GetExecution(ctx, &agentv1.ExecutionRequest{Target: target(ctx, id), ExecutionId: executionID})
	return readExecution(ctx, stream, executionID, err)
}

func readExecution(ctx context.Context, stream agentwire.ResultReader, id string, err error) (D.AgentExecution, error) {
	if err != nil {
		return D.AgentExecution{}, runtimeError(ctx, err)
	}
	result, err := agentwire.ReadExecution(ctx, stream, id)
	return result, runtimeError(ctx, err)
}

// Events возвращает страницу только после успешного чтения и проверки всех объектов.
func (a *Agent) Events(ctx context.Context, id uuid.UUID, after int64, limits D.EventLimits) ([]D.AgentEvent, error) {
	if after < 0 || id == uuid.Nil {
		return nil, D.ErrInvalid
	}
	if limits.Count <= 0 || limits.Count > 100 {
		limits.Count = 100
	}
	if limits.Bytes <= 0 || limits.Bytes > 16<<20 {
		limits.Bytes = 16 << 20
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	stream, err := a.client.ReadEvents(ctx, &agentv1.EventsRequest{
		Target: target(ctx, id), After: after, MaxEvents: uint32(limits.Count), MaxBytes: uint32(limits.Bytes),
	})
	if err != nil {
		return nil, runtimeError(ctx, err)
	}
	page, err := agentwire.ReadEventPage(ctx, stream, id, after, limits)
	return page, runtimeError(ctx, err)
}

// Activity передаёт явную пользовательскую активность.
func (a *Agent) Activity(ctx context.Context, id uuid.UUID) error {
	return healthCommand(ctx, id, a.client.Activity)
}

// Drain закрывает приём новых команд перед остановкой.
func (a *Agent) Drain(ctx context.Context, id uuid.UUID) error {
	return healthCommand(ctx, id, a.client.Drain)
}

// Stop запрашивает остановку kernel до чтения последней страницы журнала.
func (a *Agent) Stop(ctx context.Context, id uuid.UUID) error {
	return healthCommand(ctx, id, a.client.Stop)
}

func healthCommand(ctx context.Context, id uuid.UUID, call func(context.Context, *agentv1.Target, ...grpc.CallOption) (*agentv1.HealthResponse, error)) error {
	reply, err := call(ctx, target(ctx, id))
	if err != nil {
		return runtimeError(ctx, err)
	}
	if reply == nil || reply.SessionId != id.String() {
		return D.ErrProtocol
	}
	return nil
}

// Interrupt отправляет один управляющий вызов без повтора на потерянный ответ.
func (a *Agent) Interrupt(ctx context.Context, id uuid.UUID) error {
	reply, err := a.client.Interrupt(ctx, target(ctx, id))
	return acknowledged(ctx, reply, err)
}

// Restart отправляет один вызов и принимает только подтверждённое новое поколение.
func (a *Agent) Restart(ctx context.Context, id uuid.UUID) (string, error) {
	reply, err := a.client.Restart(ctx, target(ctx, id))
	if err = acknowledged(ctx, reply, err); err != nil {
		return "", err
	}
	if reply.Generation == "" {
		return "", D.ErrProtocol
	}
	return reply.Generation, nil
}

// StopExecutions сохраняет заданный usecase operation ID для дедупликации остановки.
func (a *Agent) StopExecutions(ctx context.Context, id uuid.UUID, operation string, ids []string) error {
	t := target(ctx, id)
	t.OperationId = operation
	reply, err := a.client.StopExecutions(ctx, &agentv1.StopExecutionsRequest{Target: t, ExecutionIds: ids})
	return acknowledged(ctx, reply, err)
}

func acknowledged(ctx context.Context, reply *agentv1.ControlResponse, err error) error {
	if err != nil {
		return runtimeError(ctx, err)
	}
	if reply == nil || !reply.Accepted {
		return D.ErrProtocol
	}
	return nil
}
