import type { GrowthLead, GrowthLeadTier, GrowthPlatform } from '../growth-types';

export type LeadDeliveryFilter = 'all' | 'delivered' | 'not_delivered';

export interface LeadFilters {
    delivery: LeadDeliveryFilter;
    flags: LeadFlag[];
    platform: GrowthPlatform | null;
    search: string;
    sourceId: null | number;
    tier: 'all' | GrowthLeadTier;
}

export type LeadFlag = 'competitor' | 'paying' | 'recent';

export const EMPTY_LEAD_FILTERS: LeadFilters = {
    delivery: 'all',
    flags: [],
    platform: null,
    search: '',
    sourceId: null,
    tier: 'all',
};

const RECENT_MS = 7 * 24 * 60 * 60 * 1000;

const hasFlag = (lead: GrowthLead, flag: LeadFlag, now: number): boolean => {
    if (flag === 'recent') {
        return !!lead.last_active_at && now - new Date(lead.last_active_at).getTime() <= RECENT_MS;
    }

    return lead.signals.includes(flag);
};

export function filterLeads(leads: GrowthLead[], filters: LeadFilters, now = Date.now()): GrowthLead[] {
    const search = filters.search.trim().toLowerCase();

    return leads.filter((lead) => {
        if (filters.tier !== 'all' && lead.tier !== filters.tier) {
            return false;
        }

        if (filters.delivery === 'delivered' && !lead.delivered_at) {
            return false;
        }

        if (filters.delivery === 'not_delivered' && lead.delivered_at) {
            return false;
        }

        if (filters.platform !== null && lead.platform !== filters.platform) {
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

export const sortLeads = (leads: GrowthLead[]): GrowthLead[] =>
    [...leads].sort((a, b) => b.score - a.score || activeTime(b) - activeTime(a));
