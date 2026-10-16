import { useMemo } from 'react';

import { growthApi } from './growth-api';
import { useGrowthPoll } from './use-growth-poll';

export const useGrowthLeads = (flowId: null | string) =>
    useGrowthPoll(flowId ? `leads:${flowId}` : null, () => growthApi.listLeads(flowId ?? ''));

export const useGrowthSources = () => useGrowthPoll('sources', () => growthApi.listSources());

export const ACCOUNTS_POLL_INTERVAL_MS = 30_000;

export const useGrowthAccounts = (intervalMs: null | number = null) =>
    useGrowthPoll('accounts', () => growthApi.listAccounts(), intervalMs);

export const useGrowthSettings = () => useGrowthPoll('settings', () => growthApi.listSettings(), null);

export const useGrowthProfiles = () => useGrowthPoll('profiles', () => growthApi.listProfiles(), null);

export function useGrowthCurrentRun(flowId: null | string) {
    const state = useGrowthPoll(flowId ? `runs:${flowId}` : null, () => growthApi.listRuns(flowId ?? ''));

    // The current round is the flow's latest Task; earlier rounds are browsed in the Tasks tab.
    const run = useMemo(
        () =>
            state.data?.length
                ? state.data.reduce((latest, item) => (item.task_id > latest.task_id ? item : latest))
                : null,
        [state.data],
    );

    return { ...state, run };
}
