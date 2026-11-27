package environmentsrpc

import (
	"context"
	"errors"
	"io"

	commonv1 "github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/gen/cellestial/common/v1"
	pb "github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/gen/cellestial/files/v1"
	D "github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/runtimesession/domain"
	F "github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/runtimesession/domain/workspacefile"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/runtimesession/usecase"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/transport/rpcerror"
	"github.com/google/uuid"
	"google.golang.org/grpc/status"
)

// Files реализует типизированный файловый порт через Environments.
type Files struct{ rpc pb.FilesClient }

// NewFiles использует то же mTLS-соединение, что runtime клиента.
func NewFiles(client pb.FilesClient) *Files { return &Files{rpc: client} }

var _ usecase.WorkspaceFilesPort = (*Files)(nil)

func workspace(t F.Target) *pb.Workspace {
	id := t.Session.String()
	return &pb.Workspace{SessionId: id, Generation: id, SocketId: t.Socket}
}

// Check проверяет актуальное поколение файлов, не создавая подписку.
func (a *Files) Check(ctx context.Context, t F.Target) error {
	reply, err := a.rpc.Check(ctx, &pb.CheckRequest{Workspace: workspace(t)})
	if err != nil {
		return fileError(ctx, err)
	}
	if !reply.GetReady() || reply.GetGeneration() != t.Session.String() {
		return D.ErrProtocol
	}
	return nil
}

// CloseSocket освобождает подписку и проверяет подтверждение от текущего агента.
func (a *Files) CloseSocket(ctx context.Context, t F.Target) error {
	reply, err := a.rpc.CloseSocket(ctx, &pb.SocketRequest{Workspace: workspace(t)})
	if err != nil {
		return fileError(ctx, err)
	}
	if !reply.GetClosed() || reply.GetSocketId() != t.Socket || reply.GetGeneration() != t.Session.String() {
		return D.ErrProtocol
	}
	return nil
}

