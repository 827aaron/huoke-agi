import type { ReactNode } from 'react';

import { createContext, useContext, useMemo } from 'react';

import { useFlow } from '@/providers/flow-provider';

import type {
    GrowthAccount,
    GrowthFlowProfile,
    GrowthJoinWarning,
    GrowthLead,
    GrowthProfile,
    GrowthProfileDraft,
    GrowthRun,
    GrowthSource,
} from './growth-types';

import {
    ACCOUNTS_POLL_INTERVAL_MS,
    useGrowthAccounts,
    useGrowthCurrentRun,
    useGrowthFlowProfile,
    useGrowthLeads,
    useGrowthProfileDraft,
    useGrowthProfiles,
    useGrowthSources,
} from './use-growth-data';
import { GROWTH_POLL_INTERVAL_MS } from './use-growth-poll';

interface GrowthFlowContextValue {
    // Undefined until loaded, so a slow first response never reads as "no account".
    accounts: GrowthAccount[] | undefined;
    flowProfile: GrowthFlowProfile | null;
    isLeadsLoading: boolean;
    isSourcesLoading: boolean;
    joinWarnings: GrowthJoinWarning[];
    leads: GrowthLead[];
    leadsError: unknown;
    // The profile the flow judges with; null while the owner has saved none.
    profile: GrowthProfile | null;
    profileDraft: GrowthProfileDraft | null;
    profiles: GrowthProfile[];
    refetchLeads: () => Promise<void>;
    refetchProfiles: () => Promise<void>;
    refetchSources: () => Promise<void>;
    run: GrowthRun | null;
    sources: GrowthSource[];
    sourcesError: unknown;
}

const EMPTY: GrowthFlowContextValue = {
    accounts: undefined,
    flowProfile: null,
    isLeadsLoading: false,
    isSourcesLoading: false,
    joinWarnings: [],
    leads: [],
    leadsError: null,
    profile: null,
    profileDraft: null,
    profiles: [],
    refetchLeads: async () => {},
    refetchProfiles: async () => {},
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
    // Polled: the flow's assistant can save a profile at any time.
    const profilesState = useGrowthProfiles(GROWTH_POLL_INTERVAL_MS);
    const flowProfileState = useGrowthFlowProfile(flowId);
    const draftState = useGrowthProfileDraft(flowId);
    const { run } = useGrowthCurrentRun(flowId);
    const { data: accounts } = useGrowthAccounts(ACCOUNTS_POLL_INTERVAL_MS);

    const value = useMemo<GrowthFlowContextValue>(
        () => ({
            accounts,
            flowProfile: flowProfileState.data ?? null,
            isLeadsLoading: leadsState.isLoading,
            isSourcesLoading: sourcesState.isLoading,
            joinWarnings: sourcesState.data?.warnings ?? [],
            leads: leadsState.data ?? [],
            leadsError: leadsState.error,
            profile:
                profilesState.data?.find((item) => item.id === flowProfileState.data?.profile_id) ??
                profilesState.data?.find((item) => item.default) ??
                null,
            profileDraft: draftState.data ?? null,
            profiles: profilesState.data ?? [],
            refetchLeads: leadsState.refetch,
            refetchProfiles: async () => {
                await Promise.all([profilesState.refetch(), flowProfileState.refetch(), draftState.refetch()]);
            },
            refetchSources: sourcesState.refetch,
            run,
            sources: sourcesState.data?.sources ?? [],
            sourcesError: sourcesState.error,
        }),
        [accounts, leadsState, sourcesState, profilesState, flowProfileState, draftState, run],
    );

    return <GrowthFlowContext.Provider value={value}>{children}</GrowthFlowContext.Provider>;
}

export const useGrowthFlow = (): GrowthFlowContextValue => useContext(GrowthFlowContext);
