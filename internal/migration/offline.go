package migration

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const legacyVersion = 20261122120000

type legacyTable struct {
	service Service
	name    string
}

// legacyTables перечисляет все таблицы legacy версии и владельцев данных.
func legacyTables() []legacyTable {
	return []legacyTable{
		{Identity, "users"}, {Identity, "refresh_sessions"},
		{Workspace, "notebooks"}, {Workspace, "notebook_cells"}, {Workspace, "notebook_cell_data"},
		{Workspace, "notebook_readers"}, {Workspace, "cell_comments"}, {Workspace, "runtime_sessions"}, {Workspace, "runtime_batches"},
		{Workspace, "runtime_executions"}, {Workspace, "runtime_events"}, {Workspace, "runtime_projections"},
		{Environments, "runtime_environments"}, {Environments, "runtime_jobs"}, {Environments, "runtime_pool"},
	}

}

// TransferOptions указывает внешний инвентарь и явно разрешённый режим применения.
type TransferOptions struct {
	Inventory Inventory
	Apply     bool
}

// Report фиксирует исходную версию, counts и подтверждение по идемпотентному checkpoint.
type Report struct {
	SourceVersion  int64            `json:"source_version"`
	TableRows      map[string]int64 `json:"table_rows"`
	Applied        bool             `json:"applied"`
	AlreadyApplied bool             `json:"already_applied"`
}

// Preflight выполняет те же DB-проверки, что перенос, без DDL и записей.
func Preflight(ctx context.Context, pool *pgxpool.Pool, i Inventory) (Report, error) {
	return TransferMonolith(ctx, pool, TransferOptions{Inventory: i})
}

// TransferMonolith перемещает объекты и историю атомарно. Ответ после commit безопасно перечитать.
func TransferMonolith(ctx context.Context, pool *pgxpool.Pool, opts TransferOptions) (Report, error) {
	if err := opts.Inventory.validate(); err != nil {
		return Report{}, err
	}
	body, err := json.Marshal(opts.Inventory)
	if err != nil {
		return Report{}, errors.New("cannot serialize offline inventory")
	}
	sum := sha256.Sum256(body)
	fingerprint := hex.EncodeToString(sum[:])
	tx, err := pool.Begin(ctx)
	if err != nil {
		return Report{}, errors.New("cannot open offline migration transaction")
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('cellestial_split_123'))`); err != nil {
		return Report{}, errors.New("cannot acquire offline migration lock")
	}
	prior, found, err := readCheckpoint(ctx, tx, fingerprint)
	if err != nil || found {
		return prior, err
	}
	report, err := verifySource(ctx, tx, opts.Inventory)
	if err != nil || !opts.Apply {
		return report, err
	}
	if err = moveTables(ctx, tx, opts.Inventory); err != nil {
		return report, err
	}
	report.Applied = true
	encoded, err := json.Marshal(report)
	if err != nil {
		return Report{}, errors.New("cannot encode offline checkpoint")
	}
	if _, err = tx.Exec(ctx, `CREATE TABLE public.cellestial_split_checkpoint
		(inventory_sha256 text PRIMARY KEY, report jsonb NOT NULL, created_at timestamptz NOT NULL DEFAULT now())`); err != nil {
		return Report{}, errors.New("cannot create offline checkpoint")
	}
	if _, err = tx.Exec(ctx, `INSERT INTO public.cellestial_split_checkpoint(inventory_sha256,report) VALUES($1,$2)`, fingerprint, encoded); err != nil {
		return Report{}, errors.New("cannot record offline checkpoint")
	}
	if err = tx.Commit(ctx); err != nil {
		return Report{}, errors.New("offline commit outcome unknown; repeat with the same inventory")
	}
	return report, nil
}

func readCheckpoint(ctx context.Context, tx pgx.Tx, fingerprint string) (Report, bool, error) {
	var exists bool
	if err := tx.QueryRow(ctx, `SELECT to_regclass('public.cellestial_split_checkpoint') IS NOT NULL`).Scan(&exists); err != nil || !exists {
		return Report{}, false, err
	}
	var key string
	var data []byte
	err := tx.QueryRow(ctx, `SELECT inventory_sha256,report FROM public.cellestial_split_checkpoint`).Scan(&key, &data)
	var report Report
	if err != nil || key != fingerprint || json.Unmarshal(data, &report) != nil || !report.Applied || report.SourceVersion != legacyVersion {
		return Report{}, true, errors.New("offline checkpoint is unknown or uses another inventory")
	}
	report.AlreadyApplied = true
	return report, true, nil
}

func verifySource(ctx context.Context, tx pgx.Tx, i Inventory) (Report, error) {
	report := Report{TableRows: map[string]int64{}}
	locks := make([]string, 0, len(legacyTables())+1)
	locks = append(locks, "public.goose_db_version")
	for _, table := range legacyTables() {
		locks = append(locks, quote("public", table.name))
	}
	if _, err := tx.Exec(ctx, "LOCK TABLE "+strings.Join(locks, ",")+" IN ACCESS EXCLUSIVE MODE NOWAIT"); err != nil {
		return report, errors.New("legacy tables missing or still in use")
	}
	if err := tx.QueryRow(ctx, `SELECT COALESCE(max(version_id),0) FROM public.goose_db_version WHERE is_applied`).Scan(&report.SourceVersion); err != nil || report.SourceVersion != legacyVersion {
		return report, errors.New("unsupported monolith schema version")
	}
	for _, svc := range services() {
		var collision bool
		roles := i.Roles[svc]
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname=$1)
		  OR NOT EXISTS(SELECT 1 FROM pg_roles WHERE rolname=$2)
		  OR NOT EXISTS(SELECT 1 FROM pg_roles WHERE rolname=$3)`, string(svc), roles.Owner, roles.App).Scan(&collision); err != nil || collision {
			return report, errors.New("destination schema contains objects or configured roles are missing: " + string(svc))
		}
	}
	if err := verifyDurableBarriers(ctx, tx, i); err != nil {
		return report, err
	}
	for _, table := range legacyTables() {
		var count int64
		if err := tx.QueryRow(ctx, "SELECT count(*) FROM "+quote("public", table.name)).Scan(&count); err != nil {
			return report, errors.New("cannot count legacy table: " + table.name)
		}
		report.TableRows[table.name] = count
	}
	return report, nil
}

