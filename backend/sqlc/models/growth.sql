-- name: UpsertChannelAccount :one
INSERT INTO channel_accounts (user_id, platform, kind, label, external_id, profile_dir, status)
VALUES (
  sqlc.arg(user_id), sqlc.arg(platform), sqlc.arg(kind), sqlc.arg(label), sqlc.arg(external_id),
  sqlc.arg(profile_dir), sqlc.arg(status)
)
ON CONFLICT (user_id, platform, label) DO UPDATE
SET kind        = EXCLUDED.kind,
    external_id = COALESCE(NULLIF(EXCLUDED.external_id, ''), channel_accounts.external_id),
    profile_dir = COALESCE(NULLIF(EXCLUDED.profile_dir, ''), channel_accounts.profile_dir),
    status      = EXCLUDED.status
RETURNING *;

-- name: GetChannelAccount :one
SELECT * FROM channel_accounts WHERE id = sqlc.arg(id);

-- name: GetUserChannelAccounts :many
SELECT * FROM channel_accounts WHERE user_id = sqlc.arg(user_id) ORDER BY id;

-- name: GetUsableChannelAccounts :many
-- Accounts that may take a call now or after their rate limit ends; joined_source tells
-- whether the account already joined source_id (0 matches none). The join columns count
-- the joins of the last 24 hours, all and through invite links.
SELECT ca.*, COALESCE(acs.join_status = 'joined', false)::boolean AS joined_source,
  j.joins_24h, j.private_joins_24h
FROM channel_accounts ca
CROSS JOIN LATERAL (
  SELECT count(*)::bigint AS joins_24h,
    count(*) FILTER (WHERE a.private_join)::bigint AS private_joins_24h
  FROM account_sources a
  WHERE a.account_id = ca.id AND a.join_attempted_at > now() - interval '24 hours'
) j
LEFT JOIN account_sources acs ON acs.account_id = ca.id AND acs.source_id = sqlc.arg(source_id)
WHERE ca.user_id = sqlc.arg(user_id) AND ca.platform = sqlc.arg(platform)
  AND ca.status IN ('active', 'rate_limited')
ORDER BY ca.id;

-- name: MarkChannelAccountUsed :exec
UPDATE channel_accounts SET last_used_at = now() WHERE id = sqlc.arg(id);

-- name: RecordChannelAccountSuccess :exec
UPDATE channel_accounts
SET last_success_at = now(),
    first_signed_in_at = COALESCE(first_signed_in_at, now()),
    consecutive_errors = 0,
    last_error = '',
    rate_limit_until = NULL,
    status = CASE WHEN status = 'rate_limited' THEN 'active' ELSE status END
WHERE id = sqlc.arg(id);

-- name: RecordChannelAccountRateLimit :exec
UPDATE channel_accounts
SET last_rate_limited_at = now(),
    rate_limit_until = GREATEST(COALESCE(rate_limit_until, now()), sqlc.arg(until)::timestamptz),
    status = CASE WHEN status = 'active' THEN 'rate_limited' ELSE status END
WHERE id = sqlc.arg(id);

-- name: RecordChannelAccountError :exec
UPDATE channel_accounts
SET consecutive_errors = consecutive_errors + 1, last_error = sqlc.arg(last_error)
WHERE id = sqlc.arg(id);

-- name: GetChannelAccountSession :one
SELECT session FROM channel_accounts WHERE id = sqlc.arg(id);

-- name: UpdateChannelAccountSession :exec
UPDATE channel_accounts SET session = sqlc.arg(session) WHERE id = sqlc.arg(id);

-- name: MarkChannelAccountSignedIn :one
UPDATE channel_accounts
SET status = 'active', first_signed_in_at = COALESCE(first_signed_in_at, now())
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: ScheduleChannelAccountJoin :exec
UPDATE channel_accounts SET next_join_at = sqlc.arg(next_join_at) WHERE id = sqlc.arg(id);

-- name: GetUserAccountJoinCounts :many
-- Per account: the sources joined or awaiting approval, and the joins of the last 24 hours,
-- all and through invite links.
SELECT ca.id AS account_id, ca.platform, ca.label,
  count(acs.id) FILTER (WHERE acs.join_status IN ('joined', 'pending'))::bigint AS joined,
  count(acs.id) FILTER (WHERE acs.join_attempted_at > now() - interval '24 hours')::bigint AS joins_24h,
  count(acs.id) FILTER (WHERE acs.join_attempted_at > now() - interval '24 hours' AND acs.private_join)::bigint
    AS private_joins_24h
