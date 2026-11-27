package postgres

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/notebook/domain"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/testutil/pgtest"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TestWorkspaceQualifiedSQL выполняет repository с app-role и search_path=pg_catalog без users grants.
func TestWorkspaceQualifiedSQL(t *testing.T) {
	owner := pgtest.NewDatabase(t, "SERVICE_TEST_DATABASE_URL")
	ctx := context.Background()
	role := fmt.Sprintf("workspace_app_%d", time.Now().UnixNano())
	q := pgx.Identifier{role}.Sanitize()
	if _, err := owner.Exec(ctx, "CREATE ROLE "+q+" LOGIN PASSWORD 'test-role'"); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = owner.Exec(context.Background(), "DROP OWNED BY "+q)
		_, _ = owner.Exec(context.Background(), "DROP ROLE "+q)
	}()
	for _, sql := range []string{"GRANT USAGE ON SCHEMA workspace TO " + q, "GRANT SELECT,INSERT,UPDATE,DELETE ON ALL TABLES IN SCHEMA workspace TO " + q, "REVOKE INSERT,UPDATE,DELETE ON workspace.goose_db_version FROM " + q, "GRANT USAGE ON ALL SEQUENCES IN SCHEMA workspace TO " + q} {
		if _, err := owner.Exec(ctx, sql); err != nil {
			t.Fatal(err)
		}
	}
	config := owner.Config().Copy()
	config.ConnConfig.User = role
	config.ConnConfig.Password = "test-role"
	config.ConnConfig.RuntimeParams["search_path"] = "pg_catalog"
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	repository := NewNotebookRepository(pool)
	created, err := repository.Create(ctx, domain.Notebook{OwnerID: 42, Name: "owned", Document: domain.Document{NBFormat: 4, NBFormatMinor: 5}})
	if err != nil {
		t.Fatal(err)
	}
	found, err := repository.Get(ctx, created.ID, 42)
	if err != nil || found.Name != "owned" {
		t.Fatalf("qualified read: %v", err)
	}
	if _, err = pool.Exec(ctx, "SELECT * FROM identity.users"); err == nil {
		t.Fatal("Workspace can read Identity")
	}
}