func verifyDurableBarriers(ctx context.Context, tx pgx.Tx, inv Inventory) error {
	checks := []struct{ query, reason string }{
		{`SELECT EXISTS(SELECT 1 FROM public.runtime_sessions WHERE lease_until>now())
		 OR EXISTS(SELECT 1 FROM public.runtime_jobs WHERE status='running' AND lease_until>now())`, "legacy workers still hold leases"},
		{`SELECT EXISTS(SELECT 1 FROM public.runtime_executions
		 WHERE COALESCE(data->'Public'->>'state', '') NOT IN ('SUCCEEDED','FAILED','INTERRUPTED','TIMED_OUT','CANCELLED','UNKNOWN','REJECTED'))`,
			"runtime execution queue is not drained"},
		{`SELECT EXISTS(SELECT 1 FROM public.runtime_sessions WHERE state NOT IN ('STOPPED','FAILED')
		 OR COALESCE((data->'public'->>'cleanup_pending')::boolean,false)
		 OR COALESCE((data->>'CreateUnknown')::boolean,false)
		 GROUP BY owner_id HAVING count(*)>1)`, "multiple uncleared sessions must be reconciled"},
	}
	for _, check := range checks {
		var found bool
		if err := tx.QueryRow(ctx, check.query).Scan(&found); err != nil {
			return errors.New("cannot verify legacy barrier")
		}
		if found {
			return errors.New(check.reason)
		}
	}
	actions := map[uuid.UUID]CloudAction{}
	for _, action := range inv.CloudActions {
		actions[action.OperationID] = action
	}
	rows, err := tx.Query(ctx, `SELECT e.operation_id, COALESCE(e.server_id,''), COALESCE(e.volume_id,''), COALESCE(e.port_id,'')
		FROM public.runtime_environments e
		WHERE e.state IN ('CREATING','CONFIGURING','VERIFYING','DELETING','CLEANUP_PENDING')
		   OR EXISTS (SELECT 1 FROM public.runtime_jobs j WHERE j.session_id=e.session_id
		       AND (j.status='running' OR (j.status='queued' AND j.attempt>0)))
		UNION ALL
		SELECT p.id, COALESCE(p.server_id,''), COALESCE(p.volume_id,''), COALESCE(p.port_id,'')
		FROM public.runtime_pool p WHERE p.state IN ('PREPARING','DELETING')`)
	if err != nil {
		return errors.New("cannot load active provider operations")
	}
	for rows.Next() {
		var actual CloudAction
		if err = rows.Scan(&actual.OperationID, &actual.ServerID, &actual.VolumeID, &actual.PortID); err != nil {
			rows.Close()
			return errors.New("cannot read provider inventory")
		}
		proof, ok := actions[actual.OperationID]
		if !ok {
			rows.Close()
			return errors.New("running provider operation lacks a confirmed outcome")
		}
		actual.Outcome = proof.Outcome
		if actual != proof {
			rows.Close()
			return errors.New("confirmed provider resources do not match the reconciled legacy row")
		}
	}
	if err = rows.Err(); err != nil {
		return errors.New("cannot complete provider inventory")
	}
	proofs := map[uuid.UUID]bool{}
	for _, p := range inv.Agents {
		proofs[p.HostID] = p.GRPCReady && p.StatePreserved
	}
	rows, err = tx.Query(ctx, `SELECT DISTINCT COALESCE(pool_id,session_id) FROM public.runtime_environments
		WHERE state='READY' AND (server_id IS NOT NULL OR private_ip IS NOT NULL)`)
	if err != nil {
		return errors.New("cannot load assigned runtime agents")
	}
	defer rows.Close()
	for rows.Next() {
		var id uuid.UUID
		if err = rows.Scan(&id); err != nil || !proofs[id] {
			return errors.New("assigned VPS lacks verified gRPC agent and persisted state")
		}
	}
	if rows.Err() != nil {
		return errors.New("cannot complete agent inventory")
	}
	return nil
}