FROM channel_accounts ca
LEFT JOIN account_sources acs ON acs.account_id = ca.id
WHERE ca.user_id = sqlc.arg(user_id)
GROUP BY ca.id
ORDER BY ca.id;

-- name: UpdateChannelAccountStatus :one
UPDATE channel_accounts SET status = sqlc.arg(status) WHERE id = sqlc.arg(id) RETURNING *;

-- name: GetUserChannelAccount :one
SELECT * FROM channel_accounts WHERE id = sqlc.arg(id) AND user_id = sqlc.arg(user_id);

-- name: UpdateUserChannelAccount :one
UPDATE channel_accounts
SET label               = sqlc.arg(label),
    status              = sqlc.arg(status),
    proxy               = sqlc.narg(proxy),
    fingerprint         = sqlc.narg(fingerprint),
    external_browser_ws = sqlc.narg(external_browser_ws)
WHERE id = sqlc.arg(id) AND user_id = sqlc.arg(user_id)
RETURNING *;

-- name: DeleteUserChannelAccount :execrows
DELETE FROM channel_accounts WHERE id = sqlc.arg(id) AND user_id = sqlc.arg(user_id);

-- name: UpsertGrowthSource :one
INSERT INTO growth_sources (
  user_id, platform, source_key, external_id, username, title, link, kind, member_count,
  language, description, origin, query, status, status_reason, discovered_from_id,
  discovered_flow_id, discovered_task_id
) VALUES (
  sqlc.arg(user_id), sqlc.arg(platform), sqlc.arg(source_key), sqlc.arg(external_id), sqlc.arg(username),
  sqlc.arg(title), sqlc.arg(link), sqlc.arg(kind), sqlc.arg(member_count), sqlc.arg(language),
  sqlc.arg(description), sqlc.arg(origin), sqlc.arg(query), sqlc.arg(status), sqlc.arg(status_reason),
  sqlc.narg(discovered_from_id), sqlc.narg(discovered_flow_id), sqlc.narg(discovered_task_id)
)
ON CONFLICT (user_id, platform, source_key) DO UPDATE
SET external_id  = COALESCE(NULLIF(EXCLUDED.external_id, ''), growth_sources.external_id),
    username     = COALESCE(NULLIF(EXCLUDED.username, ''), growth_sources.username),
    title        = COALESCE(NULLIF(EXCLUDED.title, ''), growth_sources.title),
    link         = COALESCE(NULLIF(EXCLUDED.link, ''), growth_sources.link),
    kind         = CASE WHEN EXCLUDED.kind = 'unknown' THEN growth_sources.kind ELSE EXCLUDED.kind END,
    member_count = GREATEST(EXCLUDED.member_count, growth_sources.member_count),
    language     = COALESCE(NULLIF(EXCLUDED.language, ''), growth_sources.language),
    description  = COALESCE(NULLIF(EXCLUDED.description, ''), growth_sources.description),
    origin       = CASE WHEN EXCLUDED.origin = 'manual' THEN 'manual' ELSE growth_sources.origin END,
    status       = CASE WHEN growth_sources.status = 'invalid' THEN growth_sources.status ELSE EXCLUDED.status END,
    discovered_from_id = COALESCE(growth_sources.discovered_from_id, EXCLUDED.discovered_from_id)
RETURNING *, (xmax = 0)::boolean AS inserted;

-- name: GetGrowthSource :one
SELECT * FROM growth_sources WHERE id = sqlc.arg(id);

-- name: GetUserGrowthSource :one
SELECT * FROM growth_sources WHERE id = sqlc.arg(id) AND user_id = sqlc.arg(user_id);

-- name: GetGrowthSourceByKey :one
SELECT * FROM growth_sources
WHERE user_id = sqlc.arg(user_id) AND platform = sqlc.arg(platform) AND source_key = sqlc.arg(source_key);

-- name: GetUserGrowthSources :many
SELECT * FROM growth_sources
WHERE user_id = sqlc.arg(user_id)
ORDER BY qualified_leads DESC, updated_at DESC, id DESC;

-- name: RefreshSourceYield :exec
UPDATE growth_sources gs
SET qualified_leads = (
  SELECT COUNT(DISTINCT le.lead_id)
  FROM lead_evidence le
  JOIN leads l ON l.id = le.lead_id
  WHERE le.source_id = gs.id AND l.tier = 'qualified'
)
WHERE gs.id = ANY(sqlc.arg(ids)::bigint[]);

