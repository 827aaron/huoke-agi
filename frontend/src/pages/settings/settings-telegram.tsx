import { Info, Plus, Send, Terminal, TriangleAlert } from 'lucide-react';
import { useEffect, useState } from 'react';
import { Link } from 'react-router-dom';
import { toast } from 'sonner';

import type { GrowthAccount, GrowthAccountStatus } from '@/features/growth/growth-types';

import { AppHeader, AppHeaderContent, AppHeaderTitle } from '@/components/layouts/app/app-header';
import { Alert, AlertDescription } from '@/components/ui/alert';
import { Badge, type BadgeVariant } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Spinner } from '@/components/ui/spinner';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table';
import { accountName, accountsBanner, accountsBannerText, formatClock } from '@/features/growth/account-health';
import { growthApi } from '@/features/growth/growth-api';
import { ACCOUNTS_POLL_INTERVAL_MS, useGrowthAccounts, useGrowthSettings } from '@/features/growth/use-growth-data';
import { getApiErrorMessage } from '@/lib/axios';
import { copy } from '@/lib/copy';
import { routes } from '@/lib/routes';

const health = copy.telegram.health;

const statusVariant: Record<GrowthAccountStatus, BadgeVariant> = {
    connected: 'green',
    disabled: 'secondary',
    flood_wait: 'yellow',
    restricted: 'red',
    signed_out: 'red',
};

const statusHint = (account: GrowthAccount): string =>
    account.status === 'flood_wait'
        ? health.hint.flood_wait(account.resume_at ? formatClock(new Date(account.resume_at)) : copy.common.dash)
        : health.hint[account.status];

const limitText = (account: GrowthAccount, now: number): string => {
    if (account.status === 'flood_wait' && account.resume_at) {
        const resumeAt = new Date(account.resume_at);
        const minutesLeft = Math.max(1, Math.ceil((resumeAt.getTime() - now) / 60_000));

        return health.limitedUntil(formatClock(resumeAt), minutesLeft);
    }

    return account.last_flood_wait_at
        ? health.recoveredAt(copy.common.relative(new Date(account.last_flood_wait_at)))
        : copy.common.dash;
};

