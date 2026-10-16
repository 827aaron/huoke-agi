import { Briefcase, Info } from 'lucide-react';

import { AppHeader, AppHeaderContent, AppHeaderTitle } from '@/components/layouts/app/app-header';
import { Alert, AlertDescription } from '@/components/ui/alert';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Spinner } from '@/components/ui/spinner';
import { GrowthProfileForm } from '@/features/growth/settings/growth-profile-form';
import { useGrowthProfiles } from '@/features/growth/use-growth-data';
import { copy } from '@/lib/copy';

function SettingsGrowthProfile() {
    const { data: profiles, error, isLoading, refetch } = useGrowthProfiles();
    const profile = profiles?.[0] ?? null;

    return (
        <>
            <AppHeader>
                <AppHeaderContent>
                    <AppHeaderTitle icon={<Briefcase className="size-4 shrink-0" />}>
                        {copy.profile.title}
                    </AppHeaderTitle>
                </AppHeaderContent>
            </AppHeader>
            <div className="mx-auto flex w-full max-w-2xl flex-col gap-4 p-4">
                {!isLoading && !profile && !error && (
                    <Alert>
                        <Info />
                        <AlertDescription>{copy.profile.emptyHint}</AlertDescription>
                    </Alert>
                )}
                <Card>
                    <CardHeader>
                        <CardTitle>{copy.profile.title}</CardTitle>
                        <CardDescription>{copy.profile.description}</CardDescription>
                    </CardHeader>
                    <CardContent>
                        {isLoading ? (
                            <div className="flex justify-center py-8">
                                <Spinner variant="circle" />
                            </div>
                        ) : (
                            <GrowthProfileForm
                                key={profile?.id ?? 'new'}
                                onSaved={() => refetch()}
                                profile={profile}
                            />
                        )}
                    </CardContent>
                </Card>
            </div>
        </>
    );
}

export default SettingsGrowthProfile;
