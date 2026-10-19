import { render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { describe, expect, it, vi } from 'vitest';

import type { GrowthRun } from '../growth-types';

import GrowthChatActions from './growth-chat-actions';

const run: GrowthRun = {
    current_action: null,
    deadline_at: null,
    delivered: 20,
    error: null,
    finished_at: '2026-10-19T09:30:00Z',
    flood_wait_seconds: null,
    flow_id: 1,
    found: 25,
    notify_error: null,
    notify_status: 'sent',
    round: 1,
    schedule_id: null,
    sources: { exhausted: 0, invalid: 0, joined: 2, not_joinable: 0, pending: 0, read: 2, total: 2, unresolved: 0 },
    started_at: '2026-10-19T08:00:00Z',
    status: 'delivered',
    target: 20,
    task_id: 11,
    watch: 9,
};

vi.mock('../growth-flow-provider', () => ({
    useGrowthFlow: () => ({ leads: [], profile: null, profiles: [], run, sources: [] }),
}));
vi.mock('@/providers/flow-provider', () => ({
    useFlow: () => ({ flowData: null, flowStatus: 'finished', submitAutomationMessage: vi.fn() }),
}));

describe('GrowthChatActions', () => {
    it('tells the delivered count and the qualified count apart on the delivery card', () => {
        render(
            <MemoryRouter>
                <GrowthChatActions />
            </MemoryRouter>,
        );

        expect(screen.getByText('本轮交付 20 位潜客')).toBeInTheDocument();
        expect(screen.getByText('合格 25 位 · 待观察 9 位（未交付，仍在潜客标签里）')).toBeInTheDocument();
    });
});
