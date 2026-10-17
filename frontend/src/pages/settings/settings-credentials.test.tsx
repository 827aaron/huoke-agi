import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import type { GrowthSetting } from '@/features/growth/growth-types';

import { SidebarProvider } from '@/components/ui/sidebar';
import { growthApi } from '@/features/growth/growth-api';

import SettingsCredentials from './settings-credentials';

const providers = vi.hoisted(() => ({ refreshProviders: vi.fn() }));

vi.mock('@/providers/providers-provider', () => ({ useProviders: () => providers }));

const saved = (kind: GrowthSetting['kind'], provider: null | string): GrowthSetting => ({
    fields: { api_key: '••••a1b2' },
    kind,
    provider,
    providers: provider ? [provider] : [],
    source: 'database',
    status: 'connected',
    status_reason: null,
    tested_at: '2026-10-17T00:00:00Z',
    updated_at: '2026-10-17T00:00:00Z',
});

vi.mock('@/features/growth/growth-api', () => ({
    growthApi: {
        clearSetting: vi.fn(async () => undefined),
        listAccounts: vi.fn(async () => []),
        listSettings: vi.fn(async () => [saved('llm', 'openrouter'), saved('youtube', null)]),
        testSetting: vi.fn(async (kind: GrowthSetting['kind']) => saved(kind, kind === 'llm' ? 'openrouter' : null)),
    },
}));

const renderPage = () =>
    render(
        <MemoryRouter>
            <SidebarProvider>
                <SettingsCredentials />
            </SidebarProvider>
        </MemoryRouter>,
    );

const card = async (title: string) =>
    (await screen.findByText(title, { selector: '[data-slot="card-title"]' })).closest(
        '[data-slot="card"]',
    ) as HTMLElement;

describe('SettingsCredentials', () => {
    beforeEach(() => vi.clearAllMocks());

    it('refreshes the task providers when the model key is cleared', async () => {
        renderPage();
        await waitFor(() => expect(growthApi.testSetting).toHaveBeenCalledTimes(2));
        providers.refreshProviders.mockClear();

        fireEvent.click(within(await card('大模型')).getByRole('button', { name: '清除' }));

        await waitFor(() => expect(growthApi.clearSetting).toHaveBeenCalledWith('llm'));
        await waitFor(() => expect(providers.refreshProviders).toHaveBeenCalledTimes(1));
    });

    it('leaves the task providers alone when another key changes', async () => {
        renderPage();
        await waitFor(() => expect(growthApi.testSetting).toHaveBeenCalledTimes(2));
        providers.refreshProviders.mockClear();

        fireEvent.click(within(await card('YouTube')).getByRole('button', { name: '清除' }));

        await waitFor(() => expect(growthApi.clearSetting).toHaveBeenCalledWith('youtube'));
        expect(providers.refreshProviders).not.toHaveBeenCalled();
    });
});
