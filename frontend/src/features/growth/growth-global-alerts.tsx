import { TriangleAlert } from 'lucide-react';
import { useEffect, useRef } from 'react';
import { Link, useLocation, useNavigate } from 'react-router-dom';
import { toast } from 'sonner';

import { copy } from '@/lib/copy';
import { routes } from '@/lib/routes';

import type { GrowthAccount } from './growth-types';

import { accountTransitions } from './account-health';
import { growthApi } from './growth-api';
import { ACCOUNTS_POLL_INTERVAL_MS, useGrowthAccounts } from './use-growth-data';
import { useGrowthPoll } from './use-growth-poll';

const SETTINGS_POLL_INTERVAL_MS = 60_000;

const settingTitle = (kind: string) => copy.credentials.card[kind]?.title ?? kind;

function GrowthGlobalAlerts() {
    const { pathname } = useLocation();
    const { data: settings } = useGrowthPoll(
        'settings:global',
        () => growthApi.listSettings(),
        SETTINGS_POLL_INTERVAL_MS,
    );

    useAccountNotices();

    const invalid = settings?.filter((item) => item.source !== 'none' && item.status === 'invalid') ?? [];

    if (!invalid.length || pathname.startsWith(routes.settings.credentials)) {
        return null;
    }

    return (
        <div className="flex flex-col border-b border-yellow-500/30 bg-yellow-500/10 px-4 py-2 text-sm text-yellow-900 dark:text-yellow-300">
            {invalid.map((item) => (
                <div
                    className="flex items-center gap-2"
                    key={item.kind}
                >
                    <TriangleAlert className="size-4 shrink-0" />
                    <span>{copy.credentials.globalInvalid(settingTitle(item.kind))}</span>
                    <Link
                        className="font-medium underline"
                        to={routes.settings.credentials}
                    >
                        {copy.credentials.globalInvalidLink}
                    </Link>
                </div>
            ))}
        </div>
    );
}

// Announces account status changes as toasts while any page is open. The first snapshot only sets the
// baseline, so opening the app never replays old events.
function useAccountNotices() {
    const { data } = useGrowthAccounts(ACCOUNTS_POLL_INTERVAL_MS);
    const navigate = useNavigate();
    const previous = useRef<GrowthAccount[] | null>(null);

    useEffect(() => {
        if (!data) {
            return;
        }

        if (previous.current) {
            for (const notice of accountTransitions(previous.current, data)) {
                const action = notice.link
                    ? { label: copy.telegram.health.reloginLink, onClick: () => navigate(routes.settings.accounts) }
                    : undefined;

                if (notice.isPersistent) {
                    toast.error(notice.message, { action, closeButton: true, duration: Infinity });
                } else {
                    toast(notice.message, { action });
                }
            }
        }

        previous.current = data;
    }, [data, navigate]);
}

export default GrowthGlobalAlerts;
