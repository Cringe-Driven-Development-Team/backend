// Package lifecycle запускает серверы и ограничивает ожидание фоновых задач при остановке.
package lifecycle

import (
	"context"
	"errors"
	"net"
	"net/http"
	"sync"
	"time"

	"google.golang.org/grpc"
)

// Endpoint связывает имя listener с запуском и остановкой его транспорта.
type Endpoint struct {
	Address string
	Serve   func(net.Listener) error
	Stop    func(context.Context)
}

// ErrDrainTimeout означает, что ещё используются ресурсы процесса. Закрывать их под worker нельзя.
var ErrDrainTimeout = errors.New("service shutdown deadline exceeded")

// HTTP открывает plain admin listener или TLS listener по конфигурации сервера.
func HTTP(address string, server *http.Server) Endpoint {
	serve := server.Serve
	if server.TLSConfig != nil {
		serve = func(listener net.Listener) error { return server.ServeTLS(listener, "", "") }
	}
	return Endpoint{Address: address, Serve: serve, Stop: func(ctx context.Context) {
		if server.Shutdown(ctx) != nil {
			_ = server.Close()
		}
	}}
}

// RPC освобождает listener после graceful drain или принудительно по общему deadline.
func RPC(address string, server *grpc.Server) Endpoint {
	return Endpoint{Address: address, Serve: server.Serve, Stop: func(ctx context.Context) {
		done := make(chan struct{})
		go func() { server.GracefulStop(); close(done) }()
		select {
		case <-done:
		case <-ctx.Done():
			server.Stop()
		}
	}}
}

// Run останавливает выдачу readiness, отменяет worker и дожидается транспорта перед выходом.
func Run(parent context.Context, timeout time.Duration, endpoints []Endpoint, notReady func(), workers ...func(context.Context) error) error {
	listeners := make([]net.Listener, 0, len(endpoints))
	defer func() {
		for _, listener := range listeners {
			_ = listener.Close()
		}
	}()
	for _, endpoint := range endpoints {
		listener, err := net.Listen("tcp", endpoint.Address)
		if err != nil {
			return errors.New("cannot listen on service address")
		}
		listeners = append(listeners, listener)
	}
	ctx, cancel := context.WithCancel(context.WithoutCancel(parent))
	defer cancel()
	results := make(chan error, len(endpoints)+len(workers))
	var running sync.WaitGroup
	for i, endpoint := range endpoints {
		running.Go(func() { results <- endpoint.Serve(listeners[i]) })
	}
	for _, worker := range workers {
		running.Go(func() { results <- worker(ctx) })
	}
	var cause error
	select {
	case <-parent.Done():
	case <-results:
		cause = errors.New("service component stopped unexpectedly")
	}
	notReady()
	cancel()
	drain, stopDrain := context.WithTimeout(context.Background(), timeout)
	defer stopDrain()
	done := make(chan struct{})
	go func() {
		var stops sync.WaitGroup
		for _, endpoint := range endpoints {
			stops.Go(func() { endpoint.Stop(drain) })
		}
		stops.Wait()
		running.Wait()
		close(done)
	}()
	select {
	case <-done:
		return cause
	case <-drain.Done():
		return ErrDrainTimeout
	}
}
