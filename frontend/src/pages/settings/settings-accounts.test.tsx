import { render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { describe, expect, it, vi } from 'vitest';

import type { GrowthAccount } from '@/features/growth/growth-types';

import SettingsAccounts from './settings-accounts';

const account: GrowthAccount = {
    available: true,
    consecutive_errors: 0,
    external_browser_ws: null,
    external_id: '+66800001234',
    fingerprint: null,
    id: 7,
    join_advice: ['daily_joins_reached'],
    joined_sources: 38,
    joins_today: 5,
    kind: 'user',
    label: 'Bangkok',
    last_error: null,
    last_rate_limited_at: null,
    last_success_at: null,
    last_used_at: null,
    platform: 'telegram',
    proxy_source: 'direct',
    proxy_url: null,
    rate_limit_until: null,
    status: 'active',
};

vi.mock('@/components/layouts/app/app-header', () => ({
    AppHeader: ({ children }: { children: unknown }) => children,
    AppHeaderContent: ({ children }: { children: unknown }) => children,
    AppHeaderTitle: ({ children }: { children: unknown }) => children,
}));

// The page reseeds its state whenever the polled array changes identity, so each poll returns the same one.
const accountsState = { data: [account], error: null, isLoading: false };
const settingsState = { data: [], error: null, isLoading: false };

vi.mock('@/features/growth/use-growth-data', () => ({
    ACCOUNTS_POLL_INTERVAL_MS: 30_000,
    useGrowthAccounts: () => accountsState,
    useGrowthSettings: () => settingsState,
}));

describe('SettingsAccounts', () => {
    it('shows a Telegram account’s join counts and its pace reminder', () => {
        render(
            <MemoryRouter>
                <SettingsAccounts />
            </MemoryRouter>,
        );

        expect(screen.getByText('今天加了 5 个群，共加入 38 个')).toBeInTheDocument();
        expect(screen.getByText('今天加群已到建议上限，建议明天再加')).toHaveClass('text-yellow-800');
    });
});
