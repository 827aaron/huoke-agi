-- +goose Up
-- +goose StatementBegin
INSERT INTO privileges (role_id, name) VALUES
  (1, 'growth.admin'),
  (1, 'growth.view'),
  (1, 'growth.edit'),
  (2, 'growth.view'),
  (2, 'growth.edit')
  ON CONFLICT DO NOTHING;

-- One row per platform account a customer authorised. An api_session account keeps the
-- connector's own session blob (gotd session.Storage for Telegram), which never leaves the
-- backend; a browser_session account points at a persistent browser profile an operator
-- logs in by hand, and sits in needs_login until they do.
-- A customer may attach several accounts per platform (one label each); calls go to the
-- healthy account used least recently, and a rate-limited one rests until rate_limit_until.
-- proxy and external_browser_ws are sealed like growth_settings secrets, since both may
-- carry credentials; fingerprint is the browser service's own profile, stored as given.
CREATE TABLE IF NOT EXISTS channel_accounts (
  id                   BIGINT      PRIMARY KEY GENERATED ALWAYS AS IDENTITY,
  user_id              BIGINT      NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  platform             TEXT        NOT NULL,
  kind                 TEXT        NOT NULL DEFAULT 'api_session',
  label                TEXT        NOT NULL DEFAULT 'default',
  external_id          TEXT        NOT NULL DEFAULT '',
  session              BYTEA,
  profile_dir          TEXT        NOT NULL DEFAULT '',
  status               TEXT        NOT NULL DEFAULT 'active',
  last_used_at         TIMESTAMPTZ NULL,
  last_success_at      TIMESTAMPTZ NULL,
  last_rate_limited_at TIMESTAMPTZ NULL,
  rate_limit_until     TIMESTAMPTZ NULL,
  consecutive_errors   INTEGER     NOT NULL DEFAULT 0,
  last_error           TEXT        NOT NULL DEFAULT '',
  proxy                BYTEA       NULL,
  fingerprint          JSONB       NULL,
  external_browser_ws  BYTEA       NULL,
  created_at           TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
  updated_at           TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,

  CONSTRAINT channel_accounts_user_platform_label_unique UNIQUE (user_id, platform, label),
  CONSTRAINT channel_accounts_kind_check CHECK (kind IN ('api_session', 'browser_session')),
  CONSTRAINT channel_accounts_status_check
    CHECK (status IN ('active', 'rate_limited', 'needs_login', 'disabled'))
);

CREATE INDEX IF NOT EXISTS channel_accounts_user_platform_idx ON channel_accounts(user_id, platform);

