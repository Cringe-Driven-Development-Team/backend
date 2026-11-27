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
	"github.com/google/uuid"
	"google.golang.org/protobuf/proto"
)

// eventReader задаёт повреждённые страницы без имитации их сборки.
type eventReader struct{ frames []*agentv1.EventFrame }

func (r *eventReader) Recv() (*agentv1.EventFrame, error) {
	if len(r.frames) == 0 {
		return nil, io.EOF
	}
	v := r.frames[0]
	r.frames = r.frames[1:]
	return v, nil
}

// TestEventPageIsAtomic не позволяет продвинуть cursor после оборванного/нецелого события.
func TestEventPageIsAtomic(t *testing.T) {
	sid := uuid.New()
	data, err := proto.Marshal(&agentv1.EventPayload{ContentJson: []byte(`{"text":"hi"}`)})
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	makeFrames := func() []*agentv1.EventFrame {
		return []*agentv1.EventFrame{
			{Frame: &agentv1.EventFrame_Header{Header: &agentv1.EventHeader{Seq: 1, SessionId: sid.String(), Kind: "kernel_message", Object: &commonv1.ObjectHeader{ObjectId: "1", Length: uint64(len(data)), Sha256: sum[:]}}}},
			{Frame: &agentv1.EventFrame_Chunk{Chunk: &commonv1.ObjectChunk{Payload: data}}},
			{Frame: &agentv1.EventFrame_End{End: &commonv1.ObjectEnd{Length: uint64(len(data)), Sha256: sum[:]}}},
			{Frame: &agentv1.EventFrame_PageEnd{PageEnd: &agentv1.EventPageEnd{NextSeq: 1}}}}
	}
	got, err := ReadEventPage(context.Background(), &eventReader{makeFrames()}, sid, 0, D.EventLimits{})
	if err != nil || len(got) != 1 || got[0].Seq != 1 {
		t.Fatalf("page: %v %v", got, err)
	}
	for _, length := range []int{1, 2, 3} {
		out, err := ReadEventPage(context.Background(), &eventReader{makeFrames()[:length]}, sid, 0, D.EventLimits{})
		if !errors.Is(err, D.ErrProtocol) || len(out) != 0 {
			t.Fatalf("partial page leaked: %v %v", out, err)
		}
	}
}
