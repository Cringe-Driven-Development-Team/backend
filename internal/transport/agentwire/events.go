package agentwire

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"strconv"

	agentv1 "github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/gen/cellestial/agent/v1"
	D "github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/runtimesession/domain"
	"github.com/google/uuid"
	"google.golang.org/protobuf/proto"
)

// ReadEventPage возвращает страницу только после целых объектов и подтверждённого page_end/EOF.
func ReadEventPage(ctx context.Context, stream EventReader, session uuid.UUID, after int64, limits D.EventLimits) ([]D.AgentEvent, error) {
	if after < 0 || session == uuid.Nil {
		return nil, D.ErrInvalid
	}
	count := limits.Count
	if count <= 0 || count > 100 {
		count = 100
	}
	budget := limits.Bytes
	if budget <= 0 || budget > 16<<20 {
		budget = 16 << 20
	}
	result := make([]D.AgentEvent, 0)
	cursor := after
	used := 0
	var header *agentv1.EventHeader
	var object objectAccumulator
	pageEnded := false
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		frame, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			if !pageEnded {
				return nil, D.ErrProtocol
			}
			return result, nil
		}
		if err != nil {
			return nil, err
		}
		if frame == nil || pageEnded {
			return nil, D.ErrProtocol
		}
		switch value := frame.Frame.(type) {
		case *agentv1.EventFrame_Header:
			h := value.Header
			if header != nil || h == nil || h.Seq <= cursor || h.SessionId != session.String() || h.Kind == "" || h.GetObject().GetObjectId() != strconv.FormatInt(h.Seq, 10) || math.IsNaN(h.Timestamp) || math.IsInf(h.Timestamp, 0) || h.Timestamp < 0 || len(result) >= count {
				return nil, D.ErrProtocol
			}
			header = h
			object = objectAccumulator{}
			if err = object.begin(h.Object); err != nil {
				return nil, err
			}
		case *agentv1.EventFrame_Chunk:
			if err = object.chunk(value.Chunk); err != nil {
				return nil, err
			}
		case *agentv1.EventFrame_End:
			data, endErr := object.end(value.End)
			if endErr != nil || header == nil {
				return nil, D.ErrProtocol
			}
			var payload agentv1.EventPayload
			if proto.Unmarshal(data, &payload) != nil {
				return nil, D.ErrProtocol
			}
			for _, jsonValue := range [][]byte{payload.ContentJson, payload.MetadataJson, payload.ResultJson} {
				if len(jsonValue) > 0 && !json.Valid(jsonValue) {
					return nil, D.ErrProtocol
				}
			}
			if len(result) > 0 && used+len(data) > budget {
				return nil, D.ErrProtocol
			}
			result = append(result, D.AgentEvent{Seq: header.Seq, SessionID: session, ExecutionID: header.ExecutionId, Kind: header.Kind, State: header.State, Reason: header.Reason, Generation: header.Generation, Channel: header.Channel, ParentMsgID: header.ParentMsgId, MsgType: header.MsgType, Timestamp: header.Timestamp, Content: payload.ContentJson, Metadata: payload.MetadataJson, Result: payload.ResultJson, Buffers: payload.Buffers})
			used += len(data)
			cursor = header.Seq
			header = nil
		case *agentv1.EventFrame_PageEnd:
			if header != nil || value.PageEnd == nil || value.PageEnd.NextSeq != cursor {
				return nil, D.ErrProtocol
			}
			pageEnded = true
		default:
			return nil, D.ErrProtocol
		}
	}
}
