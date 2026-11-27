package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"os"
	"time"

	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/migration"
	"github.com/jackc/pgx/v5/pgxpool"
)

// transfer принимает только явную dry-run или apply и независимую временную роль.
func transfer(args []string) error {
	flags := flag.NewFlagSet("transfer-monolith", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	file := flags.String("inventory-file", "", "path to verified cutover inventory")
	dry := flags.Bool("dry-run", false, "verify only")
	apply := flags.Bool("apply", false, "move tables after preflight")
	if flags.Parse(args) != nil || flags.NArg() != 0 || *file == "" || *dry == *apply {
		return errors.New("usage: migrate transfer-monolith --dry-run|--apply --inventory-file <file>")
	}
	source, err := os.Open(*file)
	if err != nil {
		return errors.New("cannot open offline inventory")
	}
	defer source.Close()
	const maxInventory = 16 << 20
	stream := &io.LimitedReader{R: source, N: maxInventory + 1}
	decoder := json.NewDecoder(stream)
	decoder.DisallowUnknownFields()
	var inventory migration.Inventory
	if err = decoder.Decode(&inventory); err != nil {
		return errors.New("invalid offline inventory file")
	}
	if err = decoder.Decode(new(any)); !errors.Is(err, io.EOF) || stream.N == 0 {
		return errors.New("invalid offline inventory length")
	}
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		return errors.New("DATABASE_URL is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return errors.New("invalid offline database configuration")
	}
	defer pool.Close()
	report, err := migration.TransferMonolith(ctx, pool, migration.TransferOptions{Inventory: inventory, Apply: *apply})
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(report)
}
