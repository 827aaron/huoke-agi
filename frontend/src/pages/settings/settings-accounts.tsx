import { Globe, Info, Plus, TriangleAlert, Users } from 'lucide-react';
import { useEffect, useState } from 'react';
import { Link } from 'react-router-dom';
import { toast } from 'sonner';

import type { GrowthAccount, GrowthAccountPlatform, GrowthAccountStatus } from '@/features/growth/growth-types';

import { AppHeader, AppHeaderContent, AppHeaderTitle } from '@/components/layouts/app/app-header';
import { Alert, AlertDescription } from '@/components/ui/alert';
import { Badge, type BadgeVariant } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Spinner } from '@/components/ui/spinner';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table';
import {
    accountLabel,
    accountLast4,
    accountName,
    accountsBanner,
    accountsBannerText,
    formatClock,
    proxyRouteText,
} from '@/features/growth/account-health';
import AccountDetailSheet from '@/features/growth/accounts/account-detail-sheet';
import AccountProxyDialog from '@/features/growth/accounts/account-proxy-dialog';
import RemoteLoginDialog, { fingerprintSummary } from '@/features/growth/accounts/remote-login-dialog';
import TelegramLoginDialog from '@/features/growth/accounts/telegram-login-dialog';
import { growthApi } from '@/features/growth/growth-api';
import { settingState } from '@/features/growth/settings/growth-credential-row';
import { ACCOUNTS_POLL_INTERVAL_MS, useGrowthAccounts, useGrowthSettings } from '@/features/growth/use-growth-data';
import { getApiErrorMessage } from '@/lib/axios';
import { copy } from '@/lib/copy';
import { routes } from '@/lib/routes';

const health = copy.telegram.health;
const text = copy.telegram;

const ACCOUNT_PLATFORMS: GrowthAccountPlatform[] = ['telegram', 'facebook', 'instagram', 'tiktok'];

const statusVariant: Record<GrowthAccountStatus, BadgeVariant> = {
    active: 'green',
    disabled: 'secondary',
    needs_login: 'red',
    rate_limited: 'yellow',
};

const statusHint = (account: GrowthAccount): string =>
    account.status === 'rate_limited'
        ? health.hint.rate_limited(
              account.rate_limit_until ? formatClock(new Date(account.rate_limit_until)) : copy.common.dash,
          )
        : health.hint[account.status];

const limitText = (account: GrowthAccount, now: number): string => {
    if (account.status === 'rate_limited' && account.rate_limit_until) {
        const resumeAt = new Date(account.rate_limit_until);
        const minutesLeft = Math.max(1, Math.ceil((resumeAt.getTime() - now) / 60_000));

        return health.limitedUntil(formatClock(resumeAt), minutesLeft);
    }

    return account.last_rate_limited_at
        ? health.recoveredAt(copy.common.relative(new Date(account.last_rate_limited_at)))
        : copy.common.dash;
};

type LoginTarget = { account: GrowthAccount | null; platform: GrowthAccountPlatform };

