import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { describe, expect, it, vi } from 'vitest';

import type { GrowthSetting } from '../growth-types';

import { growthApi } from '../growth-api';
import { GrowthCredentialCard } from './growth-credential-row';

vi.mock('../growth-api', () => ({
    growthApi: {
        saveSetting: vi.fn(async () => savedProxy),
        testSetting: vi.fn(async () => savedProxy),
    },
}));

const savedProxy: GrowthSetting = {
    fields: { url: 'http://••••@203.0.113.24:8080' },
    kind: 'proxy',
    provider: null,
    providers: [],
    source: 'database',
    status: 'connected',
    status_reason: null,
    tested_at: '2026-10-16T00:00:00Z',
    updated_at: '2026-10-16T00:00:00Z',
};

describe('GrowthCredentialCard', () => {
    it('starts a masked proxy empty and keeps it by not resending the mask', async () => {
        render(
            <MemoryRouter>
                <GrowthCredentialCard
                    kind="proxy"
                    onChange={() => {}}
                    setting={savedProxy}
                />
            </MemoryRouter>,
        );

        fireEvent.click(screen.getByRole('button', { name: '更换密钥' }));

        expect(screen.getByLabelText(/代理地址/)).toHaveValue('');

        fireEvent.click(screen.getByRole('button', { name: /保存/ }));

        await waitFor(() =>
            expect(growthApi.saveSetting).toHaveBeenCalledWith('proxy', { fields: {}, provider: null }),
        );
    });
});
