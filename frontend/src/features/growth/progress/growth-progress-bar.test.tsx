import { render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { describe, expect, it, vi } from 'vitest';

import type { GrowthRun } from '../growth-types';

import GrowthProgressBar from './growth-progress-bar';

const flow = vi.hoisted(() => ({ run: null as GrowthRun | null }));

vi.mock('../growth-flow-provider', () => ({
    useGrowthFlow: () => ({ accounts: [], leads: [], profile: null, profiles: [], run: flow.run, sources: [] }),
}));
vi.mock('@/providers/flow-provider', () => ({
    useFlow: () => ({ flowData: null, submitAutomationMessage: vi.fn() }),
}));

// A round whose delivery handed over one lead while its task went on to qualify fifteen more.
const round = (patch: Partial<GrowthRun>): GrowthRun => ({
    current_action: null,
    deadline_at: null,
    delivered: 1,
    error: null,
    finished_at: '2026-10-19T09:30:00Z',
    flood_wait_seconds: null,
    flow_id: 1,
    found: 16,
    notify_error: null,
    notify_status: 'skipped',
    round: 1,
    schedule_id: null,
    sources: { exhausted: 0, invalid: 0, joined: 2, not_joinable: 0, pending: 0, read: 2, total: 2, unresolved: 0 },
    started_at: '2026-10-19T08:00:00Z',
    status: 'missed',
    target: 20,
    task_id: 11,
    watch: 9,
    ...patch,
});

const renderBar = (run: GrowthRun) => {
    flow.run = run;

    return render(
        <MemoryRouter>
            <GrowthProgressBar />
        </MemoryRouter>,
    );
};

describe('GrowthProgressBar', () => {
    it('measures a missed round by its qualified leads and shows the delivery beside them', () => {
        renderBar(round({}));

        expect(screen.getByRole('progressbar', { name: '本轮合格 16 / 20 位潜客' })).toBeInTheDocument();
        expect(screen.getByText('已交付 1')).toBeInTheDocument();
        expect(screen.getByText('目标未达成，原因见汇报')).toBeInTheDocument();
        expect(screen.getByRole('button', { name: '再找 19 个' })).toBeInTheDocument();
    });

    it('keeps the qualified count when a round delivered its target out of more', () => {
        renderBar(round({ delivered: 20, found: 25, status: 'delivered' }));

        expect(screen.getByRole('progressbar', { name: '本轮合格 25 / 20 位潜客' })).toBeInTheDocument();
        expect(screen.getByText('已交付 20')).toBeInTheDocument();
    });

    it('shows no delivery while the round is still running', () => {
        renderBar(round({ delivered: 0, finished_at: null, notify_status: null, status: 'running' }));

        expect(screen.getByRole('progressbar', { name: '本轮已找到 16 / 20 位潜客' })).toBeInTheDocument();
        expect(screen.queryByText(/已交付/)).not.toBeInTheDocument();
        expect(screen.getByText('待观察 9')).toBeInTheDocument();
    });
});
