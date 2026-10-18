import { fireEvent, render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { TooltipProvider } from '@/components/ui/tooltip';

import type { GrowthLead } from '../growth-types';

import FlowLeads from './flow-leads';

const flow = vi.hoisted(() => ({ leads: [] as unknown[], profiles: [] as unknown[] }));

vi.mock('../growth-flow-provider', () => ({
    useGrowthFlow: () => ({
        ...flow,
        isLeadsLoading: false,
        leadsError: null,
        profile: null,
        refetchLeads: vi.fn(async () => {}),
        run: null,
        sources: [],
    }),
}));

vi.mock('@/providers/flow-provider', () => ({ useFlow: () => ({ flowId: '1' }) }));

vi.mock('./leads-email-button', () => ({ default: () => null }));

// Day groups follow the browser's zone, so the instants are built in it.
const local = (day: number, hour: number) => new Date(2026, 9, day, hour, 0).toISOString();

const lead = (id: number, name: string, patch: Partial<GrowthLead>): GrowthLead => ({
    created_at: local(16, 9),
    delivered_at: null,
    delivered_task_id: null,
    display_name: name,
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
    score: 9,
    score_breakdown: [],
    signals: [],
    source_id: 1,
    source_title: 'Group',
    tier: 'qualified',
    username: null,
    ...patch,
});

const renderLeads = () =>
    render(
        <MemoryRouter>
            <TooltipProvider>
                <FlowLeads />
            </TooltipProvider>
        </MemoryRouter>,
    );

describe('FlowLeads', () => {
    beforeEach(() => {
        vi.useFakeTimers({ toFake: ['Date'] });
        vi.setSystemTime(new Date(2026, 9, 16, 12, 0));
        flow.leads = [
            lead(1, 'Ace', { created_at: local(16, 9), first_round: 3 }),
            lead(2, 'Bea', { created_at: local(16, 8), first_round: 2, score: 5, tier: 'watch' }),
            lead(3, 'Cid', { created_at: local(14, 10), first_round: 1, score: 8 }),
        ];
    });

    afterEach(() => {
        vi.useRealTimers();
    });

    it('opens the newest day and keeps an older one collapsed until clicked', () => {
        renderLeads();

        const today = screen.getByRole('button', { name: /2026-10-16 · 今天 · 第 2、3 轮/ });
        const older = screen.getByRole('button', { name: /2026-10-14 · 第 1 轮/ });

        expect(today).toHaveAttribute('aria-expanded', 'true');
        expect(today).toHaveTextContent('2 位 · 合格 1 · 待观察 1');
        expect(older).toHaveAttribute('aria-expanded', 'false');
        expect(screen.getByText('Ace')).toBeInTheDocument();
        expect(screen.getByText('Bea')).toBeInTheDocument();
        expect(screen.queryByText('Cid')).not.toBeInTheDocument();

        fireEvent.click(older);

        expect(screen.getByText('Cid')).toBeInTheDocument();
    });

    it('drops the prospects below the minimum score', async () => {
        renderLeads();

        fireEvent.keyDown(screen.getByRole('combobox', { name: '最低分' }), { key: 'ArrowDown' });
        fireEvent.click(await screen.findByRole('option', { name: '7 分及以上' }));

        expect(screen.queryByText('Bea')).not.toBeInTheDocument();
        expect(screen.getByText('Ace')).toBeInTheDocument();
        expect(screen.getByRole('button', { name: /2026-10-16 · 今天/ })).toHaveTextContent('1 位 · 合格 1 · 待观察 0');
        expect(screen.getByText('共 2 位潜客')).toBeInTheDocument();
    });

    it('keeps only what was found today', async () => {
        renderLeads();

        fireEvent.keyDown(screen.getByRole('combobox', { name: '发现日期' }), { key: 'ArrowDown' });
        fireEvent.click(await screen.findByRole('option', { name: '今天' }));

        expect(screen.queryByRole('button', { name: /2026-10-14/ })).not.toBeInTheDocument();
        expect(screen.getByText('共 2 位潜客')).toBeInTheDocument();
    });

    it('keeps only the prospects a round found', async () => {
        renderLeads();

        fireEvent.keyDown(screen.getByRole('combobox', { name: '轮次' }), { key: 'ArrowDown' });
        fireEvent.click(await screen.findByRole('option', { name: '第 2 轮' }));

        expect(screen.getByRole('button', { name: /2026-10-16 · 今天 · 第 2 轮/ })).toHaveTextContent(
            '1 位 · 合格 0 · 待观察 1',
        );
        expect(screen.queryByText('Ace')).not.toBeInTheDocument();
        expect(screen.queryByRole('button', { name: /2026-10-14/ })).not.toBeInTheDocument();
        expect(screen.getByText('共 1 位潜客')).toBeInTheDocument();
    });

    it('shows the signals a prospect scored on and the points of each', async () => {
        flow.profiles = [
            { scoring: { dimensions: [{ description: '', key: 'agency', name: '代理经验', points: 5 }] } },
        ];
        flow.leads = [
            lead(1, 'Ace', {
                flags: ['unwanted'],
                score_breakdown: [
                    { points: 3, signal: 'player' },
                    { points: 5, signal: 'agency' },
                    { points: 2, signal: 'active' },
                ],
                signals: ['player', 'agency', 'active'],
            }),
        ];
        renderLeads();

        expect(screen.getByText('不想要')).toBeInTheDocument();
        fireEvent.click(screen.getByText('Ace'));
        const sheet = await screen.findByRole('dialog');

        expect(sheet).toHaveTextContent('属于目标人群 +3');
        expect(sheet).toHaveTextContent('代理经验 +5');
        expect(sheet).toHaveTextContent('近期活跃 +2');
        flow.profiles = [];
    });

    it('names what each select’s first option covers', () => {
        renderLeads();

        expect(screen.getByRole('combobox', { name: '评分' })).toHaveTextContent('全部档位');
        expect(screen.getByRole('combobox', { name: '状态' })).toHaveTextContent('全部状态');
        expect(screen.getByRole('combobox', { name: '发现日期' })).toHaveTextContent('全部日期');
        expect(screen.getByRole('combobox', { name: '轮次' })).toHaveTextContent('全部轮次');
        expect(screen.getByRole('combobox', { name: '最低分' })).toHaveTextContent('全部分数');
    });
});
