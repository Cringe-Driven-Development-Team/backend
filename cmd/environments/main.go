// Environments запускает gRPC provisioning и фоновые задачи облака.
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/app/environments"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/observability"
)

// main связывает параметры процесса, structured logger и управление сигналами.
func main() {
	err := run()
	if err != nil {
		slog.Error("environments.failed", "error", err.Error())
		os.Exit(1)
	}
}

// run выполняет lifecycle до сигнала завершения или отказа компонента.
func run() error {
	cfg, err := environments.LoadConfig(os.LookupEnv)
	if err != nil {
		return err
	}
	logger := slog.New(observability.NewHandler(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.LogLevel}))).With("service", "environments", "version", cfg.Version)
	slog.SetDefault(logger)
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer cancel()
	return environments.Run(ctx, cfg, logger)
}
