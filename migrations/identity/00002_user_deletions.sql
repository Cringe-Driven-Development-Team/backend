-- +goose Up
ALTER TABLE identity.users ADD COLUMN deleting boolean NOT NULL DEFAULT false;

CREATE TABLE identity.user_deletions (
    operation_id uuid PRIMARY KEY,
    user_id bigint UNIQUE NOT NULL,
    stage text NOT NULL CHECK (stage IN ('revoking','workspace','avatar','done')),
    avatar_key text NOT NULL DEFAULT '',
    correlation_id varchar(64) NOT NULL,
    version bigint NOT NULL DEFAULT 0,
    lease_until timestamptz DEFAULT '-infinity' NOT NULL,
    next_run_at timestamptz DEFAULT now() NOT NULL,
    error_code text NOT NULL DEFAULT ''
);
CREATE INDEX user_deletions_ready ON identity.user_deletions(next_run_at) WHERE stage <> 'done';

-- +goose Down
DROP TABLE identity.user_deletions;
ALTER TABLE identity.users DROP COLUMN deleting;
