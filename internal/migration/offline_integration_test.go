package migration

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/migrations"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// legacyDatabase применяет полный legacy DDL в disposable DB и назначает отдельные service roles.
func legacyDatabase(t *testing.T) (*pgxpool.Pool, Inventory) {
	t.Helper()
	db, _ := serviceDatabase(t)
	ctx := t.Context()
	if err := migrations.Up(ctx, db); err != nil {
		t.Fatal(err)
	}
	i := Inventory{Version: 1, SourceSchema: "public", AdmissionStopped: true, QueueDrained: true, ProcessesStopped: true, AutomationStopped: true,
		CloudReconciled: true, BackupVerified: true, Roles: map[Service]Roles{}}
	for _, svc := range services() {
		base := fmt.Sprintf("split_%s_%d", svc, time.Now().UnixNano())
		r := Roles{Owner: base + "_own", App: base + "_app"}
		for _, role := range []string{r.Owner, r.App} {
			name := quote(role)
			if _, err := db.Exec(ctx, "CREATE ROLE "+name); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				_, _ = db.Exec(context.Background(), "DROP OWNED BY "+name)
				_, _ = db.Exec(context.Background(), "DROP ROLE "+name)
			})
		}
		i.Roles[svc] = r
	}
	return db, i
}

// putLegacyFixture хранит Unicode, неизменные ID и terminal-pending слот, ожидающий recovery.
func putLegacyFixture(t *testing.T, db *pgxpool.Pool) (uuid.UUID, uuid.UUID) {
	t.Helper()
	ctx := t.Context()
	commands := []string{
		`INSERT INTO public.users(id,login,password_hash) OVERRIDING SYSTEM VALUE VALUES(25,'author','hash'),(26,'reader','hash')`,
		`SELECT setval('public.users_id_seq',71,true)`,
		`INSERT INTO public.notebooks(id,owner_id,name,revision,sharing_mode) OVERRIDING SYSTEM VALUE
			VALUES (47,25,'Блокнот','old-revision','restricted')`,
		`INSERT INTO public.notebook_readers(notebook_id,user_id) VALUES(47,26)`,
		`INSERT INTO public.notebook_cells(notebook_id,cell_id,position,kind,source,source_version,preview_candidate,edit_bytes,runtime_metadata_overhead,metadata,extra)
			VALUES(47,'source-cell',0,'code','print("тест")','old-source-version',true,23,0,'{}','{}')`,
		`INSERT INTO public.cell_comments(id,notebook_id,cell_id,author_id,text,created_at,updated_at) OVERRIDING SYSTEM VALUE
			VALUES(55,47,'source-cell',26,'Комментарий',now(),now())`,
	}
	for _, sql := range commands {
		if _, err := db.Exec(ctx, sql); err != nil {
			t.Fatal(err)
		}
	}
	session, operation := uuid.New(), uuid.New()
	if _, err := db.Exec(ctx, `INSERT INTO public.runtime_sessions(id,owner_id,notebook_id,request_id,language,state,data)
	VALUES($1,25,47,'runtime-request','python','STOPPED','{"public":{"cleanup_pending":true}}')`, session); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `INSERT INTO public.runtime_environments(session_id,operation_id,language,profile,state,desired_state,agent_version)
		VALUES($1,$2,'python','default','CLEANUP_PENDING','DELETED','legacy')`, session, operation); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `INSERT INTO public.runtime_jobs(session_id,kind,status,lease_until) VALUES($1,'delete','running',now()-interval '1 minute')`, session); err != nil {
		t.Fatal(err)
	}
	return session, operation
}

