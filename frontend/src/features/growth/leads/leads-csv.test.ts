import { describe, expect, it } from 'vitest';

import type { GrowthLead } from '../growth-types';

import { buildLeadsCsv, leadsCsvFileName } from './leads-csv';

const evidence = (n: number) => ({
    excerpt: `quote ${n}, with "marks"`,
    id: n,
    message_id: String(n),
    message_url: `https://t.me/g/${n}`,
    posted_at: '2026-10-01T08:30:00Z',
    reason: '',
    source_id: 1,
    source_title: 'Group',
});

const lead: GrowthLead = {
    contacts: [
        { type: 'phone', value: '+66 81 234 5678' },
        { type: 'line', value: 'jay88' },
    ],
    delivered_at: null,
    delivered_task_id: null,
    display_name: '阿杰',
    evidence: [evidence(1), evidence(2), evidence(3), evidence(4)],
    first_flow_id: 1,
    first_flow_title: 'Project',
    first_round: 2,
    first_task_id: 1,
    id: 1,
    last_active_at: null,
    note: '',
    origin: 'agent',
    platform: 'telegram',
    platform_user_id: '700001',
    profile: null,
    score: 9,
    score_breakdown: [],
    signals: ['player', 'paying'],
    source_id: 1,
    source_title: 'Group',
    tier: 'qualified',
    username: 'jay',
};

describe('buildLeadsCsv', () => {
    const csv = buildLeadsCsv([lead], new Map([[1, 'https://t.me/g']]));
    const [header, row] = csv.slice(1).trimEnd().split('\r\n');

    it('starts with a BOM so Excel reads UTF-8', () => {
        expect(csv.startsWith('﻿')).toBe(true);
    });

    it('exports at most three pieces of evidence', () => {
        expect(header).toContain('证据 3 原话');
        expect(header).not.toContain('证据 4');
        expect(row).not.toContain('quote 4');
    });

    it('quotes cells that carry commas or quotes', () => {
        expect(row).toContain('"quote 1, with ""marks"""');
    });

    it('writes the row in header order', () => {
        expect(
            row?.startsWith(
                '阿杰,@jay,telegram,9,合格,属于目标人群、有成交迹象,手机号：+66 81 234 5678; Line：jay88,Group,https://t.me/g,',
            ),
        ).toBe(true);
    });
});

describe('leadsCsvFileName', () => {
    it('names the business and the time, without characters a file system rejects', () => {
        expect(leadsCsvFileName('Star/Era', new Date(2026, 9, 16, 9, 5))).toBe('潜客_Star_Era_20261016-0905.csv');
    });

    it('falls back when there is no business profile', () => {
        expect(leadsCsvFileName(undefined, new Date(2026, 9, 16, 9, 5))).toBe('潜客_未命名业务_20261016-0905.csv');
    });
});
