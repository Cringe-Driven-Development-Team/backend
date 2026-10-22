package observability

import (
	"context"
	"log/slog"
)

type requestHandler struct{ next slog.Handler }

func NewHandler(next slog.Handler) slog.Handler { return requestHandler{next: next} }

func (h requestHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

func (h requestHandler) Handle(ctx context.Context, record slog.Record) error {
	if id := RequestID(ctx); id != "" {
		record = record.Clone()
		record.AddAttrs(slog.String("request_id", id))
	}
	return h.next.Handle(ctx, record)
}

func (h requestHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return requestHandler{next: h.next.WithAttrs(attrs)}
}

func (h requestHandler) WithGroup(name string) slog.Handler {
	return requestHandler{next: h.next.WithGroup(name)}
}
