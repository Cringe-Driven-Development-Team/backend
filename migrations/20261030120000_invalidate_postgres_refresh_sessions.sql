-- +goose Up
-- Stop the old API before switching to Redis. No sessions are migrated.
DELETE FROM refresh_sessions;

-- +goose Down
-- Invalidated credentials must never be restored by rollback.
