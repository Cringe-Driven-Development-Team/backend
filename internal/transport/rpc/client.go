package rpc

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"math/rand/v2"
	"sync"
	"time"

	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/observability"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/transport/mtls"
	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// MethodPolicy разрешает retry только явно указанному чтению/дедуплицируемой записи.
type MethodPolicy struct {
	Retry                  bool
	Timeout, StreamTimeout time.Duration
}

// ClientConfig задаёт собственные credentials, реестр методов и конечный бюджет вызовов.
type ClientConfig struct {
	TLS                          mtls.Config
	ServerName, Service, Version string
	Logger                       *slog.Logger
	Timeout, StreamTimeout       time.Duration
	Methods                      map[string]MethodPolicy
	Metrics                      *Metrics
	MaxMessageBytes              int
}

// ClientOptions не допускает plaintext fallback и независимый gRPC retry поверх application retry.
func ClientOptions(c ClientConfig) ([]grpc.DialOption, error) {
	config, err := mtls.ClientConfig(c.TLS, c.ServerName)
	if err != nil {
		return nil, err
	}
	limit := c.MaxMessageBytes
	if limit <= 0 {
		limit = 8 << 20
	}
	return []grpc.DialOption{grpc.WithTransportCredentials(credentials.NewTLS(config)), grpc.WithDisableRetry(), grpc.WithMaxHeaderListSize(16 << 10), grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(limit), grpc.MaxCallSendMsgSize(limit)), grpc.WithChainUnaryInterceptor(UnaryClient(c)), grpc.WithChainStreamInterceptor(StreamClient(c))}, nil
}

// outgoingContext сохраняет единственный correlation и ограничивает суммарную metadata до transport.
func outgoingContext(ctx context.Context) (context.Context, error) {
	md, _ := metadata.FromOutgoingContext(ctx)
	md = md.Copy()
	id := observability.RequestID(ctx)
	if !observability.ValidRequestID(id) {
		values := md.Get("x-request-id")
		if len(values) == 1 && observability.ValidRequestID(values[0]) {
			id = values[0]
		} else {
			id = uuid.NewString()
		}
	}
	ctx = observability.WithRequestID(ctx, id)
	md.Set("x-request-id", id)
	size := 0
	for key, values := range md {
		for _, value := range values {
			size += len(key) + len(value) + 32
		}
	}
	if size > 16<<10 {
		return ctx, status.Error(codes.ResourceExhausted, "metadata limit exceeded")
	}
	return metadata.NewOutgoingContext(ctx, md), nil
}

// methodName сводит неизвестные методы к одной метке, не раскрывая пользовательский URL.
func methodName(c ClientConfig, method string) string {
	if _, ok := c.Methods[method]; ok {
		return method
	}
	return "unmatched"
}

// clientLogger использует переданный логгер либо штатный structured logger процесса.
func clientLogger(c ClientConfig) *slog.Logger {
	if c.Logger != nil {
		return c.Logger
	}
	return slog.Default()
}

