-- +goose Up
ALTER TABLE environments.runtime_environments ADD COLUMN workspace_fence bigint NOT NULL DEFAULT 0 CHECK (workspace_fence >= 0);
ALTER TABLE environments.runtime_environments ADD COLUMN agent_fence bigint NOT NULL DEFAULT 1 CHECK (agent_fence > 0);
ALTER TABLE environments.runtime_jobs ADD COLUMN correlation_id varchar(64);

CREATE TABLE environments.runtime_intents (
    operation_id uuid PRIMARY KEY,
    session_id uuid NOT NULL REFERENCES environments.runtime_environments(session_id),
    kind text NOT NULL CHECK (kind IN ('create', 'delete')),
    fingerprint bytea NOT NULL CHECK (octet_length(fingerprint) = 32),
    correlation_id varchar(64) NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE environments.runtime_intents;
ALTER TABLE environments.runtime_jobs DROP COLUMN correlation_id;
ALTER TABLE environments.runtime_environments DROP COLUMN agent_fence;
ALTER TABLE environments.runtime_environments DROP COLUMN workspace_fence;
