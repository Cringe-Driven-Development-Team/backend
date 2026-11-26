-- +goose Up
CREATE TABLE identity.users (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 login text NOT NULL,
 password_hash text NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now(),
 avatar_key text,
 totp_secret bytea,
 totp_enabled boolean NOT NULL DEFAULT false,
 totp_last_step bigint NOT NULL DEFAULT -1,
 CONSTRAINT users_totp_state CHECK (
  (totp_secret IS NULL AND NOT totp_enabled AND totp_last_step = -1)
  OR (totp_secret IS NOT NULL AND octet_length(totp_secret)>0 AND
   ((NOT totp_enabled AND totp_last_step = -1) OR (totp_enabled AND totp_last_step >= 0)))
 )
);
CREATE UNIQUE INDEX users_login_lower_key ON identity.users(lower(login));
CREATE INDEX users_suggest_prefix_idx ON identity.users(lower(login) COLLATE "C" text_pattern_ops, id);

-- +goose Down
DROP TABLE identity.users;
