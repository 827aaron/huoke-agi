import type { Locator, Page } from '@playwright/test';

import type { Cassette } from '../../mocks/cassette.ts';

import { expect, test } from '../../fixtures/test.ts';
import { expectCleanPage } from '../../helpers/errors.ts';
import { baseQueries, baseRest } from '../../mocks/cassettes/base.ts';

// The mock tier answers /api/v1/growth from the bundle's sample data: three Telegram accounts (healthy, rate
// limited, signed out), one healthy Facebook account, an Instagram account that needs a login and a TikTok one.
const growthCassette = (): Cassette => ({ queries: baseQueries(), rest: baseRest() });

const accountRow = (page: Page, last4: string): Locator => page.getByRole('row').filter({ hasText: `…${last4}` });

test.describe('growth account connections', { tag: '@settings' }, () => {
    test.use({ cassette: growthCassette() });

    test('lists every account under its platform with its health', async ({ page, pageErrorLog }) => {
        await page.goto('/settings/accounts');

        await expect(accountRow(page, '8842').getByText('正常', { exact: true })).toBeVisible();
        await expect(accountRow(page, '1907').getByText('限流中', { exact: true })).toBeVisible();
        await expect(accountRow(page, '5531').getByText('需要重新登录', { exact: true })).toBeVisible();
        await expect(accountRow(page, '1207').getByText('正常', { exact: true })).toBeVisible();
        await expect(accountRow(page, '_sea').getByText('需要重新登录', { exact: true })).toBeVisible();

        expectCleanPage(pageErrorLog);
    });

    test('disables and re-enables an account immediately, without a confirmation', async ({ page, pageErrorLog }) => {
        await page.goto('/settings/accounts');

        const row = accountRow(page, '8842');

        await row.getByRole('button', { name: '停用' }).click();

        await expect(row.getByText('已停用', { exact: true })).toBeVisible();
        await expect(page.getByRole('alertdialog')).toHaveCount(0);
        await expect(page.getByRole('dialog')).toHaveCount(0);

        await row.getByRole('button', { name: '启用' }).click();

        await expect(row.getByText('正常', { exact: true })).toBeVisible();

        expectCleanPage(pageErrorLog);
    });

    test('offers the login only to accounts that lost it', async ({ page, pageErrorLog }) => {
        await page.goto('/settings/accounts');

        await expect(accountRow(page, '8842').getByRole('button', { name: '重新登录' })).toHaveCount(0);
        await expect(accountRow(page, '1907').getByRole('button', { name: '重新登录' })).toHaveCount(0);

        await accountRow(page, '5531').getByRole('button', { name: '重新登录' }).click();

        await expect(page.getByRole('dialog').getByText('重新登录 Telegram 账号')).toBeVisible();

        expectCleanPage(pageErrorLog);
    });

    test('opens the remote browser login for a browser platform', async ({ page, pageErrorLog }) => {
        await page.goto('/settings/accounts');

        await accountRow(page, '_sea').getByRole('button', { name: '远程登录' }).click();

        await expect(page.getByRole('dialog').getByText('登录 Instagram')).toBeVisible();

        expectCleanPage(pageErrorLog);
    });

    test('shows the route each account uses and lets the user change it', async ({ page, pageErrorLog }) => {
        await page.goto('/settings/accounts');

        await expect(accountRow(page, '1207').getByText(/203\.0\.113\.24/)).toBeVisible();

        await accountRow(page, '1207').getByRole('button', { name: '代理' }).click();

        await expect(page.getByRole('dialog').getByText('代理（可选）')).toBeVisible();

        expectCleanPage(pageErrorLog);
    });
});