func moveTables(ctx context.Context, tx pgx.Tx, inventory Inventory) error {
	for _, svc := range services() {
		role := inventory.Roles[svc]
		schema := quote(string(svc))
		if _, err := tx.Exec(ctx, "CREATE SCHEMA IF NOT EXISTS "+schema+" AUTHORIZATION "+quote(role.Owner)); err != nil {
			return errors.New("cannot create target schema: " + string(svc))
		}
		if _, err := tx.Exec(ctx, "ALTER SCHEMA "+schema+" OWNER TO "+quote(role.Owner)); err != nil {
			return errors.New("cannot assign target schema ownership: " + string(svc))
		}
	}
	if err := removeCrossServiceFKs(ctx, tx); err != nil {
		return err
	}
	for _, t := range legacyTables() {
		schema := string(t.service)
		if _, err := tx.Exec(ctx, "ALTER TABLE "+quote("public", t.name)+" SET SCHEMA "+quote(schema)); err != nil {
			return errors.New("cannot move legacy table: " + t.name)
		}
		if _, err := tx.Exec(ctx, "ALTER TABLE "+quote(schema, t.name)+" OWNER TO "+quote(inventory.Roles[t.service].Owner)); err != nil {
			return errors.New("cannot assign legacy table owner: " + t.name)
		}
		if err := removeLegacyGrants(ctx, tx, schema, t.name, inventory.Roles[t.service].Owner); err != nil {
			return err
		}
	}
	for _, svc := range services() {
		if err := baselineAndUpgrade(ctx, tx, svc, inventory.Roles[svc]); err != nil {
			return err
		}
	}
	return nil
}

func removeCrossServiceFKs(ctx context.Context, tx pgx.Tx) error {
	child := []string{"notebooks", "notebook_readers", "cell_comments", "runtime_sessions"}
	rows, err := tx.Query(ctx, `SELECT a.relname,c.conname FROM pg_constraint c
		JOIN pg_class a ON a.oid=c.conrelid
		WHERE c.contype='f' AND c.confrelid='public.users'::regclass AND a.relnamespace='public'::regnamespace AND a.relname=ANY($1)`, child)
	if err != nil {
		return errors.New("cannot inspect legacy cross-service references")
	}
	type fk struct{ table, name string }
	var keys []fk
	for rows.Next() {
		var k fk
		if err = rows.Scan(&k.table, &k.name); err != nil {
			rows.Close()
			return errors.New("cannot read legacy cross-service reference")
		}
		keys = append(keys, k)
	}
	if rows.Err() != nil {
		return errors.New("cannot list legacy cross-service references")
	}
	for _, k := range keys {
		if _, err = tx.Exec(ctx, "ALTER TABLE "+quote("public", k.table)+" DROP CONSTRAINT "+quote(k.name)); err != nil {
			return errors.New("cannot remove legacy cross-service reference")
		}
	}
	return nil
}

