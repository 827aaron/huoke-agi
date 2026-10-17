import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';

const apollo = vi.hoisted(() => ({ refetch: vi.fn(async () => undefined) }));
const growth = vi.hoisted(() => ({ listSettings: vi.fn(async () => []) }));

vi.mock('@apollo/client/react', () => ({
    useApolloClient: () => ({ refetchQueries: vi.fn() }),
    useQuery: () => ({ data: { providers: [{ name: 'openrouter', type: 'openrouter' }] }, refetch: apollo.refetch }),
    useSubscription: () => undefined,
}));
vi.mock('@/features/growth/growth-api', () => ({ growthApi: growth }));
vi.mock('@/providers/user-provider', () => ({ useUser: () => ({ isAuthenticated: () => true }) }));

import { ProvidersProvider, useProviders } from './providers-provider';

function Probe() {
    const { providers, refreshProviders } = useProviders();

    return (
        <button
            onClick={refreshProviders}
            type="button"
        >
            {providers.map((provider) => provider.name).join(',')}
        </button>
    );
}

describe('ProvidersProvider', () => {
    beforeEach(() => vi.clearAllMocks());

    it('refetches the providers and the growth model setting on refresh', async () => {
        render(
            <ProvidersProvider>
                <Probe />
            </ProvidersProvider>,
        );

        expect(screen.getByText('openrouter')).toBeInTheDocument();
        await waitFor(() => expect(growth.listSettings).toHaveBeenCalledTimes(1));
        expect(apollo.refetch).not.toHaveBeenCalled();

        fireEvent.click(screen.getByRole('button', { name: 'openrouter' }));

        expect(apollo.refetch).toHaveBeenCalledTimes(1);
        await waitFor(() => expect(growth.listSettings).toHaveBeenCalledTimes(2));
    });
});
