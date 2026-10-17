import { fireEvent, render, screen, within } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import type { GrowthAccount } from '@/features/growth/growth-types';

import { growthApi } from '@/features/growth/growth-api';

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

vi.mock('@/features/growth/growth-api', () => ({ growthApi: { getAccountDetail: vi.fn() } }));

const renderAccounts = () =>
    render(
        <MemoryRouter>
            <SettingsAccounts />
        </MemoryRouter>,
    );

describe('SettingsAccounts', () => {
    beforeEach(() => vi.clearAllMocks());

    it('opens what an account has joined and how many chats it is in', async () => {
        // The JSON of GET /growth/accounts/7/detail: a group the account never read omits last_synced_at.
        vi.mocked(growthApi.getAccountDetail).mockResolvedValue(
            JSON.parse(`{
                "joined_groups_count": 2, "dialogs_count": 64,
                "groups": [
                    {"source_id": 3, "title": "MY slots", "link": "https://t.me/myslots", "member_count": 1200,
                        "joined_at": "2026-10-16T08:00:00Z", "last_synced_at": "2026-10-17T08:00:00Z"},
                    {"source_id": 4, "title": "", "link": "https://t.me/thaislots", "member_count": 0,
                        "joined_at": "2026-10-17T07:00:00Z"}
                ]
            }`),
        );
        renderAccounts();

        fireEvent.click(screen.getByRole('button', { name: '详情 Bangkok' }));
        const sheet = await screen.findByRole('dialog');

        expect(await within(sheet).findByText('通过本系统加入了 2 个群')).toBeInTheDocument();
        expect(within(sheet).getByText('账号里共 64 个聊天')).toBeInTheDocument();
        expect(within(sheet).getByRole('link', { name: 'MY slots' })).toHaveAttribute('href', 'https://t.me/myslots');
        expect(within(sheet).getByRole('link', { name: 'https://t.me/thaislots' })).toBeInTheDocument();
        expect(within(sheet).getByText('1200')).toBeInTheDocument();
        expect(growthApi.getAccountDetail).toHaveBeenCalledWith(7);
    });

    it('says when the chat count could not be read and the account joined nothing', async () => {
        vi.mocked(growthApi.getAccountDetail).mockResolvedValue({ groups: [], joined_groups_count: 0 });
        renderAccounts();

        fireEvent.click(screen.getByRole('button', { name: '详情 Bangkok' }));
        const sheet = await screen.findByRole('dialog');

        expect(await within(sheet).findByText('聊天数未取到（账号不在线或读取超时）')).toBeInTheDocument();
        expect(within(sheet).getByText('这个账号还没有通过本系统加入任何群。')).toBeInTheDocument();
        expect(within(sheet).queryByRole('table')).not.toBeInTheDocument();
    });

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
