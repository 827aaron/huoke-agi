import { useEffect, useState } from 'react';

import Logo from '@/components/icons/logo';
import { copy } from '@/lib/copy';
import { routes } from '@/lib/routes';

function OAuthResult() {
    const [statusMessage, setStatusMessage] = useState(copy.oauth.inProgress);

    const successDelay = 2000;
    const errorDelay = 5000;

    useEffect(() => {
        const params = new URLSearchParams(window.location.search);
        const status = params.get('status');
        const error = params.get('error');

        let redirectTimer: null | ReturnType<typeof setTimeout> = null;
        let cleanupTimer: null | ReturnType<typeof setTimeout> = null;
        let closeTimer: null | ReturnType<typeof setTimeout> = null;

        const updateMessage = (message: string) => {
            setStatusMessage(message);
        };

        const handleClose = (delay: number) => {
            closeTimer = setTimeout(() => {
                try {
                    if (window && !window.closed) {
                        window.close();
                    }
                } catch (e) {
                    console.error('Delayed window close failed:', e);
                }
            }, delay);
        };

        const handleRedirect = (url: string, delay: number) => {
            redirectTimer = setTimeout(() => {
                try {
                    window.location.href = url;
                } catch (e) {
                    console.error('Redirection failed:', e);
                }
            }, delay);

            cleanupTimer = setTimeout(() => {
                if (redirectTimer) {
                    clearTimeout(redirectTimer);
                    redirectTimer = null;
                }
            }, delay + 100);
        };

        if (window.opener) {
            try {
                window.opener.postMessage(
                    {
                        error,
                        status,
                        type: 'oauth-result',
                    },
                    window.location.origin,
                );

                updateMessage(copy.oauth.complete);
                handleClose(successDelay);
            } catch (e) {
                console.error('Failed to send message to opener:', e);
                updateMessage(copy.oauth.parentError);
                handleClose(errorDelay);
            }
        } else {
            updateMessage(copy.oauth.direct);
            handleRedirect(routes.login(), errorDelay / 2);
            handleClose(errorDelay);
        }

        return () => {
            if (redirectTimer) {
                clearTimeout(redirectTimer);
            }

            if (cleanupTimer) {
                clearTimeout(cleanupTimer);
            }

            if (closeTimer) {
                clearTimeout(closeTimer);
            }
        };
    }, [successDelay, errorDelay]);

    return (
        <div className="flex h-screen w-full items-center justify-center bg-linear-to-r from-slate-800 to-slate-950">
            <Logo
                className="m-auto w-72"
                tone="dark"
                variant="horizontal"
            />
            <div className="fixed bottom-4 text-sm text-white">{statusMessage}</div>
        </div>
    );
}

export default OAuthResult;
