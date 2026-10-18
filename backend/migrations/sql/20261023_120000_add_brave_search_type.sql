-- +goose Up
-- +goose StatementBegin
CREATE TYPE SEARCHENGINE_TYPE_NEW AS ENUM (
  'google',
  'tavily',
  'firecrawl',
  'traversaal',
  'browser',
  'duckduckgo',
  'perplexity',
  'searxng',
  'sploitus',
  'brave'
);

ALTER TABLE searchlogs
    ALTER COLUMN engine TYPE SEARCHENGINE_TYPE_NEW USING engine::text::SEARCHENGINE_TYPE_NEW;

DROP TYPE SEARCHENGINE_TYPE;
ALTER TYPE SEARCHENGINE_TYPE_NEW RENAME TO SEARCHENGINE_TYPE;

ALTER TABLE searchlogs
    ALTER COLUMN engine SET NOT NULL;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
CREATE TYPE SEARCHENGINE_TYPE_NEW AS ENUM (
  'google',
  'tavily',
  'firecrawl',
  'traversaal',
  'browser',
  'duckduckgo',
  'perplexity',
  'searxng',
  'sploitus'
);

-- 'brave' is a link index like 'google', so its rows survive the narrowing cast as that.
UPDATE searchlogs
    SET engine = 'google'
    WHERE engine = 'brave';

ALTER TABLE searchlogs
    ALTER COLUMN engine TYPE SEARCHENGINE_TYPE_NEW USING engine::text::SEARCHENGINE_TYPE_NEW;

DROP TYPE SEARCHENGINE_TYPE;
ALTER TYPE SEARCHENGINE_TYPE_NEW RENAME TO SEARCHENGINE_TYPE;

ALTER TABLE searchlogs
    ALTER COLUMN engine SET NOT NULL;
-- +goose StatementEnd
