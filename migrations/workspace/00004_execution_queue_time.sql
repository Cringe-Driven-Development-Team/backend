-- +goose Up
ALTER TABLE workspace.runtime_executions ADD COLUMN enqueued_at timestamptz NOT NULL DEFAULT now();
CREATE INDEX workspace_execution_queue_age ON workspace.runtime_executions(enqueued_at)
WHERE data->'Public'->>'state' IN ('PENDING','QUEUED','RUNNING');

-- +goose Down
DROP INDEX workspace.workspace_execution_queue_age;
ALTER TABLE workspace.runtime_executions DROP COLUMN enqueued_at;
