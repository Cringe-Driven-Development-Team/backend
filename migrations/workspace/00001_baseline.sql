-- +goose Up
CREATE TABLE workspace.cell_comments (
    id bigint NOT NULL,
    notebook_id bigint NOT NULL,
    cell_id text NOT NULL,
    author_id bigint NOT NULL,
    text text NOT NULL,
    created_at timestamp with time zone NOT NULL,
    updated_at timestamp with time zone NOT NULL,
    CONSTRAINT cell_comments_check CHECK ((updated_at >= created_at)),
    CONSTRAINT cell_comments_text_check CHECK (((char_length(text) >= 1) AND (char_length(text) <= 5000)))
);

ALTER TABLE workspace.cell_comments ALTER COLUMN id ADD GENERATED ALWAYS AS IDENTITY (
    SEQUENCE NAME workspace.cell_comments_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);

CREATE TABLE workspace.notebook_cell_data (
    notebook_id bigint NOT NULL,
    cell_id text NOT NULL,
    output_truncated boolean DEFAULT false NOT NULL,
    has_runtime_metadata boolean DEFAULT false NOT NULL,
    runtime_metadata_bytes bigint DEFAULT 0 NOT NULL,
    outputs jsonb,
    execution_count bigint,
    has_execution_count boolean DEFAULT false NOT NULL,
    attachments jsonb,
    outputs_bytes bigint DEFAULT 0 NOT NULL,
    execution_bytes bigint DEFAULT 0 NOT NULL,
    attachments_bytes bigint DEFAULT 0 NOT NULL,
    CONSTRAINT notebook_cell_data_attachments_bytes_check CHECK ((attachments_bytes >= 0)),
    CONSTRAINT notebook_cell_data_execution_bytes_check CHECK ((execution_bytes >= 0)),
    CONSTRAINT notebook_cell_data_outputs_bytes_check CHECK ((outputs_bytes >= 0)),
    CONSTRAINT notebook_cell_data_outputs_check CHECK (((outputs IS NULL) OR (jsonb_typeof(outputs) = 'array'::text))),
    CONSTRAINT notebook_cell_data_runtime_metadata_bytes_check CHECK ((runtime_metadata_bytes >= 0))
);

CREATE TABLE workspace.notebook_cells (
    notebook_id bigint NOT NULL,
    cell_id text NOT NULL,
    "position" integer NOT NULL,
    kind text NOT NULL,
    source text NOT NULL,
    source_version text NOT NULL,
    preview_candidate boolean NOT NULL,
    metadata jsonb,
    extra jsonb DEFAULT '{}'::jsonb NOT NULL,
    edit_bytes bigint NOT NULL,
    runtime_metadata_overhead bigint NOT NULL,
    CONSTRAINT notebook_cells_edit_bytes_check CHECK ((edit_bytes >= 0)),
    CONSTRAINT notebook_cells_kind_check CHECK ((kind = ANY (ARRAY['code'::text, 'markdown'::text, 'raw'::text]))),
    CONSTRAINT notebook_cells_position_check CHECK (("position" >= 0)),
    CONSTRAINT notebook_cells_runtime_metadata_overhead_check CHECK ((runtime_metadata_overhead >= '-1'::integer)),
    CONSTRAINT notebook_cells_source_check CHECK ((octet_length(source) <= 65536))
);

CREATE TABLE workspace.notebook_readers (
    notebook_id bigint NOT NULL,
    user_id bigint NOT NULL
);

CREATE TABLE workspace.notebooks (
    id bigint NOT NULL,
    owner_id bigint NOT NULL,
    name text NOT NULL,
    cells_count integer DEFAULT 0 NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    revision text,
    preview_code text DEFAULT ''::text NOT NULL,
    size_bytes bigint DEFAULT 0 NOT NULL,
    header_bytes bigint DEFAULT 0 NOT NULL,
    nbformat integer DEFAULT 4 NOT NULL,
    nbformat_minor integer DEFAULT 5 NOT NULL,
    metadata jsonb,
    extra jsonb DEFAULT '{}'::jsonb NOT NULL,
    sharing_mode text DEFAULT 'private'::text NOT NULL,
    CONSTRAINT notebooks_cells_count_check CHECK (((cells_count >= 0) AND (cells_count <= 1000))),
    CONSTRAINT notebooks_header_bytes_check CHECK ((header_bytes >= 0)),
    CONSTRAINT notebooks_sharing_mode_check CHECK ((sharing_mode = ANY (ARRAY['private'::text, 'restricted'::text, 'public'::text]))),
    CONSTRAINT notebooks_size_bytes_check CHECK (((size_bytes >= 0) AND (size_bytes <= 134217728)))
);

