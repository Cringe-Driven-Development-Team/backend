package agentwire

import (
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"testing"

	agentv1 "github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/gen/cellestial/agent/v1"
	commonv1 "github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/gen/cellestial/common/v1"
	D "github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/runtimesession/domain"
	"google.golang.org/protobuf/proto"
)

// resultReader воспроизводит окончание stream и повреждения на границе полной сборки объекта.
type resultReader struct {
	frames []*agentv1.ResultFrame
	err    error
}

func (r *resultReader) Recv() (*agentv1.ResultFrame, error) {
	if len(r.frames) == 0 {
		if r.err != nil {
			return nil, r.err
		}
		return nil, io.EOF
	}
	v := r.frames[0]
	r.frames = r.frames[1:]
	return v, nil
}

// resultFrames создаёт отдельные header/chunk/end, не заменяя проверяемый parser.
func resultFrames(t *testing.T) []*agentv1.ResultFrame {
	t.Helper()
	data, err := proto.Marshal(&agentv1.ExecutionPayload{ResultJson: []byte(`{"status":"ok"}`)})
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	return []*agentv1.ResultFrame{
		{Frame: &agentv1.ResultFrame_Header{Header: &agentv1.ExecutionHeader{ExecutionId: "exec", State: "SUCCEEDED", Object: &commonv1.ObjectHeader{ObjectId: "exec", Length: uint64(len(data)), Sha256: sum[:]}}}},
		{Frame: &agentv1.ResultFrame_Chunk{Chunk: &commonv1.ObjectChunk{Offset: 0, Payload: data}}},
		{Frame: &agentv1.ResultFrame_End{End: &commonv1.ObjectEnd{Length: uint64(len(data)), Sha256: sum[:]}}},
	}
}

// TestResultStreamIntegrity не допускает частичный/переставленный/повторный результат после сетевой ошибки.
func TestResultStreamIntegrity(t *testing.T) {
	good, err := ReadExecution(context.Background(), &resultReader{frames: resultFrames(t)}, "exec")
	if err != nil || good.State != "SUCCEEDED" || string(good.Result) != `{"status":"ok"}` {
		t.Fatalf("valid result: %v %v", good, err)
	}
	for _, name := range []string{"truncated", "gap", "duplicate", "digest", "oversize", "wrong id", "extra after end"} {
		t.Run(name, func(t *testing.T) {
			frames := resultFrames(t)
			switch name {
			case "truncated":
				frames = frames[:2]
			case "gap":
				frames[1].GetChunk().Offset = 1
			case "duplicate":
				frames = append(frames[:2], frames[1], frames[2])
			case "digest":
				frames[2].GetEnd().Sha256 = []byte("wrong")
			case "oversize":
				frames[0].GetHeader().Object.Length = (64 << 20) + 1
			case "wrong id":
				frames[0].GetHeader().ExecutionId = "other"
			case "extra after end":
				frames = append(frames, frames[1])
			}
			out, err := ReadExecution(context.Background(), &resultReader{frames: frames}, "exec")
			if !errors.Is(err, D.ErrProtocol) || out.ExecutionID != "" {
				t.Fatalf("partial result accepted: %v %v", out, err)
			}
		})
	}
}
