import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import type { GrowthProfile } from '../growth-types';

import { growthApi } from '../growth-api';
import { GrowthProfileForm } from './growth-profile-form';

vi.mock('../growth-api', () => ({
    growthApi: {
        updateProfile: vi.fn(async (_id: number, input: object) => ({ ...profile, ...input })),
    },
}));

const profile: GrowthProfile = {
    competitors: [],
    default_target: 50,
    excludes: [],
    games: ['Star Era'],
    genre: 'MOBA',
    id: 3,
    languages: ['th'],
    name: 'Star Era',
    notes: '',
    pitch: 'New server opens',
    regions: ['Thailand'],
    signals: [],
    updated_at: '2026-10-16T00:00:00Z',
};

const renderForm = () =>
    render(
        <GrowthProfileForm
            onSaved={() => {}}
            profile={profile}
        />,
    );

describe('GrowthProfileForm', () => {
    beforeEach(() => vi.clearAllMocks());

    it('saves the chosen contact methods and the default scoring of a profile that has none', async () => {
        renderForm();

        fireEvent.click(screen.getByRole('button', { name: '手机号' }));
        fireEvent.click(screen.getByRole('button', { name: '保存' }));

        await waitFor(() =>
            expect(growthApi.updateProfile).toHaveBeenCalledWith(
                3,
                expect.objectContaining({
                    contact_types: ['platform_handle', 'phone'],
                    scoring: {
                        qualified: 7,
                        watchlist: 4,
                        weights: { active: 2, match: 3, pain: 3, paying: 2, player: 3 },
                    },
                }),
            ),
        );
    });

    it('refuses a qualified line the weights cannot reach', async () => {
        renderForm();

        fireEvent.click(screen.getByText('高级：打分规则'));
        fireEvent.change(screen.getByLabelText('合格线'), { target: { value: '14' } });
        fireEvent.click(screen.getByRole('button', { name: '保存' }));

        expect(await screen.findByText('各项权重加起来低于合格线，没有潜客能合格')).toBeInTheDocument();
        expect(growthApi.updateProfile).not.toHaveBeenCalled();
    });
});