-- name: RecordSourceRead :one
UPDATE growth_sources
SET empty_streak = CASE
      WHEN last_synced_at IS NULL OR qualified_leads > qualified_at_sync THEN 0
      ELSE empty_streak + 1
    END,
    health = CASE
      WHEN last_synced_at IS NULL OR qualified_leads > qualified_at_sync THEN 'active'
      WHEN empty_streak + 1 >= sqlc.arg(dead_after)::int THEN 'dead'
      WHEN empty_streak + 1 >= sqlc.arg(stale_after)::int THEN 'stale'
      ELSE 'active'
    END,
    qualified_at_sync = qualified_leads,
    last_synced_at = CURRENT_TIMESTAMP
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: ResolveGrowthSource :one
UPDATE growth_sources
SET source_key   = sqlc.arg(source_key),
    external_id  = sqlc.arg(external_id),
    username     = COALESCE(NULLIF(sqlc.arg(username)::text, ''), username),
    title        = COALESCE(NULLIF(sqlc.arg(title)::text, ''), title),
    link         = COALESCE(NULLIF(sqlc.arg(link)::text, ''), link),
    kind         = sqlc.arg(kind),
    member_count = sqlc.arg(member_count),
    description  = COALESCE(NULLIF(sqlc.arg(description)::text, ''), description),
    status       = 'active',
    status_reason = ''
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: UpdateGrowthSourceStatus :one
UPDATE growth_sources
SET status = sqlc.arg(status), status_reason = sqlc.arg(status_reason)
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: DeleteUserGrowthSource :one
DELETE FROM growth_sources WHERE id = sqlc.arg(id) AND user_id = sqlc.arg(user_id) RETURNING *;

-- name: UpdateUserGrowthSource :one
UPDATE growth_sources
SET title = COALESCE(sqlc.narg(title), title), note = COALESCE(sqlc.narg(note), note)
WHERE id = sqlc.arg(id) AND user_id = sqlc.arg(user_id)
RETURNING *;

-- name: UpsertAccountSource :one
INSERT INTO account_sources (account_id, source_id, peer)
VALUES (sqlc.arg(account_id), sqlc.arg(source_id), sqlc.narg(peer))
ON CONFLICT (account_id, source_id) DO UPDATE
SET peer = COALESCE(EXCLUDED.peer, account_sources.peer)
RETURNING *;

-- name: GetAccountSource :one
SELECT * FROM account_sources WHERE account_id = sqlc.arg(account_id) AND source_id = sqlc.arg(source_id);

-- name: GetUserAccountSources :many
SELECT acs.*
FROM account_sources acs
JOIN channel_accounts ga ON ga.id = acs.account_id
WHERE ga.user_id = sqlc.arg(user_id)
ORDER BY acs.source_id, acs.account_id;

-- name: GetAccountJoinedSources :many
SELECT gs.id AS source_id, gs.title, gs.link, gs.member_count, acs.joined_at, acs.synced_at
FROM account_sources acs
JOIN growth_sources gs ON gs.id = acs.source_id
JOIN channel_accounts ca ON ca.id = acs.account_id
WHERE ca.user_id = sqlc.arg(user_id) AND acs.account_id = sqlc.arg(account_id) AND acs.join_status = 'joined'
ORDER BY acs.joined_at DESC NULLS LAST, gs.id DESC;

