// Wire shapes of the REST API under /api/v1/growth. Field names are the JSON keys the backend sends.

export interface GrowthAccount {
    // True while the account may be picked for work right now.
    available: boolean;
    consecutive_errors: number;
    // DevTools address of the customer's own fingerprint browser (AdsPower, BitBrowser), masked; when set it
    // supplies the proxy and the fingerprint, and the system's own are unused.
    external_browser_ws?: null | string;
    // Phone number or platform user id; only its last four characters are shown. Absent on a browser account.
    external_id?: string;
    // Generated per browser_session account; absent for Telegram.
    fingerprint?: GrowthFingerprint | null;
    first_signed_in_at?: null | string;
    id: number;
    // Reminder codes of a Telegram account past its suggested join pace; they never stop a join.
    join_advice?: GrowthJoinAdvice[];
    joined_sources?: number;
    joins_today?: number;
    kind: string;
    label: string;
    last_error?: null | string;
    last_rate_limited_at?: null | string;
    last_success_at?: null | string;
    last_used_at?: null | string;
    // When the suggested gap after the last join ends.
    next_join_at?: null | string;
    platform: GrowthAccountPlatform;
    // The account's own proxy, credentials masked (scheme://••••@host:port); absent when it has none.
    proxy?: null | string;
    // Which route the account's traffic takes: its own proxy, the default proxy, or none.
    proxy_source: GrowthProxySource;
    // Set while `rate_limited`: when the platform lets the account act again.
    rate_limit_until?: null | string;
    status: GrowthAccountStatus;
}

// The groups come from the source pool's join records; dialogs_count is counted live and absent when the
// account is offline or does not answer in time.
export interface GrowthAccountDetail {
    dialogs_count?: number;
    groups: GrowthAccountGroup[];
    joined_groups_count: number;
}

export interface GrowthAccountGroup {
    joined_at?: string;
    last_synced_at?: string;
    link: string;
    member_count: number;
    source_id: number;
    title: string;
}

// Facebook, Instagram and TikTok accounts sign in through a remote browser rather than a code sent by Telegram.
export type GrowthAccountPlatform = 'facebook' | 'instagram' | 'telegram' | 'tiktok';

// `rate_limited` is not an error: the connector sleeps for as long as the platform asked and resumes on its
// own. `disabled` is the customer's choice; the account is kept but never used.
export type GrowthAccountStatus = 'active' | 'disabled' | 'needs_login' | 'rate_limited';

export const isAccountSignedIn = (account: Pick<GrowthAccount, 'status'>): boolean =>
    account.status === 'active' || account.status === 'rate_limited';

export interface GrowthAccountsResponse {
    accounts: GrowthAccount[];
}

// `to` lists where the server actually sent the email.
export interface GrowthEmailResult {
    sent: boolean;
    to: string[];
}

export type GrowthJoinAdvice =
    | 'daily_joins_reached'
    | 'join_interval'
    | 'joined_groups_high'
    | 'private_joins_reached'
    | 'warming_up';

// A Telegram account that has joined as many groups as the warning line in its pace; Telegram refuses about 500.
export interface GrowthJoinWarning {
    account_id: number;
    joined_sources: number;
    label: string;
    platform: string;
    warn_at: number;
}

export interface GrowthLead {
    // Ways to reach the lead beside its platform account; empty when it has none.
    contacts?: GrowthLeadContact[];
    // When the pool first took the lead in: found by the agent or added by hand.
    created_at: string;
    delivered_at: null | string;
    delivered_task_id: null | number;
    display_name: string;
    // Newest first.
    evidence: GrowthLeadEvidence[];
    first_flow_id: number;
    first_flow_title: string;
    first_round: number;
    first_task_id: number;
    // `unwanted` when the lead matched an unwanted line of the profile it was judged with; absent means none.
    flags?: string[];
    id: number;
    last_active_at: null | string;
    // The agent's one-line reason the lead fits, in the customer's language; absent on a lead added by hand.
    match_reason?: string;
    // The owner's own words about the lead; empty when none.
    note: string;
    origin: GrowthLeadOrigin;
    platform: GrowthPlatform;
    platform_user_id: string;
    profile: GrowthLeadProfile | null;
    score: number;
    score_breakdown: { points: number; signal: GrowthLeadSignal }[];
    signals: GrowthLeadSignal[];
    source_id: number;
    source_title: string;
    tier: GrowthLeadTier;
    username: null | string;
}

export interface GrowthLeadContact {
    // A GrowthContactType, a name the owner added, or 'messenger' on leads delivered before the split.
    type: string;
    value: string;
}

export interface GrowthLeadEvidence {
    excerpt: string;
    id: number;
    message_id: string;
    message_url: string;
    posted_at: string;
    reason: string;
    source_id: number;
    source_title: string;
}

