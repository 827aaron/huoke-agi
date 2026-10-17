import { describe, expect, it } from 'vitest';

import type { GrowthLead } from '../growth-types';

import { EMPTY_LEAD_FILTERS, filterLeads, sortLeads } from './leads-filter';

const NOW = Date.UTC(2026, 9, 16, 12, 0, 0);

const lead = (id: number, patch: Partial<GrowthLead> = {}): GrowthLead => ({
    delivered_at: null,
    delivered_task_id: null,
    display_name: `Player ${id}`,
    evidence: [],
    first_flow_id: 1,
    first_flow_title: 'Round',
    first_round: 1,
    first_task_id: 1,
    id,
    last_active_at: null,
    note: '',
    origin: 'ai',
    platform: 'telegram',
    platform_user_id: String(id),
    profile: null,
    score: 7,
    score_breakdown: [],
    signals: [],
    source_id: 1,
    source_title: 'Group',
    tier: 'qualified',
    username: null,
    ...patch,
});

const ids = (leads: GrowthLead[]) => leads.map(({ id }) => id);

describe('filterLeads', () => {
    const leads = [
        lead(1, {
            contacts: [{ type: 'email', value: 'ace@example.com' }],
            delivered_at: '2026-10-15T00:00:00Z',
            signals: ['paying'],
            username: 'ace',
        }),
        lead(2, { last_active_at: '2026-10-14T00:00:00Z', platform: 'facebook', source_id: 2, tier: 'watch' }),
        lead(3, {
            evidence: [
                {
                    excerpt: 'Looking for a new SLG',
                    id: 1,
                    message_id: '1',
                    message_url: 'https://t.me/g/1',
                    posted_at: '2026-10-01T00:00:00Z',
                    reason: '',
                    source_id: 1,
                    source_title: 'Group',
                },
            ],
            last_active_at: '2026-09-01T00:00:00Z',
            signals: ['competitor', 'paying'],
        }),
    ];

    it.each([
        ['no filter keeps everyone', {}, [1, 2, 3]],
        ['tier', { tier: 'watch' as const }, [2]],
        ['delivered', { delivery: 'delivered' as const }, [1]],
        ['not delivered', { delivery: 'not_delivered' as const }, [2, 3]],
        ['source', { sourceId: 2 }, [2]],
        ['platform', { platform: 'facebook' as const }, [2]],
        ['with a contact', { contact: 'with' as const }, [1]],
        ['platform account only', { contact: 'without' as const }, [2, 3]],
        ['every flag must hold', { flags: ['paying' as const, 'competitor' as const] }, [3]],
        ['recent means within seven days', { flags: ['recent' as const] }, [2]],
        ['search matches the username', { search: 'ACE' }, [1]],
        ['search matches a quote', { search: 'new slg' }, [3]],
    ])('%s', (_, patch, expected) => {
        expect(ids(filterLeads(leads, { ...EMPTY_LEAD_FILTERS, ...patch }, NOW))).toEqual(expected);
    });
});

describe('sortLeads', () => {
    it('orders by score, then by the latest activity', () => {
        const sorted = sortLeads([
            lead(1, { last_active_at: '2026-10-01T00:00:00Z', score: 8 }),
            lead(2, { last_active_at: null, score: 9 }),
            lead(3, { last_active_at: '2026-10-10T00:00:00Z', score: 8 }),
        ]);

        expect(ids(sorted)).toEqual([2, 3, 1]);
    });
});
