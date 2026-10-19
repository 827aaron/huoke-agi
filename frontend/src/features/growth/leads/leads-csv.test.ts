import { describe, expect, it } from 'vitest';

import { dictionaries } from '@/lib/copy';

import type { GrowthLead } from '../growth-types';

import spec from '../../../../../backend/pkg/growth/leadscsv.json';
import { buildLeadsCsv, LEADS_CSV_COLUMNS, leadsCsvFileName } from './leads-csv';

// Zone-less timestamps read the same clock under any TZ.
const evidence = (n: number, postedAt: string) => ({
    excerpt: `quote ${n}, with "marks"`,
    id: n,
    message_id: String(n),
    message_url: `https://t.me/g/${n}`,
    posted_at: postedAt,
    reason: '',
    source_id: n,
    source_title: `Group ${n}`,
});

const lead: GrowthLead = {
    contacts: [
        { type: 'phone', value: '+66 81 234 5678' },
        { type: 'line', value: 'jay88' },
    ],
    created_at: '2026-10-15T08:00:00Z',
    delivered_at: null,
    delivered_task_id: null,
    display_name: '阿杰',
    evidence: [evidence(1, '2026-10-01T08:30:00'), evidence(2, '2026-10-03T09:45:00')],
    first_flow_id: 1,
    first_flow_title: 'Project',
    first_round: 2,
    first_task_id: 1,
    id: 1,
    last_active_at: null,
    match_reason: '在群里问哪个平台返水高，带量进场',
    note: '',
    origin: 'ai',
    platform: 'telegram',
    platform_user_id: '700001',
    profile: null,
    score: 18,
    score_breakdown: [],
    signals: ['player', 'agent_identity', 'active'],
    source_id: 1,
    source_title: 'Group 1',
    tier: 'qualified',
    username: 'jay',
};

const rows = (csv: string) => csv.slice(1).trimEnd().split('\r\n');

describe('buildLeadsCsv', () => {
    const csv = buildLeadsCsv([lead], {
        signalLabel: (signal) =>
            ({ active: '近期活跃', agent_identity: '代理身份', player: '玩家资源与量' })[signal] ?? signal,
    });
    const [header, row] = rows(csv);

    it('starts with a BOM so Excel reads UTF-8', () => {
        expect(csv.startsWith('\uFEFF')).toBe(true);
    });

    it('names the columns as the list page does, contacts then profile then provenance', () => {
        expect(header).toBe('昵称,用户名,平台,联系方式,评分,信号,画像,来源,原话,原文链接,发言时间');
    });

    it('writes the lead as the list page shows it, with its newest quote', () => {
        expect(row).toBe(
            [
                '阿杰',
                '@jay',
                'Telegram',
                '手机号：+66 81 234 5678; Line：jay88',
                '18 · 合格',
                '玩家资源与量、代理身份、近期活跃',
                '在群里问哪个平台返水高，带量进场',
                'Group 2',
                '"quote 2, with ""marks"""',
                'https://t.me/g/2',
                '2026-10-03 09:45',
            ].join(','),
        );
    });

    it('falls back to the built-in signal labels and the raw key', () => {
        const [, cells] = rows(buildLeadsCsv([lead]));

        expect(cells).toContain('属于目标人群、agent_identity、近期活跃');
    });

    it('reaches a lead without contacts through its handle or profile link', () => {
        const telegram = { ...lead, contacts: [] };
        const web = {
            ...lead,
            contacts: [],
            platform: 'web' as const,
            platform_user_id: 'forum.example.com/u/7',
            username: null,
        };
        const [, telegramRow, webRow] = rows(buildLeadsCsv([telegram, web]));

        expect(telegramRow).toContain(',https://t.me/jay,');
        expect(webRow).toContain(',forum.example.com/u/7,网页 / 论坛,https://forum.example.com/u/7,');
    });

    it('links the quote to its source when the message itself has no link', () => {
        const unlinked = { ...lead, evidence: [{ ...evidence(3, '2026-10-05T00:00:00'), message_url: '' }] };
        const [, cells] = rows(buildLeadsCsv([unlinked], { sourceLinks: new Map([[3, 'https://t.me/group3']]) }));

        expect(cells).toContain(',https://t.me/group3,');
    });

    it('leaves the provenance cells empty for a lead without evidence', () => {
        const manual = { ...lead, evidence: [], match_reason: undefined, source_title: '' };
        const [, cells] = rows(buildLeadsCsv([manual]));

        expect(cells?.endsWith(',属于目标人群、agent_identity、近期活跃,,,,,')).toBe(true);
    });
});

// The e-mail attachment and the REST export write the same sheet from this spec; a label
// changed on one side only shows up here.
describe('the backend sheet spec', () => {
    type Table = Record<string, Record<string, string>>;
    const languages = [
        ['chinese', dictionaries.zh],
        ['english', dictionaries.en],
    ] as const;
    const pick = (table: Table, language: string) =>
        Object.fromEntries(Object.entries(table).map(([key, texts]) => [key, texts[language]]));

    it('orders the columns as the web export does', () => {
        expect(spec.columns).toEqual([...LEADS_CSV_COLUMNS]);
    });

    it.each(languages)('carries the %s labels of copy.ts', (language, dictionary) => {
        expect(pick(spec.header, language)).toEqual(dictionary.export.header);
        expect(pick(spec.tier, language)).toEqual({
            qualified: dictionary.leads.tier.qualified,
            watchlist: dictionary.leads.tier.watch,
        });
        expect(pick(spec.signal, language)).toEqual(dictionary.leads.signal);
        expect(pick(spec.platform, language)).toEqual(dictionary.telegram.health.platform);
        expect(pick(spec.contact, language)).toEqual(dictionary.profile.contactType);
        expect(dictionary.leads.contactText('T', 'V')).toBe(`T${spec.contactSeparator[language]}V`);
        expect(dictionary.export.fileName('B', 'S', 'D')).toBe(`${spec.file.prefix[language]}_B_S_D.csv`);
        expect(spec.file.round[language].replace('%d', '7')).toBe(dictionary.export.roundScope(7));
        expect(spec.file.filtered[language]).toBe(dictionary.export.filtered);
        expect(spec.file.delivered[language]).toBe(dictionary.export.allDelivered);
        expect(spec.file.unnamed[language]).toBe(dictionary.export.unnamedBusiness);
    });
});

describe('leadsCsvFileName', () => {
    it('names the business, the scope and the time, without characters a file system rejects', () => {
        expect(leadsCsvFileName('Star/Era', '第1轮', new Date(2026, 9, 16, 9, 5))).toBe(
            '潜客_Star_Era_第1轮_20261016-0905.csv',
        );
    });

    it('falls back when there is no business profile', () => {
        expect(leadsCsvFileName(undefined, '当前筛选结果', new Date(2026, 9, 16, 9, 5))).toBe(
            '潜客_未命名业务_当前筛选结果_20261016-0905.csv',
        );
    });
});
