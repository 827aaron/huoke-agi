import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router-dom';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import type { GrowthRun } from './growth-types';

import { proxyRouteText } from './account-health';
import RemoteLoginDialog from './accounts/remote-login-dialog';
import { growthApi } from './growth-api';
import GrowthProgressBar from './progress/growth-progress-bar';

const server = vi.hoisted(() => ({ delete: vi.fn(), get: vi.fn(), post: vi.fn(), put: vi.fn() }));
const flow = vi.hoisted(() => ({ run: null as GrowthRun | null }));

vi.mock('@/lib/axios', async (importOriginal) => ({
    ...(await importOriginal<typeof import('@/lib/axios')>()),
    api: server,
}));
vi.mock('./growth-flow-provider', () => ({ useGrowthFlow: () => ({ accounts: [], run: flow.run }) }));
vi.mock('@/providers/flow-provider', () => ({ useFlow: () => ({ flowData: null }) }));

const ok = (data: unknown) => ({ data, status: 'success' });

// Payloads as backend/pkg/server/services/growth.go writes them for a flow in its second round.
const pool = { exhausted: 0, invalid: 1, joined: 1, not_joinable: 1, pending: 1, read: 2, total: 6, unresolved: 2 };

const runs = [
    {
        created_at: '2026-10-17T14:00:00Z',
        delivered_count: 2,
        finished_at: '2026-10-17T14:30:00Z',
        flow_id: 1,
        found: 2,
        id: 7,
        notify_status: 'skipped',
        round: 1,
        sources: pool,
        status: 'partial',
        target: 5,
        task_id: 11,
        watch: 0,
    },
    {
        created_at: '2026-10-17T14:39:37Z',
        delivered_count: 0,
        flow_id: 1,
        found: 4,
        id: 9,
        round: 2,
        sources: pool,
        status: 'running',
        target: 50,
        task_id: 12,
        watch: 3,
    },
];

const leads = [
    {
        contacts: [{ type: 'line', value: 'jay88' }],
        created_at: '2026-10-17T14:10:00Z',
        delivered_at: '2026-10-17T14:30:00Z',
        delivered_run_id: 7,
        display_name: 'Jay',
        evidence: [
            {
                captured_at: '2026-10-17T14:10:00Z',
                excerpt: 'any slot site paying fast?',
                message_id: '501',
                message_time: '2026-10-16T09:00:00Z',
                source_id: 3,
            },
            {
                captured_at: '2026-10-17T14:12:00Z',
                excerpt: 'need a new site',
                match_reason: 'asks for a new site',
                message_id: '502',
                message_link: 'https://t.me/myslots/502',
                message_time: '2026-10-17T08:00:00Z',
                source_id: 4,
            },
        ],
        flow_id: 1,
        lead_id: 21,
        match_reason: 'wants a site that pays out',
        platform: 'telegram',
        platform_user_id: '9001',
        score: 9,
        score_breakdown: [
            { points: 3, signal: 'player' },
            { points: 2, signal: 'active' },
        ],
        signals: ['player', 'active'],
        task_id: 11,
        tier: 'qualified',
        username: 'jay88',
    },
    {
        contacts: null,
        created_at: '2026-10-17T14:40:00Z',
        evidence: null,
        flow_id: 1,
        lead_id: 22,
        platform: 'web',
        platform_user_id: 'u-77',
        score: 5,
        score_breakdown: [],
        signals: [],
        task_id: 12,
        tier: 'watchlist',
    },
    {
        contacts: [],
        created_at: '2026-10-17T14:41:00Z',
        evidence: [],
        flow_id: 1,
        lead_id: 23,
        note: 'met at the Bangkok expo',
        origin: 'manual',
        platform: 'telegram',
        platform_user_id: '9003',
        score: 8,
        score_breakdown: [],
        signals: [],
        task_id: 12,
        tier: 'qualified',
    },
];

const sources = [
    {
        join_status: 'not_joinable',
        kind: 'forum',
        link: 'https://forum.example.my/slots',
        origin: 'manual',
        platform: 'web',
        qualified_leads: 0,
        source_id: 3,
        title: 'MY slots forum',
    },
    {
        join_status: 'pending',
        kind: 'group',
        last_synced_at: '2026-10-17T14:12:00Z',
        link: 'https://t.me/myslots',
        member_count: 1200,
        note: 'busy at night',
        origin: 'ai',
        platform: 'telegram',
        qualified_leads: 1,
        source_id: 4,
        username: 'myslots',
    },
];

const serve = (body: { leads: unknown; runs: unknown; sources: unknown }) =>
    server.get.mockImplementation(async (url: string) => {
        if (url === '/growth/leads/') {
            return ok({ leads: body.leads, total: 0 });
        }

        if (url === '/growth/runs/') {
            return ok({ runs: body.runs, total: 0 });
        }

        if (url === '/growth/sources/') {
            return ok({ sources: body.sources, total: 0, warnings: null });
        }

        throw new Error(url);
    });

