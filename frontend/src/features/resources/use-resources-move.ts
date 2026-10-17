import { useCallback, useState } from 'react';
import { toast } from 'sonner';
import { z } from 'zod';

import type { OverwriteOutcome } from '@/components/shared/overwrite';

import { api, getApiErrorMessage, getApiErrorStatusCode } from '@/lib/axios';
import { copy } from '@/lib/copy';

import { RESOURCES_MOVE_API_PATH } from './resources-constants';

export const resourcesMoveFormSchema = z.object({
    destination: z
        .string()
        .trim()
        .min(1, { message: copy.resources.destinationEmpty })
        .refine((value) => !value.startsWith('/'), { message: copy.resources.destinationAbsolute })
        .refine((value) => !value.split('/').includes('..'), { message: copy.resources.destinationDotDot }),
});

export type ResourcesMoveFormValues = z.infer<typeof resourcesMoveFormSchema>;

interface MoveRequestBody {
    destination: string;
    force: boolean;
    sources: readonly string[];
}

interface UseResourcesMoveResult {
    isMoving: boolean;
    /**
     * Issue a batch move in a single atomic request and return a discriminated outcome:
     *   - `ok`        — every source was moved (success toast already fired),
     *   - `conflict`  — at least one destination is occupied (no toast, caller
     *                   resolves via the shared overwrite workflow),
     *   - `error`     — anything else (failure toast already fired).
     *
     * Backend semantics: with one source, `destination` is the exact target
     * path; with multiple sources, `destination` is treated as a base directory
     * and each source lands at `destination/<basename>`. Either way, the whole
     * batch executes inside one DB transaction — partial state is impossible.
     */
    move: (sources: readonly string[], destination: string, force: boolean) => Promise<OverwriteOutcome>;
}

/** Wraps `PUT /resources/move` for rename / move / batch-move operations. */
export function useResourcesMove(): UseResourcesMoveResult {
    const [isMoving, setIsMoving] = useState(false);

    const move = useCallback(
        async (sources: readonly string[], destination: string, force: boolean): Promise<OverwriteOutcome> => {
            if (sources.length === 0) {
                return { kind: 'error' };
            }

            setIsMoving(true);

            try {
                await api.put<void, MoveRequestBody>(RESOURCES_MOVE_API_PATH, {
                    destination,
                    force,
                    sources,
                });

                const description =
                    sources.length === 1
                        ? copy.resources.movedTo(destination)
                        : copy.resources.movedInto(sources.length, destination);

                toast.success(copy.resources.moved, { description });

                return { kind: 'ok' };
            } catch (error) {
                // 409 = at least one destination already exists. Surface as
                // `conflict` so the shared overwrite workflow can prompt the
                // user; success / error toasts stay aligned with the outcome
                // they pick.
                if (!force && getApiErrorStatusCode(error) === 409) {
                    return { kind: 'conflict' };
                }

                const description = getApiErrorMessage(error, copy.resources.moveFailedHint);

                toast.error(copy.resources.moveFailed, { description });

                return { kind: 'error' };
            } finally {
                setIsMoving(false);
            }
        },
        [],
    );

    return {
        isMoving,
        move,
    };
}