-- name: UpdateAccountSourceJoin :one
UPDATE account_sources
SET join_status = sqlc.arg(join_status),
    join_error  = sqlc.arg(join_error),
    join_attempted_at = CURRENT_TIMESTAMP,
    private_join = sqlc.arg(private_join),
    joined_at   = CASE WHEN sqlc.arg(join_status)::text = 'joined' THEN COALESCE(joined_at, CURRENT_TIMESTAMP) ELSE joined_at END
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: UpdateAccountSourceSync :one
UPDATE account_sources
SET newest_message_id = GREATEST(newest_message_id, sqlc.arg(newest_message_id)),
    oldest_message_id = CASE
      WHEN sqlc.arg(oldest_message_id)::bigint = 0 THEN oldest_message_id
      WHEN oldest_message_id = 0 THEN sqlc.arg(oldest_message_id)::bigint
      ELSE LEAST(oldest_message_id, sqlc.arg(oldest_message_id)::bigint)
    END,
    history_exhausted = history_exhausted OR sqlc.arg(history_exhausted)::boolean,
    messages_read     = messages_read + sqlc.arg(messages_read)::bigint,
    synced_at         = CURRENT_TIMESTAMP
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: GetUserSourceStats :one
WITH per_source AS (
  SELECT gs.id, gs.status,
    bool_or(acs.join_status = 'joined') AS joined,
    bool_or(acs.join_status = 'pending') AS pending,
    bool_and(acs.join_status = 'not_joinable') AS not_joinable,
    bool_or(acs.synced_at IS NOT NULL) AS read,
    bool_or(acs.history_exhausted) AS exhausted
  FROM growth_sources gs
  LEFT JOIN account_sources acs ON acs.source_id = gs.id
  WHERE gs.user_id = sqlc.arg(user_id)
  GROUP BY gs.id, gs.status
)
SELECT
  COUNT(*)::bigint AS total,
  COUNT(*) FILTER (WHERE status = 'unresolved')::bigint AS unresolved,
  COUNT(*) FILTER (WHERE status = 'invalid')::bigint AS invalid,
  COUNT(*) FILTER (WHERE joined)::bigint AS joined,
  COUNT(*) FILTER (WHERE pending AND NOT COALESCE(joined, false))::bigint AS pending,
  COUNT(*) FILTER (WHERE not_joinable)::bigint AS not_joinable,
  COUNT(*) FILTER (WHERE read)::bigint AS read,
  COUNT(*) FILTER (WHERE exhausted)::bigint AS exhausted
FROM per_source;

-- name: GetLeadByPlatformUser :one
SELECT * FROM leads
WHERE user_id = sqlc.arg(user_id) AND platform = sqlc.arg(platform) AND platform_user_id = sqlc.arg(platform_user_id);

-- name: UpsertLead :one
INSERT INTO leads (
  user_id, platform, platform_user_id, identity_kind, username, display_name, score, tier, match_reason,
  contacts, first_flow_id, first_task_id, last_flow_id, last_task_id, flags
) VALUES (
  sqlc.arg(user_id), sqlc.arg(platform), sqlc.arg(platform_user_id), sqlc.arg(identity_kind),
  sqlc.arg(username), sqlc.arg(display_name),
  sqlc.arg(score), sqlc.arg(tier), sqlc.arg(match_reason), sqlc.arg(contacts),
  sqlc.narg(flow_id), sqlc.narg(task_id), sqlc.narg(flow_id), sqlc.narg(task_id), sqlc.arg(flags)::text[]
)
ON CONFLICT (user_id, platform, platform_user_id) DO UPDATE
SET username     = COALESCE(NULLIF(EXCLUDED.username, ''), leads.username),
    display_name = COALESCE(NULLIF(EXCLUDED.display_name, ''), leads.display_name),
    score        = GREATEST(EXCLUDED.score, leads.score),
    tier         = CASE WHEN EXCLUDED.tier = 'qualified' OR leads.tier = 'qualified' THEN 'qualified' ELSE 'watchlist' END,
    match_reason = CASE WHEN EXCLUDED.score > leads.score THEN EXCLUDED.match_reason ELSE leads.match_reason END,
    contacts     = EXCLUDED.contacts,
    last_flow_id = COALESCE(EXCLUDED.last_flow_id, leads.last_flow_id),
    last_task_id = COALESCE(EXCLUDED.last_task_id, leads.last_task_id),
    flags        = ARRAY(SELECT DISTINCT f FROM unnest(leads.flags || EXCLUDED.flags) AS f ORDER BY f)
RETURNING *;

-- name: InsertLeadEvidence :execrows
INSERT INTO lead_evidence (
  lead_id, source_id, message_id, message_link, message_time, captured_at, excerpt, match_reason, flow_id, task_id
) VALUES (
  sqlc.arg(lead_id), sqlc.arg(source_id), sqlc.arg(message_id), sqlc.arg(message_link), sqlc.narg(message_time),
  sqlc.arg(captured_at),
  sqlc.arg(excerpt), sqlc.arg(match_reason), sqlc.narg(flow_id), sqlc.narg(task_id)
)
ON CONFLICT (source_id, message_id) DO NOTHING;

-- name: GetLeadsEvidence :many
SELECT * FROM lead_evidence
WHERE lead_id = ANY(sqlc.arg(lead_ids)::bigint[])
ORDER BY lead_id, message_time DESC NULLS LAST, id DESC;

-- name: GetLeadsBestScores :many
SELECT DISTINCT ON (le.lead_id) le.lead_id, gms.score, gms.signals
FROM lead_evidence le
JOIN leads l ON l.id = le.lead_id
JOIN growth_message_scores gms
  ON gms.user_id = l.user_id AND gms.source_id = le.source_id AND gms.message_id = le.message_id
