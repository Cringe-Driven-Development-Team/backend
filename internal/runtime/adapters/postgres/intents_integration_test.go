package postgres

import (
	"context"
	"errors"
	"net"
	"sync"
	"testing"
	"time"

	pb "github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/gen/cellestial/environments/v1"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/observability"
	delivery "github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/runtime/delivery/grpc"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/runtime/domain"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/runtime/usecase"
	remote "github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/runtimesession/adapters/environmentsrpc"
	session "github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/runtimesession/domain"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/transport/environmentwire"
	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

// TestLostCreateReply проверяет повтор после commit: одна операция, одно задание, исходный correlation.
func TestLostCreateReply(t *testing.T) {
	repo := integrationRepo(t)
	control := intentControl(repo)
	var lose sync.Once
	server := grpc.NewServer(grpc.UnaryInterceptor(func(ctx context.Context, request any, info *grpc.UnaryServerInfo, next grpc.UnaryHandler) (any, error) {
		response, err := next(observability.WithRequestID(ctx, "original-start"), request)
		if err == nil && info.FullMethod == pb.Control_RequestCreate_FullMethodName {
			lose.Do(func() { err = status.Error(codes.Unavailable, "response lost after commit") })
		}
		return response, err
	}))
	pb.RegisterControlServer(server, control)
	listener := bufconn.Listen(1 << 20)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)
	conn, err := grpc.NewClient("passthrough:///test", grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	api := pb.NewControlClient(conn)
	client := remote.NewControl(api)
	id := uuid.New()
	request := session.EnvironmentRequest{SessionID: id, Language: "python", Profile: "default"}
	ctx, cancel := context.WithTimeout(session.WithFence(t.Context(), id, 1), 5*time.Second)
	defer cancel()
	if _, err = client.RequestCreate(ctx, request); !errors.Is(err, session.ErrUnavailable) {
		t.Fatalf("expected lost reply: %v", err)
	}
	op, err := api.GetOperation(ctx, &pb.OperationRequest{OperationId: environmentwire.OperationID(id, "create").String()})
	if err != nil || op.SessionId != id.String() || op.Done {
		t.Fatalf("operation after commit: %v %v", op, err)
	}
	env, err := client.RequestCreate(session.WithFence(ctx, id, 2), request)
	if err != nil || env.State != "QUEUED" {
		t.Fatalf("repeat: %+v %v", env, err)
	}
	var jobs int
	if err = repo.db.QueryRow(ctx, `SELECT count(*) FROM environments.runtime_jobs WHERE session_id=$1`, id).Scan(&jobs); err != nil || jobs != 1 {
		t.Fatalf("jobs=%d %v", jobs, err)
	}
	job, ok, err := repo.Claim(ctx, "restarted", time.Minute)
	if err != nil || !ok || job.CorrelationID != "original-start" {
		t.Fatalf("recovered job: %+v %v %v", job, ok, err)
	}
}

// TestDeleteBeforeCreate проверяет, что поздний create не запускает VPS после tombstone.
func TestDeleteBeforeCreate(t *testing.T) {
	repo := integrationRepo(t)
	control := intentControl(repo)
	ctx := t.Context()
	id := uuid.New()
	deleted, err := control.RequestDelete(ctx, &pb.DeleteRequest{
		SessionId: id.String(), OperationId: environmentwire.OperationID(id, "delete").String(),
		ReasonCode: "requested", Fingerprint: environmentwire.Fingerprint("delete", id, "requested"), Fence: 2,
	})
	if err != nil || deleted.State != string(domain.StateDeleting) || deleted.CleanupConfirmed {
		t.Fatalf("delete acceptance: %v %v", deleted, err)
	}
	create := &pb.CreateRequest{SessionId: id.String(), OperationId: environmentwire.OperationID(id, "create").String(),
		Language: "python", Profile: "default", Fingerprint: environmentwire.Fingerprint("create", id, "python", "default"), Fence: 1}
	if _, err = control.RequestCreate(ctx, create); status.Code(err) != codes.Aborted {
		t.Fatalf("stale create: %v", err)
	}
	create.Fence = 3
	env, err := control.RequestCreate(ctx, create)
	if err != nil || env.State != string(domain.StateDeleting) || env.CleanupConfirmed {
		t.Fatalf("late create changed tombstone: %v %v", env, err)
	}
	job, ok, err := repo.Claim(ctx, "cleanup", time.Minute)
	if err != nil || !ok || job.Kind != usecase.JobDelete || job.SessionID != id {
		t.Fatalf("job: %+v %v %v", job, ok, err)
	}
}

// intentControl собирает проверки транспорта и реальное хранилище в тестовой базе.
func intentControl(repo *Repository) *delivery.Control {
	settings := usecase.Settings{MaxActive: 5, AgentVersion: "test", Profiles: map[string]bool{"default": true}}
	return delivery.NewControl(usecase.NewIntentService(repo, usecase.NewService(repo, settings), settings))
}
