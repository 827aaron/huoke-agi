import { describe, expect, it } from 'vitest';

import type { GrowthAccount } from './growth-types';

import { accountsBanner, accountTransitions } from './account-health';

const account = (id: number, status: GrowthAccount['status'], extra: Partial<GrowthAccount> = {}): GrowthAccount => ({
    display_name: `A${id}`,
    id,
    last_flood_wait_at: null,
    last_success_at: null,
    phone_last4: `000${id}`,
    platform: 'telegram',
    resume_at: null,
    status,
    today_joins: 0,
    today_reads: 0,
    ...extra,
});

describe('accountsBanner', () => {
    it('stays hidden while one Telegram account is healthy', () => {
        expect(accountsBanner([account(1, 'connected'), account(2, 'signed_out')])).toBeNull();
    });

    it('reports the earliest recovery when every active account is rate limited', () => {
        const banner = accountsBanner([
            account(1, 'flood_wait', { resume_at: '2026-10-16T10:30:00Z' }),
            account(2, 'flood_wait', { resume_at: '2026-10-16T10:05:00Z' }),
            account(3, 'disabled'),
        ]);

        expect(banner).toEqual({ kind: 'all_limited', resumeAt: new Date('2026-10-16T10:05:00Z') });
    });

    it('says the agent is paused when an account needs attention and none is healthy', () => {
        expect(
            accountsBanner([account(1, 'flood_wait', { resume_at: '2026-10-16T10:05:00Z' }), account(2, 'signed_out')]),
        ).toEqual({ kind: 'none_usable' });
        expect(accountsBanner([])).toEqual({ kind: 'none_usable' });
    });

    it('ignores accounts of platforms the agent does not use yet', () => {
        expect(accountsBanner([account(1, 'connected', { platform: 'facebook' })])).toEqual({ kind: 'none_usable' });
    });
});

describe('accountTransitions', () => {
    it('names the backup account when a rate-limited one is switched away from', () => {
        const [notice] = accountTransitions(
            [account(1, 'connected'), account(2, 'connected')],
            [account(1, 'flood_wait'), account(2, 'connected')],
        );

        expect(notice).toEqual({
            isPersistent: false,
            link: null,
            message: '账号 A1（…0001） 被限流，已自动切换到账号 A2（…0002），工作不受影响。',
        });
    });

    it('keeps sign-out and restriction notices on screen with a link', () => {
        const notices = accountTransitions(
            [account(1, 'connected'), account(2, 'connected')],
            [account(1, 'signed_out'), account(2, 'restricted')],
        );

        expect(notices).toEqual([
            {
                isPersistent: true,
                link: 'relogin',
                message: '账号 A1（…0001） 登录已失效，没有其他可用账号，进行中的项目已暂停。请重新登录。',
            },
            {
                isPersistent: true,
                link: 'add',
                message: '账号 A2（…0002） 被 Telegram 限制，系统已停止使用它。建议添加新账号。',
            },
        ]);
    });

    it('announces recovery and new accounts but not the first load', () => {
        expect(accountTransitions([], [account(1, 'connected')])).toEqual([]);
        expect(
            accountTransitions([account(1, 'flood_wait')], [account(1, 'connected'), account(2, 'connected')]).map(
                (notice) => notice.message,
            ),
        ).toEqual(['账号 A1（…0001） 已恢复，重新加入可用账号。', '账号 A2（…0002） 已添加，当前共 2 个可用账号。']);
    });

    it('stays silent when nothing changed', () => {
        expect(accountTransitions([account(1, 'signed_out')], [account(1, 'signed_out')])).toEqual([]);
    });
});
