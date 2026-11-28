package migration

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5/pgxpool"
)

// checkRole запрещает app-role административные/owner права и изменение migration history.
func checkRole(ctx context.Context, pool *pgxpool.Pool, s Service) error {
	schema := string(s)
	var unsafe bool
	if err := pool.QueryRow(ctx, `SELECT rolsuper OR rolcreaterole OR rolcreatedb OR rolreplication OR rolbypassrls OR has_schema_privilege(current_user,$1,'CREATE') OR has_schema_privilege(current_user,'public','CREATE') FROM pg_roles WHERE rolname=current_user`, schema).Scan(&unsafe); err != nil {
		return errors.New("cannot verify application role")
	}
	if unsafe {
		return errors.New("application role must have no administration or schema CREATE privileges")
	}
	var owned bool
	if err := pool.QueryRow(ctx, `SELECT
 EXISTS(SELECT 1 FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname=$1 AND pg_has_role(current_user,c.relowner,'MEMBER'))
 OR EXISTS(SELECT 1 FROM pg_namespace WHERE nspname=$1 AND pg_has_role(current_user,nspowner,'MEMBER'))
 OR EXISTS(SELECT 1 FROM pg_database WHERE datname=current_database() AND pg_has_role(current_user,datdba,'MEMBER'))`, schema).Scan(&owned); err != nil {
		return errors.New("cannot verify service object ownership")
	}
	if owned {
		return errors.New("application role must not own database/schema/tables or belong to owner roles")
	}
	var permitted bool
	if err := pool.QueryRow(ctx, `SELECT has_schema_privilege(current_user,$1,'USAGE')
 AND NOT EXISTS(
 SELECT 1 FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace CROSS JOIN unnest(ARRAY['SELECT','INSERT','UPDATE','DELETE']) p(privilege)
 WHERE n.nspname=$1 AND c.relkind IN ('r','p') AND c.relname NOT IN ('goose_db_version','refresh_sessions')
 AND NOT (n.nspname='identity' AND c.relname='onlineconf_admins') AND NOT has_table_privilege(current_user,c.oid,p.privilege))
 AND NOT EXISTS(SELECT 1 FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname=$1 AND c.relkind='S' AND c.relname NOT LIKE 'goose_db_version%' AND NOT has_sequence_privilege(current_user,c.oid,'USAGE'))`, schema).Scan(&permitted); err != nil || !permitted {
		return errors.New("application role lacks required data privileges")
	}
	if s == Identity {
		var readOnly bool
		if err := pool.QueryRow(ctx, `SELECT has_table_privilege(current_user,'identity.onlineconf_admins','SELECT')
 AND NOT has_table_privilege(current_user,'identity.onlineconf_admins','INSERT,UPDATE,DELETE,TRUNCATE')`).Scan(&readOnly); err != nil || !readOnly {
			return errors.New("application role requires read-only access to OnlineConf role assignments")
		}
	}
	var modifiesHistory bool
	if err := pool.QueryRow(ctx, `SELECT has_table_privilege(current_user,$1,'INSERT,UPDATE,DELETE')`, schema+".goose_db_version").Scan(&modifiesHistory); err != nil {
		return errors.New("cannot verify migration history privileges")
	}
	if modifiesHistory {
		return errors.New("application role must not change migration history")
	}
	return nil
}
