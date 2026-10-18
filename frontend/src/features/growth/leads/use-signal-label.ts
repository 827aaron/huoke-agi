import { useCallback, useMemo } from 'react';

import { copy } from '@/lib/copy';

import { useGrowthFlow } from '../growth-flow-provider';

// A built-in signal reads from the copy; a dimension the owner added reads by its name in any saved profile.
export function useSignalLabel(): (signal: string) => string {
    const { profiles } = useGrowthFlow();

    const names = useMemo(
        () =>
            new Map(
                profiles.flatMap((profile) =>
                    (profile.scoring?.dimensions ?? []).map((dimension) => [dimension.key, dimension.name] as const),
                ),
            ),
        [profiles],
    );

    return useCallback((signal: string) => copy.leads.signal[signal] ?? names.get(signal) ?? signal, [names]);
}
