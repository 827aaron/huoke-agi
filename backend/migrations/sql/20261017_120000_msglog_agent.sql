-- +goose Up
-- +goose StatementBegin
-- The agent whose tool call wrote the message, so the task view can label who did the work.
-- Messages written before this column, and those no agent wrote, stay NULL.
ALTER TABLE msglogs ADD COLUMN IF NOT EXISTS agent MSGCHAIN_TYPE NULL;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE msglogs DROP COLUMN IF EXISTS agent;
-- +goose StatementEnd