WHERE le.lead_id = ANY(sqlc.arg(lead_ids)::bigint[])
ORDER BY le.lead_id, gms.score DESC, gms.id DESC;

-- name: GetUserLead :one
SELECT * FROM leads WHERE id = sqlc.arg(id) AND user_id = sqlc.arg(user_id);

-- name: UpdateUserLead :one
UPDATE leads
SET note     = COALESCE(sqlc.narg(note), note),
    tier     = COALESCE(sqlc.narg(tier), tier),
    contacts = COALESCE(sqlc.narg(contacts), contacts)
WHERE id = sqlc.arg(id) AND user_id = sqlc.arg(user_id)
RETURNING *;

-- A lead's evidence goes with it by cascade; the scores of its messages go in the same
-- statement, so a later read of those messages is judged afresh.
-- name: DeleteUserLead :one
WITH evidence AS (
  SELECT e.source_id, e.message_id FROM lead_evidence e
  JOIN leads l ON l.id = e.lead_id
  WHERE l.id = sqlc.arg(id)::bigint AND l.user_id = sqlc.arg(user_id)::bigint
), dropped AS (
  DELETE FROM growth_message_scores s USING evidence
  WHERE s.user_id = sqlc.arg(user_id)::bigint AND s.source_id = evidence.source_id AND s.message_id = evidence.message_id
)
DELETE FROM leads WHERE id = sqlc.arg(id)::bigint AND user_id = sqlc.arg(user_id)::bigint
RETURNING *;

-- name: InsertManualLead :one
INSERT INTO leads (
  user_id, platform, platform_user_id, identity_kind, display_name, score, tier, contacts, note, origin,
  first_flow_id, last_flow_id
) VALUES (
  sqlc.arg(user_id), sqlc.arg(platform), sqlc.arg(platform_user_id), sqlc.arg(identity_kind),
  sqlc.arg(display_name), sqlc.arg(score), sqlc.arg(tier), sqlc.arg(contacts), sqlc.arg(note), 'manual',
  sqlc.narg(flow_id), sqlc.narg(flow_id)
)
ON CONFLICT (user_id, platform, platform_user_id) DO NOTHING
RETURNING *;

-- name: CountUserLeads :one
SELECT
  COUNT(*) FILTER (WHERE tier = 'qualified' AND delivered_at IS NULL)::bigint AS deliverable,
  COUNT(*) FILTER (WHERE tier = 'watchlist' AND delivered_at IS NULL)::bigint AS watchlist,
  COUNT(*) FILTER (WHERE delivered_at IS NOT NULL)::bigint AS delivered
FROM leads
WHERE user_id = sqlc.arg(user_id)
  AND (sqlc.narg(flow_id)::bigint IS NULL OR last_flow_id = sqlc.narg(flow_id)::bigint)
  AND (sqlc.narg(since)::timestamptz IS NULL OR created_at >= sqlc.narg(since)::timestamptz);

-- name: GetDeliverableLeads :many
SELECT * FROM leads
WHERE user_id = sqlc.arg(user_id) AND last_flow_id = sqlc.arg(flow_id)
  AND tier = 'qualified' AND delivered_at IS NULL
  AND (NOT sqlc.arg(has_contact)::boolean OR jsonb_array_length(contacts) > 0)
ORDER BY score DESC, id ASC
LIMIT sqlc.arg(lim);

-- name: MarkLeadsDelivered :many
UPDATE leads
SET delivered_at = CURRENT_TIMESTAMP, delivered_run_id = sqlc.arg(run_id)
WHERE id = ANY(sqlc.arg(ids)::bigint[]) AND delivered_at IS NULL
RETURNING *;

-- name: GetRunDeliveredLeads :many
SELECT * FROM leads WHERE delivered_run_id = sqlc.arg(run_id) ORDER BY score DESC, id ASC;

-- name: GetUserLeads :many
SELECT * FROM leads
WHERE user_id = sqlc.arg(user_id)
  AND (sqlc.narg(flow_id)::bigint IS NULL OR last_flow_id = sqlc.narg(flow_id)::bigint)
  AND (sqlc.narg(task_id)::bigint IS NULL OR last_task_id = sqlc.narg(task_id)::bigint)
  AND (sqlc.narg(run_id)::bigint IS NULL OR delivered_run_id = sqlc.narg(run_id)::bigint)
  AND (sqlc.narg(tier)::text IS NULL OR tier = sqlc.narg(tier)::text)
  AND (sqlc.narg(delivered)::boolean IS NULL OR (delivered_at IS NOT NULL) = sqlc.narg(delivered)::boolean)
  AND (sqlc.narg(since)::timestamptz IS NULL OR created_at >= sqlc.narg(since)::timestamptz)
  AND (sqlc.narg(has_contact)::boolean IS NULL OR (jsonb_array_length(contacts) > 0) = sqlc.narg(has_contact)::boolean)
  AND (sqlc.narg(flagged)::boolean IS NULL OR (cardinality(flags) > 0) = sqlc.narg(flagged)::boolean)
