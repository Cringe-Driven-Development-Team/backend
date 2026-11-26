package agentrpc

import (
	"context"
	"net/http"

	pb "github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/gen/cellestial/agent/v1"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/runtime/usecase"
	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Provisioning подключает worker подготовки VPS к gRPC агенту.
type Provisioning struct{ dialer *Dialer }

// NewProvisioning связывает клиент с транспортом runtime PKI.
func NewProvisioning(dialer *Dialer) *Provisioning { return &Provisioning{dialer: dialer} }

var _ usecase.AgentClient = (*Provisioning)(nil)

// Health читает назначение или состояние WAITING прогретой VPS.
func (p *Provisioning) Health(ctx context.Context, ep usecase.AgentEndpoint) (usecase.AgentHealth, int, error) {
	return p.call(ep.Target, func(client pb.ControlClient, t *pb.Target) (*pb.HealthResponse, error) {
		return client.Health(ctx, t)
	})
}

// Assign назначает существующую VPS с одноразовой идентичностью её сессии.
func (p *Provisioning) Assign(ctx context.Context, ep usecase.AgentEndpoint, id uuid.UUID, language string) (usecase.AgentHealth, int, error) {
	if ep.Target.SessionID != id {
		return usecase.AgentHealth{}, http.StatusBadRequest, usecase.ErrInvalidSession
	}
	return p.call(ep.Target, func(client pb.ControlClient, t *pb.Target) (*pb.HealthResponse, error) {
		return client.Assign(ctx, &pb.AssignRequest{Target: t, Language: language})
	})
}

// Drain прекращает приём новых команд в текущем cleanup fence.
func (p *Provisioning) Drain(ctx context.Context, ep usecase.AgentEndpoint) error {
	_, _, err := p.call(ep.Target, func(client pb.ControlClient, t *pb.Target) (*pb.HealthResponse, error) {
		return client.Drain(ctx, t)
	})
	return err
}

// Stop останавливает назначенный kernel перед очисткой ресурсов VPS.
func (p *Provisioning) Stop(ctx context.Context, ep usecase.AgentEndpoint) error {
	_, _, err := p.call(ep.Target, func(client pb.ControlClient, t *pb.Target) (*pb.HealthResponse, error) {
		return client.Stop(ctx, t)
	})
	return err
}

// call переводит gRPC к существующему provisioning port на время обновления worker states.
func (p *Provisioning) call(target usecase.AgentTarget, invoke func(pb.ControlClient, *pb.Target) (*pb.HealthResponse, error)) (usecase.AgentHealth, int, error) {
	conn, err := p.dialer.Dial(target)
	if err != nil {
		return usecase.AgentHealth{}, http.StatusServiceUnavailable, err
	}
	defer conn.Close()
	wire := &pb.Target{Fence: target.Fence}
	if target.SessionID != uuid.Nil {
		wire.SessionId = target.SessionID.String()
	}
	h, err := invoke(pb.NewControlClient(conn), wire)
	if err != nil {
		switch status.Code(err) {
		case codes.InvalidArgument:
			return usecase.AgentHealth{}, http.StatusBadRequest, usecase.ErrInvalidSession
		case codes.AlreadyExists, codes.FailedPrecondition, codes.PermissionDenied:
			return usecase.AgentHealth{}, http.StatusConflict, usecase.ErrConflict
		default:
			return usecase.AgentHealth{}, http.StatusServiceUnavailable, status.Error(status.Code(err), "agent unavailable")
		}
	}
	return usecase.AgentHealth{SessionID: h.GetSessionId(), Language: h.GetLanguage(), State: h.GetState(), Ready: h.GetReady(), Reason: h.GetReason(),
		Generation: h.GetGeneration(), ActiveExecutionID: h.ActiveExecutionId, Queued: int(h.GetQueued()), Draining: h.GetDraining()}, http.StatusOK, nil
}