-- Yield tracking: qualified_leads counts distinct qualified leads with evidence here;
-- empty_streak counts reads in a row that added none (qualified_at_sync is the count at the
-- previous read), and health follows from it, so the next round reads productive sources first.
-- platform 'web' holds public forum threads and pages; their content is read with the
-- browser tool, so a web source never gets an account_sources row.
-- source_key is the dedup key: the platform id once resolved, 'link:<canonical link>' before that,
-- so a group found by search and the same group pasted by hand collapse into one row.
CREATE TABLE IF NOT EXISTS growth_sources (
  id                    BIGINT      PRIMARY KEY GENERATED ALWAYS AS IDENTITY,
  user_id               BIGINT      NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  platform              TEXT        NOT NULL,
  source_key            TEXT        NOT NULL,
  external_id           TEXT        NOT NULL DEFAULT '',
  username              TEXT        NOT NULL DEFAULT '',
  title                 TEXT        NOT NULL DEFAULT '',
  link                  TEXT        NOT NULL DEFAULT '',
  kind                  TEXT        NOT NULL DEFAULT 'unknown',
  member_count          BIGINT      NOT NULL DEFAULT 0,
  language              TEXT        NOT NULL DEFAULT '',
  description           TEXT        NOT NULL DEFAULT '',
  origin                TEXT        NOT NULL,
  query                 TEXT        NOT NULL DEFAULT '',
  status                TEXT        NOT NULL DEFAULT 'active',
  status_reason         TEXT        NOT NULL DEFAULT '',
  qualified_leads       INTEGER     NOT NULL DEFAULT 0,
  qualified_at_sync     INTEGER     NOT NULL DEFAULT 0,
  empty_streak          INTEGER     NOT NULL DEFAULT 0,
  health                TEXT        NOT NULL DEFAULT 'active',
  last_synced_at        TIMESTAMPTZ,
  discovered_from_id    BIGINT      REFERENCES growth_sources(id) ON DELETE SET NULL,
  discovered_flow_id    BIGINT      REFERENCES flows(id) ON DELETE SET NULL,
  discovered_task_id    BIGINT      REFERENCES tasks(id) ON DELETE SET NULL,
  created_at            TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
  updated_at            TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,

  CONSTRAINT growth_sources_user_platform_key_unique UNIQUE (user_id, platform, source_key),
  CONSTRAINT growth_sources_key_not_empty CHECK (length(trim(source_key)) > 0),
  CONSTRAINT growth_sources_kind_check    CHECK (kind IN ('group', 'channel', 'forum', 'unknown')),
  CONSTRAINT growth_sources_origin_check  CHECK (origin IN ('ai', 'manual')),
  CONSTRAINT growth_sources_status_check  CHECK (status IN ('active', 'unresolved', 'invalid')),
  CONSTRAINT growth_sources_health_check  CHECK (health IN ('active', 'stale', 'dead'))
);

CREATE INDEX IF NOT EXISTS growth_sources_user_id_idx ON growth_sources(user_id, updated_at DESC);
CREATE INDEX IF NOT EXISTS growth_sources_user_yield_idx ON growth_sources(user_id, qualified_leads DESC);

-- Per-account view of a source: Telegram access hashes differ per account, so peer data,
-- membership and the read cursor cannot be global.
CREATE TABLE IF NOT EXISTS account_sources (
  id                BIGINT      PRIMARY KEY GENERATED ALWAYS AS IDENTITY,
  account_id        BIGINT      NOT NULL REFERENCES channel_accounts(id) ON DELETE CASCADE,
  source_id         BIGINT      NOT NULL REFERENCES growth_sources(id) ON DELETE CASCADE,
  peer              JSONB,
  join_status       TEXT        NOT NULL DEFAULT 'not_joined',
  join_error        TEXT        NOT NULL DEFAULT '',
  newest_message_id BIGINT      NOT NULL DEFAULT 0,
  oldest_message_id BIGINT      NOT NULL DEFAULT 0,
  history_exhausted BOOLEAN     NOT NULL DEFAULT FALSE,
  messages_read     BIGINT      NOT NULL DEFAULT 0,
  joined_at         TIMESTAMPTZ,
  synced_at         TIMESTAMPTZ,
  created_at        TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
  updated_at        TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,

  CONSTRAINT account_sources_account_source_unique UNIQUE (account_id, source_id),
  CONSTRAINT account_sources_join_status_check
    CHECK (join_status IN ('not_joined', 'pending', 'joined', 'not_joinable'))
);

CREATE INDEX IF NOT EXISTS account_sources_source_id_idx ON account_sources(source_id);

-- A round of lead collection; one per task because every flow input opens a new task.
-- Everything lead-related belongs to the customer (user_id), not to a flow: deleting a flow
-- or task only detaches these rows, so dedup and the delivered mark survive it.
CREATE TABLE IF NOT EXISTS growth_runs (
  id              BIGINT      PRIMARY KEY GENERATED ALWAYS AS IDENTITY,
  user_id         BIGINT      NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  flow_id         BIGINT      REFERENCES flows(id) ON DELETE SET NULL,
  task_id         BIGINT      REFERENCES tasks(id) ON DELETE SET NULL,
  target          INTEGER     NOT NULL DEFAULT 50,
  status          TEXT        NOT NULL DEFAULT 'running',
  delivered_count INTEGER     NOT NULL DEFAULT 0,
  finished_at     TIMESTAMPTZ,
  notify_status   TEXT        NOT NULL DEFAULT '',
  notify_error    TEXT        NOT NULL DEFAULT '',
  notified_at     TIMESTAMPTZ,
  created_at      TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
  updated_at      TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,

  CONSTRAINT growth_runs_task_unique   UNIQUE (task_id),
  CONSTRAINT growth_runs_target_check  CHECK (target > 0),
  CONSTRAINT growth_runs_status_check  CHECK (status IN ('running', 'partial', 'completed')),
  CONSTRAINT growth_runs_notify_check  CHECK (notify_status IN ('', 'sent', 'failed', 'skipped'))
);

