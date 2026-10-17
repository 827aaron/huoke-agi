-- +goose Up
-- +goose StatementBegin
-- A round with a deadline is closed once its flow has been stopped at the end time,
-- whatever its status: the agents may have delivered part of it before.
ALTER TABLE growth_runs ADD COLUMN IF NOT EXISTS deadline_closed_at TIMESTAMPTZ;

DROP INDEX IF EXISTS growth_runs_deadline_idx;
CREATE INDEX IF NOT EXISTS growth_runs_deadline_idx ON growth_runs(deadline_at) WHERE deadline_at IS NOT NULL AND deadline_closed_at IS NULL;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS growth_runs_deadline_idx;
ALTER TABLE growth_runs DROP COLUMN IF EXISTS deadline_closed_at;
CREATE INDEX IF NOT EXISTS growth_runs_deadline_idx ON growth_runs(deadline_at) WHERE status = 'running' AND deadline_at IS NOT NULL;
-- +goose StatementEnd
