package environmentsrpc

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"hash"
	"io"
	"strings"

	pb "github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/gen/cellestial/files/v1"
	D "github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/runtimesession/domain"
	F "github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/runtimesession/domain/workspacefile"
	"github.com/google/uuid"
)

// Download подтверждает header до выдачи HTTP-метаданных и удерживает последний chunk до проверки digest.
func (a *Files) Download(parent context.Context, t F.Target, id uuid.UUID, bounds *F.Range, ifRange string) (F.Download, error) {
	ctx, cancel := context.WithCancel(parent)
	fail := func(err error) (F.Download, error) { cancel(); return F.Download{}, err }
	q := &pb.DownloadRequest{Workspace: workspace(t), TransferId: id.String(), IfRange: ifRange}
	if bounds != nil {
		q.Range = &pb.ByteRange{Start: bounds.Start, End: bounds.End, Suffix: bounds.Suffix}
	}
	src, err := a.rpc.Download(ctx, q)
	if err != nil {
		return fail(fileError(ctx, err))
	}
	header, err := src.Recv()
	if err != nil {
		return fail(fileError(ctx, err))
	}
	if header.GetError().GetCode() == "invalid_range" && header.TotalBytes != nil && *header.TotalBytes <= F.MaxFileBytes {
		return fail(&F.InvalidRange{Total: *header.TotalBytes})
	}
	total, length, start := header.GetTotalBytes(), header.GetSelectedBytes(), header.GetOffset()
	version := header.GetVersion()
	if header.TotalBytes == nil || header.SelectedBytes == nil || header.Error != nil || header.Complete || len(header.Payload) != 0 ||
		total > F.MaxFileBytes || start > total || length > total-start || (!header.Partial && (start != 0 || length != total)) ||
		(header.Partial && (bounds == nil || length == 0)) || len(version) == 0 || len(version) > 128 ||
		strings.ContainsAny(version, "\"\\\r\n\t") || header.Filename == "" || strings.ContainsAny(header.Filename, "/\x00\r\n") {
		return fail(D.ErrProtocol)
	}
	reader := &downloadReader{ctx: ctx, cancel: cancel, recv: src.Recv, offset: start, left: length, digest: sha256.New()}
	if length == 0 {
		if err = reader.finish(); err != nil {
			return fail(err)
		}
	}
	return F.Download{Offset: start, Length: length, Total: total, Version: version, Filename: header.Filename, Partial: header.Partial, Body: reader}, nil
}

// downloadReader удерживает один chunk и освобождает RPC при Close.
type downloadReader struct {
	ctx          context.Context
	cancel       context.CancelFunc
	recv         func() (*pb.DownloadChunk, error)
	digest       hash.Hash
	offset, left uint64
	verified     bool
	pending      []byte
	failure      error
}

// Read проверяет последовательность и не выдаёт последние байты без подтверждённого hash.
func (r *downloadReader) Read(dst []byte) (int, error) {
	if len(dst) == 0 {
		return 0, nil
	}
	if len(r.pending) == 0 {
		if r.failure != nil {
			return 0, r.failure
		}
		if r.verified {
			return 0, io.EOF
		}
		if err := r.readChunk(); err != nil {
			r.failure = err
			r.cancel()
			return 0, err
		}
	}
	n := copy(dst, r.pending)
	r.pending = r.pending[n:]
	return n, nil
}

// Close завершает RPC при отмене потребителем.
func (r *downloadReader) Close() error {
	r.cancel()
	return nil
}

func (r *downloadReader) readChunk() error {
	frame, err := r.recv()
	if err != nil {
		if errors.Is(err, io.EOF) {
			return D.ErrProtocol
		}
		return fileError(r.ctx, err)
	}
	data := frame.GetPayload()
	if len(data) == 0 || len(data) > F.ChunkBytes || frame.GetOffset() != r.offset || uint64(len(data)) > r.left ||
		frame.Complete || frame.Error != nil || frame.Version != nil || frame.TotalBytes != nil || frame.Sha256 != nil {
		return D.ErrProtocol
	}
	_, _ = r.digest.Write(data)
	r.offset += uint64(len(data))
	r.left -= uint64(len(data))
	if r.left == 0 {
		if err = r.finish(); err != nil {
			return err
		}
	}
	r.pending = data
	return nil
}

func (r *downloadReader) finish() error {
	last, err := r.recv()
	if err != nil {
		if errors.Is(err, io.EOF) {
			return D.ErrProtocol
		}
		return fileError(r.ctx, err)
	}
	if !last.GetComplete() || last.GetOffset() != r.offset || len(last.GetPayload()) != 0 || last.Error != nil ||
		last.GetSha256() != hex.EncodeToString(r.digest.Sum(nil)) {
		return D.ErrProtocol
	}
	if _, err = r.recv(); !errors.Is(err, io.EOF) {
		if err == nil {
			return D.ErrProtocol
		}
		return fileError(r.ctx, err)
	}
	r.verified = true
	return nil
}
