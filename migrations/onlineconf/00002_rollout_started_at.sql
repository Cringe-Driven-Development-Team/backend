-- +goose Up
ALTER TABLE onlineconf.keys ADD COLUMN rollout_started_at timestamptz;
UPDATE onlineconf.keys SET rollout_started_at = statement_timestamp()
WHERE type = 'flag' AND data->>'enabled' = 'true' AND (data->>'rollout_percent')::integer > 0;

-- +goose Down
ALTER TABLE onlineconf.keys DROP COLUMN rollout_started_at;