ALTER TABLE workspace.notebooks ALTER COLUMN id ADD GENERATED ALWAYS AS IDENTITY (
    SEQUENCE NAME workspace.notebooks_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);

CREATE TABLE workspace.runtime_batches (
    session_id uuid NOT NULL,
    request_id text NOT NULL,
    request jsonb NOT NULL,
    result jsonb NOT NULL
);

CREATE TABLE workspace.runtime_events (
    session_id uuid NOT NULL,
    public_seq bigint NOT NULL,
    agent_seq bigint,
    kind text NOT NULL,
    body bytea NOT NULL
);

CREATE TABLE workspace.runtime_executions (
    admission_order bigint NOT NULL,
    session_id uuid NOT NULL,
    id text NOT NULL,
    notebook_id bigint NOT NULL,
    cell_id text NOT NULL,
    attempt bigint NOT NULL,
    data jsonb NOT NULL
);

CREATE SEQUENCE workspace.runtime_executions_admission_order_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

ALTER SEQUENCE workspace.runtime_executions_admission_order_seq OWNED BY workspace.runtime_executions.admission_order;

CREATE TABLE workspace.runtime_projections (
    next_attempt timestamp with time zone DEFAULT now() NOT NULL,
    session_id uuid NOT NULL,
    execution_id text NOT NULL,
    version bigint NOT NULL
);

CREATE TABLE workspace.runtime_sessions (
    id uuid NOT NULL,
    owner_id bigint NOT NULL,
    notebook_id bigint NOT NULL,
    request_id text NOT NULL,
    language text NOT NULL,
    state text NOT NULL,
    data jsonb NOT NULL,
    lease_owner text DEFAULT ''::text NOT NULL,
    lease_version bigint DEFAULT 0 NOT NULL,
    lease_until timestamp with time zone DEFAULT '-infinity'::timestamp with time zone NOT NULL,
    next_check timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT runtime_sessions_language_check CHECK ((language = 'python'::text))
);

ALTER TABLE ONLY workspace.runtime_executions ALTER COLUMN admission_order SET DEFAULT nextval('workspace.runtime_executions_admission_order_seq'::regclass);

ALTER TABLE ONLY workspace.cell_comments
    ADD CONSTRAINT cell_comments_pkey PRIMARY KEY (id);

ALTER TABLE ONLY workspace.notebook_cell_data
    ADD CONSTRAINT notebook_cell_data_pkey PRIMARY KEY (notebook_id, cell_id);

ALTER TABLE ONLY workspace.notebook_cells
    ADD CONSTRAINT notebook_cells_pkey PRIMARY KEY (notebook_id, cell_id);

ALTER TABLE ONLY workspace.notebook_cells
    ADD CONSTRAINT notebook_cells_position_key UNIQUE (notebook_id, "position") DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE ONLY workspace.notebook_readers
    ADD CONSTRAINT notebook_readers_pkey PRIMARY KEY (notebook_id, user_id);

ALTER TABLE ONLY workspace.notebooks
    ADD CONSTRAINT notebooks_pkey PRIMARY KEY (id);

ALTER TABLE ONLY workspace.runtime_batches
    ADD CONSTRAINT runtime_batches_pkey PRIMARY KEY (session_id, request_id);

ALTER TABLE ONLY workspace.runtime_events
    ADD CONSTRAINT runtime_events_pkey PRIMARY KEY (session_id, public_seq);

ALTER TABLE ONLY workspace.runtime_events
    ADD CONSTRAINT runtime_events_session_id_agent_seq_key UNIQUE (session_id, agent_seq);

ALTER TABLE ONLY workspace.runtime_executions
    ADD CONSTRAINT runtime_executions_notebook_id_cell_id_attempt_key UNIQUE (notebook_id, cell_id, attempt);

ALTER TABLE ONLY workspace.runtime_executions
    ADD CONSTRAINT runtime_executions_pkey PRIMARY KEY (session_id, id);

ALTER TABLE ONLY workspace.runtime_projections
    ADD CONSTRAINT runtime_projections_pkey PRIMARY KEY (session_id, execution_id);

ALTER TABLE ONLY workspace.runtime_sessions
    ADD CONSTRAINT runtime_sessions_owner_id_request_id_key UNIQUE (owner_id, request_id);

