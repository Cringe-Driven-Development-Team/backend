package lifecycle

import (
	"context"
	"errors"
	"net"
	"net/http"
	"testing"
	"time"
)

// TestShutdownWaitsForWorker проверяет, что stop прекращает readiness до отмены работы и ждёт ресурсы.
func TestShutdownWaitsForWorker(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	workerStarted := make(chan struct{})
	notReady := make(chan struct{})
	workerDone := make(chan struct{})
	mainDone := make(chan error, 1)
	go func() {
		mainDone <- Run(ctx, time.Second, []Endpoint{HTTP("127.0.0.1:0", &http.Server{})}, func() { close(notReady) },
			func(ctx context.Context) error {
				close(workerStarted)
				<-ctx.Done()
				select {
				case <-notReady:
				default:
					return errors.New("worker canceled while ready")
				}
				close(workerDone)
				return nil
			})
	}()
	<-workerStarted
	cancel()
	if err := <-mainDone; err != nil {
		t.Fatal(err)
	}
	select {
	case <-workerDone:
	default:
		t.Fatal("returned before worker stopped")
	}
}

// TestDrainTimeout освобождает listener по deadline, оставляя долгую работу caller process exit.
func TestDrainTimeout(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	release := make(chan struct{})
	started := make(chan struct{})
	result := make(chan error, 1)
	done := make(chan struct{})
	defer func() { close(release); <-done }()
	endpoints := []Endpoint{{Address: "127.0.0.1:0", Serve: func(l net.Listener) error { _, err := l.Accept(); return err }, Stop: func(context.Context) {}}}
	go func() {
		result <- Run(ctx, 25*time.Millisecond, endpoints, func() {}, func(context.Context) error {
			close(started)
			<-release
			close(done)
			return nil
		})
	}()
	<-started
	cancel()
	if err := <-result; !errors.Is(err, ErrDrainTimeout) {
		t.Fatal(err)
	}
}
