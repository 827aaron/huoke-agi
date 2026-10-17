import { TriangleAlert } from 'lucide-react';
import { useEffect, useSyncExternalStore } from 'react';
import { useRouteError } from 'react-router-dom';

import { Button } from '@/components/ui/button';
import { Empty, EmptyContent, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from '@/components/ui/empty';
import { isChunkLoadError, isDomDesyncError, reloadOnce } from '@/lib/chunk-reload';
import { copy } from '@/lib/copy';

const subscribeToConnection = (onChange: () => void) => {
    window.addEventListener('online', onChange);
    window.addEventListener('offline', onChange);

    return () => {
        window.removeEventListener('online', onChange);
        window.removeEventListener('offline', onChange);
    };
};

const isBrowserOnline = () => navigator.onLine;

const describeFailure = ({
    isChunk,
    isDesync,
    isOnline,
}: {
    isChunk: boolean;
    isDesync: boolean;
    isOnline: boolean;
}) => {
    if (isChunk && !isOnline) {
        return copy.routeError.offline;
    }

    if (isChunk) {
        return copy.routeError.chunk;
    }

    if (isDesync) {
        return copy.routeError.desync;
    }

    return copy.routeError.unexpected;
};

/**
 * Root `errorElement` for the data router — replaces React Router's built-in
 * dev crash screen. Two production failures land here: a code-split chunk that
 * 404s after a redeploy (auto-reload once, unless `vite:preloadError` in
 * `main.tsx` already did), and any other render/commit crash (e.g. a `removeChild`
 * desync from browser auto-translation) — both shown as a recoverable card.
 */
function RouteErrorBoundary() {
    const error = useRouteError();
    const isChunk = isChunkLoadError(error);
    const isDesync = isDomDesyncError(error);
    const isOnline = useSyncExternalStore(subscribeToConnection, isBrowserOnline);
    const isWaitingForConnection = isChunk && !isOnline;

    useEffect(() => {
        if (isChunk || isDesync) {
            reloadOnce();
        }
    }, [isChunk, isDesync, isOnline]);

    return (
        <div
            className="grid min-h-svh w-full place-items-center p-4"
            role="alert"
        >
            <Empty>
                <EmptyHeader>
                    <EmptyMedia variant="icon">
                        <TriangleAlert />
                    </EmptyMedia>
                    <EmptyTitle>{copy.routeError.title}</EmptyTitle>
                    <EmptyDescription>{describeFailure({ isChunk, isDesync, isOnline })}</EmptyDescription>
                </EmptyHeader>
                <EmptyContent>
                    <Button
                        disabled={isWaitingForConnection}
                        onClick={() => window.location.reload()}
                        variant="secondary"
                    >
                        {copy.action.reload}
                    </Button>
                </EmptyContent>
            </Empty>
        </div>
    );
}

export default RouteErrorBoundary;
