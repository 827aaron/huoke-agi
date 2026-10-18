-- +goose Up
-- +goose StatementBegin
-- A customer keeps several business profiles and marks one the default. A flow records the
-- profile it was started with, so its follow-up tasks and its lead judgement keep using that
-- profile when the default changes; a flow with no record, or whose profile is deleted, uses
-- the default. Message scores are per profile, since a message is judged against one.
ALTER TABLE growth_profiles DROP CONSTRAINT IF EXISTS growth_profiles_pkey;
ALTER TABLE growth_profiles ADD COLUMN IF NOT EXISTS id BIGINT NOT NULL GENERATED ALWAYS AS IDENTITY;
ALTER TABLE growth_profiles ADD PRIMARY KEY (id);
ALTER TABLE growth_profiles ADD COLUMN IF NOT EXISTS title TEXT NOT NULL DEFAULT '';
ALTER TABLE growth_profiles ADD COLUMN IF NOT EXISTS is_default BOOLEAN NOT NULL DEFAULT FALSE;
UPDATE growth_profiles SET is_default = TRUE, title = COALESCE(profile->>'name', '');

CREATE INDEX IF NOT EXISTS growth_profiles_user_idx ON growth_profiles(user_id);
-- Deferred, so one statement can move the default from one profile to another.
ALTER TABLE growth_profiles ADD CONSTRAINT growth_profiles_one_default
  EXCLUDE USING btree (user_id WITH =) WHERE (is_default) DEFERRABLE INITIALLY DEFERRED;

CREATE TABLE IF NOT EXISTS growth_flow_profiles (
  flow_id     BIGINT      PRIMARY KEY REFERENCES flows(id) ON DELETE CASCADE,
  profile_id  BIGINT      REFERENCES growth_profiles(id) ON DELETE SET NULL,
  title       TEXT        NOT NULL DEFAULT '',
  created_at  TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
);

-- The profile a flow's assistant drafted and the customer has not saved yet, one per flow.
-- base_profile_id is the saved profile the draft changes; NULL drafts a new one.
CREATE TABLE IF NOT EXISTS growth_profile_drafts (
  flow_id          BIGINT      PRIMARY KEY REFERENCES flows(id) ON DELETE CASCADE,
  user_id          BIGINT      NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  base_profile_id  BIGINT      REFERENCES growth_profiles(id) ON DELETE SET NULL,
  title            TEXT        NOT NULL DEFAULT '',
  profile          JSONB       NOT NULL,
  created_at       TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
  updated_at       TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
);

CREATE OR REPLACE TRIGGER update_growth_profile_drafts_modified
  BEFORE UPDATE ON growth_profile_drafts
  FOR EACH ROW EXECUTE PROCEDURE update_modified_column();

-- A lead's flags mark what its score does not show, such as matching the profile's
-- unwanted list.
ALTER TABLE leads ADD COLUMN IF NOT EXISTS flags TEXT[] NOT NULL DEFAULT '{}';

ALTER TABLE growth_schedules ADD COLUMN IF NOT EXISTS profile_id BIGINT REFERENCES growth_profiles(id) ON DELETE SET NULL;

-- profile_id 0 is the built-in default profile of a customer who saved none.
ALTER TABLE growth_message_scores ADD COLUMN IF NOT EXISTS profile_id BIGINT NOT NULL DEFAULT 0;
UPDATE growth_message_scores ms SET profile_id = gp.id FROM growth_profiles gp WHERE gp.user_id = ms.user_id;
ALTER TABLE growth_message_scores DROP CONSTRAINT IF EXISTS growth_message_scores_unique;
ALTER TABLE growth_message_scores ADD CONSTRAINT growth_message_scores_unique UNIQUE (user_id, profile_id, source_id, message_id);

-- The profile file is now written per flow from the flow's profile; the copy kept among the
-- customer's resources would shadow it.
DELETE FROM user_resources ur USING growth_profiles gp
WHERE ur.user_id = gp.user_id AND ur.path = 'business_profile.md';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE growth_message_scores DROP CONSTRAINT IF EXISTS growth_message_scores_unique;
DELETE FROM growth_message_scores
WHERE user_id IN (SELECT user_id FROM growth_profiles)
  AND profile_id NOT IN (SELECT id FROM growth_profiles WHERE is_default);
ALTER TABLE growth_message_scores DROP COLUMN IF EXISTS profile_id;
ALTER TABLE growth_message_scores ADD CONSTRAINT growth_message_scores_unique UNIQUE (user_id, source_id, message_id);
ALTER TABLE growth_schedules DROP COLUMN IF EXISTS profile_id;
ALTER TABLE leads DROP COLUMN IF EXISTS flags;
DROP TABLE IF EXISTS growth_profile_drafts;
DROP TABLE IF EXISTS growth_flow_profiles;
DELETE FROM growth_profiles WHERE NOT is_default;
ALTER TABLE growth_profiles DROP CONSTRAINT IF EXISTS growth_profiles_one_default;
DROP INDEX IF EXISTS growth_profiles_user_idx;
ALTER TABLE growth_profiles DROP CONSTRAINT IF EXISTS growth_profiles_pkey;
ALTER TABLE growth_profiles DROP COLUMN IF EXISTS is_default;
ALTER TABLE growth_profiles DROP COLUMN IF EXISTS title;
ALTER TABLE growth_profiles DROP COLUMN IF EXISTS id;
ALTER TABLE growth_profiles ADD PRIMARY KEY (user_id);
-- +goose StatementEnd
