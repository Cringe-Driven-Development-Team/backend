package migration

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// serviceDatabase создаёт собственную disposable DB, не затрагивая существующие данные.
func serviceDatabase(t *testing.T) (*pgxpool.Pool, string) {
	t.Helper()
	dsn := os.Getenv("SERVICE_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set disposable SERVICE_TEST_DATABASE_URL")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	name := fmt.Sprintf("services_%d", time.Now().UnixNano())
	quoted := pgx.Identifier{name}.Sanitize()
	if _, err = admin.Exec(ctx, "CREATE DATABASE "+quoted); err != nil {
		admin.Close()
		t.Fatal(err)
	}
	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	parsed.Path = "/" + name
	pool, err := pgxpool.New(ctx, parsed.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		pool.Close()
		_, _ = admin.Exec(context.Background(), "DROP DATABASE "+quoted+" WITH (FORCE)")
		admin.Close()
	})
	return pool, parsed.String()
}

func TestUpUsesProvisionedSchemaWithoutDatabaseCreatePrivilege(t *testing.T) {
	admin, dsn := serviceDatabase(t)
	ctx := t.Context()
	role := fmt.Sprintf("migration_owner_%d", time.Now().UnixNano())
	quoted := pgx.Identifier{role}.Sanitize()
	if _, err := admin.Exec(ctx, "CREATE ROLE "+quoted+" LOGIN PASSWORD 'test-only-role'"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = admin.Exec(context.Background(), "DROP OWNED BY "+quoted)
		_, _ = admin.Exec(context.Background(), "DROP ROLE "+quoted)
	})
	if _, err := admin.Exec(ctx, "CREATE SCHEMA identity AUTHORIZATION "+quoted); err != nil {
		t.Fatal(err)
	}
	target, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	target.User = url.UserPassword(role, "test-only-role")
	pool, err := pgxpool.New(ctx, target.String())
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	var canCreate bool
	if err = pool.QueryRow(ctx, "SELECT has_database_privilege(current_database(), 'CREATE')").Scan(&canCreate); err != nil || canCreate {
		t.Fatalf("unexpected database CREATE privilege: %v, %v", canCreate, err)
	}
	if err = Up(ctx, pool, Identity); err != nil {
		t.Fatal(err)
	}
	state, err := Status(ctx, pool, Identity)
	if err != nil || state.Pending {
		t.Fatalf("identity state: %+v, %v", state, err)
	}
}

// TestFreshSchemasAndPrivileges проверяет независимые histories, локальные FK и реальные app-роли.
func TestFreshSchemasAndPrivileges(t *testing.T) {
	pool, dsn := serviceDatabase(t)
	ctx := context.Background()
	for _, s := range []Service{Identity, Workspace, Environments} {
		if err := Up(ctx, pool, s); err != nil {
			t.Fatal(err)
		}
		if err := Up(ctx, pool, s); err != nil {
			t.Fatal(err)
		}
		state, err := Status(ctx, pool, s)
		if err != nil || state.Version != state.Expected || state.Pending {
			t.Fatalf("%s status %+v %v", s, state, err)
		}
	}
	var foreign int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM pg_constraint c JOIN pg_class a ON a.oid=c.conrelid JOIN pg_class b ON b.oid=c.confrelid WHERE c.contype='f' AND a.relnamespace<>b.relnamespace`).Scan(&foreign); err != nil || foreign != 0 {
		t.Fatalf("cross-schema FKs=%d %v", foreign, err)
	}
	for _, s := range []Service{Identity, Workspace, Environments} {
		t.Run(string(s), func(t *testing.T) {
			role := fmt.Sprintf("app_%s_%d", s, time.Now().UnixNano())
			q := pgx.Identifier{role}.Sanitize()
			if _, err := pool.Exec(ctx, "CREATE ROLE "+q+" LOGIN PASSWORD 'test-only-role'"); err != nil {
				t.Fatal(err)
			}
			// Все записи/grants принадлежат этой disposable DB; DROP OWNED выполняется только в ней.
			t.Cleanup(func() {
				_, _ = pool.Exec(context.Background(), "DROP OWNED BY "+q)
				_, _ = pool.Exec(context.Background(), "DROP ROLE "+q)
			})
			schema := string(s)
			for _, sql := range []string{"GRANT USAGE ON SCHEMA " + schema + " TO " + q, "GRANT SELECT,INSERT,UPDATE,DELETE ON ALL TABLES IN SCHEMA " + schema + " TO " + q, "REVOKE INSERT,UPDATE,DELETE ON " + schema + ".goose_db_version FROM " + q, "GRANT USAGE,SELECT ON ALL SEQUENCES IN SCHEMA " + schema + " TO " + q} {
				if _, err := pool.Exec(ctx, sql); err != nil {
					t.Fatal(err)
				}
			}
			if s == Identity {
				if _, err := pool.Exec(ctx, "REVOKE INSERT,UPDATE,DELETE ON identity.onlineconf_admins FROM "+q); err != nil {
					t.Fatal(err)
				}
			}
			parsed, err := url.Parse(dsn)
			if err != nil {
				t.Fatal(err)
			}
			parsed.User = url.UserPassword(role, "test-only-role")
			app, err := pgxpool.New(ctx, parsed.String())
			if err != nil {
				t.Fatal(err)
			}
			defer app.Close()
			if err = CheckCompatible(ctx, app, s); err != nil {
				t.Fatal(err)
			}
			for _, other := range []Service{Identity, Workspace, Environments} {
				if other == s {
					continue
				}
				if _, err = app.Exec(ctx, "SELECT * FROM "+string(other)+".goose_db_version"); err == nil {
					t.Fatal("cross-schema read allowed")
				}
				if _, err = app.Exec(ctx, "CREATE TABLE "+string(other)+".forbidden(id bigint)"); err == nil {
					t.Fatal("cross-schema DDL allowed")
				}
			}
			if _, err = app.Exec(ctx, "CREATE TABLE "+schema+".forbidden(id bigint)"); err == nil {
				t.Fatal("own schema DDL allowed")
			}
			if _, err = app.Exec(ctx, "UPDATE "+schema+".goose_db_version SET is_applied=false"); err == nil {
				t.Fatal("application can change migration history")
			}
		})
	}
}
