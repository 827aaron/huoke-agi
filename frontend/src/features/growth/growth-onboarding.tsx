import { Circle, CircleCheck } from 'lucide-react';
import { Link } from 'react-router-dom';

import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { copy } from '@/lib/copy';
import { routes } from '@/lib/routes';
import { cn } from '@/lib/utils';
import { useSidebarFlows } from '@/providers/sidebar-flows-provider';

import { REQUIRED_KINDS } from './settings/growth-credential-row';
import { useGrowthAccounts, useGrowthProfiles, useGrowthSettings } from './use-growth-data';

// Each step ticks itself off from live data, and the card is gone for good once all four are done.
function GrowthOnboarding() {
    const { data: settings, isLoading: isSettingsLoading } = useGrowthSettings();
    const { data: accounts, isLoading: isAccountsLoading } = useGrowthAccounts();
    const { data: profiles, isLoading: isProfilesLoading } = useGrowthProfiles();
    const { flows } = useSidebarFlows();

    if (isSettingsLoading || isAccountsLoading || isProfilesLoading) {
        return null;
    }

    const steps = [
        {
            isDone: REQUIRED_KINDS.every((kind) =>
                settings?.some((item) => item.kind === kind && item.status === 'connected'),
            ),
            label: copy.onboarding.stepCredentials,
            to: routes.settings.credentials,
        },
        {
            isDone: !!accounts?.some((account) => account.status === 'connected'),
            label: copy.onboarding.stepAccount,
            to: routes.settings.telegram,
        },
        { isDone: !!profiles?.length, label: copy.onboarding.stepProfile, to: routes.settings.growthProfile },
        { isDone: flows.length > 0, label: copy.onboarding.stepFlow, to: routes.newFlow },
    ];

    if (steps.every((step) => step.isDone)) {
        return null;
    }

    return (
        <Card className="gap-3 py-4">
            <CardHeader className="px-4">
                <CardTitle className="text-base">{copy.onboarding.title}</CardTitle>
            </CardHeader>
            <CardContent className="flex flex-col gap-2 px-4 sm:flex-row sm:flex-wrap sm:gap-6">
                {steps.map((step, index) => (
                    <Link
                        className={cn(
                            'flex items-center gap-2 text-sm hover:underline',
                            step.isDone && 'text-muted-foreground line-through',
                        )}
                        key={step.to}
                        to={step.to}
                    >
                        {step.isDone ? (
                            <CircleCheck className="size-4 text-green-500" />
                        ) : (
                            <Circle className="size-4" />
                        )}
                        {index + 1}. {step.label}
                    </Link>
                ))}
            </CardContent>
        </Card>
    );
}

export default GrowthOnboarding;