ORDER BY score DESC, updated_at DESC, id DESC
LIMIT sqlc.arg(lim) OFFSET sqlc.arg(off);

-- name: EnsureGrowthRun :one
INSERT INTO growth_runs (user_id, flow_id, task_id, target)
VALUES (sqlc.arg(user_id), sqlc.arg(flow_id), sqlc.arg(task_id), sqlc.arg(target))
ON CONFLICT (task_id) DO UPDATE SET task_id = EXCLUDED.task_id
RETURNING *;

-- name: GetGrowthRun :one
SELECT * FROM growth_runs WHERE id = sqlc.arg(id);

-- name: GetUserGrowthRun :one
SELECT * FROM growth_runs WHERE id = sqlc.arg(id) AND user_id = sqlc.arg(user_id);

-- name: GetGrowthRunByTask :one
SELECT * FROM growth_runs WHERE task_id = sqlc.arg(task_id);

-- name: GetUserGrowthRuns :many
SELECT * FROM growth_runs
WHERE user_id = sqlc.arg(user_id)
  AND (sqlc.narg(flow_id)::bigint IS NULL OR flow_id = sqlc.narg(flow_id)::bigint)
ORDER BY id DESC;

-- name: CountGrowthRunLeads :many
SELECT r.id AS run_id,
  COUNT(l.id) FILTER (WHERE l.tier = 'qualified')::bigint AS found,
  COUNT(l.id) FILTER (WHERE l.tier = 'watchlist')::bigint AS watch
FROM growth_runs r
JOIN leads l ON l.user_id = r.user_id AND (l.last_task_id = r.task_id OR l.delivered_run_id = r.id)
WHERE r.user_id = sqlc.arg(user_id) AND r.id = ANY(sqlc.arg(run_ids)::bigint[])
GROUP BY r.id;

-- name: UpdateGrowthRunTarget :one
UPDATE growth_runs SET target = sqlc.arg(target) WHERE id = sqlc.arg(id) RETURNING *;

-- name: UpdateGrowthRunDelivery :one
UPDATE growth_runs
SET delivered_count = sqlc.arg(delivered_count),
    status          = sqlc.arg(status),
    finished_at     = CASE WHEN sqlc.arg(status)::text = 'running' THEN NULL ELSE CURRENT_TIMESTAMP END
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: UpsertGrowthSetting :one
INSERT INTO growth_settings (user_id, kind, provider, secret)
VALUES ($1, $2, $3, $4)
ON CONFLICT (user_id, kind) DO UPDATE SET
  provider = EXCLUDED.provider,
  secret = EXCLUDED.secret,
  status = 'untested',
  status_reason = '',
  tested_at = NULL
RETURNING *;

-- name: GetUserGrowthSettings :many
SELECT * FROM growth_settings WHERE user_id = $1 ORDER BY kind;

-- name: GetUserGrowthSetting :one
SELECT * FROM growth_settings WHERE user_id = $1 AND kind = $2;

-- name: UpdateGrowthSettingStatus :one
UPDATE growth_settings
SET status = $3, status_reason = $4, tested_at = now()
WHERE user_id = $1 AND kind = $2
RETURNING *;

-- name: DeleteUserGrowthSetting :execrows
DELETE FROM growth_settings WHERE user_id = $1 AND kind = $2;

-- name: RecordGrowthRunNotify :exec
UPDATE growth_runs
SET notify_status = sqlc.arg(notify_status), notify_error = sqlc.arg(notify_error), notified_at = now()
WHERE id = sqlc.arg(id);

-- name: GetUserGrowthProfile :one
SELECT * FROM growth_profiles WHERE id = sqlc.arg(id) AND user_id = sqlc.arg(user_id);

-- name: GetDefaultGrowthProfile :one
SELECT * FROM growth_profiles WHERE user_id = sqlc.arg(user_id) AND is_default;

