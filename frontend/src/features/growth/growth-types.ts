// Wire shapes of the REST API under /api/v1/growth. Field names are the JSON keys the backend sends.

export interface GrowthAccount {
    // True while the account may be picked for work right now.
    available: boolean;
    consecutive_errors: number;
    // DevTools address of the customer's own fingerprint browser (AdsPower, BitBrowser), masked; when set it
    // supplies the proxy and the fingerprint, and the system's own are unused.
    external_browser_ws: null | string;
    // Phone number or platform user id; only its last four characters are shown.
    external_id: string;
    // Generated per browser_session account; null for Telegram.
    fingerprint: GrowthFingerprint | null;
    id: number;
    kind: string;
    label: string;
    last_error: null | string;
    last_rate_limited_at: null | string;
    last_success_at: null | string;
    last_used_at: null | string;
    platform: GrowthAccountPlatform;
    // Which route the account's traffic takes: its own proxy, the default proxy, or none.
    proxy_source: GrowthProxySource;
    // The account's own proxy, credentials masked (scheme://••••@host:port); null when it has none.
    proxy_url: null | string;
    // Set while `rate_limited`: when the platform lets the account act again.
    rate_limit_until: null | string;
    status: GrowthAccountStatus;
}

// Facebook and Instagram accounts sign in through a remote browser rather than a code sent by Telegram.
export type GrowthAccountPlatform = 'facebook' | 'instagram' | 'telegram';

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

export interface GrowthLead {
    delivered_at: null | string;
    delivered_task_id: null | number;
    display_name: string;
    // Newest first.
    evidence: GrowthLeadEvidence[];
    first_flow_id: number;
    first_flow_title: string;
    first_round: number;
    first_task_id: number;
    id: number;
    last_active_at: null | string;
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

// Public-profile enrichment; every field is optional and an absent one is not rendered.
export interface GrowthLeadProfile {
    bio?: string;
    languages?: string[];
    messages_30d?: number;
    related_groups?: number;
}

export type GrowthLeadSignal = 'competitor' | 'genre_match' | 'paying' | 'player' | 'recent' | 'switching';

export type GrowthLeadTier = 'qualified' | 'watch';

export type GrowthPlatform = 'facebook' | 'instagram' | 'telegram' | 'web' | 'youtube';

export const GROWTH_PLATFORMS: GrowthPlatform[] = ['telegram', 'facebook', 'instagram', 'youtube', 'web'];

// Stored as the server generated it; only these keys are shown.
export interface GrowthFingerprint {
    language?: string;
    os?: string;
    timezone?: string;
}

export interface GrowthProfile {
    competitors: string[];
    default_target: number;
    excludes: string[];
    games: string[];
    genre: string;
    id: number;
    languages: string[];
    name: string;
    // Free text handed to the agents as written.
    notes: string;
    pitch: string;
    regions: string[];
    signals: string[];
    updated_at: string;
}

export type GrowthProfileInput = Omit<GrowthProfile, 'id' | 'updated_at'>;

export type GrowthProxySource = 'account' | 'direct' | 'global';

// Tests the route the account's traffic takes now, by fetching api.telegram.org through it.
export interface GrowthProxyTestResult {
    error: null | string;
    latency_ms: null | number;
    ok: boolean;
    proxy: null | string;
    source: GrowthProxySource;
}

// A browser opened on the server for Facebook or Instagram; `view_url` is the live view to embed.
export interface GrowthRemoteLogin {
    fingerprint: GrowthFingerprint | null;
    session_id: string;
    view_url: null | string;
}

// One round is one Task of the flow. `target` is null for an assistant question that set no goal.
export interface GrowthRun {
    current_action: null | string;
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
    sources: GrowthRunSources;
    started_at: string;
    status: GrowthRunStatus;
    target: null | number;
    task_id: number;
    watch: number;
}

export interface GrowthRunSources {
    joined: number;
    not_joined: number;
    pending: number;
    unjoinable: number;
}

export type GrowthRunStatus = 'delivered' | 'failed' | 'missed' | 'running' | 'waiting';

// One card of the "Keys & connections" page. Secret fields come back masked ("••••abcd"); `source` says
// whether the value was saved here, preset by the server, or is missing.
export interface GrowthSetting {
    fields: Record<string, string>;
    kind: GrowthSettingKind;
    provider: null | string;
    providers: string[];
    source: GrowthSettingSource;
    status: GrowthSettingStatus;
    status_reason: null | string;
    tested_at: null | string;
    updated_at: null | string;
}

// A field left empty keeps the value already saved.
export interface GrowthSettingInput {
    fields: Record<string, string>;
    provider: null | string;
}

export type GrowthSettingKind = 'email' | 'embedding' | 'llm' | 'proxy' | 'telegram' | 'web_search' | 'youtube';

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

export type GrowthSourceJoinStatus = 'joined' | 'not_joined' | 'pending_approval' | 'unjoinable';

export type GrowthSourceOrigin = 'ai' | 'manual';

export interface GrowthSourcesAddResponse {
    results: GrowthSourceAddResult[];
}

// Telegram sign-in is a short conversation: the phone gets a code, and an account with two-step
// verification then asks for its password.
export interface GrowthTelegramLoginStep {
    account: GrowthAccount | null;
    login_id: string;
    next: 'code' | 'done' | 'password';
}
