import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import type { GrowthProfile, GrowthProfileInput } from '../growth-types';

import { growthApi } from '../growth-api';
import { GrowthProfileForm } from './growth-profile-form';

vi.mock('../growth-api', () => ({
    growthApi: {
        createProfile: vi.fn(async (input: GrowthProfileInput) => ({ ...profile, ...input, id: 9 })),
        listProfilePresets: vi.fn(async () => [
            {
                default: true,
                id: 'licensed_casino',
                profile: {
                    exclude: 'bots and spam',
                    name: '',
                    need: 'Offer: A licensed online casino.',
                    scoring: 'player = 3 | Audience | Plays.\nqualified >= 7\nwatchlist >= 4',
                    title: '',
                },
            },
        ]),
        restoreProfile: vi.fn(async () => ({
            ...profile,
            edited_by: 'user',
            exclude: 'account sellers',
            has_previous: false,
            need: 'Offer: the previous one',
        })),
        updateProfile: vi.fn(async (_id: number, input: GrowthProfileInput) => ({ ...profile, ...input })),
    },
}));

const profile: GrowthProfile = {
    default: true,
    exclude: 'account sellers',
    id: 3,
    name: 'Star Era',
    need: 'Offer: a new server for players in Thailand.',
    scoring: 'player = 3 | Audience | Plays.\nqualified >= 2\nwatchlist >= 1',
    sections: [{ body: '只找愿意付费的', id: 's1', title: '预算' }],
    title: '泰国玩家',
    updated_at: '2026-10-16T00:00:00Z',
};

const renderForm = (value: GrowthProfile | null = profile) =>
    render(
        <GrowthProfileForm
            onSaved={() => {}}
            profile={value}
        />,
    );

describe('GrowthProfileForm', () => {
    beforeEach(() => vi.clearAllMocks());

    it('shows the three prompts and an added one, and says when a change is unsaved', () => {
        renderForm();

        expect(screen.getByLabelText('需要什么')).toHaveValue('Offer: a new server for players in Thailand.');
        expect(screen.getByLabelText('排除什么')).toHaveValue('account sellers');
        expect(screen.getByLabelText('Jev 分数')).toHaveValue(profile.scoring);
        expect(screen.getByLabelText('标题 1')).toHaveValue('预算');
        expect(screen.queryByText('有未保存的修改')).not.toBeInTheDocument();

        fireEvent.change(screen.getByLabelText('需要什么'), { target: { value: '改过的需要' } });

        expect(screen.getByText('有未保存的修改')).toBeInTheDocument();
        expect(screen.getByText('5 / 8000')).toBeInTheDocument();
    });

    it('saves the three prompts and a renamed added prompt', async () => {
        renderForm();

        fireEvent.change(screen.getByLabelText('标题 1'), { target: { value: '付费意愿' } });
        fireEvent.change(screen.getByLabelText('内容 1'), { target: { value: '提到充值的优先' } });
        fireEvent.click(screen.getByRole('button', { name: '保存' }));

        await waitFor(() => expect(growthApi.updateProfile).toHaveBeenCalled());
        expect(growthApi.updateProfile).toHaveBeenCalledWith(3, {
            exclude: 'account sellers',
            name: '泰国玩家',
            need: 'Offer: a new server for players in Thailand.',
            scoring: profile.scoring,
            sections: [{ body: '提到充值的优先', id: 's1', title: '付费意愿' }],
            title: '泰国玩家',
        });
    });

    it('refuses a scoring text whose watchlist is not below the qualified line', async () => {
        renderForm();

        fireEvent.change(screen.getByLabelText('Jev 分数'), {
            target: { value: 'player = 3\nqualified >= 4\nwatchlist >= 4' },
        });
        fireEvent.click(screen.getByRole('button', { name: '保存' }));

        expect(await screen.findByText('待观察线至少为 1，且要低于合格线')).toBeInTheDocument();
        expect(growthApi.updateProfile).not.toHaveBeenCalled();
    });

    it('refuses a save that leaves every prompt empty', async () => {
        renderForm(null);

        fireEvent.click(screen.getByRole('button', { name: '保存' }));

        expect(await screen.findByText('请至少填写需要什么、排除什么，或添加一段')).toBeInTheDocument();
        expect(growthApi.createProfile).not.toHaveBeenCalled();
    });

    it('fills the three prompts from the default example and keeps the typed name', async () => {
        renderForm(null);

        fireEvent.change(screen.getByLabelText('画像名称'), { target: { value: '我的画像' } });
        fireEvent.click(screen.getByRole('button', { name: '填入示例' }));

        expect(await screen.findByLabelText('需要什么')).toHaveValue('Offer: A licensed online casino.');
        expect(screen.getByLabelText('排除什么')).toHaveValue('bots and spam');
        expect(screen.getByLabelText('Jev 分数')).toHaveValue(
            'player = 3 | Audience | Plays.\nqualified >= 7\nwatchlist >= 4',
        );
        expect(screen.getByLabelText('画像名称')).toHaveValue('我的画像');
    });

    it('adds a prompt and moves it above the previous one', async () => {
        renderForm({ ...profile, sections: [] });

        fireEvent.click(screen.getByRole('button', { name: '添加一项' }));
        fireEvent.change(screen.getByLabelText('标题 1'), { target: { value: '预算' } });
        fireEvent.change(screen.getByLabelText('内容 1'), { target: { value: '只找付费的' } });
        fireEvent.click(screen.getByRole('button', { name: '添加一项' }));
        fireEvent.change(screen.getByLabelText('标题 2'), { target: { value: '地区' } });
        fireEvent.click(screen.getAllByRole('button', { name: '上移' })[1] as HTMLElement);
        fireEvent.click(screen.getByRole('button', { name: '保存' }));

        await waitFor(() => expect(growthApi.updateProfile).toHaveBeenCalled());
        const input = vi.mocked(growthApi.updateProfile).mock.calls[0]?.[1];

        expect(input?.sections?.map((section) => section.title)).toEqual(['地区', '预算']);
    });

    it('restores the previous prompts after an assistant edit', async () => {
        renderForm({ ...profile, edited_by: 'assistant', has_previous: true });

        expect(screen.getByText('最近由助手修改')).toBeInTheDocument();
        fireEvent.click(screen.getByRole('button', { name: '恢复上一版' }));

        await waitFor(() => expect(growthApi.restoreProfile).toHaveBeenCalledWith(3));
        expect(screen.getByLabelText('需要什么')).toHaveValue('Offer: the previous one');
        expect(screen.queryByText('最近由助手修改')).not.toBeInTheDocument();
    });
});
