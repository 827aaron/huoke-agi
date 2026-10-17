import type { Locator, Page } from '@playwright/test';

import type { Cassette } from '../../mocks/cassette.ts';

import { infoEntryFor, seedAuthenticatedAs, SEEDED_USER } from '../../fixtures/auth.ts';
import { expect, test } from '../../fixtures/test.ts';
import { expectCleanPage } from '../../helpers/errors.ts';
import { baseQueries, baseRest } from '../../mocks/cassettes/base.ts';

const GH_USER = { mail: 'gh@example.com', name: 'GH User', provider: 'github', type: 'oauth' } as const;
const ANON_USER = { mail: 'x@example.com', name: 'X User', type: 'oauth' } as const;

const CURRENT_PASSWORD = 'CurrentPass1!';
// The server-side half of this boundary lives in specs/real/account-password.spec.ts.
const MAX_LENGTH_PASSWORD = 'n'.repeat(72);

const accountCassette = (rest: Cassette['rest'] = {}): Cassette => ({
    queries: baseQueries(),
    rest: { ...baseRest(), ...rest },
});

const section = (page: Page, title: string): Locator =>
    page.locator('[data-slot="card"]').filter({ has: page.locator('[data-slot="card-title"]', { hasText: title }) });

const openSection = async (page: Page, title: string): Promise<Locator> => {
    const card = section(page, title);

    await card.getByRole('button', { name: '修改' }).click();

    return card;
};

const fillPasswordForm = async (page: Page, next: string, confirm = next): Promise<void> => {
    await page.getByLabel('当前密码').fill(CURRENT_PASSWORD);
    await page.getByLabel('新密码', { exact: true }).fill(next);
    await page.getByLabel('确认新密码').fill(confirm);
};

const submitPasswordForm = (page: Page): Promise<void> => page.getByRole('button', { name: '修改密码' }).click();

