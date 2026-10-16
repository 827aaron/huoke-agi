import { Info, KeyRound } from 'lucide-react';
import { useEffect, useState } from 'react';
import { Link } from 'react-router-dom';

import type { GrowthSetting } from '@/features/growth/growth-types';

import { AppHeader, AppHeaderContent, AppHeaderTitle } from '@/components/layouts/app/app-header';
import { Alert, AlertDescription } from '@/components/ui/alert';
import { Button } from '@/components/ui/button';
import { Spinner } from '@/components/ui/spinner';
import { growthApi } from '@/features/growth/growth-api';
import { isAccountUsable } from '@/features/growth/growth-types';
import { GrowthCredentialCard, REQUIRED_KINDS } from '@/features/growth/settings/growth-credential-row';
import { useGrowthAccounts, useGrowthSettings } from '@/features/growth/use-growth-data';
import { copy } from '@/lib/copy';
import { routes } from '@/lib/routes';

const text = copy.credentials;

function SettingsCredentials() {
    const { data, error, isLoading } = useGrowthSettings();
    const { data: accounts } = useGrowthAccounts();

    const [settings, setSettings] = useState<GrowthSetting[] | null>(null);
    const [seededFrom, setSeededFrom] = useState<GrowthSetting[] | undefined>(undefined);
    const [testingIds, setTestingIds] = useState<Set<string>>(new Set());
    const [extraSearchCards, setExtraSearchCards] = useState(0);
    const [autoTestIds, setAutoTestIds] = useState<null | string[]>(null);

    if (data !== seededFrom) {
        setSeededFrom(data);
        setSettings(data ?? null);

        if (data?.length && autoTestIds === null) {
            setAutoTestIds(data.map((item) => item.id));
            setTestingIds(new Set(data.map((item) => item.id)));
        }
    }

    // On open, every saved item is re-tested once without any toast, so the badges reflect the keys as they
    // are now rather than when they were saved.
    useEffect(() => {
        for (const id of autoTestIds ?? []) {
            growthApi
                .testSetting(id)
                .then((tested) =>
                    setSettings((previous) => previous?.map((s) => (s.id === tested.id ? tested : s)) ?? null),
                )
                .catch(() => {})
                .finally(() =>
                    setTestingIds((previous) => {
                        const next = new Set(previous);
                        next.delete(id);

                        return next;
                    }),
                );
        }
    }, [autoTestIds]);

    const handleChange = (previousId: null | string, next: GrowthSetting | null) =>
        setSettings((previous) => {
            const rest = (previous ?? []).filter((item) => item.id !== previousId && item.id !== next?.id);

            return next ? [...rest, next] : rest;
        });

    const byKind = (kind: GrowthSetting['kind']) => (settings ?? []).filter((item) => item.kind === kind);
    const searchSettings = byKind('web_search');
    const searchCardCount = Math.max(1, searchSettings.length + extraSearchCards);
    const isTelegramSignedIn = !!accounts?.some(isAccountUsable);
    const isReady = REQUIRED_KINDS.every((kind) => byKind(kind)[0]?.status === 'connected');
    const isEmpty = !!settings && settings.length === 0;

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

                        {(['llm', 'telegram'] as const).map((kind) => {
                            const setting = byKind(kind)[0];

                            return (
                                <GrowthCredentialCard
                                    isTelegramSignedIn={isTelegramSignedIn}
                                    isTesting={!!setting && testingIds.has(setting.id)}
                                    key={setting?.id ?? kind}
                                    kind={kind}
                                    onChange={handleChange}
                                    setting={setting}
                                />
                            );
                        })}

                        {Array.from({ length: searchCardCount }, (_, index) => {
                            const setting = searchSettings[index];

                            return (
                                <GrowthCredentialCard
                                    isTesting={!!setting && testingIds.has(setting.id)}
                                    key={setting?.id ?? `web_search-draft-${index}`}
                                    kind="web_search"
                                    onChange={(previousId, next) => {
                                        if (!setting && next) {
                                            setExtraSearchCards((count) => Math.max(0, count - 1));
                                        }

                                        handleChange(previousId, next);
                                    }}
                                    setting={setting}
                                />
                            );
                        })}
                        {searchSettings.length > 0 && searchCardCount === searchSettings.length && (
                            <Button
                                className="self-start"
                                onClick={() => setExtraSearchCards((count) => count + 1)}
                                size="sm"
                                variant="ghost"
                            >
                                {text.addSearch}
                            </Button>
                        )}

                        {(['youtube', 'email'] as const).map((kind) => {
                            const setting = byKind(kind)[0];

                            return (
                                <GrowthCredentialCard
                                    isTesting={!!setting && testingIds.has(setting.id)}
                                    key={setting?.id ?? kind}
                                    kind={kind}
                                    onChange={handleChange}
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
