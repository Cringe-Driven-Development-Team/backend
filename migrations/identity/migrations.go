// Package identitymigrations сохраняет отдельную команду миграций Identity поверх общего runner.
package identitymigrations

import (
	"context"

	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/migration"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Up выполняется отдельной командой владельца; API эту функцию не вызывает.
func Up(ctx context.Context, pool *pgxpool.Pool) error {
	return migration.Up(ctx, pool, migration.Identity)
}

// Check отклоняет неподдерживаемую schema и небезопасную роль приложения.
func Check(ctx context.Context, pool *pgxpool.Pool) error {
	return migration.CheckCompatible(ctx, pool, migration.Identity)
}