// Command сохраняет operation ID и преобразует один ответ с типизированным результатом.
func (a *Files) Command(ctx context.Context, t F.Target, in F.Command) (F.Result, error) {
	q := &pb.CommandRequest{Workspace: workspace(t), RequestId: in.RequestID, OperationId: in.OperationID}
	link := &pb.TransferReference{TransferId: in.TransferID}
	switch in.Kind {
	case "list":
		cursor := ""
		if in.Cursor != nil {
			cursor = *in.Cursor
		}
		q.Command = &pb.CommandRequest_List{List: &pb.List{Path: in.Path, Limit: in.Limit, Cursor: cursor}}
	case "mkdir":
		q.Command = &pb.CommandRequest_Mkdir{Mkdir: &pb.Mkdir{Path: in.Path}}
	case "rename":
		q.Command = &pb.CommandRequest_Rename{Rename: &pb.Rename{Path: in.Path, Name: in.Name, ExpectedVersion: in.Version}}
	case "delete":
		q.Command = &pb.CommandRequest_Delete{Delete: &pb.Delete{Path: in.Path, ExpectedVersion: in.Version, Recursive: in.Recursive}}
	case "upload_begin":
		if in.Size == nil {
			return F.Result{}, D.ErrInvalid
		}
		q.Command = &pb.CommandRequest_UploadBegin{UploadBegin: &pb.UploadBegin{Path: in.Path, SizeBytes: *in.Size, Sha256: in.Hash}}
	case "download_begin":
		q.Command = &pb.CommandRequest_DownloadBegin{DownloadBegin: &pb.DownloadBegin{Path: in.Path}}
	case "upload_status":
		q.Command = &pb.CommandRequest_UploadStatus{UploadStatus: link}
	case "upload_commit":
		q.Command = &pb.CommandRequest_UploadCommit{UploadCommit: link}
	case "transfer_status":
		q.Command = &pb.CommandRequest_TransferStatus{TransferStatus: link}
	case "transfer_cancel":
		q.Command = &pb.CommandRequest_TransferCancel{TransferCancel: link}
	case "subscribe":
		q.Command = &pb.CommandRequest_Subscribe{Subscribe: &pb.WatchPaths{Paths: in.Paths, After: in.After}}
	case "unsubscribe":
		q.Command = &pb.CommandRequest_Unsubscribe{Unsubscribe: &pb.WatchPaths{Paths: in.Paths}}
	default:
		return F.Result{}, D.ErrInvalid
	}
	reply, err := a.rpc.Command(ctx, q)
	if err != nil {
		return F.Result{}, fileError(ctx, err)
	}
	if reply.GetRequestId() != in.RequestID || reply.GetResult() == nil {
		return F.Result{}, D.ErrProtocol
	}
	out := F.Result{}
	switch r := reply.GetResult().(type) {
	case *pb.CommandResponse_Directory:
		dir := r.Directory
		items := make([]F.Entry, 0, len(dir.GetItems()))
		for _, value := range dir.GetItems() {
			items = append(items, mapEntry(value))
		}
		out.Directory = &F.Directory{Path: dir.Path, Items: items, NextCursor: dir.NextCursor, Revision: dir.Revision, Generation: dir.Generation, Seq: dir.Seq, Quota: mapQuota(dir.Quota)}
	case *pb.CommandResponse_Transfer:
		transfer := mapTransfer(r.Transfer)
		out.Transfer = &transfer
	case *pb.CommandResponse_Operation:
		op := r.Operation
		out.Mutation = &F.Mutation{OperationID: op.GetOperationId(), Revision: op.GetRevision(), Resync: op.GetResyncRequired(), ResultingSeq: op.GetResultingSeq()}
	case *pb.CommandResponse_Error:
		out.Error = mapFileError(r.Error)
	case *pb.CommandResponse_Watch:
		watch := r.Watch
		snapshot := make([]F.Snapshot, 0, len(watch.GetSnapshots()))
		for _, item := range watch.GetSnapshots() {
			snapshot = append(snapshot, F.Snapshot{Path: item.GetPath(), Revision: item.GetRevision(), Generation: t.Session.String()})
		}
		events := make([]F.Event, 0, len(watch.GetEvents()))
		for _, item := range watch.GetEvents() {
			events = append(events, mapEvent(item))
		}
		out.Watch = &F.Watch{Replay: F.Replay{Events: events, Seq: watch.GetSeq()}, Snapshots: snapshot, Synchronization: watch.GetSynchronization()}
	}
	return out, nil
}

// Replay публикует только полностью полученную страницу с подтверждённым курсором.
func (a *Files) Replay(ctx context.Context, t F.Target, after uint64) (F.Replay, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	src, err := a.rpc.Subscribe(ctx, &pb.SubscriptionRequest{Workspace: workspace(t), After: &after})
	if err != nil {
		return F.Replay{}, fileError(ctx, err)
	}
	page := F.Replay{Seq: after, Events: make([]F.Event, 0)}
	complete := false
	for {
		event, err := src.Recv()
		if errors.Is(err, io.EOF) {
			if !complete {
				return F.Replay{}, D.ErrProtocol
			}
			return page, nil
		}
		if err != nil {
			return F.Replay{}, fileError(ctx, err)
		}
		if complete || event.GetWorkspace().GetSessionId() != t.Session.String() || event.GetWorkspace().GetGeneration() != t.Session.String() {
			return F.Replay{}, D.ErrProtocol
		}
		if end := event.GetReplay(); end != nil {
			if end.NextSeq != page.Seq {
				return F.Replay{}, D.ErrProtocol
			}
			complete = true
			continue
		}
		if len(page.Events) >= 256 || event.Type == "" || (event.Seq <= page.Seq && event.Type != "resync_required") {
			return F.Replay{}, D.ErrProtocol
		}
		page.Seq = event.Seq
		page.Events = append(page.Events, mapEvent(event))
	}
}

