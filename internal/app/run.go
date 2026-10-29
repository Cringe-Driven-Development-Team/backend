package app

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/config"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/observability"
)

func startupError(stage string, err error) error {
	switch stage {
	case "database_connect", "database_migrate", "s3_client", "redis_connect", "http_listen":
		return observability.WithCode(err, stage)
	default:
		return observability.WithCode(err, "internal_error")
	}
}

// Run запускает API с настройками окружения и освобождает ресурсы при завершении.
func Run(cfg config.Config) error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	server, closeResources, err := newServer(ctx, cfg)
	if err != nil {
		return err
	}
	defer closeResources()
	slog.Info("starting server", slog.String("addr", server.Addr), slog.String("write_timeout", server.WriteTimeout.String()), slog.String("read_timeout", server.ReadTimeout.String()))
	done := make(chan error, 1)
	go func() { done <- server.ListenAndServe() }()
	select {
	case err := <-done:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return startupError("http_listen", err)
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			_ = server.Close()
			return err
		}
		return nil
	}
}
