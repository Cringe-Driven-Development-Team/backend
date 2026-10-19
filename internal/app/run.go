package app

import (
	"context"
	"log/slog"

	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/config"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/observability"
)

func startupError(stage string, err error) error {
	switch stage {
	case "database_connect", "database_migrate", "s3_client", "http_listen":
		return observability.WithCode(err, stage)
	default:
		return observability.WithCode(err, "internal_error")
	}
}

// Run запускает API с настройками окружения и освобождает ресурсы при завершении.
func Run(cfg config.Config) error {
	server, closeResources, err := newServer(context.Background(), cfg)
	if err != nil {
		return err
	}
	defer closeResources()
	slog.Info("starting server", slog.String("addr", server.Addr), slog.String("write_timeout", server.WriteTimeout.String()), slog.String("read_timeout", server.ReadTimeout.String()))
	if err := server.ListenAndServe(); err != nil {
		return startupError("http_listen", err)
	}
	return nil
}
