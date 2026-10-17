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
    proxy: null,
    proxy_source: 'direct',
    rate_limit_until: null,
    status: 'active',
};

vi.mock('@/components/layouts/app/app-header', () => ({
    AppHeader: ({ children }: { children: unknown }) => children,
    AppHeaderContent: ({ children }: { children: unknown }) => children,
    AppHeaderTitle: ({ children }: { children: unknown }) => children,
}));

// The page reseeds its state whenever the polled array changes identity, so each poll returns the same one.
const accountsState: { data: GrowthAccount[]; error: null; isLoading: boolean } = {
    data: [account],
    error: null,
    isLoading: false,
};
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

    it('renders a Facebook account signed in through the remote browser, which has no label or external id', () => {
        // The JSON of GET /growth/accounts for such an account: external_id and the empty fields are omitted.
        const facebook = JSON.parse(`{
            "id": 5, "platform": "facebook", "kind": "browser_session", "label": "", "status": "active",
            "available": true, "consecutive_errors": 0, "proxy_source": "none",
            "last_success_at": "2026-10-17T08:00:00Z", "first_signed_in_at": "2026-10-17T08:00:00Z",
            "joined_sources": 0, "joins_today": 0,
            "fingerprint": {"version": 1, "os": "windows", "region": "US", "chrome_version": "131",
                "locale": "en-US", "timezone": "America/Chicago", "user_agent": "Mozilla/5.0",
                "platform": "Win32", "viewport": {"width": 1920, "height": 950},
                "screen": {"width": 1920, "height": 1080}, "device_scale_factor": 1, "hardware_concurrency": 8}
        }`) as GrowthAccount;

        accountsState.data = [facebook];

        try {
            render(
                <MemoryRouter>
                    <SettingsAccounts />
                </MemoryRouter>,
            );
        } finally {
            accountsState.data = [account];
        }

        expect(screen.getByText('#5')).toBeInTheDocument();
        expect(screen.getByText('自动指纹：windows · en-US · America/Chicago')).toBeInTheDocument();
    });
});