describe('growth wire', () => {
    beforeEach(() => {
        vi.clearAllMocks();
        serve({ leads, runs, sources });
    });

    it('reads the rounds with the number, lead counts and pool the server sends', async () => {
        expect(await growthApi.listRuns(1)).toEqual([
            {
                current_action: null,
                deadline_at: null,
                delivered: 2,
                error: null,
                finished_at: '2026-10-17T14:30:00Z',
                flood_wait_seconds: null,
                flow_id: 1,
                found: 2,
                notify_error: null,
                notify_status: 'skipped',
                round: 1,
                schedule_id: null,
                sources: pool,
                started_at: '2026-10-17T14:00:00Z',
                status: 'missed',
                target: 5,
                task_id: 11,
                watch: 0,
            },
            {
                current_action: null,
                deadline_at: null,
                delivered: 0,
                error: null,
                finished_at: null,
                flood_wait_seconds: null,
                flow_id: 1,
                found: 4,
                notify_error: null,
                notify_status: null,
                round: 2,
                schedule_id: null,
                sources: pool,
                started_at: '2026-10-17T14:39:37Z',
                status: 'running',
                target: 50,
                task_id: 12,
                watch: 3,
            },
        ]);
        expect(server.get).toHaveBeenCalledWith('/growth/runs/', { params: { flow_id: 1 } });
    });

    it('marks a round that reached its target as delivered and one without a target as having none', async () => {
        serve({
            leads: null,
            runs: [
                { ...runs[0], delivered_count: 5, id: 1, status: 'completed', target: 5 },
                { ...runs[1], delivered_count: 0, id: 2, status: 'running', target: 0 },
            ],
            sources: null,
        });

        const [reached, question] = await growthApi.listRuns(1);

        expect([reached?.status, reached?.target, question?.status, question?.target]).toEqual([
            'delivered',
            5,
            'running',
            null,
        ]);
    });

    it('reads the leads with their delivery round and the titles of their sources', async () => {
        const [jay, watched, bare] = await growthApi.listLeads(1);

        expect(jay).toEqual({
            contacts: [{ type: 'line', value: 'jay88' }],
            created_at: '2026-10-17T14:10:00Z',
            delivered_at: '2026-10-17T14:30:00Z',
            delivered_task_id: 11,
            display_name: 'Jay',
            evidence: [
                {
                    excerpt: 'any slot site paying fast?',
                    id: 0,
                    message_id: '501',
                    message_url: '',
                    posted_at: '2026-10-16T09:00:00Z',
                    reason: 'wants a site that pays out',
                    source_id: 3,
                    source_title: 'MY slots forum',
                },
                {
                    excerpt: 'need a new site',
                    id: 1,
                    message_id: '502',
                    message_url: 'https://t.me/myslots/502',
                    posted_at: '2026-10-17T08:00:00Z',
                    reason: 'asks for a new site',
                    source_id: 4,
                    source_title: 'myslots',
                },
            ],
            first_flow_id: 1,
            first_flow_title: '',
            first_round: 1,
            first_task_id: 11,
            id: 21,
            last_active_at: '2026-10-17T08:00:00Z',
            note: '',
            origin: 'ai',
            platform: 'telegram',
            platform_user_id: '9001',
            profile: null,
            score: 9,
            score_breakdown: [
                { points: 3, signal: 'player' },
                { points: 2, signal: 'active' },
            ],
            signals: ['player', 'active'],
            source_id: 4,
            source_title: 'myslots',
            tier: 'qualified',
            username: 'jay88',
        });
        expect(watched).toMatchObject({
            contacts: [],
            delivered_at: null,
            delivered_task_id: null,
            display_name: 'u-77',
            evidence: [],
            first_round: 2,
            last_active_at: null,
            source_title: '',
            tier: 'watch',
            username: null,
        });
        expect(bare).toMatchObject({ id: 23, note: 'met at the Bangkok expo', origin: 'manual' });
    });

    it('reads the sources, a forum as read without joining', async () => {
        expect(await growthApi.listSources()).toEqual({
            sources: [
                {
                    created_at: '',
                    id: 3,
                    join_status: 'direct',
                    kind: 'group',
                    last_read_at: null,
                    leads_count: 0,
                    link: 'https://forum.example.my/slots',
                    members_count: null,
                    note: '',
                    origin: 'manual',
                    platform: 'web',
                    source_id: 'https://forum.example.my/slots',
                    title: 'MY slots forum',
                },
                {
                    created_at: '',
                    id: 4,
                    join_status: 'pending_approval',
                    kind: 'group',
                    last_read_at: '2026-10-17T14:12:00Z',
                    leads_count: 1,
                    link: 'https://t.me/myslots',
                    members_count: 1200,
                    note: 'busy at night',
                    origin: 'ai',
                    platform: 'telegram',
                    source_id: 'myslots',
                    title: 'myslots',
                },
            ],
            total: 0,
            warnings: null,
        });
    });

    it('takes which sources can be joined from the server when it says so', async () => {
        serve({
            leads: null,
            runs: null,
            sources: [
                { ...sources[1], joinable: false, source_id: 5 },
                { ...sources[0], join_status: 'not_joined', joinable: true, source_id: 6 },
            ],
        });

        const { sources: pool } = await growthApi.listSources();

        expect(pool.map(({ id, join_status }) => [id, join_status])).toEqual([
            [5, 'direct'],
            [6, 'not_joined'],
        ]);
    });

    it('reads an empty pool the server sends as null', async () => {
        serve({ leads: null, runs: null, sources: null });

        expect(await growthApi.listSources()).toEqual({ sources: [], total: 0, warnings: null });
        expect(await growthApi.listLeads(1)).toEqual([]);
        expect(await growthApi.listRuns(1)).toEqual([]);
    });

    it('reads what became of each added link', async () => {
        server.post.mockResolvedValue(
            ok({
                results: [
                    { input: 'https://t.me/myslots', result: 'existing', source: sources[1] },
                    { input: 'not a link', reason: 'no platform', result: 'invalid' },
                ],
            }),
        );

        const { results } = await growthApi.addSources(['https://t.me/myslots', 'not a link']);

        expect(results.map(({ link, outcome, source }) => [link, outcome, source?.id])).toEqual([
            ['https://t.me/myslots', 'existing', 4],
            ['not a link', 'invalid', undefined],
        ]);
    });

    it('sends the pool edits with the tier names the server knows', async () => {
        server.put.mockResolvedValueOnce(ok({ ...sources[1], note: 'quiet now', title: 'MY slots chat' }));
        server.put.mockResolvedValueOnce(ok(leads[1]));
        server.post.mockResolvedValue(ok(leads[2]));
        server.delete.mockResolvedValue(ok(null));

        const source = await growthApi.updateSource(4, { note: 'quiet now', title: 'MY slots chat' });
        await growthApi.updateLead(22, { note: 'asked about rebates', tier: 'watch' });
        await growthApi.createLead({ handle: '@kopi', platform: 'telegram', tier: 'watch' });
        await growthApi.deleteLead(22);

        expect([source.id, source.title, source.note, source.join_status]).toEqual([
            4,
            'MY slots chat',
            'quiet now',
            'pending_approval',
        ]);
        expect(server.put).toHaveBeenNthCalledWith(1, '/growth/sources/4', {
            note: 'quiet now',
            title: 'MY slots chat',
        });
        expect(server.put).toHaveBeenNthCalledWith(2, '/growth/leads/22', {
            note: 'asked about rebates',
            tier: 'watchlist',
        });
        expect(server.post).toHaveBeenCalledWith('/growth/leads/', {
            handle: '@kopi',
            platform: 'telegram',
            tier: 'watchlist',
        });
        expect(server.delete).toHaveBeenCalledWith('/growth/leads/22');
    });

    it('shows a running round from the server on the progress bar', async () => {
        flow.run = (await growthApi.listRuns(1))[1] ?? null;

        render(
            <MemoryRouter>
                <GrowthProgressBar />
            </MemoryRouter>,
        );

        expect(screen.getByRole('progressbar', { name: '本轮已找到 4 / 50 位潜客' })).toBeInTheDocument();
        expect(screen.getByText('待观察 3')).toBeInTheDocument();
        expect(screen.getByText('来源 6 个 · 已加入 1 · 待审批 1 · 无需加入 1')).toBeInTheDocument();
    });

    it('sends an account proxy under the key the server binds and reads it back from the same key', async () => {
        server.put.mockResolvedValue(
            ok({ id: 5, platform: 'facebook', proxy: 'http://••••@203.0.113.24:8080', proxy_source: 'account' }),
        );

        const account = await growthApi.setAccountProxy(5, 'http://user:pass@203.0.113.24:8080');

        expect(server.put).toHaveBeenCalledWith('/growth/accounts/5', { proxy: 'http://user:pass@203.0.113.24:8080' });
        expect(proxyRouteText(account)).toBe('http://••••@203.0.113.24:8080');
    });

    it('sends the proxy typed into the remote sign-in under the key the server binds', async () => {
        server.post.mockResolvedValue(ok({ fingerprint: null, session_id: 's1', view_url: '/browser/login/x' }));

        render(
            <RemoteLoginDialog
                onClose={() => {}}
                onSignedIn={() => {}}
                open
                platform="facebook"
            />,
        );
        await userEvent.type(screen.getByLabelText('代理（可选）'), 'socks5://203.0.113.24:1080');
        await userEvent.click(screen.getByRole('button', { name: '远程登录' }));

        expect(server.post).toHaveBeenCalledWith('/growth/accounts/remote-login', {
            account_id: undefined,
            external_browser_ws: null,
            platform: 'facebook',
            proxy: 'socks5://203.0.113.24:1080',
        });
    });
});
