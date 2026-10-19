import type {
    GrowthLead,
    GrowthLeadEvidence,
    GrowthLeadTier,
    GrowthPlatform,
    GrowthRun,
    GrowthRunSources,
    GrowthRunStatus,
    GrowthSource,
    GrowthSourceAddOutcome,
    GrowthSourceAddResult,
    GrowthSourceJoinStatus,
    GrowthSourceOrigin,
} from './growth-types';

// The JSON the backend sends (backend/pkg/growth and pkg/server/services/growth.go); the pages read the
// shapes in growth-types.ts, which the converters below fill in. Omitted keys are the Go `omitempty` ones.

export interface WireEvidence {
    captured_at: string;
    excerpt: string;
    match_reason?: string;
    message_id: string;
    message_link?: string;
    message_time?: string;
    source_id: number;
}

export interface WireLead {
    contacts: null | { type: string; value: string }[];
    created_at: string;
    delivered_at?: string;
    delivered_run_id?: number;
    display_name?: string;
    evidence: null | WireEvidence[];
    flags?: null | string[];
    flow_id?: number;
    lead_id: number;
    match_reason?: string;
    note?: string;
    origin?: string;
    platform: string;
    platform_user_id: string;
    score: number;
    score_breakdown: { points: number; signal: string }[];
    signals: string[];
    task_id?: number;
    tier: string;
    username?: string;
}

export interface WireRun {
    created_at?: string;
    deadline_at?: string;
    delivered_count: number;
    finished_at?: string;
    flow_id?: number;
    found: number;
    id: number;
    notify_error?: string;
    notify_status?: string;
    round: number;
    schedule_id?: number;
    sources: GrowthRunSources;
    status: string;
    target: number;
    task_id?: number;
    watch: number;
}

export interface WireSource {
    join_status: string;
    joinable?: boolean;
    kind: string;
    last_synced_at?: string;
    link?: string;
    member_count?: number;
    note?: string;
    origin: string;
    platform: string;
    qualified_leads: number;
    source_id: number;
    title?: string;
    username?: string;
}

export interface WireSourceAddResult {
    input: string;
    reason?: string;
    result: string;
    source?: WireSource;
}

const JOIN_STATUS: Record<string, GrowthSourceJoinStatus> = {
    not_joinable: 'unjoinable',
    pending: 'pending_approval',
};

// A server that does not say whether a source can be joined: only these platforms have groups to join.
const JOINABLE_PLATFORMS = new Set(['facebook', 'telegram']);

const RUN_STATUS: Record<string, GrowthRunStatus> = {
    completed: 'delivered',
    partial: 'missed',
};

const TIER: Record<string, GrowthLeadTier> = { watchlist: 'watch' };

export const toWireTier = (tier: GrowthLeadTier): string => (tier === 'watch' ? 'watchlist' : tier);

export const fromWireSource = (source: WireSource): GrowthSource => ({
    created_at: '',
    id: source.source_id,
    join_status: !(source.joinable ?? JOINABLE_PLATFORMS.has(source.platform))
        ? 'direct'
        : (JOIN_STATUS[source.join_status] ?? (source.join_status as GrowthSourceJoinStatus)),
    kind: source.kind === 'channel' ? 'channel' : 'group',
    last_read_at: source.last_synced_at ?? null,
    leads_count: source.qualified_leads,
    link: source.link ?? '',
    members_count: source.member_count ?? null,
    note: source.note ?? '',
    origin: source.origin as GrowthSourceOrigin,
    platform: source.platform as GrowthPlatform,
    source_id: source.username ?? source.link ?? '',
    title: source.title || source.username || source.link || '',
});

export const fromWireSourceAddResult = (result: WireSourceAddResult): GrowthSourceAddResult => ({
    link: result.input,
    outcome: result.result as GrowthSourceAddOutcome,
    source: result.source ? fromWireSource(result.source) : undefined,
});

export const fromWireRun = (run: WireRun): GrowthRun => ({
    current_action: null,
    deadline_at: run.deadline_at ?? null,
    delivered: run.delivered_count,
    error: null,
    finished_at: run.finished_at ?? null,
    flood_wait_seconds: null,
    flow_id: run.flow_id ?? 0,
    found: run.found,
    notify_error: run.notify_error || null,
    notify_status: (run.notify_status || null) as GrowthRun['notify_status'],
    round: run.round,
    schedule_id: run.schedule_id ?? null,
    sources: run.sources,
    started_at: run.created_at ?? '',
    status: RUN_STATUS[run.status] ?? (run.status as GrowthRunStatus),
    target: run.target > 0 ? run.target : null,
    task_id: run.task_id ?? 0,
    watch: run.watch,
});

// Runs and sources supply what a lead row names by id: the task of its delivery, its round, the source title.
export const fromWireLeads = (leads: WireLead[], runs: WireRun[], sources: GrowthSource[]): GrowthLead[] => {
    const titles = new Map(sources.map((source) => [source.id, source.title]));
    const taskOfRun = new Map(runs.map((run) => [run.id, run.task_id ?? null]));
    const roundOfTask = new Map(runs.map((run) => [run.task_id, run.round]));

    return leads.map((lead) => {
        const evidence: GrowthLeadEvidence[] = (lead.evidence ?? []).map((item, index) => ({
            excerpt: item.excerpt,
            id: index,
            message_id: item.message_id,
            message_url: item.message_link ?? '',
            posted_at: item.message_time ?? item.captured_at,
            reason: item.match_reason || lead.match_reason || '',
            source_id: item.source_id,
            source_title: titles.get(item.source_id) ?? '',
        }));
        const newest = evidence.reduce<GrowthLeadEvidence | null>(
            (latest, item) => (!latest || item.posted_at > latest.posted_at ? item : latest),
            null,
        );
        const sourceId = newest?.source_id ?? 0;

        return {
            contacts: (lead.contacts ?? []) as GrowthLead['contacts'],
            created_at: lead.created_at,
            delivered_at: lead.delivered_at ?? null,
            delivered_task_id:
                lead.delivered_run_id === undefined ? null : (taskOfRun.get(lead.delivered_run_id) ?? null),
            display_name: lead.display_name || lead.username || lead.platform_user_id,
            evidence,
            first_flow_id: lead.flow_id ?? 0,
            first_flow_title: '',
            first_round: lead.task_id === undefined ? 0 : (roundOfTask.get(lead.task_id) ?? 0),
            first_task_id: lead.task_id ?? 0,
            flags: lead.flags ?? [],
            id: lead.lead_id,
            last_active_at: newest?.posted_at ?? null,
            match_reason: lead.match_reason || undefined,
            note: lead.note ?? '',
            origin: lead.origin === 'manual' ? 'manual' : 'ai',
            platform: lead.platform as GrowthPlatform,
            platform_user_id: lead.platform_user_id,
            profile: null,
            score: lead.score,
            score_breakdown: lead.score_breakdown,
            signals: lead.signals,
            source_id: sourceId,
            source_title: titles.get(sourceId) ?? '',
            tier: TIER[lead.tier] ?? (lead.tier as GrowthLeadTier),
            username: lead.username ?? null,
        };
    });
};
