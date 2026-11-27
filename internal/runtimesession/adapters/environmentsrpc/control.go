// Package environmentsrpc подключает Workspace к внутреннему API окружений.
package environmentsrpc

import (
	"context"
	"errors"

	pb "github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/gen/cellestial/environments/v1"
	D "github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/runtimesession/domain"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/runtimesession/usecase"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/transport/environmentwire"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/transport/rpcerror"
	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Control отправляет на Environments намерения, сохранённые в очереди Workspace.
type Control struct{ client pb.ControlClient }

// NewControl принимает клиент с mTLS и ограничениями общего транспорта.
func NewControl(client pb.ControlClient) *Control { return &Control{client: client} }

var _ usecase.Environments = (*Control)(nil)

// RequestCreate повторяет то же намерение и fingerprint после потери ответа.
func (c *Control) RequestCreate(ctx context.Context, request D.EnvironmentRequest) (D.Environment, error) {
	id := request.SessionID
	reply, err := c.client.RequestCreate(ctx, &pb.CreateRequest{
		SessionId: id.String(), OperationId: environmentwire.OperationID(id, "create").String(),
		Fingerprint: environmentwire.Fingerprint("create", id, request.Language, request.Profile),
		Language:    request.Language, Profile: request.Profile, Fence: D.Fence(ctx, id),
	})
	return environment(ctx, id, reply, err)
}

// Get возвращает только безопасное состояние окружения сессии.
func (c *Control) Get(ctx context.Context, id uuid.UUID) (D.Environment, error) {
	reply, err := c.client.GetEnvironment(ctx, &pb.EnvironmentRequest{SessionId: id.String()})
	return environment(ctx, id, reply, err)
}

// RequestDelete подтверждает сохранение намерения, оставляя проверку очистки отдельному Get.
func (c *Control) RequestDelete(ctx context.Context, id uuid.UUID, reason string) error {
	reply, err := c.client.RequestDelete(ctx, &pb.DeleteRequest{
		SessionId: id.String(), OperationId: environmentwire.OperationID(id, "delete").String(),
		Fingerprint: environmentwire.Fingerprint("delete", id, reason),
		ReasonCode:  reason, Fence: D.Fence(ctx, id),
	})
	_, err = environment(ctx, id, reply, err)
	return err
}

// environment отклоняет чужую, неизвестную или внутренне противоречивую запись.
func environment(ctx context.Context, id uuid.UUID, reply *pb.Environment, err error) (D.Environment, error) {
	if err != nil {
		return D.Environment{}, runtimeError(ctx, err)
	}
	if reply == nil || id == uuid.Nil || reply.SessionId != id.String() {
		return D.Environment{}, D.ErrProtocol
	}
	switch reply.State {
	case "QUEUED", "CREATING", "CONFIGURING", "VERIFYING", "READY", "DELETING", "DELETED", "CLEANUP_PENDING", "FAILED":
	default:
		return D.Environment{}, D.ErrProtocol
	}
	if reply.CleanupConfirmed && reply.State != "DELETED" && reply.State != "FAILED" {
		return D.Environment{}, D.ErrProtocol
	}
	reason := ""
	if reply.ReasonCode != "" {
		reason = "environment_failed"
	}
	return D.Environment{SessionID: id, State: reply.State, ReasonCode: reason, CleanupConfirmed: reply.CleanupConfirmed}, nil
}

// runtimeError переводит конечный набор статусов и исключает сырой текст RPC.
func runtimeError(ctx context.Context, err error) error {
	return rpcerror.Preserve(err, rpcerror.CallRuntime, runtimeCause(ctx, err))
}

// runtimeCause сохраняет прежние решения воркера и проверки errors.Is.
func runtimeCause(ctx context.Context, err error) error {
	if err == nil {
		return nil
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if errors.Is(err, D.ErrProtocol) {
		return D.ErrProtocol
	}
	switch status.Code(err) {
	case codes.NotFound:
		return D.ErrNotFound
	case codes.InvalidArgument:
		return D.ErrInvalid
	case codes.AlreadyExists, codes.FailedPrecondition:
		return D.ErrConflict
	case codes.Aborted:
		return D.ErrLeaseLost
	case codes.ResourceExhausted:
		return D.ErrQueueFull
	case codes.DeadlineExceeded:
		return errors.Join(D.ErrUnavailable, context.DeadlineExceeded)
	default:
		return D.ErrUnavailable
	}
}
