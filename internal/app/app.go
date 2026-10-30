// Package app собирает зависимости, HTTP-роутер и запускает API.
package app

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strconv"

	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/config"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/notebook/adapters/ipynb"
	notebookpostgres "github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/notebook/adapters/postgres"
	notebooks3 "github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/notebook/adapters/s3"
	notebookdelivery "github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/notebook/delivery/http"
	notebookusecase "github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/notebook/usecase"
	httptransport "github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/transport/http"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/user/adapters/postgres"
	userredis "github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/user/adapters/redis"
	usersecurity "github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/user/adapters/security"
	userdelivery "github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/user/delivery/http"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/user/usecase"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/migrations"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

// newServer собирает приложение; вызывающий закрывает ресурсы после остановки.
func newServer(ctx context.Context, cfg config.Config) (*http.Server, func(), error) {
	dbURL := cfg.PostgresURL()

	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		return nil, nil, startupError("database_connect", fmt.Errorf("unable to connect to database: %w", err))
	}
	client := redis.NewClient(&redis.Options{Addr: net.JoinHostPort(cfg.RedisHost, strconv.Itoa(cfg.RedisPort)), Password: cfg.RedisPassword, DB: cfg.RedisDB,
		DialTimeout: cfg.RedisDialTimeout, ReadTimeout: cfg.RedisOperationTimeout, WriteTimeout: cfg.RedisOperationTimeout, PoolTimeout: cfg.RedisOperationTimeout, ContextTimeoutEnabled: true, MaxRetries: -1})
	closeResources := func() { _ = client.Close(); pool.Close() }
	ready := false
	defer func() {
		if !ready {
			closeResources()
		}
	}()

	if err = pool.Ping(ctx); err != nil {
		return nil, nil, startupError("database_connect", fmt.Errorf("database ping failed: %w", err))
	}
	slog.Info("connected to postgres successfully")

	if err = migrations.Up(ctx, pool); err != nil {
		return nil, nil, startupError("database_migrate", err)
	}

	redisCtx, redisCancel := context.WithTimeout(ctx, cfg.RedisDialTimeout)
	err = client.Ping(redisCtx).Err()
	redisCancel()
	if err != nil {
		return nil, nil, startupError("redis_connect", fmt.Errorf("redis ping: %w", err))
	}
	sessions := userredis.NewStore(client, "", cfg.RedisOperationTimeout)
	tokens := usersecurity.NewAccessToken([]byte(cfg.JWTSecret), cfg.AccessTTL)
	userRepo := postgres.NewUserRepository(pool)
	userUsecase := usecase.NewUserUsecase(userRepo, sessions, tokens, usersecurity.PasswordHasher{}, usersecurity.RefreshTokens{}, cfg.RefreshTTL)

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
	router, err := httptransport.NewRouter(srv, tokens, sessions, cfg.BFFAPIKey)
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
	return httpServer, closeResources, nil
}
