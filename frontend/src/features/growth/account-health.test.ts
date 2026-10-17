import { describe, expect, it } from 'vitest';

import type { GrowthAccount } from './growth-types';

import { accountsBanner, accountTransitions } from './account-health';

const account = (id: number, status: GrowthAccount['status'], extra: Partial<GrowthAccount> = {}): GrowthAccount => ({
    available: status === 'active',
    consecutive_errors: 0,
    external_browser_ws: null,
    external_id: `+86139000${id}`,
    fingerprint: null,
    id,
    kind: 'user',
    label: `A${id}`,
    last_error: null,
    last_rate_limited_at: null,
    last_success_at: null,
    last_used_at: null,
    platform: 'telegram',
    proxy: null,
    proxy_source: 'direct',
    rate_limit_until: null,
    status,
    ...extra,
});

describe('accountsBanner', () => {
    it('stays hidden while one Telegram account is healthy', () => {
        expect(accountsBanner([account(1, 'active'), account(2, 'needs_login')])).toBeNull();
    });

    it('reports the earliest recovery when every active account is rate limited', () => {
        const banner = accountsBanner([
            account(1, 'rate_limited', { rate_limit_until: '2026-10-16T10:30:00Z' }),
            account(2, 'rate_limited', { rate_limit_until: '2026-10-16T10:05:00Z' }),
            account(3, 'disabled'),
        ]);

        expect(banner).toEqual({ kind: 'all_limited', resumeAt: new Date('2026-10-16T10:05:00Z') });
    });

    it('says the agent is paused when an account needs attention and none is healthy', () => {
        expect(
            accountsBanner([
                account(1, 'rate_limited', { rate_limit_until: '2026-10-16T10:05:00Z' }),
                account(2, 'needs_login'),
            ]),
        ).toEqual({ kind: 'none_usable' });
        expect(accountsBanner([])).toEqual({ kind: 'none_usable' });
    });

    it('ignores accounts of platforms the agent does not use yet', () => {
        expect(accountsBanner([account(1, 'active', { platform: 'facebook' })])).toEqual({ kind: 'none_usable' });
    });
});

describe('accountTransitions', () => {
    it('names the backup account when a rate-limited one is switched away from', () => {
        const [notice] = accountTransitions(
            [account(1, 'active'), account(2, 'active')],
            [account(1, 'rate_limited'), account(2, 'active')],
        );

        expect(notice).toEqual({
            isPersistent: false,
            link: null,
            message: '账号 A1（…0001） 被限流，已自动切换到账号 A2（…0002），工作不受影响。',
        });
    });

    it('keeps a sign-out notice on screen with a link to sign in again', () => {
        const notices = accountTransitions(
            [account(1, 'active'), account(2, 'active')],
            [account(1, 'needs_login'), account(2, 'disabled')],
        );

        expect(notices).toEqual([
            {
                isPersistent: true,
                link: 'relogin',
                message: '账号 A1（…0001） 登录已失效，没有其他可用账号，进行中的项目已暂停。请重新登录。',
            },
        ]);
    });

    it('announces recovery and new accounts but not the first load', () => {
        expect(accountTransitions([], [account(1, 'active')])).toEqual([]);
        expect(
            accountTransitions([account(1, 'rate_limited')], [account(1, 'active'), account(2, 'active')]).map(
                (notice) => notice.message,
            ),
        ).toEqual(['账号 A1（…0001） 已恢复，重新加入可用账号。', '账号 A2（…0002） 已添加，当前共 2 个可用账号。']);
    });

    it('stays silent when nothing changed', () => {
        expect(accountTransitions([account(1, 'needs_login')], [account(1, 'needs_login')])).toEqual([]);
    });
});