function SettingsAccounts() {
    const { data, error, isLoading } = useGrowthAccounts(ACCOUNTS_POLL_INTERVAL_MS);
    const { data: settings } = useGrowthSettings();

    const [accounts, setAccounts] = useState<GrowthAccount[] | null>(null);
    const [seededFrom, setSeededFrom] = useState<GrowthAccount[] | undefined>(undefined);
    const [busyIds, setBusyIds] = useState<Set<number>>(new Set());
    const [login, setLogin] = useState<LoginTarget | null>(null);
    const [proxyAccount, setProxyAccount] = useState<GrowthAccount | null>(null);
    const [detailAccount, setDetailAccount] = useState<GrowthAccount | null>(null);
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
    const banner = accounts ? accountsBanner(list) : null;
    const telegramSetting = settings?.find((item) => item.kind === 'telegram');
    const youtubeSetting = settings?.find((item) => item.kind === 'youtube');
    const isCredentialsMissing = !!settings && settingState(telegramSetting) === 'unset';

    const upsert = (next: GrowthAccount) =>
        setAccounts((previous) => {
            const rest = previous ?? [];

            return rest.some((item) => item.id === next.id)
                ? rest.map((item) => (item.id === next.id ? next : item))
                : [...rest, next];
        });

    const toggleDisabled = async (account: GrowthAccount) => {
        const isDisabling = account.status !== 'disabled';

        setBusyIds((previous) => new Set(previous).add(account.id));

        try {
            upsert(await growthApi.setAccountDisabled(account.id, isDisabling));
            toast.success(isDisabling ? health.disabled(accountName(account)) : health.enabled(accountName(account)));
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

    const renderTable = (platform: GrowthAccountPlatform) => {
        const rows = list.filter((account) => account.platform === platform);

        if (!rows.length) {
            return <div className="text-muted-foreground text-sm">{health.empty}</div>;
        }

        return (
            <Table aria-label={health.platform[platform] ?? platform}>
                <TableHeader>
                    <TableRow>
                        <TableHead>{health.column.account}</TableHead>
                        <TableHead>{health.column.status}</TableHead>
                        <TableHead>{health.column.lastSuccess}</TableHead>
                        <TableHead>{health.column.limit}</TableHead>
                        <TableHead>{text.proxy.column}</TableHead>
                        <TableHead className="text-right">{health.column.actions}</TableHead>
                    </TableRow>
                </TableHeader>
                <TableBody>
                    {rows.map((account) => {
                        const isBusy = busyIds.has(account.id);
                        const browserNote = account.external_browser_ws
                            ? text.fingerprint.usingExternal
                            : account.fingerprint
                              ? fingerprintSummary(account.fingerprint)
                              : null;

                        return (
                            <TableRow key={account.id}>
                                <TableCell className="whitespace-nowrap">
                                    {accountLabel(account)}
                                    {accountLast4(account) && (
                                        <span className="text-muted-foreground"> …{accountLast4(account)}</span>
                                    )}
                                    {browserNote && <p className="text-muted-foreground text-xs">{browserNote}</p>}
                                </TableCell>
                                <TableCell className="w-52 min-w-52 whitespace-normal">
                                    <Badge variant={statusVariant[account.status]}>
                                        {health.status[account.status]}
                                    </Badge>
                                    <p className="text-muted-foreground mt-1 text-xs whitespace-normal">
                                        {statusHint(account)}
                                    </p>
                                    {account.joined_sources !== undefined && (
                                        <p className="text-muted-foreground mt-1 text-xs whitespace-normal">
                                            {health.joinCounts(account.joins_today ?? 0, account.joined_sources)}
                                        </p>
                                    )}
                                    {account.join_advice?.map((code) => (
                                        <p
                                            className="mt-1 text-xs whitespace-normal text-yellow-800 dark:text-yellow-400"
                                            key={code}
                                        >
                                            {health.joinAdvice[code] ?? code}
                                        </p>
                                    ))}
                                </TableCell>
                                <TableCell className="whitespace-nowrap">
                                    {account.last_success_at
                                        ? copy.common.relative(new Date(account.last_success_at))
                                        : copy.common.dash}
                                </TableCell>
                                <TableCell className="whitespace-nowrap">{limitText(account, now)}</TableCell>
                                <TableCell className="font-mono text-xs whitespace-nowrap">
                                    {account.external_browser_ws ? copy.common.dash : proxyRouteText(account)}
                                </TableCell>
                                <TableCell>
                                    <div className="flex justify-end gap-1">
                                        <Button
                                            aria-label={`${text.detail.open} ${accountLabel(account)}`}
                                            onClick={() => setDetailAccount(account)}
                                            size="sm"
                                            variant="ghost"
                                        >
                                            {text.detail.open}
                                        </Button>
                                        {account.status === 'needs_login' && (
                                            <Button
                                                disabled={isBusy}
                                                onClick={() => setLogin({ account, platform })}
                                                size="sm"
                                                variant="outline"
                                            >
                                                {platform === 'telegram' ? health.relogin : text.remoteLogin}
                                            </Button>
                                        )}
                                        {!account.external_browser_ws && (
                                            <Button
                                                disabled={isBusy}
                                                onClick={() => setProxyAccount(account)}
                                                size="sm"
                                                variant="ghost"
                                            >
                                                {text.proxy.edit}
                                            </Button>
                                        )}
                                        <Button
                                            disabled={isBusy}
                                            onClick={() => toggleDisabled(account)}
                                            size="sm"
                                            variant={account.status === 'disabled' ? 'outline' : 'ghost'}
                                        >
                                            {account.status === 'disabled' ? health.enable : health.disable}
                                        </Button>
                                    </div>
                                </TableCell>
                            </TableRow>
                        );
                    })}
                </TableBody>
            </Table>
        );
    };

    return (
        <>
            <AppHeader>
                <AppHeaderContent>
                    <AppHeaderTitle icon={<Users className="size-4 shrink-0" />}>{text.title}</AppHeaderTitle>
                </AppHeaderContent>
            </AppHeader>
            <div className="mx-auto flex w-full max-w-6xl flex-col gap-4 p-4">
                <p className="text-muted-foreground text-sm">{health.note}</p>

                {banner && (
                    <div className="flex items-start gap-2 rounded-md border border-yellow-500/30 bg-yellow-500/10 px-3 py-2 text-sm text-yellow-900 dark:text-yellow-300">
                        <TriangleAlert className="mt-0.5 size-4 shrink-0" />
                        <span>{accountsBannerText(banner)}</span>
                    </div>
                )}

                {isLoading ? (
                    <div className="flex justify-center py-8">
                        <Spinner variant="circle" />
                    </div>
                ) : error ? (
                    <div className="text-destructive text-sm">{text.loadFailed}</div>
                ) : (
                    ACCOUNT_PLATFORMS.map((platform) => (
                        <Card key={platform}>
                            <CardHeader>
                                <CardTitle>{health.platform[platform]}</CardTitle>
                                <CardDescription>{text.sections[platform]}</CardDescription>
                            </CardHeader>
                            <CardContent className="flex flex-col gap-4">
                                {platform === 'telegram' && isCredentialsMissing && (
                                    <Alert>
                                        <Info />
                                        <AlertDescription className="flex flex-wrap items-center gap-2">
                                            {text.credentialsMissing}
                                            <Link
                                                className="font-medium underline"
                                                to={routes.settings.credentials}
                                            >
                                                {text.credentialsMissingLink}
                                            </Link>
                                        </AlertDescription>
                                    </Alert>
                                )}
                                {renderTable(platform)}
                                <Button
                                    className="self-start"
                                    onClick={() => setLogin({ account: null, platform })}
                                    size="sm"
                                    variant="outline"
                                >
                                    <Plus />
                                    {health.add}
                                </Button>
                            </CardContent>
                        </Card>
                    ))
                )}

                <Card>
                    <CardHeader>
                        <CardTitle>{health.platform.youtube}</CardTitle>
                        <CardDescription>{text.sections.youtube}</CardDescription>
                    </CardHeader>
                    <CardContent className="flex flex-wrap items-center gap-2 text-sm">
                        {settingState(youtubeSetting) === 'connected' ? (
                            <Badge variant="green">{text.youtubeKey.connected}</Badge>
                        ) : (
                            <Badge variant="secondary">{text.youtubeKey.missing}</Badge>
                        )}
                        <Link
                            className="font-medium underline"
                            to={routes.settings.credentials}
                        >
                            {text.youtubeKey.link}
                        </Link>
                    </CardContent>
                </Card>

                <Card>
                    <CardHeader>
                        <CardTitle className="flex items-center gap-2">
                            <Globe className="size-4" />
                            {health.platform.web}
                        </CardTitle>
                        <CardDescription>{text.sections.web}</CardDescription>
                    </CardHeader>
                </Card>
            </div>

            {login?.platform === 'telegram' && (
                <TelegramLoginDialog
                    account={login.account}
                    onClose={() => setLogin(null)}
                    onSignedIn={upsert}
                    open
                />
            )}
            {login && login.platform !== 'telegram' && (
                <RemoteLoginDialog
                    account={login.account}
                    onClose={() => setLogin(null)}
                    onSignedIn={upsert}
                    open
                    platform={login.platform}
                />
            )}
            <AccountDetailSheet
                account={detailAccount}
                onOpenChange={(isOpen) => !isOpen && setDetailAccount(null)}
            />
            {proxyAccount && (
                <AccountProxyDialog
                    account={proxyAccount}
                    onClose={() => setProxyAccount(null)}
                    onSaved={upsert}
                />
            )}
        </>
    );
}

export default SettingsAccounts;
