import { useCallback } from 'react';
import { useSearchParams } from 'react-router-dom';

import { SIDE_TAB_PARAM } from '@/hooks/use-flow-tab-detection';
import { getLatestSearchParams } from '@/lib/url-params';

import type { GrowthLeadTier } from './growth-types';

export const LEAD_TIER_PARAM = 'leadTier';
export const LEAD_SOURCE_PARAM = 'leadSource';
export const SOURCE_QUERY_PARAM = 'sourceQuery';

// Sets both `tab` (read on mobile) and `side` (read by the desktop side panel), so one call opens the
// panel whichever layout is on screen.
export function useGrowthTabLink() {
    const [, setSearchParams] = useSearchParams();

    const open = useCallback(
        (tab: 'leads' | 'sources', extra: Record<string, null | string> = {}) =>
            setSearchParams((previous) => {
                const params = getLatestSearchParams(previous);

                params.set('tab', tab);
                params.set(SIDE_TAB_PARAM, tab);

                for (const [key, value] of Object.entries(extra)) {
                    if (value === null) {
                        params.delete(key);
                    } else {
                        params.set(key, value);
                    }
                }

                return params;
            }),
        [setSearchParams],
    );

    const openLeads = useCallback(
        ({ sourceId = null, tier = null }: { sourceId?: null | number; tier?: GrowthLeadTier | null } = {}) =>
            open('leads', {
                [LEAD_SOURCE_PARAM]: sourceId === null ? null : String(sourceId),
                [LEAD_TIER_PARAM]: tier,
            }),
        [open],
    );

    const openSources = useCallback(
        (query: null | string = null) => open('sources', { [SOURCE_QUERY_PARAM]: query }),
        [open],
    );

    return { openLeads, openSources };
}
