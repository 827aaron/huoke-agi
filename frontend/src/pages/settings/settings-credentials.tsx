import { Info, KeyRound } from 'lucide-react';
import { useEffect, useState } from 'react';
import { Link } from 'react-router-dom';

import type { GrowthSetting, GrowthSettingKind } from '@/features/growth/growth-types';

import { AppHeader, AppHeaderContent, AppHeaderTitle } from '@/components/layouts/app/app-header';
import { Alert, AlertDescription } from '@/components/ui/alert';
import { Button } from '@/components/ui/button';
import { Spinner } from '@/components/ui/spinner';
import { growthApi } from '@/features/growth/growth-api';
import { isAccountSignedIn } from '@/features/growth/growth-types';
import { GrowthCredentialCard, REQUIRED_KINDS, settingState } from '@/features/growth/settings/growth-credential-row';
import { useGrowthAccounts, useGrowthSettings } from '@/features/growth/use-growth-data';
import { copy } from '@/lib/copy';
import { routes } from '@/lib/routes';

const text = copy.credentials;

const CARD_ORDER: GrowthSettingKind[] = ['llm', 'telegram', 'web_search', 'youtube', 'proxy', 'email'];

function SettingsCredentials() {
    const { data, error, isLoading } = useGrowthSettings();
    const { data: accounts } = useGrowthAccounts();

    const [settings, setSettings] = useState<GrowthSetting[] | null>(null);
    const [seededFrom, setSeededFrom] = useState<GrowthSetting[] | undefined>(undefined);
    const [testingKinds, setTestingKinds] = useState<Set<GrowthSettingKind>>(new Set());
    const [autoTestKinds, setAutoTestKinds] = useState<GrowthSettingKind[] | null>(null);

    if (data !== seededFrom) {
        setSeededFrom(data);
        setSettings(data ?? null);

        if (data && autoTestKinds === null) {
            const saved = data.filter((item) => item.source !== 'none').map((item) => item.kind);

            setAutoTestKinds(saved);
            setTestingKinds(new Set(saved));
        }
    }

    // On open, every saved item is re-tested once without any toast, so the badges reflect the keys as they
    // are now rather than when they were saved.
    useEffect(() => {
        for (const kind of autoTestKinds ?? []) {
            growthApi
                .testSetting(kind)
                .then((tested) =>
                    setSettings(
                        (previous) => previous?.map((item) => (item.kind === tested.kind ? tested : item)) ?? null,
                    ),
                )
                .catch(() => {})
                .finally(() =>
                    setTestingKinds((previous) => {
                        const next = new Set(previous);
                        next.delete(kind);

                        return next;
                    }),
                );
        }
    }, [autoTestKinds]);

    const byKind = (kind: GrowthSettingKind) => settings?.find((item) => item.kind === kind);

    const handleChange = (kind: GrowthSettingKind, next: GrowthSetting | null) =>
        setSettings((previous) => {
            const rest = (previous ?? []).filter((item) => item.kind !== kind);

            return next ? [...rest, next] : rest;
        });

    const isTelegramSignedIn = !!accounts?.some(isAccountSignedIn);
    const isReady = REQUIRED_KINDS.every((kind) => settingState(byKind(kind)) === 'connected');
    const isEmpty = !!settings && settings.every((item) => item.source === 'none');

    return (
        <>
            <AppHeader>
                <AppHeaderContent>
                    <AppHeaderTitle icon={<KeyRound className="size-4 shrink-0" />}>{text.title}</AppHeaderTitle>
                </AppHeaderContent>
            </AppHeader>
            <div className="mx-auto flex w-full max-w-2xl flex-col gap-4 p-4">
                <p className="text-muted-foreground text-sm">{text.description}</p>

                {isLoading ? (
                    <div className="flex justify-center py-8">
                        <Spinner variant="circle" />
                    </div>
                ) : error ? (
                    <div className="text-destructive text-sm">{text.loadFailed}</div>
                ) : (
                    <>
                        {isEmpty && (
                            <Alert>
                                <Info />
                                <AlertDescription>{text.emptyHint}</AlertDescription>
                            </Alert>
                        )}
                        {isReady && (
                            <Alert>
                                <Info />
                                <AlertDescription className="flex flex-wrap items-center gap-2">
                                    {text.ready}
                                    <Button
                                        asChild
                                        size="sm"
                                    >
                                        <Link to={routes.newFlow}>{text.readyLink}</Link>
                                    </Button>
                                </AlertDescription>
                            </Alert>
                        )}

                        {CARD_ORDER.map((kind) => {
                            const setting = byKind(kind);

                            return (
                                <GrowthCredentialCard
                                    isTelegramSignedIn={isTelegramSignedIn}
                                    isTesting={testingKinds.has(kind)}
                                    // Saving or clearing remounts the card, so its edit state starts over.
                                    key={`${kind}:${setting?.source ?? 'none'}:${setting?.provider ?? ''}`}
                                    kind={kind}
                                    onChange={(next) => handleChange(kind, next)}
                                    setting={setting}
                                />
                            );
                        })}
                    </>
                )}
            </div>
        </>
    );
}

export default SettingsCredentials;
