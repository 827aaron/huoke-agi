import { format, startOfDay, subDays } from 'date-fns';

import type { GrowthLead, GrowthLeadTier, GrowthPlatform } from '../growth-types';

export type LeadContactFilter = 'all' | 'with' | 'without';

export type LeadDeliveryFilter = 'all' | 'delivered' | 'not_delivered';

export interface LeadFilters {
    contact: LeadContactFilter;
    delivery: LeadDeliveryFilter;
    flags: LeadFlag[];
    foundWithin: LeadFoundWithin;
    minScore: null | number;
    platform: GrowthPlatform | null;
    round: null | number;
    search: string;
    sort: LeadSort;
    sourceId: null | number;
    tier: 'all' | GrowthLeadTier;
}

export type LeadFlag = 'pain' | 'paying' | 'recent';

// Calendar days in the browser's zone, today included.
export type LeadFoundWithin = '7d' | '30d' | 'all' | 'today';

export type LeadSort = 'newest' | 'score';

export const EMPTY_LEAD_FILTERS: LeadFilters = {
    contact: 'all',
    delivery: 'all',
    flags: [],
    foundWithin: 'all',
    minScore: null,
    platform: null,
    round: null,
    search: '',
    sort: 'score',
    sourceId: null,
    tier: 'all',
};

const FOUND_WITHIN_DAYS: Record<Exclude<LeadFoundWithin, 'all'>, number> = { '7d': 7, '30d': 30, today: 1 };

const foundSince = (within: LeadFoundWithin, now: number): number =>
    within === 'all' ? -Infinity : startOfDay(subDays(now, FOUND_WITHIN_DAYS[within] - 1)).getTime();

const RECENT_MS = 7 * 24 * 60 * 60 * 1000;

const hasFlag = (lead: GrowthLead, flag: LeadFlag, now: number): boolean => {
    if (flag === 'recent') {
        return !!lead.last_active_at && now - new Date(lead.last_active_at).getTime() <= RECENT_MS;
    }

    return lead.signals.includes(flag);
};

export function filterLeads(leads: GrowthLead[], filters: LeadFilters, now = Date.now()): GrowthLead[] {
    const search = filters.search.trim().toLowerCase();
    const since = foundSince(filters.foundWithin, now);

    return leads.filter((lead) => {
        if (new Date(lead.created_at).getTime() < since) {
            return false;
        }

        if (filters.minScore !== null && lead.score < filters.minScore) {
            return false;
        }

        if (filters.tier !== 'all' && lead.tier !== filters.tier) {
            return false;
        }

        if (filters.delivery === 'delivered' && !lead.delivered_at) {
            return false;
        }

        if (filters.delivery === 'not_delivered' && lead.delivered_at) {
            return false;
        }

        if (filters.contact !== 'all' && !!lead.contacts?.length !== (filters.contact === 'with')) {
            return false;
        }

        if (filters.platform !== null && lead.platform !== filters.platform) {
            return false;
        }

        if (filters.round !== null && lead.first_round !== filters.round) {
            return false;
        }

        if (filters.sourceId !== null && lead.source_id !== filters.sourceId) {
            return false;
        }

        if (!filters.flags.every((flag) => hasFlag(lead, flag, now))) {
            return false;
        }

        if (!search) {
            return true;
        }

        return (
            lead.display_name.toLowerCase().includes(search) ||
            !!lead.username?.toLowerCase().includes(search) ||
            lead.evidence.some((evidence) => evidence.excerpt.toLowerCase().includes(search))
        );
    });
}

const activeTime = (lead: GrowthLead): number => (lead.last_active_at ? new Date(lead.last_active_at).getTime() : 0);

const createdTime = (lead: GrowthLead): number => new Date(lead.created_at).getTime();

export const sortLeads = (leads: GrowthLead[], sort: LeadSort = 'score'): GrowthLead[] =>
    [...leads].sort((a, b) =>
        sort === 'newest'
            ? createdTime(b) - createdTime(a) || b.score - a.score
            : b.score - a.score || activeTime(b) - activeTime(a),
    );

export interface LeadDayGroup {
    // Midnight of the day in the browser's zone.
    day: Date;
    key: string;
    leads: GrowthLead[];
    qualified: number;
    // The rounds that found the day's leads, ascending; a lead added by hand has none.
    rounds: number[];
    watch: number;
}

// Newest day first; the leads keep the order they came in.
export function groupLeadsByDay(leads: GrowthLead[]): LeadDayGroup[] {
    const groups = new Map<string, LeadDayGroup>();

    for (const lead of leads) {
        const created = new Date(lead.created_at);
        const key = format(created, 'yyyy-MM-dd');
        let group = groups.get(key);

        if (!group) {
            group = { day: startOfDay(created), key, leads: [], qualified: 0, rounds: [], watch: 0 };
            groups.set(key, group);
        }

        group.leads.push(lead);
        group[lead.tier] += 1;

        if (lead.first_round > 0 && !group.rounds.includes(lead.first_round)) {
            group.rounds.push(lead.first_round);
        }
    }

    for (const group of groups.values()) {
        group.rounds.sort((a, b) => a - b);
    }

    return [...groups.values()].sort((a, b) => b.day.getTime() - a.day.getTime());
}
