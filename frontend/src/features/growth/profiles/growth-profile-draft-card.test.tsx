import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import { growthApi } from '../growth-api';
import { GrowthProfileDraftCard } from './growth-profile-draft-card';

const flow = vi.hoisted(() => ({
    flowProfile: { fallback: false, flow_id: 9, profile_id: 1, title: 'Players' } as null | object,
    profileDraft: null as null | object,
    profiles: [{ id: 1, name: 'Lucky Star', title: 'Players' }],
    refetchProfiles: vi.fn(async () => {}),
}));

vi.mock('../growth-flow-provider', () => ({ useGrowthFlow: () => flow }));

vi.mock('../growth-api', () => ({
    growthApi: { saveProfileDraft: vi.fn(async () => ({ id: 2 })) },
}));

const draft = (base: null | number) => ({
    base_profile_id: base,
    flow_id: 9,
    profile: {
        exclude: '',
        name: 'Lucky Star',
        need: 'A licensed casino looking for agents in Malaysia.',
        scoring: 'agency = 5 | 代理经验 |\nrecent = 2\nqualified >= 5\nwatchlist >= 2',
        title: '',
    },
    title: 'Malaysian agents',
    updated_at: '2026-10-18T00:00:00Z',
});

describe('GrowthProfileDraftCard', () => {
    beforeEach(() => vi.clearAllMocks());

    it('shows nothing while the task has no draft', () => {
        flow.profileDraft = null;
        const { container } = render(<GrowthProfileDraftCard />);

        expect(container).toBeEmptyDOMElement();
    });

    it('shows the three parts of the draft', () => {
        flow.profileDraft = draft(null);
        render(<GrowthProfileDraftCard />);

        expect(screen.getByText('助手起草的画像：Malaysian agents')).toBeInTheDocument();
        expect(screen.getByText('A licensed casino looking for agents in Malaysia.')).toBeInTheDocument();
        expect(screen.getByText('没有')).toBeInTheDocument();
        expect(screen.getByText(/agency = 5 \| 代理经验/)).toBeInTheDocument();
        expect(screen.getByText(/qualified >= 5/)).toBeInTheDocument();
        expect(screen.queryByText(/保存会更新画像/)).not.toBeInTheDocument();
    });

    it('saves the draft under the typed name and uses it for the task', async () => {
        flow.profileDraft = draft(null);
        render(<GrowthProfileDraftCard />);

        fireEvent.change(screen.getByLabelText('画像名称（可改）'), { target: { value: ' 马来代理 ' } });
        fireEvent.click(screen.getByRole('button', { name: '用于本任务' }));

        await waitFor(() =>
            expect(growthApi.saveProfileDraft).toHaveBeenCalledWith(9, { title: '马来代理', use_for_flow: true }),
        );
        expect(flow.refetchProfiles).toHaveBeenCalled();
    });

    it('says which profile a draft of a saved one updates, and saves it without using it', async () => {
        flow.profileDraft = draft(1);
        render(<GrowthProfileDraftCard />);

        expect(screen.getByText('保存会更新画像“Players”')).toBeInTheDocument();
        fireEvent.click(screen.getByRole('button', { name: '保存为画像' }));

        await waitFor(() =>
            expect(growthApi.saveProfileDraft).toHaveBeenCalledWith(9, { title: undefined, use_for_flow: false }),
        );
    });
});
