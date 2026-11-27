// Package agentwire адаптирует внутренний Protobuf/gRPC агента к портам исполнения.
package agentwire

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"hash"
	"io"

	agentv1 "github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/gen/cellestial/agent/v1"
	commonv1 "github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/gen/cellestial/common/v1"
	D "github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/runtimesession/domain"
	"google.golang.org/protobuf/proto"
)

const maxObjectBytes = 64 << 20
const chunkBytes = 1 << 20

// ResultReader отделяет целостную сборку результата от generated client конкретного hop.
type ResultReader interface {
	Recv() (*agentv1.ResultFrame, error)
}

// EventReader отделяет целостную страницу событий от generated stream клиента.
type EventReader interface {
	Recv() (*agentv1.EventFrame, error)
}

// objectAccumulator хранит максимум один bounded объект до подтверждённого End.
type objectAccumulator struct {
	header *commonv1.ObjectHeader
	data   bytes.Buffer
	digest hash.Hash
}

// begin отвергает безлимитный объект до выделения памяти под его объявленную длину.
func (o *objectAccumulator) begin(h *commonv1.ObjectHeader) error {
	if o.header != nil || h == nil || h.Length > maxObjectBytes || len(h.Sha256) != sha256.Size {
		return D.ErrProtocol
	}
	o.header = h
	o.data.Reset()
	o.data.Grow(int(min(h.Length, chunkBytes)))
	o.digest = sha256.New()
	return nil
}

// chunk принимает только непустую последовательную порцию в пределах объявленной длины.
func (o *objectAccumulator) chunk(c *commonv1.ObjectChunk) error {
	if o.header == nil || c == nil || len(c.Payload) == 0 || len(c.Payload) > chunkBytes || c.Offset != uint64(o.data.Len()) || uint64(len(c.Payload)) > o.header.Length-c.Offset {
		return D.ErrProtocol
	}
	_, _ = o.data.Write(c.Payload)
	_, _ = o.digest.Write(c.Payload)
	return nil
}

// end проверяет SHA-256 и точную длину, не возвращая частичный объект.
func (o *objectAccumulator) end(end *commonv1.ObjectEnd) ([]byte, error) {
	if o.header == nil || end == nil || end.Length != o.header.Length || uint64(o.data.Len()) != end.Length || !bytes.Equal(end.Sha256, o.header.Sha256) || !bytes.Equal(o.digest.Sum(nil), end.Sha256) {
		return nil, D.ErrProtocol
	}
	return o.data.Bytes(), nil
}

// ReadExecution публикует результат лишь после header/chunks/End и успешного transport EOF.
func ReadExecution(ctx context.Context, stream ResultReader, id string) (D.AgentExecution, error) {
	var header *agentv1.ExecutionHeader
	var object objectAccumulator
	var result D.AgentExecution
	ended := false
	for {
		if err := ctx.Err(); err != nil {
			return D.AgentExecution{}, err
		}
		frame, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			if !ended {
				return D.AgentExecution{}, D.ErrProtocol
			}
			return result, nil
		}
		if err != nil {
			return D.AgentExecution{}, err
		}
		if frame == nil || ended {
			return D.AgentExecution{}, D.ErrProtocol
		}
		switch value := frame.Frame.(type) {
		case *agentv1.ResultFrame_Header:
			if header != nil || value.Header == nil || value.Header.ExecutionId != id || value.Header.GetObject().GetObjectId() != id || !executionState(value.Header.State) {
				return D.AgentExecution{}, D.ErrProtocol
			}
			header = value.Header
			if err = object.begin(header.Object); err != nil {
				return D.AgentExecution{}, err
			}
		case *agentv1.ResultFrame_Chunk:
			if err = object.chunk(value.Chunk); err != nil {
				return D.AgentExecution{}, err
			}
		case *agentv1.ResultFrame_End:
			data, endErr := object.end(value.End)
			if endErr != nil || header == nil {
				return D.AgentExecution{}, D.ErrProtocol
			}
			var payload agentv1.ExecutionPayload
			if proto.Unmarshal(data, &payload) != nil || (len(payload.ResultJson) > 0 && !json.Valid(payload.ResultJson)) {
				return D.AgentExecution{}, D.ErrProtocol
			}
			result = D.AgentExecution{ExecutionID: header.ExecutionId, State: header.State, Result: payload.ResultJson}
			ended = true
		default:
			return D.AgentExecution{}, D.ErrProtocol
		}
	}
}

// executionState ограничивает ответы известными состояниями журнала, включая неоднозначный UNKNOWN.
func executionState(state string) bool {
	switch state {
	case "QUEUED", "RUNNING", "SUCCEEDED", "FAILED", "INTERRUPTED", "TIMED_OUT", "CANCELLED", "UNKNOWN", "REJECTED":
		return true
	default:
		return false
	}
}
