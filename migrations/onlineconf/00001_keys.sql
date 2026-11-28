-- +goose Up
CREATE TABLE onlineconf.keys (
    name text PRIMARY KEY,
    scope text NOT NULL CHECK (scope IN ('frontend', 'bff', 'identity', 'workspace')),
    type text NOT NULL CHECK (type IN ('config', 'flag')),
    description text NOT NULL,
    data json NOT NULL,
    created_at timestamptz NOT NULL DEFAULT statement_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT statement_timestamp(),
    created_by json NOT NULL,
    updated_by json NOT NULL
);

-- +goose Down
DROP TABLE onlineconf.keys;
