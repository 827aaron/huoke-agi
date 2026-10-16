import { TriangleAlert, X } from 'lucide-react';
import { useState } from 'react';
import { Link } from 'react-router-dom';

import { Button } from '@/components/ui/button';
import { copy } from '@/lib/copy';
import { routes } from '@/lib/routes';

import { isAccountUsable } from './growth-types';
import { useGrowthAccounts, useGrowthProfiles } from './use-growth-data';

// Hints only: an unconfigured workspace may still start a project.
function GrowthSetupHints() {
    const { data: accounts, isLoading: isAccountsLoading } = useGrowthAccounts();
    const { data: profiles, isLoading: isProfilesLoading } = useGrowthProfiles();
    const [dismissed, setDismissed] = useState<Set<string>>(new Set());

    const hints = [
        {
            id: 'account',
            isMissing: !isAccountsLoading && !!accounts && !accounts.some(isAccountUsable),
            link: copy.newFlow.missingAccountLink,
            text: copy.newFlow.missingAccount,
            to: routes.settings.telegram,
        },
        {
            id: 'profile',
            isMissing: !isProfilesLoading && !!profiles && !profiles.length,
            link: copy.newFlow.missingProfileLink,
            text: copy.newFlow.missingProfile,
            to: routes.settings.growthProfile,
        },
    ].filter((hint) => hint.isMissing && !dismissed.has(hint.id));

    if (!hints.length) {
        return null;
    }

    return (
        <div className="flex flex-col gap-2">
            {hints.map((hint) => (
                <div
                    className="flex items-start gap-2 rounded-md border border-yellow-500/30 bg-yellow-500/10 px-3 py-2 text-sm text-yellow-900 dark:text-yellow-300"
                    key={hint.id}
                >
                    <TriangleAlert className="mt-0.5 size-4 shrink-0" />
                    <span className="flex-1">
                        {hint.text}{' '}
                        <Link
                            className="font-medium underline"
                            to={hint.to}
                        >
                            {hint.link}
                        </Link>
                    </span>
                    <Button
                        aria-label="close"
                        className="size-5"
                        onClick={() => setDismissed((previous) => new Set(previous).add(hint.id))}
                        size="icon"
                        variant="ghost"
                    >
                        <X className="size-3" />
                    </Button>
                </div>
            ))}
        </div>
    );
}

export default GrowthSetupHints;
