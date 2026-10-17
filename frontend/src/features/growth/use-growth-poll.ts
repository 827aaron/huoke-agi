import { useCallback, useEffect, useRef, useState } from 'react';

export const GROWTH_POLL_INTERVAL_MS = 5000;

interface GrowthPollState<T> {
    data: T | undefined;
    error: unknown;
    isLoading: boolean;
    refetch: () => Promise<void>;
}

// Growth data has no subscription yet, so it is polled. A hidden tab skips its ticks, and a response that
// arrives after the key changed is dropped so one flow's data never lands on another.
export function useGrowthPoll<T>(
    key: null | string,
    fetcher: () => Promise<T>,
    intervalMs: null | number = GROWTH_POLL_INTERVAL_MS,
): GrowthPollState<T> {
    const [data, setData] = useState<T>();
    const [error, setError] = useState<unknown>(null);
    const [isLoading, setIsLoading] = useState(key !== null);
    const [stateKey, setStateKey] = useState(key);

    if (key !== stateKey) {
        setStateKey(key);
        setData(undefined);
        setError(null);
        setIsLoading(key !== null);
    }

    const fetcherRef = useRef(fetcher);
    const keyRef = useRef(key);

    useEffect(() => {
        fetcherRef.current = fetcher;
    });

    const load = useCallback(async () => {
        const requestKey = keyRef.current;

        if (requestKey === null) {
            return;
        }

        try {
            const next = await fetcherRef.current();

            if (keyRef.current === requestKey) {
                setData(next);
                setError(null);
            }
        } catch (loadError) {
            if (keyRef.current === requestKey) {
                setError(loadError);
            }
        } finally {
            if (keyRef.current === requestKey) {
                setIsLoading(false);
            }
        }
    }, []);

    useEffect(() => {
        keyRef.current = key;

        if (key === null) {
            return;
        }

        void load();

        if (!intervalMs) {
            return;
        }

        const timer = setInterval(() => {
            if (!document.hidden) {
                void load();
            }
        }, intervalMs);

        return () => clearInterval(timer);
    }, [key, intervalMs, load]);

    return { data, error, isLoading, refetch: load };
}
