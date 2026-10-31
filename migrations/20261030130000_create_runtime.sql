-- +goose Up
-- Окружения (VPS) пользовательских сессий и очередь заданий воркера.
CREATE TABLE runtime_environments (
  session_id uuid PRIMARY KEY, operation_id uuid NOT NULL, language text NOT NULL, profile text NOT NULL,
  state text NOT NULL, desired_state text NOT NULL, server_id text, volume_id text, port_id text,
  private_ip text, endpoint text, agent_version text NOT NULL, delete_reason text,
  token_sealed bytea, host_private_key_sealed bytea, host_public_key text,
  last_error text, cleanup_result text, version bigint NOT NULL DEFAULT 1,
  created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now());
CREATE TABLE runtime_jobs (
  id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  session_id uuid NOT NULL REFERENCES runtime_environments ON DELETE CASCADE,
  kind text NOT NULL, status text NOT NULL DEFAULT 'queued', attempt int NOT NULL DEFAULT 0,
  next_run_at timestamptz NOT NULL DEFAULT now(), lease_owner text, lease_until timestamptz,
  created_at timestamptz NOT NULL DEFAULT now());
CREATE UNIQUE INDEX runtime_jobs_one_active ON runtime_jobs (session_id) WHERE status IN ('queued','running');
CREATE INDEX runtime_jobs_ready ON runtime_jobs (next_run_at) WHERE status IN ('queued','running');

-- +goose Down
DROP TABLE runtime_jobs;
DROP TABLE runtime_environments;
