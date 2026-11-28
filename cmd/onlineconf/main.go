// Onlineconf запускает самостоятельный административный HTTP-сервис.
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/app/onlineconf"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/observability"
)

// main настраивает логи и завершает процесс по SIGTERM или SIGINT.
func main() {
	logger := slog.New(observability.NewHandler(slog.NewJSONHandler(os.Stdout, nil))).With("service", "onlineconf")
	slog.SetDefault(logger)
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	err := onlineconf.Run(ctx, logger)
	cancel()
	if err != nil {
		logger.Error("onlineconf.run", "error", err.Error())
		os.Exit(1)
	}
}
