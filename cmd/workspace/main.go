// Workspace запускает API блокнотов и фоновые обработчики пользовательских runtime-сессий.
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/app/workspace"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/observability"
)

// main связывает параметры процесса, structured logger и управление сигналами.
func main() {
	err := run()
	if err != nil {
		slog.Error("workspace.failed", "error", err.Error())
		os.Exit(1)
	}
}

// run выполняет lifecycle до сигнала завершения или отказа компонента.
func run() error {
	cfg, err := workspace.LoadConfig(os.LookupEnv)
	if err != nil {
		return err
	}
	logger := slog.New(observability.NewHandler(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.LogLevel}))).With("service", "workspace", "version", cfg.Version)
	slog.SetDefault(logger)
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer cancel()
	return workspace.Run(ctx, cfg, logger)
}
