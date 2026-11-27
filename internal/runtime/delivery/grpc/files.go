package grpcdelivery

import (
	"context"
	"errors"
	"io"

	agentv1 "github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/gen/cellestial/agent/v1"
	commonv1 "github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/gen/cellestial/common/v1"
	pb "github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/gen/cellestial/files/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

// Files маршрутизирует typed операции между Workspace и назначенным агентом.
type Files struct {
	pb.UnimplementedFilesServer
	agent *SessionAgent
}

// NewFiles использует то же назначение и fence, что команды kernel.
func NewFiles(agent *SessionAgent) *Files { return &Files{agent: agent} }

func (s *Files) connect(ctx context.Context, w *pb.Workspace) (*grpc.ClientConn, *pb.Workspace, error) {
	if w.GetGeneration() != w.GetSessionId() || len(w.GetSocketId()) > 64 {
		return nil, nil, status.Error(codes.InvalidArgument, "invalid workspace identity")
	}
	conn, target, err := s.agent.connect(ctx, &agentv1.Target{SessionId: w.GetSessionId(), Fence: w.GetFence()})
	if err != nil {
		return nil, nil, err
	}
	return conn, &pb.Workspace{SessionId: target.SessionId, Generation: w.Generation, Fence: target.Fence, SocketId: w.SocketId}, nil
}

//nolint:ireturn // Тип T выводится из конкретного protobuf-ответа каждого метода.
func fileCall[T any](ctx context.Context, s *Files, w *pb.Workspace, invoke func(pb.FilesClient, *pb.Workspace) (T, error)) (T, error) {
	conn, target, err := s.connect(ctx, w)
	if err != nil {
		var zero T
		return zero, err
	}
	defer conn.Close()
	value, err := invoke(pb.NewFilesClient(conn), target)
	return value, fileRPCError(ctx, err)
}

// Check проверяет готовность директории текущего назначения.
func (s *Files) Check(ctx context.Context, in *pb.CheckRequest) (*pb.CheckResponse, error) {
	return fileCall(ctx, s, in.GetWorkspace(), func(client pb.FilesClient, w *pb.Workspace) (*pb.CheckResponse, error) {
		return client.Check(ctx, &pb.CheckRequest{Workspace: w})
	})
}

// OpenSocket создаёт устойчивую подписку заданного клиента.
func (s *Files) OpenSocket(ctx context.Context, in *pb.SocketRequest) (*pb.SocketResponse, error) {
	return fileCall(ctx, s, in.GetWorkspace(), func(client pb.FilesClient, w *pb.Workspace) (*pb.SocketResponse, error) {
		return client.OpenSocket(ctx, &pb.SocketRequest{Workspace: w})
	})
}

// CloseSocket освобождает состояние подписки.
func (s *Files) CloseSocket(ctx context.Context, in *pb.SocketRequest) (*pb.SocketResponse, error) {
	return fileCall(ctx, s, in.GetWorkspace(), func(client pb.FilesClient, w *pb.Workspace) (*pb.SocketResponse, error) {
		return client.CloseSocket(ctx, &pb.SocketRequest{Workspace: w})
	})
}

// Command передаёт одну ограниченную команду без сырого HTTP запроса.
func (s *Files) Command(ctx context.Context, in *pb.CommandRequest) (*pb.CommandResponse, error) {
	if in.GetCommand() == nil || proto.Size(in) > 64<<10 {
		return nil, status.Error(codes.InvalidArgument, "invalid file command")
	}
	return fileCall(ctx, s, in.GetWorkspace(), func(client pb.FilesClient, w *pb.Workspace) (*pb.CommandResponse, error) {
		return client.Command(ctx, &pb.CommandRequest{Workspace: w, RequestId: in.RequestId, OperationId: in.OperationId, Command: in.Command})
	})
}

// Subscribe передаёт bounded replay последовательно до маркера страницы.
func (s *Files) Subscribe(in *pb.SubscriptionRequest, stream grpc.ServerStreamingServer[pb.Event]) error {
	ctx := stream.Context()
	conn, w, err := s.connect(ctx, in.GetWorkspace())
	if err != nil {
		return err
	}
	defer conn.Close()
	source, err := pb.NewFilesClient(conn).Subscribe(ctx, &pb.SubscriptionRequest{Workspace: w, Paths: in.Paths, After: in.After})
	if err != nil {
		return fileRPCError(ctx, err)
	}
	return fileRPCError(ctx, forward(source.Recv, stream.Send))
}

// Upload держит не более одной неподтверждённой порции и запрещает перепривязку между чанками.
func (s *Files) Upload(stream grpc.BidiStreamingServer[pb.UploadChunk, pb.UploadAck]) error {
	ctx := stream.Context()
	first, err := stream.Recv()
	if errors.Is(err, io.EOF) {
		return status.Error(codes.InvalidArgument, "upload chunk required")
	}
	if err != nil {
		return err
	}
	conn, w, err := s.connect(ctx, first.GetWorkspace())
	if err != nil {
		return err
	}
	defer conn.Close()
	destination, err := pb.NewFilesClient(conn).Upload(ctx)
	if err != nil {
		return fileRPCError(ctx, err)
	}
	for chunk := first; ; {
		if chunk.GetTransferId() != first.GetTransferId() || !proto.Equal(chunk.GetWorkspace(), first.GetWorkspace()) ||
			len(chunk.Payload) == 0 || len(chunk.Payload) > 1<<20 || chunk.Offset > 3<<30 || uint64(len(chunk.Payload)) > (3<<30)-chunk.Offset {
			return status.Error(codes.InvalidArgument, "invalid upload chunk")
		}
		if err = destination.Send(&pb.UploadChunk{Workspace: w, TransferId: chunk.TransferId, Offset: chunk.Offset, Payload: chunk.Payload}); err != nil && !errors.Is(err, io.EOF) {
			return fileRPCError(ctx, err)
		}
		ack, e := destination.Recv()
		if e != nil {
			return fileRPCError(ctx, e)
		}
		if e = stream.Send(ack); e != nil {
			return e
		}
		chunk, err = stream.Recv()
		if errors.Is(err, io.EOF) {
			if err = destination.CloseSend(); err != nil {
				return fileRPCError(ctx, err)
			}
			_, err = destination.Recv()
			if errors.Is(err, io.EOF) {
				return nil
			}
			if err == nil {
				return status.Error(codes.Internal, "unexpected upload acknowledgement")
			}
			return fileRPCError(ctx, err)
		}
		if err != nil {
			return err
		}
	}
}

// Download передаёт header, байты и конечный hash с backpressure.
func (s *Files) Download(in *pb.DownloadRequest, stream grpc.ServerStreamingServer[pb.DownloadChunk]) error {
	ctx := stream.Context()
	conn, w, err := s.connect(ctx, in.GetWorkspace())
	if err != nil {
		return err
	}
	defer conn.Close()
	source, err := pb.NewFilesClient(conn).Download(ctx, &pb.DownloadRequest{Workspace: w, TransferId: in.TransferId,
		Offset: in.Offset, Length: in.Length, Range: in.Range, IfRange: in.IfRange})
	if err != nil {
		return fileRPCError(ctx, err)
	}
	return fileRPCError(ctx, forward(source.Recv, stream.Send))
}

// fileRPCError пропускает только закрытый набор кодов файлового контракта и безопасные поля.
func fileRPCError(ctx context.Context, err error) error {
	if err == nil {
		return nil
	}
	if ctx.Err() != nil {
		return status.FromContextError(ctx.Err()).Err()
	}
	result := status.Convert(err)
	for _, item := range result.Details() {
		detail, ok := item.(*commonv1.ErrorDetail)
		if !ok {
			continue
		}
		switch detail.Code {
		case "invalid_path", "invalid_range", "file_too_large", "file_changed", "quota_exceeded", "workspace_closed", "runtime_not_ready",
			"rate_limited", "not_found", "permission_denied", "already_exists", "not_empty", "conflict", "resync_required", "directory_invalidated",
			"operation_outcome_unknown", "transfer_not_found", "transport_error", "checksum_mismatch", "transfer_expired", "offset_mismatch":
			safe := status.New(result.Code(), "file operation failed")
			with, e := safe.WithDetails(&commonv1.ErrorDetail{Code: detail.Code, Message: detail.Code})
			if e == nil {
				return with.Err()
			}
		}
	}
	return agentError(ctx, err)
}
