-- +goose Up
CREATE TABLE workspace.user_guards (
 user_id bigint PRIMARY KEY,
 deleting boolean NOT NULL DEFAULT false,
 active_session_id uuid REFERENCES workspace.runtime_sessions(id)
);
CREATE UNIQUE INDEX user_guards_active_session ON workspace.user_guards(active_session_id);

-- +goose StatementBegin
DO $$
BEGIN
 IF EXISTS (
  SELECT owner_id FROM workspace.runtime_sessions
  WHERE state NOT IN ('STOPPED','FAILED')
   OR COALESCE((data->'public'->>'cleanup_pending')::boolean,false)
   OR COALESCE((data->>'CreateUnknown')::boolean,false)
  GROUP BY owner_id HAVING count(*)>1
 ) THEN
  RAISE EXCEPTION 'multiple uncleared sessions require offline reconciliation';
 END IF;
END $$;
-- +goose StatementEnd

INSERT INTO workspace.user_guards(user_id,active_session_id)
SELECT owner_id,id FROM workspace.runtime_sessions
WHERE state NOT IN ('STOPPED','FAILED')
 OR COALESCE((data->'public'->>'cleanup_pending')::boolean,false)
 OR COALESCE((data->>'CreateUnknown')::boolean,false);

-- +goose Down
DROP TABLE workspace.user_guards;