test.describe('account settings', { tag: '@settings' }, () => {
    test.describe('read', () => {
        test.use({ cassette: accountCassette() });

        test('renders the signed-in identity and every editable section', async ({ page, pageErrorLog }) => {
            await page.goto('/settings/account');

            await expect(page.getByRole('heading', { exact: true, name: SEEDED_USER.name })).toBeVisible();
            await expect(page.getByText('2026 年 1 月加入')).toBeVisible();
            await expect(page.getByText('本地账号')).toBeVisible();
            await expect(section(page, '显示名称').getByText(SEEDED_USER.name)).toBeVisible();
            await expect(section(page, '邮箱地址').getByText(SEEDED_USER.mail)).toBeVisible();
            await expect(section(page, '密码').getByText('••••••••••••')).toBeVisible();

            expectCleanPage(pageErrorLog);
        });
    });

    test.describe('a federated account the record names', () => {
        test.use({
            cassette: accountCassette({
                'GET /api/v1/info': [infoEntryFor(GH_USER)],
            }),
            isAuthSeeded: false,
        });

        test('names the provider in the badge and in the sentence beside the address', async ({ page }) => {
            await seedAuthenticatedAs(page, GH_USER);
            await page.goto('/settings/account');

            await expect(page.getByText('GitHub', { exact: true })).toBeVisible();
            await expect(page.getByText('来自你的 GitHub。')).toBeVisible();
        });
    });

    test.describe('a federated account the record does not name', () => {
        test.use({
            cassette: accountCassette({
                'GET /api/v1/info': [infoEntryFor(ANON_USER)],
            }),
            isAuthSeeded: false,
        });

        test('falls back to the kind of account, and the sentence still parses', async ({ page }) => {
            await seedAuthenticatedAs(page, ANON_USER);
            await page.goto('/settings/account');

            await expect(page.getByText('第三方账号', { exact: true })).toBeVisible();
            await expect(page.getByText('来自你的 第三方账号。')).toBeVisible();
        });
    });

    test.describe('password validation', () => {
        test.use({ cassette: accountCassette() });

        // The cassette deliberately holds no `PUT /user/password` entry: adding one to "fix" a
        // failure here would destroy the oracle.
        test('rejects each invalid password without touching the network', async ({ page, pageErrorLog }) => {
            const attempts: string[] = [];

            page.on('request', (request) => {
                if (request.method() === 'PUT' && new URL(request.url()).pathname === '/api/v1/user/password') {
                    attempts.push(request.url());
                }
            });

            await page.goto('/settings/account');
            await openSection(page, '密码');

            // Only the first row clicks: the submit disables itself once a submitted form is invalid,
            // and every later row is then validated live on change.
            await fillPasswordForm(page, 'NewPass1!abc', 'OtherPass1!abc');
            await submitPasswordForm(page);
            await expect(page.getByText('两次输入的密码不一致')).toBeVisible();

            await fillPasswordForm(page, 'newpass1');
            await expect(page.getByText('密码需要超过 15 个字符', { exact: false })).toBeVisible();

            await fillPasswordForm(page, 'short1!');
            await expect(page.getByText('密码至少需要 8 个字符')).toBeVisible();

            await fillPasswordForm(page, `${MAX_LENGTH_PASSWORD}x`);
            await expect(page.getByText('密码不能超过 72 个字符')).toBeVisible();

            await fillPasswordForm(page, CURRENT_PASSWORD);
            await expect(page.getByText('新密码不能和当前密码相同')).toBeVisible();

            expect(attempts, 'no invalid password may reach the backend').toEqual([]);
            expectCleanPage(pageErrorLog);
        });
    });

    test.describe('password submit', () => {
        test.use({
            cassette: accountCassette({
                'PUT /api/v1/user/password': [
                    {
                        body: { data: {}, status: 'success' },
                        bodySubset: {
                            confirm_password: MAX_LENGTH_PASSWORD,
                            current_password: CURRENT_PASSWORD,
                            password: MAX_LENGTH_PASSWORD,
                        },
                    },
                ],
            }),
        });

        test('sends the three snake_case fields and closes the section', async ({ page, pageErrorLog }) => {
            await page.goto('/settings/account');
            await openSection(page, '密码');
            await fillPasswordForm(page, MAX_LENGTH_PASSWORD);
            await submitPasswordForm(page);

            await expect(page.getByText('密码已修改')).toBeVisible();
            await expect(section(page, '密码').getByText('••••••••••••')).toBeVisible();

            expectCleanPage(pageErrorLog);
        });
    });

    test.describe('password rejection', () => {
        test.use({
            cassette: accountCassette({
                'PUT /api/v1/user/password': [
                    {
                        body: {
                            code: 'Users.ChangePasswordCurrentUser.InvalidCurrentPassword',
                            msg: 'invalid current password',
                            status: 'error',
                        },
                        status: 403,
                    },
                ],
            }),
        });

        test('translates the backend error code instead of showing the raw message', async ({ page, pageErrorLog }) => {
            await page.goto('/settings/account');
            await openSection(page, '密码');
            await fillPasswordForm(page, 'NewPass1!abc');
            await submitPasswordForm(page);

            await expect(page.getByText('当前密码不正确')).toBeVisible();
            await expect(page.getByText('invalid current password')).toBeHidden();
            await expect(page.getByRole('button', { name: '修改密码' })).toBeVisible();

            expect(pageErrorLog.pageErrors, 'no uncaught page errors').toEqual([]);
            expect(
                pageErrorLog.consoleErrors.filter((text) => !text.includes('403')),
                'the rejected request is the only console error',
            ).toEqual([]);
        });
    });

    test.describe('name and email', () => {
        test.use({
            cassette: accountCassette({
                'PUT /api/v1/user/email': [
                    {
                        body: { data: {}, status: 'success' },
                        bodySubset: { current_password: CURRENT_PASSWORD, mail: 'renamed@pentagi.com' },
                    },
                ],
                'PUT /api/v1/user/name': [
                    { body: { data: {}, status: 'success' }, bodySubset: { name: 'E2E renamed admin' } },
                ],
            }),
        });

        test('sends the display name and the credential-guarded email change', async ({ page, pageErrorLog }) => {
            await page.goto('/settings/account');

            const nameCard = await openSection(page, '显示名称');

            await nameCard.getByLabel('显示名称').fill('E2E renamed admin');
            await nameCard.getByRole('button', { name: '修改名称' }).click();
            await expect(page.getByText('名称已更新')).toBeVisible();

            const emailCard = await openSection(page, '邮箱地址');

            await emailCard.getByLabel('当前密码').fill(CURRENT_PASSWORD);
            await emailCard.getByLabel('新邮箱').fill('renamed@pentagi.com');
            await emailCard.getByRole('button', { name: '修改邮箱' }).click();
            await expect(page.getByText('邮箱已更新')).toBeVisible();

            expectCleanPage(pageErrorLog);
        });
    });
});
