-- +goose Up
CREATE TABLE environments.runtime_environments (
    session_id uuid NOT NULL,
    operation_id uuid NOT NULL,
    language text NOT NULL,
    profile text NOT NULL,
    state text NOT NULL,
    desired_state text NOT NULL,
    server_id text,
    volume_id text,
    port_id text,
    private_ip text,
    endpoint text,
    agent_version text NOT NULL,
    delete_reason text,
    token_sealed bytea,
    host_private_key_sealed bytea,
    host_public_key text,
    last_error text,
    cleanup_result text,
    version bigint DEFAULT 1 NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    pool_id uuid
);

CREATE TABLE environments.runtime_jobs (
    id bigint NOT NULL,
    session_id uuid NOT NULL,
    kind text NOT NULL,
    status text DEFAULT 'queued'::text NOT NULL,
    attempt integer DEFAULT 0 NOT NULL,
    next_run_at timestamp with time zone DEFAULT now() NOT NULL,
    lease_owner text,
    lease_until timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);

ALTER TABLE environments.runtime_jobs ALTER COLUMN id ADD GENERATED ALWAYS AS IDENTITY (
    SEQUENCE NAME environments.runtime_jobs_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);

CREATE TABLE environments.runtime_pool (
    id uuid NOT NULL,
    state text NOT NULL,
    server_id text,
    volume_id text,
    port_id text,
    private_ip text,
    host_private_key_sealed bytea,
    host_public_key text,
    image_id text NOT NULL,
    agent_version text NOT NULL,
    session_id uuid,
    last_error text,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);

ALTER TABLE ONLY environments.runtime_environments
    ADD CONSTRAINT runtime_environments_pkey PRIMARY KEY (session_id);

ALTER TABLE ONLY environments.runtime_jobs
    ADD CONSTRAINT runtime_jobs_pkey PRIMARY KEY (id);

ALTER TABLE ONLY environments.runtime_pool
    ADD CONSTRAINT runtime_pool_pkey PRIMARY KEY (id);

CREATE UNIQUE INDEX runtime_jobs_one_active ON environments.runtime_jobs USING btree (session_id) WHERE (status = ANY (ARRAY['queued'::text, 'running'::text]));

CREATE INDEX runtime_jobs_ready ON environments.runtime_jobs USING btree (next_run_at) WHERE (status = ANY (ARRAY['queued'::text, 'running'::text]));

CREATE INDEX runtime_pool_ready ON environments.runtime_pool USING btree (created_at) WHERE (state = 'READY'::text);

ALTER TABLE ONLY environments.runtime_jobs
    ADD CONSTRAINT runtime_jobs_session_id_fkey FOREIGN KEY (session_id) REFERENCES environments.runtime_environments(session_id) ON DELETE CASCADE;

-- +goose Down
DROP TABLE environments.runtime_pool, environments.runtime_jobs, environments.runtime_environments CASCADE;
