// Wire shapes of the REST API under /api/v1/growth. Field names are the JSON keys the backend sends.

export interface GrowthAccount {
    display_name: string;
    id: number;
    last_flood_wait_at: null | string;
    last_success_at: null | string;
    phone_last4: string;
    platform: GrowthAccountPlatform;
    // Set while `flood_wait`: when the platform lets the account act again.
    resume_at: null | string;
    status: GrowthAccountStatus;
    // Counted since local midnight on the server; display only, nothing caps them.
    today_joins: number;
    today_reads: number;
}

// Facebook and Instagram accounts sign in through a remote browser rather than a code sent by Telegram.
export type GrowthAccountPlatform = 'facebook' | 'instagram' | 'telegram';

// `flood_wait` is not an error: the connector sleeps for as long as the platform asked and resumes on its
// own. `disabled` is the customer's choice; the account is kept but never used. `restricted` means the
// platform banned or froze the account and the system has stopped using it for good.
export type GrowthAccountStatus = 'connected' | 'disabled' | 'flood_wait' | 'restricted' | 'signed_out';

export const isAccountUsable = (account: Pick<GrowthAccount, 'status'>): boolean =>
    account.status === 'connected' || account.status === 'flood_wait';

// Where the server actually sent a test email or a lead list.
export interface GrowthEmailResult {
    sent_to: string[];
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

export type GrowthPlatform = 'telegram';

export interface GrowthProfile {
    competitors: string[];
    default_target: number;
    excludes: string[];
    games: string[];
    genre: string;
    id: number;
    languages: string[];
    name: string;
    pitch: string;
    regions: string[];
    signals: string[];
    updated_at: string;
}

export type GrowthProfileInput = Omit<GrowthProfile, 'id' | 'updated_at'>;

// One round is one Task of the flow. `target` is null for an assistant question that set no goal.
export interface GrowthRun {
    current_action: null | string;
    delivered: number;
    error: null | string;
    finished_at: null | string;
    flood_wait_seconds: null | number;
    flow_id: number;
    found: number;
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

export interface GrowthSetting {
    // The kind itself, except `web_search:<provider>` since several search services may be saved.
    id: string;
    kind: GrowthSettingKind;
    masked: null | Record<string, string>;
    provider: null | string;
    reason_code: GrowthSettingReason | null;
    status: GrowthSettingStatus;
    tested_at: null | string;
}

export interface GrowthSettingInput {
    kind: GrowthSettingKind;
    provider: null | string;
    // Field name to value: api_key, plus server_url for a custom LLM, cx for Google search, api_id and
    // api_hash for Telegram, smtp_* and notify_emails (comma-separated) for email.
    values: Record<string, string>;
}

// One card (or, for web search, one service) of the "Keys & connections" page. The full secret never comes
// back: `masked` holds each saved field already masked by the server.
export type GrowthSettingKind = 'email' | 'llm' | 'telegram' | 'web_search' | 'youtube';

export type GrowthSettingReason = 'format' | 'permission' | 'quota' | 'rejected' | 'timeout' | 'unreachable';

export type GrowthSettingStatus = 'connected' | 'invalid' | 'unset';

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
