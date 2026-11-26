// Identity-migrate запускает миграции от роли владельца, отдельно от API.
package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"time"

	identitymigrations "github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/migrations/identity"
	"github.com/jackc/pgx/v5/pgxpool"
)

// main принимает DATABASE_URL только через окружение и скрывает ошибки подключения.
func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)).With("service", "identity-migrate"))
	if err := run(); err != nil {
		slog.Error("identity.migration", "error", err.Error())
		os.Exit(1)
	}
}

// run закрывает migration pool до возврата ошибки в main.
func run() error {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		return errors.New("DATABASE_URL is required")
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return errors.New("invalid migration database configuration")
	}
	defer pool.Close()
	if err = identitymigrations.Up(ctx, pool); err != nil {
		return errors.New("identity migration failed")
	}
	slog.Info("identity schema migrated")
	return nil
}
