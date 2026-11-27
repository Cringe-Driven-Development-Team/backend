// Package rpc содержит общие проверки входящих межсервисных RPC.
package rpc

import (
	"context"
	"log/slog"
	"time"

	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/observability"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/transport/mtls"
	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// UnaryConfig задаёт безопасное логирование, deadline и точные DNS SAN по full method.
type UnaryConfig struct {
	Logger        *slog.Logger
	Service       string
	Version       string
	Timeout       time.Duration
	StreamTimeout time.Duration
	StreamMethods map[string]time.Duration
	AllowedPeers  map[string][]string
	Metrics       *Metrics
}

// ServerOptions ограничивает размер заголовков/сообщений и подключает общую unary-цепочку.
// TLS credentials задаются отдельно: конфигурация не допускает plaintext fallback.
func ServerOptions(cfg UnaryConfig) []grpc.ServerOption {
	return []grpc.ServerOption{
		grpc.ConnectionTimeout(2 * time.Second),
		grpc.MaxHeaderListSize(16 << 10),
		grpc.MaxRecvMsgSize(8 << 20),
		grpc.MaxSendMsgSize(8 << 20),
		grpc.ChainUnaryInterceptor(UnaryServer(cfg)),
		grpc.ChainStreamInterceptor(StreamServer(cfg)),
	}
}

// UnaryServer добавляет correlation, ограничивает время, проверяет peer и пишет один итог.
// req/response, metadata и исходные ошибки не включаются в технический лог.
func UnaryServer(cfg UnaryConfig) grpc.UnaryServerInterceptor {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 2 * time.Second
	}
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (response any, err error) {
		started := time.Now()
		method := info.FullMethod
		if _, ok := cfg.AllowedPeers[method]; !ok {
			method = "unmatched"
		}
		finish := cfg.Metrics.begin(method)
		md, _ := metadata.FromIncomingContext(ctx)
		id := uuid.NewString()
		if values := md.Get("x-request-id"); len(values) == 1 && observability.ValidRequestID(values[0]) {
			id = values[0]
		}
		ctx = observability.WithRequestID(ctx, id)
		_ = grpc.SetHeader(ctx, metadata.Pairs("x-request-id", id))
		ctx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		defer func() {
			if recover() != nil {
				response = nil
				err = status.Error(codes.Internal, "internal error")
			}
			finish(status.Code(err).String())
			logger.InfoContext(ctx, "rpc.request",
				slog.String("service", cfg.Service), slog.String("version", cfg.Version),
				slog.String("request_id", id), slog.String("method", method),
				slog.String("status", status.Code(err).String()), slog.Duration("duration", time.Since(started)),
			)
		}()
		if err = authorizePeer(ctx, cfg.AllowedPeers[info.FullMethod]); err != nil {
			return nil, err
		}
		if err = ctx.Err(); err != nil {
			return nil, status.FromContextError(err).Err()
		}
		cfg.Metrics.message(method, "received", req)
		response, err = handler(ctx, req)
		if err == nil {
			cfg.Metrics.message(method, "sent", response)
		}
		return response, err
	}
}

// authorizePeer разрешает только проверенный TLS peer с точным SAN из списка метода.
func authorizePeer(ctx context.Context, allowed []string) error {
	return mtls.AuthorizePeer(ctx, allowed)
}