// A lead the owner added by hand is scored at the qualified line and never screened.
export interface GrowthLeadInput {
    contacts?: GrowthLeadContact[];
    display_name?: string;
    flow_id?: number;
    // Account id, username or profile link on the platform.
    handle: string;
    note?: string;
    platform: GrowthPlatform;
    tier?: GrowthLeadTier;
}

export type GrowthLeadOrigin = 'ai' | 'manual';

// Public-profile enrichment; every field is optional and an absent one is not rendered.
export interface GrowthLeadProfile {
    bio?: string;
    languages?: string[];
    messages_30d?: number;
    related_groups?: number;
}

// A scoring dimension key of the profile, or `active` for the recency points.
export type GrowthLeadSignal = string;

export type GrowthLeadTier = 'qualified' | 'watch';

// Omitted fields stay as they are; contacts replace the whole list.
export interface GrowthLeadUpdate {
    contacts?: GrowthLeadContact[];
    note?: string;
    tier?: GrowthLeadTier;
}

// The relay's id is what the setting stores; name is its display name, when it has one.
export interface GrowthModelOption {
    id: string;
    name?: string;
}

// An empty field asks the server to use the saved one. api_url is the screening endpoint, server_url the llm one.
export interface GrowthModelsInput {
    api_key: string;
    api_url?: string;
    protocol?: string;
    provider: null | string;
    server_url?: string;
}

export interface GrowthModelsResponse {
    models: GrowthModelOption[];
}

export type GrowthPlatform = 'facebook' | 'instagram' | 'telegram' | 'tiktok' | 'web' | 'youtube';

export const GROWTH_PLATFORMS: GrowthPlatform[] = ['telegram', 'facebook', 'instagram', 'tiktok', 'youtube', 'web'];

export type GrowthContactType = 'email' | 'line' | 'other' | 'phone' | 'platform_handle' | 'telegram' | 'whatsapp';

// Stored as the server generated it; only these keys are shown.
export interface GrowthFingerprint {
    language?: string;
    // What the browser service generates; `language` is the older name.
    locale?: string;
    os?: string;
    timezone?: string;
}

// The profile a task judges with; fallback is set when the one it started with was deleted.
export interface GrowthFlowProfile {
    fallback: boolean;
    flow_id: number;
    profile_id: null | number;
    title: string;
}

// The three prompts the agents and Jev read, plus any prompts added after them.
// scoring is the Jev rules as text (see profile-scoring.ts).
export interface GrowthProfile {
    // The one a task uses when none is chosen.
    default: boolean;
    // Read back from a "Lead target: N" line in need. Absent means the usual 50.
    default_target?: number;
    edited_by?: '' | 'assistant' | 'user';
    exclude: string;
    // Set when the last assistant edit can be restored.
    has_previous?: boolean;
    id: number;
    name: string;
    need: string;
    scoring: string;
    sections?: GrowthProfileSection[];
    // The name the owner knows the profile by; the server falls back to the business name.
    title: string;
    updated_at: string;
}

// What the task's assistant drafted and the owner has not saved; base_profile_id is the profile it changes.
export interface GrowthProfileDraft {
    base_profile_id: null | number;
    flow_id: number;
    profile: GrowthProfileInput;
    title: string;
    updated_at: string;
}

// An empty title keeps the draft's.
export interface GrowthProfileDraftSave {
    as_new?: boolean;
    make_default?: boolean;
    title?: string;
    use_for_flow?: boolean;
}

export type GrowthProfileInput = Omit<GrowthProfile, 'default' | 'edited_by' | 'has_previous' | 'id' | 'updated_at'>;

// A starting profile in English, the language the agents read; the page names it by its id.
// The three prompts are the text the form pastes in. A preset leaves a prompt null when it has none.
export interface GrowthProfilePreset {
    // The one a new profile starts from; without it the first preset is.
    default?: boolean;
    id: string;
    profile: {
        exclude: null | string;
        name: null | string;
        need: null | string;
        scoring: null | string;
        sections?: GrowthProfileSection[] | null;
        title: null | string;
    };
}

export interface GrowthProfileSection {
    body: string;
    id: string;
    title: string;
}

export type GrowthProxySource = 'account' | 'direct' | 'global';

// Tests the route the account's traffic takes now, by fetching api.telegram.org through it.
export interface GrowthProxyTestResult {
    error: null | string;
    latency_ms: null | number;
    ok: boolean;
    proxy: null | string;
    source: GrowthProxySource;
}

// A browser opened on the server for Facebook, Instagram or TikTok; `view_url` is the live view to embed.
export interface GrowthRemoteLogin {
    fingerprint: GrowthFingerprint | null;
    session_id: string;
    view_url: null | string;
}

