-- +goose Up
ALTER TABLE workspace.notebook_readers ADD COLUMN role text NOT NULL DEFAULT 'viewer'
    CHECK (role IN ('viewer', 'commenter'));
ALTER TABLE workspace.notebooks DROP COLUMN sharing_mode, ADD COLUMN link_token text;
CREATE UNIQUE INDEX notebooks_link_token_idx ON workspace.notebooks (link_token) WHERE link_token IS NOT NULL;

-- +goose Down
ALTER TABLE workspace.notebooks DROP COLUMN link_token, ADD COLUMN sharing_mode text NOT NULL DEFAULT 'private'
    CHECK (sharing_mode IN ('private', 'restricted', 'public'));
UPDATE workspace.notebooks n SET sharing_mode = 'restricted'
WHERE EXISTS (SELECT 1 FROM workspace.notebook_readers nr WHERE nr.notebook_id = n.id);
ALTER TABLE workspace.notebook_readers DROP COLUMN role;
