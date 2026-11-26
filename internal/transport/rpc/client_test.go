package rpc

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/observability"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// TestClientBudgetAndRetry сохраняет correlation, ограничивает три попытки и не повторяет interrupt.
func TestClientBudgetAndRetry(t *testing.T) {
	for _, tc := range []struct {
		name      string
		retry     bool
		budget    time.Duration
		wantCalls int
		want      codes.Code
	}{{"read", true, time.Second, 3, codes.OK}, {"interrupt", false, time.Second, 1, codes.Unavailable}, {"bounded", true, 10 * time.Millisecond, 1, codes.DeadlineExceeded}} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			cfg := ClientConfig{Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), Timeout: time.Second, Methods: map[string]MethodPolicy{"/test/Call": {Retry: tc.retry}}}
			ctx, cancel := context.WithTimeout(observability.WithRequestID(context.Background(), "retry-correlation"), tc.budget)
			defer cancel()
			parentDeadline, _ := ctx.Deadline()
			err := UnaryClient(cfg)(ctx, "/test/Call", nil, nil, nil, func(child context.Context, _ string, _, _ any, _ *grpc.ClientConn, _ ...grpc.CallOption) error {
				calls++
				deadline, ok := child.Deadline()
				if !ok || deadline.After(parentDeadline) {
					t.Error("deadline budget extended")
				}
				md, _ := metadata.FromOutgoingContext(child)
				if got := md.Get("x-request-id"); len(got) != 1 || got[0] != "retry-correlation" {
					t.Error("correlation lost on retry")
				}
				if calls < 3 {
					return status.Error(codes.Unavailable, "private-error")
				}
				return nil
			})
			if calls != tc.wantCalls || status.Code(err) != tc.want {
				t.Fatalf("calls=%d status=%v", calls, err)
			}
		})
	}
}

// clientStreamFixture моделирует EOF без сетевого retry; настоящие TLS клиенты проверяются отдельно.
type clientStreamFixture struct{ ctx context.Context }

func (s clientStreamFixture) Header() (metadata.MD, error) { return nil, nil }
func (s clientStreamFixture) Trailer() metadata.MD         { return nil }
func (s clientStreamFixture) CloseSend() error             { return nil }
func (s clientStreamFixture) Context() context.Context     { return s.ctx }
func (s clientStreamFixture) SendMsg(any) error            { return nil }
func (s clientStreamFixture) RecvMsg(any) error            { return io.EOF }

// TestStreamClientBudget живёт до EOF, а не только до открытия stream.
func TestStreamClientBudget(t *testing.T) {
	cfg := ClientConfig{Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), StreamTimeout: time.Second, Methods: map[string]MethodPolicy{"/test/Stream": {}}}
	stream, err := StreamClient(cfg)(observability.WithRequestID(context.Background(), "stream-client"), &grpc.StreamDesc{}, nil, "/test/Stream", func(ctx context.Context, _ *grpc.StreamDesc, _ *grpc.ClientConn, _ string, _ ...grpc.CallOption) (grpc.ClientStream, error) {
		if _, ok := ctx.Deadline(); !ok {
			t.Error("missing stream budget")
		}
		return clientStreamFixture{ctx}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if stream.Context().Err() != nil {
		t.Fatal("stream canceled at opening")
	}
	if err = stream.RecvMsg(nil); !errors.Is(err, io.EOF) {
		t.Fatal(err)
	}
	if !errors.Is(stream.Context().Err(), context.Canceled) {
		t.Fatal("stream budget not released at EOF")
	}
}
