import { ProviderType } from '@/graphql/types';

export interface Provider {
    name: string;
    type: ProviderType;
}

export const getProviderDisplayName = (provider: Provider): string => {
    return provider.name;
};

export const isProviderValid = (provider: Provider, providers: Provider[]): boolean => {
    return providers.some((p) => p.name === provider.name && p.type === provider.type);
};

export const isProviderMissing = (provider: Provider, providers: Provider[]): boolean => {
    return providers.length > 0 && !isProviderValid(provider, providers);
};

export const findProviderByName = (providerName: string, providers: Provider[]): Provider | undefined => {
    return providers.find((provider) => provider.name === providerName);
};

export const sortProviders = (providers: Provider[]): Provider[] => {
    return [...providers].sort((a, b) => a.name.localeCompare(b.name));
};

// Prefers the provider of the type picked on the growth LLM card, and among those the env-backed default whose
// name is the type itself; without a match the first provider stays the default.
export const pickDefaultProvider = (providers: Provider[], preferredType?: null | string): null | Provider => {
    const matching = preferredType ? providers.filter((provider) => provider.type === preferredType) : [];

    return matching.find((provider) => provider.name === preferredType) ?? matching[0] ?? providers[0] ?? null;
};
