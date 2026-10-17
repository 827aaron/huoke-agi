-- +goose Up
-- +goose StatementBegin
-- An account's join pace counts from its first sign-in; account_sources keeps when each
-- join went out and whether it used an invite link, so a day's joins are counted per account.
-- growth_settings gains the screening kind, the Jev pre-screen's key and threshold.
ALTER TABLE channel_accounts ADD COLUMN IF NOT EXISTS first_signed_in_at TIMESTAMPTZ NULL;
ALTER TABLE channel_accounts ADD COLUMN IF NOT EXISTS next_join_at TIMESTAMPTZ NULL;
ALTER TABLE account_sources ADD COLUMN IF NOT EXISTS join_attempted_at TIMESTAMPTZ NULL;
ALTER TABLE account_sources ADD COLUMN IF NOT EXISTS private_join BOOLEAN NOT NULL DEFAULT FALSE;
CREATE INDEX IF NOT EXISTS account_sources_join_attempted_at_idx
  ON account_sources (account_id, join_attempted_at) WHERE join_attempted_at IS NOT NULL;
UPDATE channel_accounts SET first_signed_in_at = created_at
WHERE first_signed_in_at IS NULL AND last_success_at IS NOT NULL;
ALTER TABLE growth_settings DROP CONSTRAINT IF EXISTS growth_settings_kind_check;
ALTER TABLE growth_settings ADD CONSTRAINT growth_settings_kind_check
  CHECK (kind IN ('llm', 'embedding', 'telegram', 'youtube', 'web_search', 'email', 'proxy', 'screening'));
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DELETE FROM growth_settings WHERE kind = 'screening';
ALTER TABLE growth_settings DROP CONSTRAINT IF EXISTS growth_settings_kind_check;
ALTER TABLE growth_settings ADD CONSTRAINT growth_settings_kind_check
  CHECK (kind IN ('llm', 'embedding', 'telegram', 'youtube', 'web_search', 'email', 'proxy'));
DROP INDEX IF EXISTS account_sources_join_attempted_at_idx;
ALTER TABLE account_sources DROP COLUMN IF EXISTS private_join;
ALTER TABLE account_sources DROP COLUMN IF EXISTS join_attempted_at;
ALTER TABLE channel_accounts DROP COLUMN IF EXISTS next_join_at;
ALTER TABLE channel_accounts DROP COLUMN IF EXISTS first_signed_in_at;
-- +goose StatementEnd