// Upload ждёт durable ACK и завершения RPC перед HTTP подтверждением.
func (a *Files) Upload(ctx context.Context, t F.Target, id uuid.UUID, off uint64, data []byte) (F.Transfer, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	stream, err := a.rpc.Upload(ctx)
	if err != nil {
		return F.Transfer{}, fileError(ctx, err)
	}
	if err = stream.Send(&pb.UploadChunk{Workspace: workspace(t), TransferId: id.String(), Offset: off, Payload: data}); err != nil && !errors.Is(err, io.EOF) {
		return F.Transfer{}, fileError(ctx, err)
	}
	if err = stream.CloseSend(); err != nil {
		return F.Transfer{}, fileError(ctx, err)
	}
	ack, err := stream.Recv()
	if err != nil {
		return F.Transfer{}, fileError(ctx, err)
	}
	if ack.GetError() != nil {
		return F.Transfer{}, mapFileError(ack.GetError())
	}
	item := ack.GetTransfer()
	if ack.GetTransferId() != id.String() || item.GetTransferId() != id.String() || item.GetGeneration() != t.Session.String() ||
		ack.GetOffset() < off+uint64(len(data)) || ack.GetOffset() != item.Offset {
		return F.Transfer{}, D.ErrProtocol
	}
	if _, err = stream.Recv(); !errors.Is(err, io.EOF) {
		if err != nil {
			return F.Transfer{}, fileError(ctx, err)
		}
		return F.Transfer{}, D.ErrProtocol
	}
	return mapTransfer(item), nil
}

func mapEntry(v *pb.Entry) F.Entry {
	return F.Entry{Name: v.GetName(), Path: v.GetPath(), Type: v.GetType(), Size: v.SizeBytes, ModifiedAt: v.GetModifiedAt(), Version: v.GetVersion(), Actions: v.GetActions()}
}

func mapQuota(v *pb.Quota) F.Quota {
	return F.Quota{Capacity: v.GetCapacityBytes(), Used: v.GetUsedBytes(), Available: v.GetAvailableBytes(), Source: v.GetSource()}
}

func mapTransfer(v *pb.Transfer) F.Transfer {
	return F.Transfer{ID: v.GetTransferId(), Path: v.GetPath(), Direction: v.GetDirection(), Size: v.GetSizeBytes(), Offset: v.GetOffset(), State: v.GetState(),
		Generation: v.GetGeneration(), Hash: v.Sha256, Version: v.Version, ChunkBytes: v.GetChunkBytes(), CreditBytes: v.GetCreditBytes(), Updated: v.GetUpdated()}
}

func mapEvent(v *pb.Event) F.Event {
	o := F.Event{ProtocolVersion: 1, SessionID: v.GetWorkspace().GetSessionId(), Generation: v.GetWorkspace().GetGeneration(), Seq: v.Seq, Type: v.Type,
		Path: v.Path, Directory: v.Directory, Revision: v.Revision, OldPath: v.OldPath, NewPath: v.NewPath, TransferID: v.TransferId, Code: v.GetError().GetCode(),
		OperationID: v.OperationId, Reason: v.Reason, Synchronization: v.Synchronization, Resync: v.ResyncRequired}
	if v.Entry != nil {
		item := mapEntry(v.Entry)
		o.Entry = &item
	}
	if v.Quota != nil {
		item := mapQuota(v.Quota)
		o.Quota = &item
	}
	if v.Transfer != nil {
		item := mapTransfer(v.Transfer)
		o.Transfer = &item
	}
	return o
}

func mapFileError(v *pb.Error) *F.Error {
	return &F.Error{Code: v.GetCode(), Message: v.GetCode(), Retryable: v.GetRetryable(), Resync: v.GetResyncRequired(), OperationID: v.GetOperationId(), TransferID: v.GetTransferId()}
}

func fileError(ctx context.Context, err error) error {
	if err == nil {
		return nil
	}
	for _, item := range status.Convert(err).Details() {
		if detail, ok := item.(*commonv1.ErrorDetail); ok {
			return &F.Error{Code: detail.Code, Message: detail.Code, Resync: detail.Code == "workspace_closed" || detail.Code == "resync_required",
				Retryable: detail.Code == "runtime_not_ready" || detail.Code == "rate_limited"}
		}
	}
	return rpcerror.Preserve(err, rpcerror.CallFiles, runtimeCause(ctx, err))
}
