// Identity запускает HTTP/gRPC API пользователей и отдельный admin listener.
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/app/identity"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/observability"
)

// main связывает конфигурацию процесса, структурированные логи и сигналы завершения.
func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)).With("service", "identity"))
	if err := run(); err != nil {
		slog.Error("identity.startup", "error", err.Error())
		os.Exit(1)
	}
}

// run освобождает ресурсы и signal context до завершения процесса.
func run() error {
	level := new(slog.LevelVar)
	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level})
	cfg, err := identity.LoadConfig(os.LookupEnv)
	if err != nil {
		return err
	}
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	level.Set(cfg.LogLevel)
	slog.SetDefault(slog.New(observability.NewHandler(handler)).With("service", "identity", "version", cfg.Version))
	defer cancel()
	if err = identity.Run(ctx, cfg, slog.Default()); err != nil {
		return err
	}
	return nil
}