ALTER TABLE ONLY workspace.runtime_sessions
    ADD CONSTRAINT runtime_sessions_pkey PRIMARY KEY (id);

CREATE INDEX cell_comments_author_idx ON workspace.cell_comments USING btree (author_id);

CREATE INDEX cell_comments_wall_idx ON workspace.cell_comments USING btree (notebook_id, cell_id, created_at DESC, id DESC);

CREATE INDEX notebook_cells_preview_idx ON workspace.notebook_cells USING btree (notebook_id, "position") WHERE ((kind = 'code'::text) AND preview_candidate);

CREATE INDEX notebook_readers_user_id_idx ON workspace.notebook_readers USING btree (user_id);

CREATE INDEX notebooks_owner_id_updated_at_idx ON workspace.notebooks USING btree (owner_id, updated_at DESC, id DESC);

CREATE UNIQUE INDEX runtime_active_owner ON workspace.runtime_sessions USING btree (owner_id) WHERE (state <> ALL (ARRAY['STOPPED'::text, 'FAILED'::text]));

CREATE INDEX runtime_display_ids ON workspace.runtime_executions USING gin (((data -> 'DisplayIDs'::text)));

CREATE INDEX runtime_pending_command ON workspace.runtime_executions USING btree (session_id, admission_order) WHERE ((data ->> 'Dispatch'::text) = 'PENDING'::text);

CREATE INDEX runtime_unfinished_command ON workspace.runtime_executions USING btree (session_id, admission_order) WHERE (((data -> 'Public'::text) ->> 'state'::text) <> ALL (ARRAY['SUCCEEDED'::text, 'FAILED'::text, 'INTERRUPTED'::text, 'TIMED_OUT'::text, 'CANCELLED'::text, 'UNKNOWN'::text, 'REJECTED'::text]));

ALTER TABLE ONLY workspace.cell_comments
    ADD CONSTRAINT cell_comments_notebook_id_cell_id_fkey FOREIGN KEY (notebook_id, cell_id) REFERENCES workspace.notebook_cells(notebook_id, cell_id) ON DELETE CASCADE;

ALTER TABLE ONLY workspace.notebook_cell_data
    ADD CONSTRAINT notebook_cell_data_notebook_id_cell_id_fkey FOREIGN KEY (notebook_id, cell_id) REFERENCES workspace.notebook_cells(notebook_id, cell_id) ON DELETE CASCADE;

ALTER TABLE ONLY workspace.notebook_cells
    ADD CONSTRAINT notebook_cells_notebook_id_fkey FOREIGN KEY (notebook_id) REFERENCES workspace.notebooks(id) ON DELETE CASCADE;

ALTER TABLE ONLY workspace.notebook_readers
    ADD CONSTRAINT notebook_readers_notebook_id_fkey FOREIGN KEY (notebook_id) REFERENCES workspace.notebooks(id) ON DELETE CASCADE;

ALTER TABLE ONLY workspace.runtime_batches
    ADD CONSTRAINT runtime_batches_session_id_fkey FOREIGN KEY (session_id) REFERENCES workspace.runtime_sessions(id);

ALTER TABLE ONLY workspace.runtime_events
    ADD CONSTRAINT runtime_events_session_id_fkey FOREIGN KEY (session_id) REFERENCES workspace.runtime_sessions(id);

ALTER TABLE ONLY workspace.runtime_executions
    ADD CONSTRAINT runtime_executions_notebook_id_fkey FOREIGN KEY (notebook_id) REFERENCES workspace.notebooks(id);

ALTER TABLE ONLY workspace.runtime_executions
    ADD CONSTRAINT runtime_executions_session_id_fkey FOREIGN KEY (session_id) REFERENCES workspace.runtime_sessions(id);

ALTER TABLE ONLY workspace.runtime_projections
    ADD CONSTRAINT runtime_projections_session_id_execution_id_fkey FOREIGN KEY (session_id, execution_id) REFERENCES workspace.runtime_executions(session_id, id);

ALTER TABLE ONLY workspace.runtime_sessions
    ADD CONSTRAINT runtime_sessions_notebook_id_fkey FOREIGN KEY (notebook_id) REFERENCES workspace.notebooks(id);

-- +goose Down
DROP TABLE workspace.runtime_sessions, workspace.runtime_projections, workspace.runtime_executions, workspace.runtime_events, workspace.runtime_batches, workspace.notebooks, workspace.notebook_readers, workspace.notebook_cells, workspace.notebook_cell_data, workspace.cell_comments CASCADE;