// Sign-in itself needs a backend login endpoint (Telegram) or the remote browser connector (Facebook,
// Instagram). Until they exist, the sign-in buttons explain the interim route instead of opening a wizard.
function SettingsTelegram() {
    const { data, error, isLoading } = useGrowthAccounts(ACCOUNTS_POLL_INTERVAL_MS);
    const { data: settings } = useGrowthSettings();

    const [accounts, setAccounts] = useState<GrowthAccount[] | null>(null);
    const [seededFrom, setSeededFrom] = useState<GrowthAccount[] | undefined>(undefined);
    const [busyIds, setBusyIds] = useState<Set<number>>(new Set());
    const [signInHint, setSignInHint] = useState<null | string>(null);
    const [now, setNow] = useState(() => Date.now());

    useEffect(() => {
        const timer = setInterval(() => setNow(Date.now()), 30_000);

        return () => clearInterval(timer);
    }, []);

    if (data !== seededFrom) {
        setSeededFrom(data);
        setAccounts(data ?? null);
    }

    const list = accounts ?? [];
    const banner = list.length ? accountsBanner(list) : null;
    const telegramSetting = settings?.find((item) => item.kind === 'telegram');
    const isCredentialsMissing = !!settings && telegramSetting?.status !== 'connected';
    const hasHealthy = list.some((account) => account.status === 'connected');

    const runAction = async (account: GrowthAccount, action: () => Promise<GrowthAccount | null>, done: string) => {
        setBusyIds((previous) => new Set(previous).add(account.id));

        try {
            const next = await action();

            setAccounts((previous) =>
                next
                    ? (previous ?? []).map((item) => (item.id === next.id ? next : item))
                    : (previous ?? []).filter((item) => item.id !== account.id),
            );
            toast.success(done);
        } catch (actionError) {
            toast.error(getApiErrorMessage(actionError, health.actionFailed));
        } finally {
            setBusyIds((previous) => {
                const next = new Set(previous);
                next.delete(account.id);

                return next;
            });
        }
    };

    const showSignIn = (platform: GrowthAccount['platform']) =>
        setSignInHint(platform === 'telegram' ? copy.telegram.cliHint : health.remoteBrowserPending);

    return (
        <>
            <AppHeader>
                <AppHeaderContent>
                    <AppHeaderTitle icon={<Send className="size-4 shrink-0" />}>{copy.telegram.title}</AppHeaderTitle>
                </AppHeaderContent>
            </AppHeader>
            <div className="mx-auto flex w-full max-w-5xl flex-col gap-4 p-4">
                {isCredentialsMissing && (
                    <Alert>
                        <Info />
                        <AlertDescription className="flex flex-wrap items-center gap-2">
                            {copy.telegram.credentialsMissing}
                            <Link
                                className="font-medium underline"
                                to={routes.settings.credentials}
                            >
                                {copy.telegram.credentialsMissingLink}
                            </Link>
                        </AlertDescription>
                    </Alert>
                )}

                {banner && (
                    <div className="flex items-start gap-2 rounded-md border border-yellow-500/30 bg-yellow-500/10 px-3 py-2 text-sm text-yellow-900 dark:text-yellow-300">
                        <TriangleAlert className="mt-0.5 size-4 shrink-0" />
                        <span>{accountsBannerText(banner)}</span>
                    </div>
                )}

                <Card>
                    <CardHeader>
                        <CardTitle>{health.title}</CardTitle>
                        <CardDescription>{health.note}</CardDescription>
                    </CardHeader>
                    <CardContent className="flex flex-col gap-4">
                        {isLoading ? (
                            <div className="flex justify-center py-4">
                                <Spinner variant="circle" />
                            </div>
                        ) : error ? (
                            <div className="text-destructive text-sm">{copy.telegram.loadFailed}</div>
                        ) : list.length === 0 ? (
                            <div className="text-muted-foreground text-sm">{health.empty}</div>
                        ) : (
                            <Table aria-label={health.title}>
                                <TableHeader>
                                    <TableRow>
                                        <TableHead>{health.column.platform}</TableHead>
                                        <TableHead>{health.column.account}</TableHead>
                                        <TableHead>{health.column.status}</TableHead>
                                        <TableHead>{health.column.lastSuccess}</TableHead>
                                        <TableHead>{health.column.limit}</TableHead>
                                        <TableHead>{health.column.today}</TableHead>
                                        <TableHead className="text-right">{health.column.actions}</TableHead>
                                    </TableRow>
                                </TableHeader>
                                <TableBody>
                                    {list.map((account) => {
                                        const name = accountName(account);
                                        const isBusy = busyIds.has(account.id);

                                        return (
                                            <TableRow key={account.id}>
                                                <TableCell>
                                                    <span className="flex items-center gap-1.5">
                                                        {account.platform === 'telegram' && (
                                                            <Send className="size-3.5" />
                                                        )}
                                                        {health.platform[account.platform] ?? account.platform}
                                                    </span>
                                                </TableCell>
                                                <TableCell className="whitespace-nowrap">
                                                    {account.display_name}{' '}
                                                    <span className="text-muted-foreground">
                                                        …{account.phone_last4}
                                                    </span>
                                                </TableCell>
                                                <TableCell className="max-w-64">
                                                    <Badge variant={statusVariant[account.status]}>
                                                        {health.status[account.status]}
                                                    </Badge>
                                                    <p className="text-muted-foreground mt-1 text-xs whitespace-normal">
                                                        {statusHint(account)}
                                                    </p>
                                                </TableCell>
                                                <TableCell className="whitespace-nowrap">
                                                    {account.last_success_at
                                                        ? copy.common.relative(new Date(account.last_success_at))
                                                        : copy.common.dash}
                                                </TableCell>
                                                <TableCell className="whitespace-nowrap">
                                                    {limitText(account, now)}
                                                </TableCell>
                                                <TableCell className="whitespace-nowrap">
                                                    {health.today(account.today_reads, account.today_joins)}
                                                </TableCell>
                                                <TableCell>
                                                    <div className="flex justify-end gap-1">
                                                        {account.status === 'signed_out' && (
                                                            <Button
                                                                disabled={isBusy}
                                                                onClick={() => showSignIn(account.platform)}
                                                                size="sm"
                                                                variant="outline"
                                                            >
                                                                {health.relogin}
                                                            </Button>
                                                        )}
                                                        {account.status === 'restricted' ? (
                                                            <Button
                                                                disabled={isBusy}
                                                                onClick={() =>
                                                                    runAction(
                                                                        account,
                                                                        async () => {
                                                                            await growthApi.removeAccount(account.id);

                                                                            return null;
                                                                        },
                                                                        health.removed(name),
                                                                    )
                                                                }
                                                                size="sm"
                                                                variant="outline"
                                                            >
                                                                {health.remove}
                                                            </Button>
                                                        ) : account.status === 'disabled' ? (
                                                            <Button
                                                                disabled={isBusy}
                                                                onClick={() =>
                                                                    runAction(
                                                                        account,
                                                                        () =>
                                                                            growthApi.setAccountDisabled(
                                                                                account.id,
                                                                                false,
                                                                            ),
                                                                        health.enabled(name),
                                                                    )
                                                                }
                                                                size="sm"
                                                                variant="outline"
                                                            >
                                                                {health.enable}
                                                            </Button>
                                                        ) : (
                                                            <Button
                                                                disabled={isBusy}
                                                                onClick={() =>
                                                                    runAction(
                                                                        account,
                                                                        () =>
                                                                            growthApi.setAccountDisabled(
                                                                                account.id,
                                                                                true,
                                                                            ),
                                                                        health.disabled(name),
                                                                    )
                                                                }
                                                                size="sm"
                                                                variant="ghost"
                                                            >
                                                                {health.disable}
                                                            </Button>
                                                        )}
                                                    </div>
                                                </TableCell>
                                            </TableRow>
                                        );
                                    })}
                                </TableBody>
                            </Table>
                        )}

                        {!isLoading && !error && list.length === 1 && (
                            <p className="text-muted-foreground text-sm">{health.onlyOne}</p>
                        )}

                        <Button
                            className="self-start"
                            onClick={() => showSignIn('telegram')}
                            size="sm"
                            variant="outline"
                        >
                            <Plus />
                            {health.add}
                        </Button>

                        {(signInHint || (import.meta.env.DEV && !isLoading && !hasHealthy)) && (
                            <div className="text-muted-foreground flex items-start gap-2 text-sm">
                                <Terminal className="mt-0.5 size-4 shrink-0" />
                                <span>{signInHint ?? copy.telegram.cliHint}</span>
                            </div>
                        )}
                    </CardContent>
                </Card>
            </div>
        </>
    );
}

export default SettingsTelegram;
