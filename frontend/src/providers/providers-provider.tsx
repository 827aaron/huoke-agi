import { useApolloClient, useQuery, useSubscription } from '@apollo/client/react';
import { createContext, useCallback, useContext, useEffect, useMemo, useState } from 'react';

import type { Provider } from '@/models/provider';

import { growthApi } from '@/features/growth/growth-api';
import { useGrowthPoll } from '@/features/growth/use-growth-poll';
import {
    ProviderCreatedDocument,
    ProviderDeletedDocument,
    ProvidersDocument,
    ProviderUpdatedDocument,
} from '@/graphql/types';
import { findProviderByName, pickDefaultProvider, sortProviders } from '@/models/provider';
import { useUser } from '@/providers/user-provider';

const SELECTED_PROVIDER_KEY = 'selectedProvider';
const APPLIED_LLM_TYPE_KEY = 'selectedProviderLlmType';

interface ProvidersContextValue {
    providers: Provider[];
    // A key saved on the growth LLM card adds a provider no subscription announces.
    refreshProviders: () => void;
    selectedProvider: null | Provider;
    setSelectedProvider: (provider: Provider) => void;
}

const ProvidersContext = createContext<ProvidersContextValue | undefined>(undefined);

interface ProvidersProviderProps {
    children: React.ReactNode;
}

export function ProvidersProvider({ children }: ProvidersProviderProps) {
    const { isAuthenticated } = useUser();
    const client = useApolloClient();

    const { data: providersData, refetch: refetchProviders } = useQuery(ProvidersDocument, {
        skip: !isAuthenticated(),
    });

    // The providers list is a separate root field from the settings page's own
    // query, so editing a provider there leaves this copy stale — and a stale
    // copy means the composer offers a provider name the backend no longer
    // resolves. Refetch on every provider mutation instead of waiting for a
    // page reload.
    const refetchOnProviderEvent = useCallback(() => {
        if (!isAuthenticated()) {
            return;
        }

        void refetchProviders();
        void client.refetchQueries({ include: ['settingsProviders'] });
    }, [client, isAuthenticated, refetchProviders]);

    const subscriptionSkip = !isAuthenticated();
    useSubscription(ProviderCreatedDocument, { onData: refetchOnProviderEvent, skip: subscriptionSkip });
    useSubscription(ProviderUpdatedDocument, { onData: refetchOnProviderEvent, skip: subscriptionSkip });
    useSubscription(ProviderDeletedDocument, { onData: refetchOnProviderEvent, skip: subscriptionSkip });

    const providers = useMemo(() => sortProviders(providersData?.providers || []), [providersData?.providers]);

    const [selectedProviderName, setSelectedProviderName] = useState<null | string>(() => {
        return localStorage.getItem(SELECTED_PROVIDER_KEY);
    });

    const { data: growthSettings, refetch: refetchGrowthSettings } = useGrowthPoll(
        isAuthenticated() ? 'settings:default-provider' : null,
        () => growthApi.listSettings(),
        null,
    );
    const llmProviderType = growthSettings?.find((setting) => setting.kind === 'llm')?.provider;

    // Choosing a provider on the growth LLM card overrides the remembered pick once, so the two never disagree;
    // a later pick in the composer stays until the card changes again.
    const [appliedLlmType, setAppliedLlmType] = useState(() => localStorage.getItem(APPLIED_LLM_TYPE_KEY));
    const cardProvider =
        llmProviderType && llmProviderType !== appliedLlmType ? pickDefaultProvider(providers, llmProviderType) : null;

    if (llmProviderType && cardProvider?.type === llmProviderType) {
        setAppliedLlmType(llmProviderType);
        setSelectedProviderName(cardProvider.name);
    }

    useEffect(() => {
        if (appliedLlmType) {
            localStorage.setItem(APPLIED_LLM_TYPE_KEY, appliedLlmType);
        }
    }, [appliedLlmType]);

    useEffect(() => {
        if (selectedProviderName) {
            localStorage.setItem(SELECTED_PROVIDER_KEY, selectedProviderName);
        }
    }, [selectedProviderName]);

    const selectedProvider = useMemo(() => {
        const savedProvider = selectedProviderName ? findProviderByName(selectedProviderName, providers) : undefined;

        return savedProvider ?? pickDefaultProvider(providers, llmProviderType);
    }, [llmProviderType, providers, selectedProviderName]);

    const refreshProviders = useCallback(() => {
        if (!isAuthenticated()) {
            return;
        }

        void refetchProviders();
        void refetchGrowthSettings();
    }, [isAuthenticated, refetchGrowthSettings, refetchProviders]);

    const setSelectedProvider = useCallback((provider: Provider) => {
        setSelectedProviderName(provider.name);
    }, []);

    const value = useMemo(
        () => ({
            providers,
            refreshProviders,
            selectedProvider,
            setSelectedProvider,
        }),
        [providers, refreshProviders, selectedProvider, setSelectedProvider],
    );

    return <ProvidersContext.Provider value={value}>{children}</ProvidersContext.Provider>;
}

export function useProviders() {
    const context = useContext(ProvidersContext);

    if (context === undefined) {
        throw new Error('useProviders must be used within a ProvidersProvider');
    }

    return context;
}