CREATE INDEX IF NOT EXISTS growth_runs_user_flow_idx ON growth_runs(user_id, flow_id);

-- identity_kind says how stable platform_user_id is: a platform id, or for web sources a
-- normalised profile URL or 'host:username' standing in until a better identity is known.
-- last_flow_id scopes what a round may deliver: a qualified, undelivered person seen in this flow.
CREATE TABLE IF NOT EXISTS leads (
  id               BIGINT      PRIMARY KEY GENERATED ALWAYS AS IDENTITY,
  user_id          BIGINT      NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  platform         TEXT        NOT NULL,
  platform_user_id TEXT        NOT NULL,
  identity_kind    TEXT        NOT NULL DEFAULT 'platform_id',
  username         TEXT        NOT NULL DEFAULT '',
  display_name     TEXT        NOT NULL DEFAULT '',
  score            INTEGER     NOT NULL DEFAULT 0,
  tier             TEXT        NOT NULL,
  match_reason     TEXT        NOT NULL DEFAULT '',
  first_flow_id    BIGINT      REFERENCES flows(id) ON DELETE SET NULL,
  first_task_id    BIGINT      REFERENCES tasks(id) ON DELETE SET NULL,
  last_flow_id     BIGINT      REFERENCES flows(id) ON DELETE SET NULL,
  last_task_id     BIGINT      REFERENCES tasks(id) ON DELETE SET NULL,
  delivered_at     TIMESTAMPTZ,
  delivered_run_id BIGINT      REFERENCES growth_runs(id) ON DELETE SET NULL,
  created_at       TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
  updated_at       TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,

  CONSTRAINT leads_user_platform_user_unique UNIQUE (user_id, platform, platform_user_id),
  CONSTRAINT leads_platform_user_not_empty CHECK (length(trim(platform_user_id)) > 0),
  CONSTRAINT leads_tier_check CHECK (tier IN ('qualified', 'watchlist')),
  CONSTRAINT leads_identity_kind_check CHECK (identity_kind IN ('platform_id', 'profile_url', 'forum_username'))
);

CREATE INDEX IF NOT EXISTS leads_deliverable_idx ON leads(user_id, last_flow_id, score DESC)
  WHERE tier = 'qualified' AND delivered_at IS NULL;
CREATE INDEX IF NOT EXISTS leads_delivered_run_idx ON leads(delivered_run_id) WHERE delivered_run_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS leads_user_updated_idx ON leads(user_id, updated_at DESC);

CREATE TABLE IF NOT EXISTS lead_evidence (
  id           BIGINT      PRIMARY KEY GENERATED ALWAYS AS IDENTITY,
  lead_id      BIGINT      NOT NULL REFERENCES leads(id) ON DELETE CASCADE,
  source_id    BIGINT      REFERENCES growth_sources(id) ON DELETE SET NULL,
  message_id   TEXT        NOT NULL,
  message_link TEXT        NOT NULL DEFAULT '',
  message_time TIMESTAMPTZ,
  captured_at  TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
  excerpt      TEXT        NOT NULL DEFAULT '',
  match_reason TEXT        NOT NULL DEFAULT '',
  flow_id      BIGINT      REFERENCES flows(id) ON DELETE SET NULL,
  task_id      BIGINT      REFERENCES tasks(id) ON DELETE SET NULL,
  created_at   TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,

  CONSTRAINT lead_evidence_source_message_unique UNIQUE (source_id, message_id),
  CONSTRAINT lead_evidence_message_not_empty CHECK (length(trim(message_id)) > 0)
);