// TestTransferPreservesData проверяет неизменные relation, данные, последовательность и готовые версии.
func TestTransferPreservesData(t *testing.T) {
	db, i := legacyDatabase(t)
	sid, operation := putLegacyFixture(t, db)
	i.CloudActions = []CloudAction{{OperationID: operation, Outcome: "completed"}}
	ctx := t.Context()
	var oldOID uint32
	if err := db.QueryRow(ctx, `SELECT 'public.notebooks'::regclass::oid`).Scan(&oldOID); err != nil {
		t.Fatal(err)
	}
	pre, err := Preflight(ctx, db, i)
	if err != nil || pre.Applied || pre.TableRows["users"] != 2 || pre.TableRows["cell_comments"] != 1 {
		t.Fatalf("preflight: %+v %v", pre, err)
	}
	report, err := TransferMonolith(ctx, db, TransferOptions{Inventory: i, Apply: true})
	if err != nil || !report.Applied || report.AlreadyApplied {
		t.Fatalf("transfer: %+v %v", report, err)
	}
	// Клиент мог потерять первый ответ после фиксации. Повтор видит сохранённый отчёт.
	repeated, err := TransferMonolith(ctx, db, TransferOptions{Inventory: i, Apply: true})
	if err != nil || !repeated.AlreadyApplied || repeated.TableRows["runtime_jobs"] != 1 {
		t.Fatalf("replay: %+v %v", repeated, err)
	}
	for _, svc := range services() {
		state, statusErr := Status(ctx, db, svc)
		if statusErr != nil || state.Pending {
			t.Fatalf("%s: %+v %v", svc, state, statusErr)
		}
	}
	var newOID uint32
	var rev, source, text string
	var author, sequence int64
	var active uuid.UUID
	if err = db.QueryRow(ctx, `SELECT 'workspace.notebooks'::regclass::oid,n.revision,c.source_version,m.text,m.author_id,
		g.active_session_id,nextval('identity.users_id_seq')
		FROM workspace.notebooks n JOIN workspace.notebook_cells c ON c.notebook_id=n.id
		JOIN workspace.cell_comments m ON m.notebook_id=n.id
		JOIN workspace.user_guards g ON g.user_id=n.owner_id WHERE n.id=47`).Scan(&newOID, &rev, &source, &text, &author, &active, &sequence); err != nil {
		t.Fatal(err)
	}
	if oldOID != newOID || rev != "old-revision" || source != "old-source-version" || text != "Комментарий" || author != 26 || active != sid || sequence != 72 {
		t.Fatalf("unexpected migrated rows: oid=%d rev=%q source=%q text=%q author=%d active=%s seq=%d", newOID, rev, source, text, author, active, sequence)
	}
	var denied bool
	app := i.Roles[Workspace].App
	if err = db.QueryRow(ctx, `SELECT NOT has_table_privilege($1,'identity.users','SELECT') AND NOT has_schema_privilege($1,'environments','USAGE')`, app).Scan(&denied); err != nil || !denied {
		t.Fatalf("cross privileges: %v %v", denied, err)
	}
	if _, err = db.Exec(ctx, `DELETE FROM identity.users WHERE id=26`); err != nil {
		t.Fatal(err)
	}
	var total int
	if err = db.QueryRow(ctx, `SELECT count(*) FROM workspace.cell_comments WHERE author_id=26`).Scan(&total); err != nil || total != 1 {
		t.Fatalf("cross-service cascade still present: %d %v", total, err)
	}
}

// TestPreflightRejectsUnsafeBoundary не считает истёкший lease подтверждением облачного эффекта.
func TestPreflightRejectsUnsafeBoundary(t *testing.T) {
	db, i := legacyDatabase(t)
	_, operation := putLegacyFixture(t, db)
	if _, err := Preflight(t.Context(), db, i); err == nil || !strings.Contains(err.Error(), "confirmed outcome") {
		t.Fatalf("unconfirmed provider outcome accepted: %v", err)
	}
	poolID := uuid.New()
	if _, err := db.Exec(t.Context(), `INSERT INTO public.runtime_pool(id,state,volume_id,image_id,agent_version)
		VALUES($1,'PREPARING','volume-a','test-image','legacy')`, poolID); err != nil {
		t.Fatal(err)
	}
	i.CloudActions = []CloudAction{{OperationID: operation, Outcome: "completed"}}
	if _, err := Preflight(t.Context(), db, i); err == nil || !strings.Contains(err.Error(), "confirmed outcome") {
		t.Fatalf("unconfirmed warm-pool operation accepted: %v", err)
	}
	i.CloudActions = append(i.CloudActions, CloudAction{OperationID: poolID, Outcome: "cancelled", VolumeID: "wrong-volume"})
	if _, err := Preflight(t.Context(), db, i); err == nil || !strings.Contains(err.Error(), "reconciled legacy row") {
		t.Fatalf("wrong resource inventory accepted: %v", err)
	}
	i.CloudActions[1].VolumeID = "volume-a"
	if _, err := Preflight(t.Context(), db, i); err != nil {
		t.Fatalf("reconciled inventory rejected: %v", err)
	}
	i.AutomationStopped = false
	if _, err := Preflight(t.Context(), db, i); err == nil {
		t.Fatal("running automation accepted")
	}
}
