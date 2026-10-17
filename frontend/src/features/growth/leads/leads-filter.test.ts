import { describe, expect, it } from 'vitest';

import type { GrowthLead } from '../growth-types';

import { EMPTY_LEAD_FILTERS, filterLeads, groupLeadsByDay, sortLeads } from './leads-filter';

const NOW = Date.UTC(2026, 9, 16, 12, 0, 0);

// Day boundaries are the browser's, so these instants are built in the local zone.
const LOCAL_NOW = new Date(2026, 9, 16, 12, 0).getTime();
const local = (month: number, day: number, hour: number, minute = 0) =>
    new Date(2026, month - 1, day, hour, minute).toISOString();

const lead = (id: number, patch: Partial<GrowthLead> = {}): GrowthLead => ({
    created_at: '2026-10-16T08:00:00Z',
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

describe('filterLeads by discovery date and score', () => {
    const leads = [
        lead(1, { created_at: local(10, 16, 0, 5), score: 9 }),
        lead(2, { created_at: local(10, 15, 23, 55), score: 7 }),
        lead(3, { created_at: local(10, 10, 0, 0), score: 5, tier: 'watch' }),
        lead(4, { created_at: local(10, 9, 23, 59), score: 4, tier: 'watch' }),
        lead(5, { created_at: local(9, 17, 0, 0), score: 8 }),
        lead(6, { created_at: local(9, 16, 23, 59), score: 10 }),
    ];

    it.each([
        ['all dates', { foundWithin: 'all' as const }, [1, 2, 3, 4, 5, 6]],
        ['today starts at local midnight', { foundWithin: 'today' as const }, [1]],
        ['last 7 days counts today as the first', { foundWithin: '7d' as const }, [1, 2, 3]],
        ['last 30 days', { foundWithin: '30d' as const }, [1, 2, 3, 4, 5]],
        ['minimum score is inclusive', { minScore: 7 }, [1, 2, 5, 6]],
        ['date and score together', { foundWithin: '7d' as const, minScore: 7 }, [1, 2]],
    ])('%s', (_, patch, expected) => {
        expect(ids(filterLeads(leads, { ...EMPTY_LEAD_FILTERS, ...patch }, LOCAL_NOW))).toEqual(expected);
    });
});

describe('groupLeadsByDay', () => {
    it('splits at local midnight, newest day first, and counts each tier', () => {
        const groups = groupLeadsByDay([
            lead(1, { created_at: local(10, 15, 23, 55), tier: 'watch' }),
            lead(2, { created_at: local(10, 16, 0, 5) }),
            lead(3, { created_at: local(10, 15, 0, 0) }),
            lead(4, { created_at: local(10, 16, 23, 59), tier: 'watch' }),
            lead(5, { created_at: local(10, 1, 9, 0) }),
        ]);

        expect(
            groups.map(({ day, key, leads, qualified, watch }) => [key, day.getDate(), ids(leads), qualified, watch]),
        ).toEqual([
            ['2026-10-16', 16, [2, 4], 1, 1],
            ['2026-10-15', 15, [1, 3], 1, 1],
            ['2026-10-01', 1, [5], 1, 0],
        ]);
    });
});

describe('sortLeads', () => {
    it('puts the newest find first when asked, the score breaking a tie', () => {
        const sorted = sortLeads(
            [
                lead(1, { created_at: '2026-10-15T08:00:00Z', score: 10 }),
                lead(2, { created_at: '2026-10-16T08:00:00Z', score: 6 }),
                lead(3, { created_at: '2026-10-16T08:00:00Z', score: 8 }),
            ],
            'newest',
        );

        expect(ids(sorted)).toEqual([3, 2, 1]);
    });

    it('orders by score, then by the latest activity', () => {
        const sorted = sortLeads([
            lead(1, { last_active_at: '2026-10-01T00:00:00Z', score: 8 }),
            lead(2, { last_active_at: null, score: 9 }),
            lead(3, { last_active_at: '2026-10-10T00:00:00Z', score: 8 }),
        ]);

        expect(ids(sorted)).toEqual([2, 3, 1]);
    });
});