-- name: GetUserGrowthProfiles :many
SELECT * FROM growth_profiles WHERE user_id = sqlc.arg(user_id) ORDER BY is_default DESC, id;

-- A customer's first profile is their default.
-- name: CreateGrowthProfile :one
INSERT INTO growth_profiles (user_id, title, profile, is_default)
VALUES (
  sqlc.arg(user_id), sqlc.arg(title), sqlc.arg(profile),
  NOT EXISTS (SELECT 1 FROM growth_profiles WHERE user_id = sqlc.arg(user_id))
)
RETURNING *;

-- name: UpdateGrowthProfile :one
UPDATE growth_profiles SET title = sqlc.arg(title), profile = sqlc.arg(profile)
WHERE id = sqlc.arg(id) AND user_id = sqlc.arg(user_id)
RETURNING *;

-- name: SetDefaultGrowthProfile :execrows
UPDATE growth_profiles SET is_default = (id = sqlc.arg(id)::bigint)
WHERE user_id = sqlc.arg(user_id)::bigint AND (is_default OR id = sqlc.arg(id)::bigint)
  AND EXISTS (SELECT 1 FROM growth_profiles p WHERE p.id = sqlc.arg(id)::bigint AND p.user_id = sqlc.arg(user_id)::bigint);

-- name: DeleteGrowthProfile :one
WITH dropped AS (
  DELETE FROM growth_message_scores WHERE user_id = sqlc.arg(user_id)::bigint AND profile_id = sqlc.arg(id)::bigint
)
DELETE FROM growth_profiles WHERE id = sqlc.arg(id)::bigint AND user_id = sqlc.arg(user_id)::bigint
RETURNING *;

-- PromoteGrowthProfile makes the oldest profile the default when the customer has none.
-- name: PromoteGrowthProfile :one
UPDATE growth_profiles SET is_default = TRUE
WHERE id = (SELECT p.id FROM growth_profiles p WHERE p.user_id = sqlc.arg(user_id) ORDER BY p.id LIMIT 1)
  AND NOT EXISTS (SELECT 1 FROM growth_profiles d WHERE d.user_id = sqlc.arg(user_id) AND d.is_default)
RETURNING *;

-- name: UpsertGrowthFlowProfile :exec
INSERT INTO growth_flow_profiles (flow_id, profile_id, title)
VALUES (sqlc.arg(flow_id), sqlc.narg(profile_id), sqlc.arg(title))
ON CONFLICT (flow_id) DO UPDATE SET profile_id = EXCLUDED.profile_id, title = EXCLUDED.title;

-- name: UpsertGrowthProfileDraft :one
INSERT INTO growth_profile_drafts (flow_id, user_id, base_profile_id, title, profile)
VALUES (sqlc.arg(flow_id), sqlc.arg(user_id), sqlc.narg(base_profile_id), sqlc.arg(title), sqlc.arg(profile))
ON CONFLICT (flow_id) DO UPDATE
SET base_profile_id = EXCLUDED.base_profile_id, title = EXCLUDED.title, profile = EXCLUDED.profile
RETURNING *;

-- name: GetGrowthProfileDraft :one
SELECT * FROM growth_profile_drafts WHERE flow_id = sqlc.arg(flow_id) AND user_id = sqlc.arg(user_id);

-- name: DeleteGrowthProfileDraft :execrows
DELETE FROM growth_profile_drafts WHERE flow_id = sqlc.arg(flow_id) AND user_id = sqlc.arg(user_id);

-- name: GetGrowthFlowProfile :one
SELECT * FROM growth_flow_profiles WHERE flow_id = sqlc.arg(flow_id);

-- name: UpsertMessageScore :exec
INSERT INTO growth_message_scores (user_id, profile_id, source_id, message_id, score, signals)
VALUES (
  sqlc.arg(user_id), sqlc.arg(profile_id), sqlc.arg(source_id), sqlc.arg(message_id), sqlc.arg(score), sqlc.arg(signals)
)
ON CONFLICT (user_id, profile_id, source_id, message_id) DO UPDATE
SET score = EXCLUDED.score, signals = EXCLUDED.signals;

-- name: GetMessageScores :many
SELECT * FROM growth_message_scores
WHERE user_id = sqlc.arg(user_id) AND profile_id = sqlc.arg(profile_id) AND source_id = sqlc.arg(source_id)
  AND message_id = ANY(sqlc.arg(message_ids)::text[]);

