package rpc

import (
	"context"
	"log/slog"
	"time"

	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/observability"
	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// contextStream передаёт обогащённый context во все stream handlers.
type contextStream struct {
	grpc.ServerStream
	ctx     context.Context
	metrics *Metrics
	method  string
}

// Context возвращает correlation и ограниченный бюджет server stream.
func (s contextStream) Context() context.Context { return s.ctx }

// SendMsg учитывает сообщение только после успешной передачи transport.
func (s contextStream) SendMsg(value any) error {
	err := s.ServerStream.SendMsg(value)
	if err == nil {
		s.metrics.message(s.method, "server", "sent", value)
	}
	return err
}

// RecvMsg сохраняет backpressure transport и учитывает только успешно прочитанные сообщения.
func (s contextStream) RecvMsg(value any) error {
	err := s.ServerStream.RecvMsg(value)
	if err == nil {
		s.metrics.message(s.method, "server", "received", value)
	}
	return err
}

// StreamServer применяет ту же проверку identity, correlation и recovery к stream RPC.
func StreamServer(cfg UnaryConfig) grpc.StreamServerInterceptor {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	timeout := cfg.StreamTimeout
	if timeout <= 0 {
		timeout = cfg.Timeout
	}
	if timeout <= 0 {
		timeout = 2 * time.Second
	}
	return func(srv any, stream grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) (err error) {
		started := time.Now()
		method := info.FullMethod
		if _, ok := cfg.AllowedPeers[method]; !ok {
			method = "unmatched"
		}
		finish := cfg.Metrics.begin(method, "stream", "server")
		ctx := stream.Context()
		md, _ := metadata.FromIncomingContext(ctx)
		id := uuid.NewString()
		if values := md.Get("x-request-id"); len(values) == 1 && observability.ValidRequestID(values[0]) {
			id = values[0]
		}
		ctx = observability.WithRequestID(ctx, id)
		budget := timeout
		if value := cfg.StreamMethods[info.FullMethod]; value > 0 {
			budget = value
		}
		ctx, cancel := context.WithTimeout(ctx, budget)
		defer cancel()
		_ = stream.SetHeader(metadata.Pairs("x-request-id", id))
		defer func() {
			if recover() != nil {
				err = status.Error(codes.Internal, "internal error")
			}
			if ctx.Err() != nil {
				err = status.FromContextError(ctx.Err()).Err()
			}
			finish(status.Code(err).String())
			logger.InfoContext(ctx, "rpc.request", slog.String("service", cfg.Service), slog.String("version", cfg.Version), slog.String("request_id", id), slog.String("method", method), slog.String("status", status.Code(err).String()), slog.Duration("duration", time.Since(started)))
		}()
		if err = authorizePeer(ctx, cfg.AllowedPeers[info.FullMethod]); err != nil {
			return err
		}
		if err = ctx.Err(); err != nil {
			return status.FromContextError(err).Err()
		}
		// Возврат interceptor закрывает transport stream и разблокирует исходный RecvMsg.
		// Одной подмены Context недостаточно: клиент может не прислать первое сообщение.
		result := make(chan error, 1)
		go func() {
			result <- invokeStreamHandler(srv, contextStream{ServerStream: stream, ctx: ctx, metrics: cfg.Metrics, method: method}, handler)
		}()
		select {
		case err := <-result:
			return err
		case <-ctx.Done():
			return status.FromContextError(ctx.Err()).Err()
		}
	}
}

// invokeStreamHandler восстанавливается после panic внутри worker goroutine без payload в ошибке.
func invokeStreamHandler(srv any, stream grpc.ServerStream, handler grpc.StreamHandler) (err error) {
	defer func() {
		if recover() != nil {
			err = status.Error(codes.Internal, "internal error")
		}
	}()
	return handler(srv, stream)
}
