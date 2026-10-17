import { format } from 'date-fns';

import { copy } from '@/lib/copy';

import type { GrowthAccount } from './growth-types';

export type AccountsBanner = { kind: 'all_limited'; resumeAt: Date } | { kind: 'none_usable' };

export interface AccountTransition {
    // Red events stay on screen until closed; the rest fade.
    isPersistent: boolean;
    link: 'relogin' | null;
    message: string;
}

const health = copy.telegram.health;

export const accountLast4 = (account: Pick<GrowthAccount, 'external_id'>): string => account.external_id.slice(-4);

export const accountName = (account: Pick<GrowthAccount, 'external_id' | 'label'>): string =>
    health.accountName(account.label || account.external_id, accountLast4(account));

export const formatClock = (date: Date): string => format(date, 'HH:mm');

// Only Telegram feeds the agent today, so only Telegram accounts decide whether it is paused.
export function accountsBanner(accounts: GrowthAccount[]): AccountsBanner | null {
    const telegram = accounts.filter((account) => account.platform === 'telegram');

    if (telegram.some((account) => account.status === 'active')) {
        return null;
    }

    const active = telegram.filter((account) => account.status !== 'disabled');
    const resumeTimes = active.flatMap((account) =>
        account.status === 'rate_limited' && account.rate_limit_until
            ? [new Date(account.rate_limit_until).getTime()]
            : [],
    );

    if (active.length > 0 && resumeTimes.length === active.length) {
        return { kind: 'all_limited', resumeAt: new Date(Math.min(...resumeTimes)) };
    }

    return { kind: 'none_usable' };
}

export const accountsBannerText = (banner: AccountsBanner): string =>
    banner.kind === 'all_limited' ? health.allLimited(formatClock(banner.resumeAt)) : health.noneUsable;

// Turns two consecutive snapshots into the notices the customer sees. Customer actions (disable, enable) are
// announced by the page that performs them, not here.
export function accountTransitions(previous: GrowthAccount[], next: GrowthAccount[]): AccountTransition[] {
    const before = new Map(previous.map((account) => [account.id, account]));
    const healthy = next.filter((account) => account.status === 'active');
    const transitions: AccountTransition[] = [];

    for (const account of next) {
        const old = before.get(account.id);
        const name = accountName(account);
        const backup = healthy.find((other) => other.id !== account.id);

        if (!old) {
            if (previous.length > 0 && account.status === 'active') {
                transitions.push({ isPersistent: false, link: null, message: health.added(name, healthy.length) });
            }

            continue;
        }

        if (old.status === account.status) {
            continue;
        }

        switch (account.status) {
            case 'active':
                if (old.status === 'rate_limited') {
                    transitions.push({ isPersistent: false, link: null, message: health.recovered(name) });
                }

                break;
            case 'needs_login':
                transitions.push({
                    isPersistent: true,
                    link: 'relogin',
                    message: backup
                        ? health.signedOutSwitched(name, accountName(backup))
                        : health.signedOutNoBackup(name),
                });
                break;
            case 'rate_limited':
                transitions.push({
                    isPersistent: false,
                    link: null,
                    message: backup
                        ? health.limitedSwitched(name, accountName(backup))
                        : health.limitedNoBackup(
                              name,
                              account.rate_limit_until
                                  ? formatClock(new Date(account.rate_limit_until))
                                  : copy.common.dash,
                          ),
                });
                break;
        }
    }

    return transitions;
}

export const proxyRouteText = (account: GrowthAccount): string =>
    account.proxy_source === 'account'
        ? (account.proxy ?? copy.telegram.proxy.direct)
        : account.proxy_source === 'global'
          ? copy.telegram.proxy.global
          : copy.telegram.proxy.direct;
