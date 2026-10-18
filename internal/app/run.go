package app

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/config"
)

// Run запускает API с настройками окружения и освобождает ресурсы при завершении.
func Run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	server, closeResources, err := newServer(context.Background(), cfg)
	if err != nil {
		return err
	}
	defer closeResources()
	slog.Info("starting server", slog.String("addr", server.Addr), slog.String("write_timeout", server.WriteTimeout.String()), slog.String("read_timeout", server.ReadTimeout.String()))
	if err := server.ListenAndServe(); err != nil {
		return fmt.Errorf("server startup failed: %w", err)
	}
	return nil
}
