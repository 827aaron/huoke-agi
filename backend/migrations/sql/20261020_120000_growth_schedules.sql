-- +goose Up
-- +goose StatementBegin
-- A schedule starts a new flow with the same input on the chosen days, in the customer's
-- zone, and closes that flow's round at the end time. Dates and times are local wall-clock
-- text, because "02:00 every weekday" must stay 02:00 across a DST change; next_start_at is
-- the next start resolved to UTC.
CREATE TABLE IF NOT EXISTS growth_schedules (
  id             BIGINT      PRIMARY KEY GENERATED ALWAYS AS IDENTITY,
  user_id        BIGINT      NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  flow_id        BIGINT      REFERENCES flows(id) ON DELETE SET NULL,
  input          TEXT        NOT NULL,
  provider_name  TEXT        NOT NULL,
  days           TEXT        NOT NULL,
  weekdays       BIGINT[]    NOT NULL DEFAULT '{}',
  date           TEXT        NOT NULL DEFAULT '',
  start_time     TEXT        NOT NULL,
  end_time       TEXT        NOT NULL,
  timezone       TEXT        NOT NULL,
  enabled        BOOLEAN     NOT NULL DEFAULT TRUE,
  next_start_at  TIMESTAMPTZ,
  last_run_id    BIGINT      REFERENCES growth_runs(id) ON DELETE SET NULL,
  last_error     TEXT        NOT NULL DEFAULT '',
  created_at     TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
  updated_at     TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,

  CONSTRAINT growth_schedules_days_check  CHECK (days IN ('daily', 'weekdays', 'weekly', 'once')),
  CONSTRAINT growth_schedules_start_check CHECK (start_time ~ '^([01][0-9]|2[0-3]):[0-5][0-9]$'),
  CONSTRAINT growth_schedules_end_check   CHECK (end_time ~ '^([01][0-9]|2[0-3]):[0-5][0-9]$'),
  CONSTRAINT growth_schedules_date_check  CHECK (date = '' OR date ~ '^[0-9]{4}-[0-9]{2}-[0-9]{2}$')
);

CREATE INDEX IF NOT EXISTS growth_schedules_user_idx ON growth_schedules(user_id);
CREATE INDEX IF NOT EXISTS growth_schedules_due_idx ON growth_schedules(next_start_at) WHERE enabled;

CREATE OR REPLACE TRIGGER update_growth_schedules_modified
  BEFORE UPDATE ON growth_schedules
  FOR EACH ROW EXECUTE PROCEDURE update_modified_column();

-- A scheduled round is opened with the flow, before its task exists (task_id NULL); the
-- flow's first task adopts it.
ALTER TABLE growth_runs ADD COLUMN IF NOT EXISTS deadline_at TIMESTAMPTZ;
ALTER TABLE growth_runs ADD COLUMN IF NOT EXISTS schedule_id BIGINT REFERENCES growth_schedules(id) ON DELETE SET NULL;

CREATE INDEX IF NOT EXISTS growth_runs_deadline_idx ON growth_runs(deadline_at) WHERE status = 'running' AND deadline_at IS NOT NULL;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS growth_runs_deadline_idx;
ALTER TABLE growth_runs DROP COLUMN IF EXISTS schedule_id;
ALTER TABLE growth_runs DROP COLUMN IF EXISTS deadline_at;
DROP TABLE IF EXISTS growth_schedules;
-- +goose StatementEnd
