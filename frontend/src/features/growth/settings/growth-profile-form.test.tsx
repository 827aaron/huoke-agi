import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import type { GrowthProfile } from '../growth-types';

import { growthApi } from '../growth-api';
import { GrowthProfileForm, PRESET_FORM_FIELDS } from './growth-profile-form';

vi.mock('../growth-api', () => ({
    growthApi: {
        listProfilePresets: vi.fn(async () => [
            {
                id: 'game_publisher',
                profile: {
                    competitors: null,
                    contact_types: ['platform_handle'],
                    default_target: 50,
                    excludes: null,
                    games: null,
                    genre: 'Mobile and PC games',
                    languages: null,
                    name: '',
                    notes: '',
                    pitch: 'A game looking for players.',
                    regions: null,
                    signals: null,
                },
            },
            {
                default: true,
                id: 'licensed_casino',
                profile: {
                    competitors: null,
                    contact_types: ['platform_handle'],
                    default_target: 50,
                    excludes: ['bots and spam'],
                    games: ['online slots', 'live casino'],
                    genre: 'Licensed online casino and sportsbook',
                    languages: null,
                    name: '',
                    notes: 'Adults only.',
                    pitch: 'A licensed online casino looking for adult players.',
                    regions: null,
                    scoring: {
                        qualified: 8,
                        watchlist: 4,
                        weights: { active: 2, match: 3, pain: 3, paying: 3, player: 3 },
                    },
                    signals: ['free spins'],
                },
            },
        ]),
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

const contactMethods = () =>
    within(screen.getByText('希望的联系方式').closest('[data-slot="form-item"]') as HTMLElement);

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

    it('saves WhatsApp, Line and Telegram as separate contact methods', async () => {
        renderForm();

        fireEvent.click(screen.getByRole('button', { name: 'Line' }));
        fireEvent.click(screen.getByRole('button', { name: 'Telegram' }));
        fireEvent.click(screen.getByRole('button', { name: '保存' }));

        await waitFor(() =>
            expect(growthApi.updateProfile).toHaveBeenCalledWith(
                3,
                expect.objectContaining({ contact_types: ['platform_handle', 'line', 'telegram'] }),
            ),
        );
        expect(screen.getByRole('button', { name: 'WhatsApp' })).toBeInTheDocument();
    });

    it('adds a named contact method under other and saves it as written', async () => {
        renderForm();

        expect(screen.queryByLabelText('其他联系方式名称')).not.toBeInTheDocument();
        fireEvent.click(contactMethods().getByRole('button', { name: '其他' }));
        fireEvent.change(screen.getByLabelText('其他联系方式名称'), { target: { value: ' Zalo ' } });
        fireEvent.click(screen.getByRole('button', { name: '添加这个联系方式' }));
        fireEvent.change(screen.getByLabelText('其他联系方式名称'), { target: { value: 'EMAIL' } });
        fireEvent.click(screen.getByRole('button', { name: '添加这个联系方式' }));
        fireEvent.change(screen.getByLabelText('其他联系方式名称'), { target: { value: '手机号' } });
        fireEvent.click(screen.getByRole('button', { name: '添加这个联系方式' }));
        fireEvent.change(screen.getByLabelText('其他联系方式名称'), { target: { value: 'zalo' } });
        fireEvent.keyDown(screen.getByLabelText('其他联系方式名称'), { key: 'Enter' });

        expect(screen.getAllByRole('button', { name: 'Zalo' })).toHaveLength(1);
        expect(screen.getByLabelText('其他联系方式名称')).toHaveValue('');

        fireEvent.click(screen.getByRole('button', { name: '保存' }));

        await waitFor(() =>
            expect(growthApi.updateProfile).toHaveBeenCalledWith(
                3,
                expect.objectContaining({ contact_types: ['platform_handle', 'other', 'Zalo', 'email', 'phone'] }),
            ),
        );
    });

    it('shows the named contact methods a saved profile carries', () => {
        render(
            <GrowthProfileForm
                onSaved={() => {}}
                profile={{ ...profile, contact_types: ['platform_handle', 'Zalo'] }}
            />,
        );

        expect(screen.getByRole('button', { name: 'Zalo' })).toHaveAttribute('data-state', 'on');
    });

    it('refuses a contact method name longer than 30 characters', () => {
        renderForm();

        fireEvent.click(contactMethods().getByRole('button', { name: '其他' }));
        fireEvent.change(screen.getByLabelText('其他联系方式名称'), { target: { value: 'x'.repeat(31) } });
        fireEvent.click(screen.getByRole('button', { name: '添加这个联系方式' }));

        expect(screen.getByText('名称最多 30 个字')).toBeInTheDocument();
        expect(screen.queryByRole('button', { name: 'x'.repeat(31) })).not.toBeInTheDocument();
    });

    it('refuses a qualified line the weights cannot reach', async () => {
        renderForm();

        fireEvent.click(screen.getByText('高级：打分规则'));
        fireEvent.change(screen.getByLabelText('合格线'), { target: { value: '14' } });
        fireEvent.click(screen.getByRole('button', { name: '保存' }));

        expect(await screen.findByText('各项权重加起来低于合格线，没有潜客能合格')).toBeInTheDocument();
        expect(growthApi.updateProfile).not.toHaveBeenCalled();
    });

    it('fills the form from a template and keeps the name, regions and languages it leaves empty', async () => {
        renderForm();

        fireEvent.keyDown(await screen.findByRole('combobox', { name: '从模板开始（可选）' }), { key: 'ArrowDown' });
        fireEvent.click(await screen.findByRole('option', { name: '持牌线上娱乐城找玩家' }));

        expect(screen.getAllByText('模板未覆盖此项，保留了原来填的内容。')).toHaveLength(4);

        fireEvent.click(screen.getByRole('button', { name: '保存' }));

        await waitFor(() =>
            expect(growthApi.updateProfile).toHaveBeenCalledWith(3, {
                competitors: [],
                contact_types: ['platform_handle'],
                default_target: 50,
                excludes: ['bots and spam'],
                games: ['online slots', 'live casino'],
                genre: 'Licensed online casino and sportsbook',
                languages: ['th'],
                name: 'Star Era',
                notes: 'Adults only.',
                pitch: 'A licensed online casino looking for adult players.',
                regions: ['Thailand'],
                scoring: {
                    qualified: 8,
                    watchlist: 4,
                    weights: { active: 2, match: 3, pain: 3, paying: 3, player: 3 },
                },
                signals: ['free spins'],
            }),
        );
    });

    it('maps every field a server preset carries onto the form', async () => {
        const [preset] = await growthApi.listProfilePresets();
        const serverFields = [
            'competitors',
            'contact_types',
            'default_target',
            'excludes',
            'games',
            'genre',
            'languages',
            'name',
            'notes',
            'pitch',
            'regions',
            'scoring',
            'signals',
        ];

        expect(Object.keys(PRESET_FORM_FIELDS).sort()).toEqual(serverFields);
        expect(Object.keys(preset?.profile ?? {}).filter((name) => !serverFields.includes(name))).toEqual([]);
    });

    it('starts a new profile from the default template without saving it', async () => {
        render(
            <GrowthProfileForm
                onSaved={() => {}}
                profile={null}
            />,
        );

        expect(await screen.findByRole('combobox', { name: '从模板开始（可选）' })).toHaveTextContent(
            '持牌线上娱乐城找玩家',
        );
        expect(screen.getByLabelText(/产品类型/)).toHaveValue('Licensed online casino and sportsbook');
        expect(screen.getByLabelText(/业务名称/)).toHaveValue('');
        expect(growthApi.updateProfile).not.toHaveBeenCalled();
    });

    it('keeps what was typed before the default template loaded', async () => {
        const presets = await growthApi.listProfilePresets();

        let release: (value: typeof presets) => void = () => {};

        vi.mocked(growthApi.listProfilePresets).mockImplementationOnce(
            () => new Promise((resolve) => (release = resolve)),
        );
        render(
            <GrowthProfileForm
                onSaved={() => {}}
                profile={null}
            />,
        );

        fireEvent.change(screen.getByLabelText(/产品类型/), { target: { value: 'Sportsbook' } });
        release(presets);

        expect(await screen.findByRole('combobox', { name: '从模板开始（可选）' })).toHaveTextContent(
            '持牌线上娱乐城找玩家',
        );
        expect(screen.getByLabelText(/产品类型/)).toHaveValue('Sportsbook');
        expect(screen.getByLabelText(/你想让目标人群知道什么/)).toHaveValue(
            'A licensed online casino looking for adult players.',
        );
    });
});
