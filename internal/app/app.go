// Package app собирает зависимости, HTTP-роутер и запускает API.
package app

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/config"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/notebook/adapters/ipynb"
	notebookpostgres "github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/notebook/adapters/postgres"
	notebooks3 "github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/notebook/adapters/s3"
	notebookdelivery "github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/notebook/delivery/http"
	notebookusecase "github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/notebook/usecase"
	httptransport "github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/transport/http"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/user/adapters/postgres"
	usersecurity "github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/user/adapters/security"
	userdelivery "github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/user/delivery/http"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/user/usecase"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/migrations"
	"github.com/jackc/pgx/v5/pgxpool"
)

// newServer собирает приложение; вызывающий закрывает ресурсы после остановки.
func newServer(ctx context.Context, cfg config.Config) (*http.Server, func(), error) {
	dbURL := cfg.PostgresURL()

	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		return nil, nil, startupError("database_connect", fmt.Errorf("unable to connect to database: %w", err))
	}
	ready := false
	defer func() {
		if !ready {
			pool.Close()
		}
	}()

	if err = pool.Ping(ctx); err != nil {
		return nil, nil, startupError("database_connect", fmt.Errorf("database ping failed: %w", err))
	}
	slog.Info("connected to postgres successfully")

	if err = migrations.Up(ctx, pool); err != nil {
		return nil, nil, startupError("database_migrate", err)
	}

	tokens := usersecurity.NewAccessToken([]byte(cfg.JWTSecret), cfg.AccessTTL)
	userRepo := postgres.NewUserRepository(pool)
	userUsecase := usecase.NewUserUsecase(userRepo, userRepo, tokens, usersecurity.PasswordHasher{}, usersecurity.RefreshTokens{}, cfg.RefreshTTL)

	s3Client, err := notebooks3.NewClient(ctx)
	if err != nil {
		return nil, nil, startupError("s3_client", err)
	}
	notebookUsecase := notebookusecase.NewNotebookUsecase(
		notebookpostgres.NewNotebookRepository(pool),
		notebooks3.NewFileStorage(s3Client, cfg.NotebooksBucket),
		ipynb.Codec{},
	)
	userHandler := userdelivery.NewHandler(userUsecase)
	srv := httptransport.Server{UserHandler: userHandler, NotebookHandler: notebookdelivery.NewHandler(notebookUsecase)}
	router, err := httptransport.NewRouter(srv, tokens, cfg.BFFAPIKey)
	if err != nil {
		return nil, nil, err
	}

	bindAddr := fmt.Sprintf("%s:%s", cfg.Host, cfg.Port)

	httpServer := &http.Server{
		Handler:      router,
		Addr:         bindAddr,
		WriteTimeout: cfg.WriteTimeout,
		ReadTimeout:  cfg.ReadTimeout,
	}

	ready = true
	return httpServer, pool.Close, nil
}
