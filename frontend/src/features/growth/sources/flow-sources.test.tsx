import { render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { describe, expect, it, vi } from 'vitest';

import FlowSources from './flow-sources';

vi.mock('../growth-flow-provider', () => ({
    useGrowthFlow: () => ({
        isSourcesLoading: false,
        joinWarnings: [{ account_id: 7, joined_sources: 452, label: 'Bangkok', platform: 'telegram', warn_at: 450 }],
        refetchSources: async () => {},
        sources: [],
        sourcesError: null,
    }),
}));

describe('FlowSources', () => {
    it('warns about an account near Telegram’s group limit', () => {
        render(
            <MemoryRouter>
                <FlowSources />
            </MemoryRouter>,
        );

        expect(screen.getByRole('status')).toHaveTextContent(
            '账号 Bangkok 已加入 452 个群（提醒线 450），接近 Telegram 约 500 个的上限，建议退出不活跃的群或换号加群。',
        );
    });
});
