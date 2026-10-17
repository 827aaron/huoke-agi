import { render, screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';

import { UsageStatsPeriod } from '@/graphql/types';

vi.mock('@apollo/client/react', () => ({
    useQuery: () => ({
        data: {
            flows: [],
            flowsExecutionStatsByPeriod: [],
            flowsStatsByPeriod: [],
            toolcallsStatsByPeriod: [],
            usageStatsByPeriod: [],
        },
        error: undefined,
        loading: true,
    }),
}));

import { DashboardAnalytics } from './dashboard-analytics';

describe('DashboardAnalytics', () => {
    it('keeps saying a quiet period has no data while it refetches', () => {
        render(<DashboardAnalytics period={UsageStatsPeriod.Week} />);

        expect(screen.getAllByText('这段时间没有数据')).toHaveLength(4);
        expect(document.querySelectorAll('.animate-spin')).toHaveLength(0);
    });
});