CREATE INDEX IF NOT EXISTS lead_evidence_lead_id_idx ON lead_evidence(lead_id);

CREATE OR REPLACE TRIGGER update_channel_accounts_modified
  BEFORE UPDATE ON channel_accounts
  FOR EACH ROW EXECUTE PROCEDURE update_modified_column();

CREATE OR REPLACE TRIGGER update_growth_sources_modified
  BEFORE UPDATE ON growth_sources
  FOR EACH ROW EXECUTE PROCEDURE update_modified_column();

CREATE OR REPLACE TRIGGER update_account_sources_modified
  BEFORE UPDATE ON account_sources
  FOR EACH ROW EXECUTE PROCEDURE update_modified_column();

CREATE OR REPLACE TRIGGER update_growth_runs_modified
  BEFORE UPDATE ON growth_runs
  FOR EACH ROW EXECUTE PROCEDURE update_modified_column();

CREATE OR REPLACE TRIGGER update_leads_modified
  BEFORE UPDATE ON leads
  FOR EACH ROW EXECUTE PROCEDURE update_modified_column();

-- Credentials a customer saves on the settings page, one row per kind. secret is the
-- AES-GCM sealed JSON of the fields (key from GROWTH_SECRET_KEY); a saved value takes
-- precedence over the matching .env variable for that customer only.
CREATE TABLE IF NOT EXISTS growth_settings (
  id             BIGINT       PRIMARY KEY GENERATED ALWAYS AS IDENTITY,
  user_id        BIGINT       NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  kind           TEXT         NOT NULL,
  provider       TEXT         NOT NULL DEFAULT '',
  secret         BYTEA        NOT NULL,
  status         TEXT         NOT NULL DEFAULT 'untested',
  status_reason  TEXT         NOT NULL DEFAULT '',
  tested_at      TIMESTAMPTZ  NULL,
  created_at     TIMESTAMPTZ  DEFAULT CURRENT_TIMESTAMP,
  updated_at     TIMESTAMPTZ  DEFAULT CURRENT_TIMESTAMP,

  CONSTRAINT growth_settings_user_kind_unique UNIQUE (user_id, kind),
  CONSTRAINT growth_settings_kind_check CHECK (kind IN ('llm', 'telegram', 'youtube', 'web_search', 'email', 'proxy')),
  CONSTRAINT growth_settings_status_check CHECK (status IN ('untested', 'connected', 'invalid'))
);

CREATE OR REPLACE TRIGGER update_growth_settings_modified
  BEFORE UPDATE ON growth_settings
  FOR EACH ROW EXECUTE PROCEDURE update_modified_column();

-- One business profile per customer. Each task's input starts with its compact block,
-- the whole profile is the user resource business_profile.md, and its long lists are
-- the customer's guide documents in the knowledge store.
CREATE TABLE IF NOT EXISTS growth_profiles (
  user_id     BIGINT      PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
  profile     JSONB       NOT NULL,
  created_at  TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
  updated_at  TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
);

CREATE OR REPLACE TRIGGER update_growth_profiles_modified
  BEFORE UPDATE ON growth_profiles
  FOR EACH ROW EXECUTE PROCEDURE update_modified_column();
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS growth_profiles;
DROP TABLE IF EXISTS growth_settings;
DROP TABLE IF EXISTS lead_evidence;
DROP TABLE IF EXISTS leads;
DROP TABLE IF EXISTS growth_runs;
DROP TABLE IF EXISTS account_sources;
DROP TABLE IF EXISTS growth_sources;
DROP TABLE IF EXISTS channel_accounts;

DELETE FROM privileges WHERE name IN ('growth.admin', 'growth.view', 'growth.edit');
-- +goose StatementEnd