// UnaryClient задаёт общий бюджет и максимум три безопасных попытки с исходным correlation.
func UnaryClient(c ClientConfig) grpc.UnaryClientInterceptor {
	return func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoke grpc.UnaryInvoker, opts ...grpc.CallOption) (err error) {
		started := time.Now()
		name := methodName(c, method)
		finish := c.Metrics.begin(name)
		attempts := 0
		policy := c.Methods[method]
		timeout := policy.Timeout
		if timeout <= 0 {
			timeout = c.Timeout
		}
		if timeout <= 0 {
			timeout = 5 * time.Second
		}
		ctx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		defer func() {
			if recover() != nil {
				err = status.Error(codes.Internal, "internal client error")
			}
			if ctx.Err() != nil {
				err = status.FromContextError(ctx.Err()).Err()
			}
			finish(status.Code(err).String())
			clientLogger(c).InfoContext(ctx, "rpc.client", slog.String("service", c.Service), slog.String("version", c.Version), slog.String("request_id", observability.RequestID(ctx)), slog.String("method", name), slog.String("status", status.Code(err).String()), slog.Int("attempts", attempts), slog.Duration("duration", time.Since(started)))
		}()
		ctx, err = outgoingContext(ctx)
		if err != nil {
			return err
		}
		maximum := 1
		if policy.Retry {
			maximum = 3
		}
		for attempts < maximum {
			if ctx.Err() != nil {
				return status.FromContextError(ctx.Err()).Err()
			}
			attempts++
			c.Metrics.message(name, "sent", req)
			err = invoke(ctx, method, req, reply, cc, opts...)
			if err == nil {
				c.Metrics.message(name, "received", reply)
				return nil
			}
			if status.Code(err) != codes.Unavailable || attempts == maximum {
				return err
			}
			delay := time.Duration(100*(1<<(attempts-1))) * time.Millisecond
			delay = time.Duration(float64(delay) * (0.5 + rand.Float64()))
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return status.FromContextError(ctx.Err()).Err()
			case <-timer.C:
			}
		}
		return err
	}
}

// measuredClientStream удерживает context и наблюдаемость до полного завершения чтения.
type measuredClientStream struct {
	grpc.ClientStream
	cancel  context.CancelFunc
	finish  func(error)
	metrics *Metrics
	method  string
}

// SendMsg учитывает bytes только после успешной отправки; EOF не заменяет чтение итогового status.
func (s *measuredClientStream) SendMsg(value any) error {
	err := s.ClientStream.SendMsg(value)
	if err == nil {
		s.metrics.message(s.method, "sent", value)
	} else if !errors.Is(err, io.EOF) {
		s.finish(err)
	}
	return err
}

// RecvMsg завершает logical RPC на EOF/status и закрывает его отдельный бюджет.
func (s *measuredClientStream) RecvMsg(value any) error {
	err := s.ClientStream.RecvMsg(value)
	if err == nil {
		s.metrics.message(s.method, "received", value)
	} else {
		s.finish(err)
	}
	return err
}

// StreamClient не отменяет stream при открытии; parent cancel/default cap действуют до EOF.
func StreamClient(c ClientConfig) grpc.StreamClientInterceptor {
	return func(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, streamer grpc.Streamer, opts ...grpc.CallOption) (stream grpc.ClientStream, err error) {
		name := methodName(c, method)
		started := time.Now()
		finishMetric := c.Metrics.begin(name)
		timeout := c.Methods[method].StreamTimeout
		if timeout <= 0 {
			timeout = c.StreamTimeout
		}
		if timeout <= 0 {
			timeout = 2 * time.Hour
		}
		ctx, cancel := context.WithTimeout(ctx, timeout)
		var once sync.Once
		finish := func(callErr error) {
			once.Do(func() {
				if errors.Is(callErr, io.EOF) {
					callErr = nil
				}
				if ctx.Err() != nil {
					callErr = status.FromContextError(ctx.Err()).Err()
				}
				finishMetric(status.Code(callErr).String())
				clientLogger(c).InfoContext(ctx, "rpc.client", slog.String("service", c.Service), slog.String("version", c.Version), slog.String("request_id", observability.RequestID(ctx)), slog.String("method", name), slog.String("status", status.Code(callErr).String()), slog.Duration("duration", time.Since(started)))
				cancel()
			})
		}
		defer func() {
			if recover() != nil {
				stream = nil
				err = status.Error(codes.Internal, "internal client error")
				finish(err)
			}
		}()
		ctx, err = outgoingContext(ctx)
		if err != nil {
			finish(err)
			return nil, err
		}
		context.AfterFunc(ctx, func() { finish(status.FromContextError(ctx.Err()).Err()) })
		stream, err = streamer(ctx, desc, cc, method, opts...)
		if err != nil {
			finish(err)
			return nil, err
		}
		return &measuredClientStream{ClientStream: stream, cancel: cancel, finish: finish, metrics: c.Metrics, method: name}, nil
	}
}
