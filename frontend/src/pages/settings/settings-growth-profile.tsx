import { Briefcase, Copy, Info, Plus, Star, Trash2 } from 'lucide-react';
import { useState } from 'react';
import { toast } from 'sonner';

import { AppHeader, AppHeaderContent, AppHeaderTitle } from '@/components/layouts/app/app-header';
import ConfirmationDialog from '@/components/shared/confirmation-dialog';
import { Alert, AlertDescription } from '@/components/ui/alert';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Spinner } from '@/components/ui/spinner';
import { growthApi } from '@/features/growth/growth-api';
import { GrowthProfileForm } from '@/features/growth/settings/growth-profile-form';
import { useGrowthProfiles } from '@/features/growth/use-growth-data';
import { getApiErrorMessage } from '@/lib/axios';
import { copy } from '@/lib/copy';

const { list } = copy.profile;

// 'new' is a blank profile not saved yet; null follows the default profile.
type Selection = 'new' | null | number;

function SettingsGrowthProfile() {
    const { data: profiles, error, isLoading, refetch } = useGrowthProfiles();
    const [selection, setSelection] = useState<Selection>(null);
    const [isDeleteOpen, setIsDeleteOpen] = useState(false);

    const saved = profiles ?? [];
    const profile =
        selection === 'new'
            ? null
            : (saved.find((item) => item.id === selection) ?? saved.find((item) => item.default) ?? saved[0] ?? null);

    const run = async (action: () => Promise<unknown>, done: string) => {
        try {
            await action();
            toast.success(done);
        } catch (actionError) {
            toast.error(getApiErrorMessage(actionError, copy.common.saveFailed));
        }
    };

    const handleCopy = () =>
        profile &&
        run(async () => {
            const { default: _default, id: _id, updated_at: _updated, ...input } = profile;
            const created = await growthApi.createProfile({ ...input, title: `${profile.title}${list.copySuffix}` });

            await refetch();
            setSelection(created.id);
        }, list.copied);

    const handleDefault = () =>
        profile &&
        run(async () => {
            await growthApi.setDefaultProfile(profile.id);
            await refetch();
        }, list.defaultSet);

    const handleDelete = async () => {
        if (!profile) {
            return;
        }

        await run(async () => {
            await growthApi.deleteProfile(profile.id);
            setSelection(null);
            await refetch();
        }, list.deleted);
        setIsDeleteOpen(false);
    };

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
                {!isLoading && !saved.length && !error && (
                    <Alert>
                        <Info />
                        <AlertDescription>{copy.profile.emptyHint}</AlertDescription>
                    </Alert>
                )}
                {!isLoading && (
                    <div className="flex flex-wrap gap-2">
                        {saved.map((item) => (
                            <Button
                                key={item.id}
                                onClick={() => setSelection(item.id)}
                                size="sm"
                                variant={profile?.id === item.id && selection !== 'new' ? 'default' : 'outline'}
                            >
                                {item.title || item.name}
                                {item.default && <Badge variant="secondary">{list.default}</Badge>}
                            </Button>
                        ))}
                        <Button
                            onClick={() => setSelection('new')}
                            size="sm"
                            variant={selection === 'new' ? 'default' : 'ghost'}
                        >
                            <Plus />
                            {list.add}
                        </Button>
                    </div>
                )}
                <Card>
                    <CardHeader>
                        <CardTitle>{profile ? profile.title || profile.name : list.newTitle}</CardTitle>
                        <CardDescription>{copy.profile.description}</CardDescription>
                        {profile && (
                            <div className="flex flex-wrap gap-2 pt-2">
                                {!profile.default && (
                                    <Button
                                        onClick={handleDefault}
                                        size="sm"
                                        variant="outline"
                                    >
                                        <Star />
                                        {list.setDefault}
                                    </Button>
                                )}
                                <Button
                                    onClick={handleCopy}
                                    size="sm"
                                    variant="outline"
                                >
                                    <Copy />
                                    {list.copy}
                                </Button>
                                <Button
                                    onClick={() => setIsDeleteOpen(true)}
                                    size="sm"
                                    variant="outline"
                                >
                                    <Trash2 />
                                    {list.delete}
                                </Button>
                            </div>
                        )}
                    </CardHeader>
                    <CardContent>
                        {isLoading ? (
                            <div className="flex justify-center py-8">
                                <Spinner variant="circle" />
                            </div>
                        ) : (
                            <GrowthProfileForm
                                key={profile?.id ?? 'new'}
                                onSaved={async (next) => {
                                    await refetch();
                                    setSelection(next.id);
                                }}
                                profile={profile}
                            />
                        )}
                    </CardContent>
                </Card>
            </div>
            <ConfirmationDialog
                confirmText={list.delete}
                description={profile ? list.deleteConfirm(profile.title || profile.name) : undefined}
                handleConfirm={handleDelete}
                handleOpenChange={setIsDeleteOpen}
                isOpen={isDeleteOpen}
                title={list.deleteTitle}
            />
        </>
    );
}

export default SettingsGrowthProfile;
