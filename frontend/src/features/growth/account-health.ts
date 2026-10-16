import { format } from 'date-fns';

import { copy } from '@/lib/copy';

import type { GrowthAccount } from './growth-types';

export type AccountsBanner = { kind: 'all_limited'; resumeAt: Date } | { kind: 'none_usable' };

export interface AccountTransition {
    // Red events stay on screen until closed; the rest fade.
    isPersistent: boolean;
    link: 'add' | 'relogin' | null;
    message: string;
}

const health = copy.telegram.health;

export const accountName = (account: Pick<GrowthAccount, 'display_name' | 'phone_last4'>): string =>
    health.accountName(account.display_name, account.phone_last4);

export const formatClock = (date: Date): string => format(date, 'HH:mm');

// Only Telegram feeds the agent today, so only Telegram accounts decide whether it is paused.
export function accountsBanner(accounts: GrowthAccount[]): AccountsBanner | null {
    const telegram = accounts.filter((account) => account.platform === 'telegram');

    if (telegram.some((account) => account.status === 'connected')) {
        return null;
    }

    const active = telegram.filter((account) => account.status !== 'disabled');
    const resumeTimes = active.flatMap((account) =>
        account.status === 'flood_wait' && account.resume_at ? [new Date(account.resume_at).getTime()] : [],
    );

    if (active.length > 0 && resumeTimes.length === active.length) {
        return { kind: 'all_limited', resumeAt: new Date(Math.min(...resumeTimes)) };
    }

    return { kind: 'none_usable' };
}

export const accountsBannerText = (banner: AccountsBanner): string =>
    banner.kind === 'all_limited' ? health.allLimited(formatClock(banner.resumeAt)) : health.noneUsable;

// Turns two consecutive snapshots into the notices the customer sees. Customer actions (disable, enable,
// remove) are announced by the page that performs them, not here.
export function accountTransitions(previous: GrowthAccount[], next: GrowthAccount[]): AccountTransition[] {
    const before = new Map(previous.map((account) => [account.id, account]));
    const healthy = next.filter((account) => account.status === 'connected');
    const transitions: AccountTransition[] = [];

    for (const account of next) {
        const old = before.get(account.id);
        const name = accountName(account);
        const backup = healthy.find((other) => other.id !== account.id);

        if (!old) {
            if (previous.length > 0 && account.status === 'connected') {
                transitions.push({ isPersistent: false, link: null, message: health.added(name, healthy.length) });
            }

            continue;
        }

        if (old.status === account.status) {
            continue;
        }

        switch (account.status) {
            case 'connected':
                if (old.status === 'flood_wait') {
                    transitions.push({ isPersistent: false, link: null, message: health.recovered(name) });
                }

                break;
            case 'flood_wait':
                transitions.push({
                    isPersistent: false,
                    link: null,
                    message: backup
                        ? health.limitedSwitched(name, accountName(backup))
                        : health.limitedNoBackup(
                              name,
                              account.resume_at ? formatClock(new Date(account.resume_at)) : copy.common.dash,
                          ),
                });
                break;
            case 'restricted':
                transitions.push({ isPersistent: true, link: 'add', message: health.restricted(name) });
                break;
            case 'signed_out':
                transitions.push({
                    isPersistent: true,
                    link: 'relogin',
                    message: backup
                        ? health.signedOutSwitched(name, accountName(backup))
                        : health.signedOutNoBackup(name),
                });
                break;
        }
    }

    return transitions;
}
