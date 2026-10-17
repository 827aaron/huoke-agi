-- +goose Up
-- +goose StatementBegin
-- growth_message_scores keeps the score the judgement model gave a message a read
-- returned, so a lead saved with that message as evidence takes the model's score.
-- leads gain the contacts found for them beside their platform account.
CREATE TABLE IF NOT EXISTS growth_message_scores (
  id          BIGINT      PRIMARY KEY GENERATED ALWAYS AS IDENTITY,
  user_id     BIGINT      NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  source_id   BIGINT      NOT NULL REFERENCES growth_sources(id) ON DELETE CASCADE,
  message_id  TEXT        NOT NULL,
  score       INTEGER     NOT NULL,
  signals     TEXT        NOT NULL DEFAULT '',
  created_at  TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,

  CONSTRAINT growth_message_scores_unique UNIQUE (user_id, source_id, message_id)
);

ALTER TABLE leads ADD COLUMN IF NOT EXISTS contacts JSONB NOT NULL DEFAULT '[]'::jsonb;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE leads DROP COLUMN IF EXISTS contacts;
DROP TABLE IF EXISTS growth_message_scores;
-- +goose StatementEnd