// One round is one Task of the flow. `target` is null for an assistant question that set no goal.
export interface GrowthRun {
    current_action: null | string;
    // A scheduled round is delivered at this time, found or not.
    deadline_at?: null | string;
    delivered: number;
    error: null | string;
    finished_at: null | string;
    flood_wait_seconds: null | number;
    flow_id: number;
    found: number;
    notify_error: null | string;
    // Outcome of emailing the delivered list; `skipped` when email notifications are not set up.
    notify_status: 'failed' | 'sent' | 'skipped' | null;
    round: number;
    schedule_id?: null | number;
    // The owner's whole pool as it is now, the same on every round.
    sources: GrowthRunSources;
    started_at: string;
    status: GrowthRunStatus;
    target: null | number;
    task_id: number;
    watch: number;
}

export interface GrowthRunSources {
    exhausted: number;
    invalid: number;
    joined: number;
    not_joinable: number;
    pending: number;
    read: number;
    total: number;
    unresolved: number;
}

export type GrowthRunStatus = 'delivered' | 'failed' | 'missed' | 'running' | 'waiting';

// A recurring round of a flow. Times are wall-clock in `timezone`; an end not after the start ends the next day.
export interface GrowthSchedule {
    date: null | string;
    days: GrowthScheduleDays;
    enabled: boolean;
    end_time: string;
    flow_id: number;
    id: number;
    input: string;
    last_error?: null | string;
    last_run?: GrowthScheduleLastRun | null;
    // UTC; absent while paused or once a one-off date has passed.
    next_start_at?: null | string;
    // Absent starts with the default profile.
    profile_id?: null | number;
    start_time: string;
    timezone: string;
    // 0 is Sunday.
    weekdays: null | number[];
}

export type GrowthScheduleDays = 'daily' | 'once' | 'weekdays' | 'weekly';

// An empty input reuses the flow's first task input; flow_id is ignored on update.
export interface GrowthScheduleInput {
    date?: string;
    days: GrowthScheduleDays;
    enabled?: boolean;
    end_time: string;
    flow_id: number;
    input?: string;
    // On create, absent takes the task's profile; on update, absent keeps it and 0 clears it.
    profile_id?: number;
    start_time: string;
    timezone: string;
    weekdays?: number[];
}

export interface GrowthScheduleLastRun {
    deadline_at: null | string;
    delivered_count: number;
    run_id: number;
    status: GrowthRunStatus;
    target: null | number;
}

export interface GrowthSchedulesResponse {
    schedules: GrowthSchedule[];
    total: number;
}

// One card of the "Keys & connections" page. Secret fields come back masked ("••••abcd"); `source` says
// whether the value was saved here, preset by the server, or is missing.
export interface GrowthSetting {
    fields: Record<string, string>;
    kind: GrowthSettingKind;
    // Email only: the server has its own SMTP server in .env, so the customer need not set one.
    platform_smtp?: boolean;
    // The server omits the empty ones of these.
    provider?: null | string;
    providers?: string[];
    source: GrowthSettingSource;
    status: GrowthSettingStatus;
    status_reason?: null | string;
    tested_at?: null | string;
    updated_at?: null | string;
}

// A field left empty keeps the value already saved.
export interface GrowthSettingInput {
    fields: Record<string, string>;
    provider: null | string;
}

export type GrowthSettingKind =
    | 'email'
    | 'embedding'
    | 'llm'
    | 'proxy'
    | 'screening'
    | 'telegram'
    | 'web_search'
    | 'youtube';

export type GrowthSettingSource = 'database' | 'env' | 'none';

export interface GrowthSettingsResponse {
    settings: GrowthSetting[];
}

export type GrowthSettingStatus = 'connected' | 'invalid' | 'untested';

export interface GrowthSource {
    created_at: string;
    id: number;
    join_status: GrowthSourceJoinStatus;
    kind: 'channel' | 'group';
    // Nil until the backend has read the source at least once.
    last_read_at: null | string;
    leads_count: number;
    link: string;
    members_count: null | number;
    // The owner's own words about the source; empty when none.
    note: string;
    origin: GrowthSourceOrigin;
    platform: GrowthPlatform;
    source_id: string;
    title: string;
}

export type GrowthSourceAddOutcome = 'added' | 'existing' | 'invalid' | 'unresolved';

export interface GrowthSourceAddResult {
    link: string;
    outcome: GrowthSourceAddOutcome;
    source?: GrowthSource;
}

export type GrowthSourceJoinStatus = 'direct' | 'joined' | 'not_joined' | 'pending_approval' | 'unjoinable';

export type GrowthSourceOrigin = 'ai' | 'manual';

export interface GrowthSourcesAddResponse {
    results: GrowthSourceAddResult[];
}

export interface GrowthSourcesResponse {
    sources: GrowthSource[];
    total: number;
    warnings: GrowthJoinWarning[] | null;
}

// Omitted fields stay as they are; the link cannot change.
export interface GrowthSourceUpdate {
    note?: string;
    title?: string;
}

// Telegram sign-in is a short conversation: the phone gets a code, and an account with two-step
// verification then asks for its password.
export interface GrowthTelegramLoginStep {
    account: GrowthAccount | null;
    login_id: string;
    next: 'code' | 'done' | 'password';
}
