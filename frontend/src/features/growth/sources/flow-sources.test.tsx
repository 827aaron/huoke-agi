import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import type { GrowthSource } from '../growth-types';

import { growthApi } from '../growth-api';
import FlowSources from './flow-sources';

const flow = vi.hoisted(() => ({
    joinWarnings: [] as object[],
    refetchSources: vi.fn(async () => {}),
    sources: [] as unknown[],
}));

vi.mock('../growth-flow-provider', () => ({
    useGrowthFlow: () => ({ ...flow, isSourcesLoading: false, sourcesError: null }),
}));

vi.mock('../growth-api', () => ({
    growthApi: { deleteSource: vi.fn(async () => {}), updateSource: vi.fn(async () => ({})) },
}));

const forum: GrowthSource = {
    created_at: '',
    id: 42,
    join_status: 'joined',
    kind: 'group',
    last_read_at: null,
    leads_count: 3,
    link: 'https://forum.example.my/board/slots',
    members_count: null,
    note: 'admins dislike ads',
    origin: 'manual',
    platform: 'web',
    source_id: 'https://forum.example.my/board/slots',
    title: 'Malaysia slots board',
};

const renderSources = () =>
    render(
        <MemoryRouter>
            <FlowSources />
        </MemoryRouter>,
    );

describe('FlowSources', () => {
    beforeEach(() => {
        vi.clearAllMocks();
        flow.joinWarnings = [];
        flow.sources = [forum];
    });

    it('warns about an account near Telegram’s group limit', () => {
        flow.joinWarnings = [
            { account_id: 7, joined_sources: 452, label: 'Bangkok', platform: 'telegram', warn_at: 450 },
        ];
        flow.sources = [];
        renderSources();

        expect(screen.getByRole('status')).toHaveTextContent(
            '账号 Bangkok 已加入 452 个群（提醒线 450），接近 Telegram 约 500 个的上限，建议退出不活跃的群或换号加群。',
        );
    });

    it('deletes a source once the owner confirms and reloads the pool', async () => {
        renderSources();

        fireEvent.click(screen.getByRole('button', { name: '删除来源 Malaysia slots board' }));
        const dialog = await screen.findByRole('dialog');

        expect(dialog).toHaveTextContent('确定要从来源池删除“Malaysia slots board”吗？已找到的潜客和证据会保留。');
        expect(growthApi.deleteSource).not.toHaveBeenCalled();

        fireEvent.click(within(dialog).getByRole('button', { name: '删除' }));

        await waitFor(() => expect(flow.refetchSources).toHaveBeenCalledTimes(1));
        expect(growthApi.deleteSource).toHaveBeenCalledWith(42);
    });

    it('keeps the source when the owner cancels the delete', async () => {
        renderSources();

        fireEvent.click(screen.getByRole('button', { name: '删除来源 Malaysia slots board' }));
        fireEvent.click(within(await screen.findByRole('dialog')).getByRole('button', { name: '取消' }));

        await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument());
        expect(growthApi.deleteSource).not.toHaveBeenCalled();
    });

    it('renames a source and rewrites its note', async () => {
        renderSources();

        expect(screen.getByText('admins dislike ads')).toBeInTheDocument();

        fireEvent.click(screen.getByRole('button', { name: '编辑来源 Malaysia slots board' }));
        const dialog = await screen.findByRole('dialog');

        fireEvent.change(within(dialog).getByLabelText('名称'), { target: { value: '  MY slots board  ' } });
        fireEvent.change(within(dialog).getByLabelText('备注'), { target: { value: 'many players' } });
        fireEvent.click(within(dialog).getByRole('button', { name: '保存' }));

        await waitFor(() => expect(flow.refetchSources).toHaveBeenCalledTimes(1));
        expect(growthApi.updateSource).toHaveBeenCalledWith(42, { note: 'many players', title: 'MY slots board' });
        await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument());
    });

    it('finds a source by the words of its note', () => {
        renderSources();

        fireEvent.change(screen.getByPlaceholderText(/搜索/), { target: { value: 'dislike' } });

        expect(screen.getByText('Malaysia slots board')).toBeInTheDocument();
    });
});
