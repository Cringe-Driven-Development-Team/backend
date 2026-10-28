package app

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/config"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/observability"
)

func startupError(stage string, err error) error {
	switch stage {
	case "database_connect", "database_migrate", "s3_client", "runtime_init", "http_listen":
		return observability.WithCode(err, stage)
	default:
		return observability.WithCode(err, "internal_error")
	}
}

// Run запускает API с настройками окружения и освобождает ресурсы при завершении.
func Run(cfg config.Config) error {
	app, err := newServer(context.Background(), cfg)
	if err != nil {
		return err
	}
	defer app.Close()
	server := app.Server

	workerCtx, stopWorker := context.WithCancel(context.Background())
	workerDone := make(chan struct{})
	if app.RuntimeWorker != nil {
		slog.Info("runtime worker started", slog.String("provider", app.RuntimeMode))
		go func() {
			defer close(workerDone)
			app.RuntimeWorker.Run(workerCtx)
		}()
	} else {
		close(workerDone)
	}
	defer func() {
		stopWorker()
		<-workerDone
	}()

	slog.Info("starting server", slog.String("addr", server.Addr), slog.String("write_timeout", server.WriteTimeout.String()), slog.String("read_timeout", server.ReadTimeout.String()))
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return startupError("http_listen", err)
	}
	return nil
}