func removeLegacyGrants(ctx context.Context, tx pgx.Tx, schema, table, owner string) error {
	rows, err := tx.Query(ctx, `SELECT DISTINCT COALESCE(role.rolname,'PUBLIC'),a.grantee=0 FROM pg_class c
		JOIN pg_namespace n ON n.oid=c.relnamespace CROSS JOIN LATERAL aclexplode(c.relacl) a
		LEFT JOIN pg_roles role ON role.oid=a.grantee WHERE n.nspname=$1 AND c.relname=$2 AND a.grantee<>c.relowner
		UNION
		SELECT DISTINCT COALESCE(role.rolname,'PUBLIC'),a.grantee=0 FROM pg_class c
		JOIN pg_namespace n ON n.oid=c.relnamespace JOIN pg_attribute col ON col.attrelid=c.oid
		CROSS JOIN LATERAL aclexplode(col.attacl) a LEFT JOIN pg_roles role ON role.oid=a.grantee
		WHERE n.nspname=$1 AND c.relname=$2 AND a.grantee<>c.relowner`, schema, table)
	if err != nil {
		return errors.New("cannot inspect legacy grants")
	}
	var grantees []string
	for rows.Next() {
		var name string
		var public bool
		if err = rows.Scan(&name, &public); err != nil {
			rows.Close()
			return errors.New("cannot inspect legacy grants")
		}
		if public {
			grantees = append(grantees, "PUBLIC")
		} else if name != owner {
			grantees = append(grantees, quote(name))
		}
	}
	if rows.Err() != nil {
		return errors.New("cannot inspect legacy grants")
	}
	for _, grantee := range grantees {
		if _, err = tx.Exec(ctx, "REVOKE ALL PRIVILEGES ON TABLE "+quote(schema, table)+" FROM "+grantee+" CASCADE"); err != nil {
			return errors.New("cannot revoke legacy table grants")
		}
	}
	return nil
}

func baselineAndUpgrade(ctx context.Context, tx pgx.Tx, svc Service, roles Roles) error {
	name := string(svc)
	history := quote(name, "goose_db_version")
	if _, err := tx.Exec(ctx, "SET LOCAL ROLE "+quote(roles.Owner)); err != nil {
		return errors.New("cannot assume service migration owner: " + name)
	}
	if _, err := tx.Exec(ctx, `CREATE TABLE `+history+` (id integer PRIMARY KEY GENERATED BY DEFAULT AS IDENTITY,
		version_id bigint NOT NULL,is_applied boolean NOT NULL,tstamp timestamp NOT NULL DEFAULT now())`); err != nil {
		return errors.New("cannot initialize service history: " + name)
	}
	if _, err := tx.Exec(ctx, "INSERT INTO "+history+"(version_id,is_applied) VALUES(0,true),(1,true)"); err != nil {
		return errors.New("cannot record moved baseline: " + name)
	}
	files, _, err := source(svc)
	if err != nil {
		return err
	}
	paths, err := fs.Glob(files, "*.sql")
	if err != nil {
		return err
	}
	slices.Sort(paths)
	for _, path := range paths {
		var version int64
		if _, err = fmt.Sscanf(path, "%d_", &version); err != nil {
			return errors.New("invalid service migration filename")
		}
		if version == 1 {
			continue
		}
		body, readErr := fs.ReadFile(files, path)
		if readErr != nil {
			return readErr
		}
		up, _, ok := strings.Cut(string(body), "-- +goose Down")
		if !ok || strings.Contains(up, "-- +goose NO TRANSACTION") {
			return errors.New("service migration incompatible with atomic transfer")
		}
		if _, err = tx.Exec(ctx, up); err != nil {
			return errors.New("cannot apply service migration: " + name + "/" + path)
		}
		if _, err = tx.Exec(ctx, "INSERT INTO "+history+"(version_id,is_applied) VALUES($1,true)", version); err != nil {
			return errors.New("cannot advance service history: " + name)
		}
	}
	if err = applyGrants(ctx, tx, name, roles); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, "RESET ROLE"); err != nil {
		return errors.New("cannot restore offline migration role")
	}
	return nil
}

func applyGrants(ctx context.Context, tx pgx.Tx, schema string, roles Roles) error {
	name, app := quote(schema), quote(roles.App)
	for _, statement := range []string{
		"REVOKE ALL ON SCHEMA " + name + " FROM PUBLIC",
		"GRANT USAGE ON SCHEMA " + name + " TO " + app,
		"GRANT SELECT,INSERT,UPDATE,DELETE ON ALL TABLES IN SCHEMA " + name + " TO " + app,
		"REVOKE INSERT,UPDATE,DELETE ON " + quote(schema, "goose_db_version") + " FROM " + app,
		"GRANT USAGE ON ALL SEQUENCES IN SCHEMA " + name + " TO " + app,
	} {
		if _, err := tx.Exec(ctx, statement); err != nil {
			return errors.New("cannot assign application privileges: " + schema)
		}
	}
	if schema == "identity" {
		if _, err := tx.Exec(ctx, "REVOKE INSERT,UPDATE,DELETE,TRUNCATE ON identity.onlineconf_admins FROM "+app); err != nil {
			return errors.New("cannot protect OnlineConf role assignments")
		}
		if _, err := tx.Exec(ctx, "REVOKE ALL ON identity.refresh_sessions FROM "+app); err != nil {
			return errors.New("cannot protect legacy refresh table")
		}
	}
	return nil
}

func quote(names ...string) string { return pgx.Identifier(names).Sanitize() }
