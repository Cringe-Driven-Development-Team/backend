-- +goose Up
CREATE TABLE identity.onlineconf_admins (
 user_id bigint PRIMARY KEY REFERENCES identity.users(id) ON DELETE CASCADE
);

-- +goose Down
DROP TABLE identity.onlineconf_admins;