-- name: AdoptGrowthRun :one
UPDATE growth_runs SET task_id = sqlc.arg(task_id)
WHERE id = (
  SELECT r.id FROM growth_runs r
  WHERE r.user_id = sqlc.arg(user_id) AND r.flow_id = sqlc.arg(flow_id) AND r.task_id IS NULL AND r.status = 'running'
  ORDER BY r.id
  LIMIT 1
  FOR UPDATE SKIP LOCKED
)
RETURNING *;

-- name: OpenScheduledGrowthRun :one
INSERT INTO growth_runs (user_id, flow_id, target, deadline_at, schedule_id)
VALUES (sqlc.arg(user_id), sqlc.arg(flow_id), sqlc.arg(target), sqlc.arg(deadline_at), sqlc.arg(schedule_id))
RETURNING *;

-- name: GetOverdueGrowthRuns :many
SELECT * FROM growth_runs
WHERE deadline_at IS NOT NULL AND deadline_closed_at IS NULL AND deadline_at <= sqlc.arg(now)::timestamptz
ORDER BY deadline_at, id;

-- name: CloseGrowthRunDeadline :exec
UPDATE growth_runs SET deadline_closed_at = now() WHERE id = sqlc.arg(id) AND deadline_closed_at IS NULL;

-- name: CreateGrowthSchedule :one
INSERT INTO growth_schedules (
  user_id, flow_id, input, provider_name, days, weekdays, date, start_time, end_time, timezone, enabled, next_start_at,
  profile_id
) VALUES (
  sqlc.arg(user_id), sqlc.arg(flow_id), sqlc.arg(input), sqlc.arg(provider_name), sqlc.arg(days), sqlc.arg(weekdays),
  sqlc.arg(date), sqlc.arg(start_time), sqlc.arg(end_time), sqlc.arg(timezone), sqlc.arg(enabled), sqlc.arg(next_start_at),
  sqlc.narg(profile_id)
)
RETURNING *;

-- name: UpdateGrowthSchedule :one
UPDATE growth_schedules
SET input = sqlc.arg(input), days = sqlc.arg(days), weekdays = sqlc.arg(weekdays), date = sqlc.arg(date),
    start_time = sqlc.arg(start_time), end_time = sqlc.arg(end_time), timezone = sqlc.arg(timezone),
    enabled = sqlc.arg(enabled), next_start_at = sqlc.arg(next_start_at), profile_id = sqlc.narg(profile_id),
    last_error = ''
WHERE id = sqlc.arg(id) AND user_id = sqlc.arg(user_id)
RETURNING *;

-- name: GetUserGrowthSchedule :one
SELECT * FROM growth_schedules WHERE id = sqlc.arg(id) AND user_id = sqlc.arg(user_id);

-- name: GetUserGrowthSchedules :many
SELECT * FROM growth_schedules
WHERE user_id = sqlc.arg(user_id)
  AND (sqlc.narg(flow_id)::bigint IS NULL OR flow_id = sqlc.narg(flow_id)::bigint)
ORDER BY id;

-- name: DeleteUserGrowthSchedule :execrows
DELETE FROM growth_schedules WHERE id = sqlc.arg(id) AND user_id = sqlc.arg(user_id);

-- name: GetDueGrowthSchedules :many
SELECT * FROM growth_schedules
WHERE enabled AND next_start_at IS NOT NULL AND next_start_at <= sqlc.arg(now)::timestamptz
ORDER BY next_start_at, id;

-- AdvanceGrowthSchedule claims the start at handled_start_at by moving the schedule past
-- it; zero rows means another pass has claimed it already.
-- name: AdvanceGrowthSchedule :execrows
UPDATE growth_schedules
SET next_start_at = sqlc.narg(next_start_at), enabled = sqlc.arg(enabled)
WHERE id = sqlc.arg(id) AND next_start_at = sqlc.arg(handled_start_at);

-- name: RecordGrowthScheduleStart :exec
UPDATE growth_schedules
SET last_run_id = COALESCE(sqlc.narg(last_run_id), last_run_id), last_error = sqlc.arg(last_error)
WHERE id = sqlc.arg(id);

-- One statement, so the profile and the scores it outdates change together.
-- name: UpdateGrowthProfileDroppingScores :one
WITH dropped AS (
  DELETE FROM growth_message_scores WHERE user_id = sqlc.arg(user_id)::bigint AND profile_id = sqlc.arg(id)::bigint
)
UPDATE growth_profiles SET title = sqlc.arg(title)::text, profile = sqlc.arg(profile)::jsonb
WHERE id = sqlc.arg(id)::bigint AND user_id = sqlc.arg(user_id)::bigint
RETURNING *;
