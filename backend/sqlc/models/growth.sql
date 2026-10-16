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
-- whether the account already joined source_id (0 matches none).
SELECT ca.*, COALESCE(acs.join_status = 'joined', false)::boolean AS joined_source
FROM channel_accounts ca
LEFT JOIN account_sources acs ON acs.account_id = ca.id AND acs.source_id = sqlc.arg(source_id)
WHERE ca.user_id = sqlc.arg(user_id) AND ca.platform = sqlc.arg(platform)
  AND ca.status IN ('active', 'rate_limited')
ORDER BY ca.id;

-- name: MarkChannelAccountUsed :exec
UPDATE channel_accounts SET last_used_at = now() WHERE id = sqlc.arg(id);

-- name: RecordChannelAccountSuccess :exec
UPDATE channel_accounts
SET last_success_at = now(),
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

-- name: UpdateAccountSourceJoin :one
UPDATE account_sources
SET join_status = sqlc.arg(join_status),
    join_error  = sqlc.arg(join_error),
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
  first_flow_id, first_task_id, last_flow_id, last_task_id
) VALUES (
  sqlc.arg(user_id), sqlc.arg(platform), sqlc.arg(platform_user_id), sqlc.arg(identity_kind),
  sqlc.arg(username), sqlc.arg(display_name),
  sqlc.arg(score), sqlc.arg(tier), sqlc.arg(match_reason),
  sqlc.narg(flow_id), sqlc.narg(task_id), sqlc.narg(flow_id), sqlc.narg(task_id)
)
ON CONFLICT (user_id, platform, platform_user_id) DO UPDATE
SET username     = COALESCE(NULLIF(EXCLUDED.username, ''), leads.username),
    display_name = COALESCE(NULLIF(EXCLUDED.display_name, ''), leads.display_name),
    score        = GREATEST(EXCLUDED.score, leads.score),
    tier         = CASE WHEN EXCLUDED.tier = 'qualified' OR leads.tier = 'qualified' THEN 'qualified' ELSE 'watchlist' END,
    match_reason = CASE WHEN EXCLUDED.score > leads.score THEN EXCLUDED.match_reason ELSE leads.match_reason END,
    last_flow_id = COALESCE(EXCLUDED.last_flow_id, leads.last_flow_id),
    last_task_id = COALESCE(EXCLUDED.last_task_id, leads.last_task_id)
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

-- name: GetUserLead :one
SELECT * FROM leads WHERE id = sqlc.arg(id) AND user_id = sqlc.arg(user_id);

-- name: CountFlowLeads :one
SELECT
  COUNT(*) FILTER (WHERE tier = 'qualified' AND delivered_at IS NULL)::bigint AS deliverable,
  COUNT(*) FILTER (WHERE tier = 'watchlist' AND delivered_at IS NULL)::bigint AS watchlist,
  COUNT(*) FILTER (WHERE delivered_at IS NOT NULL)::bigint AS delivered
FROM leads
WHERE user_id = sqlc.arg(user_id) AND last_flow_id = sqlc.arg(flow_id);

-- name: GetDeliverableLeads :many
SELECT * FROM leads
WHERE user_id = sqlc.arg(user_id) AND last_flow_id = sqlc.arg(flow_id)
  AND tier = 'qualified' AND delivered_at IS NULL
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
