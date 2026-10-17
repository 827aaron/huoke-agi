import type {
    GrowthLead,
    GrowthLeadEvidence,
    GrowthLeadTier,
    GrowthPlatform,
    GrowthRun,
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
    flow_id?: number;
    lead_id: number;
    match_reason?: string;
    platform: string;
    platform_user_id: string;
    score: number;
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
    id: number;
    notify_error?: string;
    notify_status?: string;
    schedule_id?: number;
    status: string;
    target: number;
    task_id?: number;
}

export interface WireSource {
    join_status: string;
    kind: string;
    last_synced_at?: string;
    link?: string;
    member_count?: number;
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

const RUN_STATUS: Record<string, GrowthRunStatus> = {
    completed: 'delivered',
    partial: 'missed',
};

const TIER: Record<string, GrowthLeadTier> = { watchlist: 'watch' };

export const fromWireSource = (source: WireSource): GrowthSource => ({
    created_at: '',
    id: source.source_id,
    join_status: JOIN_STATUS[source.join_status] ?? (source.join_status as GrowthSourceJoinStatus),
    kind: source.kind === 'channel' ? 'channel' : 'group',
    last_read_at: source.last_synced_at ?? null,
    leads_count: source.qualified_leads,
    link: source.link ?? '',
    members_count: source.member_count ?? null,
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

// Rounds are numbered by their task, oldest first.
export const fromWireRuns = (runs: WireRun[], leads: WireLead[]): GrowthRun[] => {
    const rounds = new Map(
        [...runs].sort((a, b) => (a.task_id ?? 0) - (b.task_id ?? 0)).map((run, index) => [run.id, index + 1]),
    );

    return runs.map((run) => {
        const found = leads.filter((lead) => lead.task_id !== undefined && lead.task_id === run.task_id);

        return {
            current_action: null,
            deadline_at: run.deadline_at ?? null,
            delivered: run.delivered_count,
            error: null,
            finished_at: run.finished_at ?? null,
            flood_wait_seconds: null,
            flow_id: run.flow_id ?? 0,
            found: found.filter((lead) => lead.tier === 'qualified').length,
            notify_error: run.notify_error || null,
            notify_status: (run.notify_status || null) as GrowthRun['notify_status'],
            round: rounds.get(run.id) ?? 0,
            schedule_id: run.schedule_id ?? null,
            sources: null,
            started_at: run.created_at ?? '',
            status: RUN_STATUS[run.status] ?? (run.status as GrowthRunStatus),
            target: run.target > 0 ? run.target : null,
            task_id: run.task_id ?? 0,
            watch: found.filter((lead) => lead.tier === 'watchlist').length,
        };
    });
};

// Runs and sources supply what a lead row names by id: the task of its delivery, its round, the source title.
export const fromWireLeads = (
    leads: WireLead[],
    runs: GrowthRun[],
    wireRuns: WireRun[],
    sources: GrowthSource[],
): GrowthLead[] => {
    const titles = new Map(sources.map((source) => [source.id, source.title]));
    const taskOfRun = new Map(wireRuns.map((run) => [run.id, run.task_id ?? null]));
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
            delivered_at: lead.delivered_at ?? null,
            delivered_task_id:
                lead.delivered_run_id === undefined ? null : (taskOfRun.get(lead.delivered_run_id) ?? null),
            display_name: lead.display_name || lead.username || lead.platform_user_id,
            evidence,
            first_flow_id: lead.flow_id ?? 0,
            first_flow_title: '',
            first_round: lead.task_id === undefined ? 0 : (roundOfTask.get(lead.task_id) ?? 0),
            first_task_id: lead.task_id ?? 0,
            id: lead.lead_id,
            last_active_at: newest?.posted_at ?? null,
            platform: lead.platform as GrowthPlatform,
            platform_user_id: lead.platform_user_id,
            profile: null,
            score: lead.score,
            score_breakdown: [],
            signals: [],
            source_id: sourceId,
            source_title: titles.get(sourceId) ?? '',
            tier: TIER[lead.tier] ?? (lead.tier as GrowthLeadTier),
            username: lead.username ?? null,
        };
    });
};
