import { useCallback, useMemo } from 'react';

import { copy } from '@/lib/copy';

import { useGrowthFlow } from '../growth-flow-provider';
import { signalNames } from '../profile-scoring';

// A built-in signal reads from the copy; a dimension the owner added reads by its name in any saved profile.
export function useSignalLabel(): (signal: string) => string {
    const { profiles } = useGrowthFlow();

    const names = useMemo(() => {
        const found = new Map<string, string>();

        for (const profile of profiles) {
            for (const [key, name] of signalNames(profile.scoring ?? '')) {
                found.set(key, name);
            }
        }

        return found;
    }, [profiles]);

    return useCallback((signal: string) => copy.leads.signal[signal] ?? names.get(signal) ?? signal, [names]);
}
