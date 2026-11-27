// Migrate применяет отдельную service schema от роли владельца, вне API lifecycle.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"time"

	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/migration"
	"github.com/jackc/pgx/v5/pgxpool"
)

// main пишет безопасный startup outcome без DSN или SQL аргументов.
func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, nil)).With("service", "migrate"))
	if err := run(os.Args[1:]); err != nil {
		slog.Error("migration.failed", "error", err.Error())
		os.Exit(1)
	}
}

// run требует явное имя владельца и bounded up/status, не читая secrets других сервисов.
func run(args []string) error {
	if len(args) > 0 && args[0] == "transfer-monolith" {
		return transfer(args[1:])
	}
	flags := flag.NewFlagSet("migrate", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	name := flags.String("service", "", "identity|workspace|environments")
	if err := flags.Parse(args); err != nil {
		return errors.New("usage: migrate --service identity|workspace|environments up|status")
	}
	s := migration.Service(*name)
	if s != migration.Identity && s != migration.Workspace && s != migration.Environments {
		return errors.New("unknown migration service")
	}
	if flags.NArg() != 1 || (flags.Arg(0) != "up" && flags.Arg(0) != "status") {
		return errors.New("usage: migrate --service <service> up|status")
	}
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		return errors.New("DATABASE_URL is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return errors.New("invalid migration database configuration")
	}
	defer pool.Close()
	if flags.Arg(0) == "up" {
		if err = migration.Up(ctx, pool, s); err != nil {
			return errors.New("service migration failed")
		}
		slog.InfoContext(ctx, "migration.applied", "owner", string(s))
		return nil
	}
	state, err := migration.Status(ctx, pool, s)
	if err != nil {
		return err
	}
	data, err := json.Marshal(state)
	if err != nil {
		return errors.New("cannot encode migration status")
	}
	_, err = fmt.Fprintln(os.Stdout, string(data))
	return err
}
