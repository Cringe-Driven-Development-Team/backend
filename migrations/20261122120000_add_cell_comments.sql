-- +goose Up
CREATE TABLE cell_comments (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 notebook_id bigint NOT NULL,
 cell_id text NOT NULL,
 author_id bigint NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 text text NOT NULL CHECK (char_length(text) BETWEEN 1 AND 5000),
 created_at timestamptz NOT NULL,
 updated_at timestamptz NOT NULL CHECK (updated_at >= created_at),
 FOREIGN KEY (notebook_id,cell_id) REFERENCES notebook_cells(notebook_id,cell_id) ON DELETE CASCADE
);
CREATE INDEX cell_comments_wall_idx ON cell_comments(notebook_id,cell_id,created_at DESC,id DESC);
CREATE INDEX cell_comments_author_idx ON cell_comments(author_id);

-- +goose Down
DROP TABLE cell_comments;
