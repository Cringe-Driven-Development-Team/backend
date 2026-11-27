-- +goose Up
CREATE TABLE workspace.user_cleanup (
    operation_id uuid PRIMARY KEY,
    user_id bigint NOT NULL UNIQUE REFERENCES workspace.user_guards(user_id),
    done boolean NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE workspace.user_cleanup;
