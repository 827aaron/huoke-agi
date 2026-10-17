import type { ReactNode } from 'react';

import { createContext, useContext, useMemo } from 'react';

import { useFlow } from '@/providers/flow-provider';

import type {
    GrowthAccount,
    GrowthJoinWarning,
    GrowthLead,
    GrowthProfile,
    GrowthRun,
    GrowthSource,
} from './growth-types';

import {
    ACCOUNTS_POLL_INTERVAL_MS,
    useGrowthAccounts,
    useGrowthCurrentRun,
    useGrowthLeads,
    useGrowthProfiles,
    useGrowthSources,
} from './use-growth-data';

interface GrowthFlowContextValue {
    // Undefined until loaded, so a slow first response never reads as "no account".
    accounts: GrowthAccount[] | undefined;
    isLeadsLoading: boolean;
    isSourcesLoading: boolean;
    joinWarnings: GrowthJoinWarning[];
    leads: GrowthLead[];
    leadsError: unknown;
    profile: GrowthProfile | null;
    refetchSources: () => Promise<void>;
    run: GrowthRun | null;
    sources: GrowthSource[];
    sourcesError: unknown;
}

const EMPTY: GrowthFlowContextValue = {
    accounts: undefined,
    isLeadsLoading: false,
    isSourcesLoading: false,
    joinWarnings: [],
    leads: [],
    leadsError: null,
    profile: null,
    refetchSources: async () => {},
    run: null,
    sources: [],
    sourcesError: null,
};

// Outside the flow page (a component rendered on its own in a test) the growth panels see no data.
const GrowthFlowContext = createContext<GrowthFlowContextValue>(EMPTY);

// One poll per resource for the whole flow page: the progress bar, the Prospects and the Sources tabs read
// the same snapshot instead of each fetching its own.
export function GrowthFlowProvider({ children }: { children: ReactNode }) {
    const { flowId } = useFlow();

    const leadsState = useGrowthLeads(flowId);
    const sourcesState = useGrowthSources();
    const profilesState = useGrowthProfiles();
    const { run } = useGrowthCurrentRun(flowId);
    const { data: accounts } = useGrowthAccounts(ACCOUNTS_POLL_INTERVAL_MS);

    const value = useMemo<GrowthFlowContextValue>(
        () => ({
            accounts,
            isLeadsLoading: leadsState.isLoading,
            isSourcesLoading: sourcesState.isLoading,
            joinWarnings: sourcesState.data?.warnings ?? [],
            leads: leadsState.data ?? [],
            leadsError: leadsState.error,
            profile: profilesState.data?.[0] ?? null,
            refetchSources: sourcesState.refetch,
            run,
            sources: sourcesState.data?.sources ?? [],
            sourcesError: sourcesState.error,
        }),
        [accounts, leadsState, sourcesState, profilesState.data, run],
    );

    return <GrowthFlowContext.Provider value={value}>{children}</GrowthFlowContext.Provider>;
}

export const useGrowthFlow = (): GrowthFlowContextValue => useContext(GrowthFlowContext);
