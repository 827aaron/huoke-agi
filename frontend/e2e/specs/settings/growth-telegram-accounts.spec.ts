import type { Locator, Page } from '@playwright/test';

import type { Cassette } from '../../mocks/cassette.ts';

import { expect, test } from '../../fixtures/test.ts';
import { expectCleanPage } from '../../helpers/errors.ts';
import { baseQueries, baseRest } from '../../mocks/cassettes/base.ts';

// The mock tier answers /api/v1/growth from the bundle's sample data: four Telegram accounts, one per state
// that matters to the operator.
const growthCassette = (): Cassette => ({ queries: baseQueries(), rest: baseRest() });

const accountRow = (page: Page, last4: string): Locator => page.getByRole('row').filter({ hasText: `…${last4}` });

test.describe('growth telegram account health', { tag: '@settings' }, () => {
    test.use({ cassette: growthCassette() });

    test('lists every account with its status and today usage', async ({ page, pageErrorLog }) => {
        await page.goto('/settings/telegram');

        await expect(accountRow(page, '8842').getByText('正常', { exact: true })).toBeVisible();
        await expect(accountRow(page, '8842').getByText('读取 1240 条 · 加入 3 个群')).toBeVisible();
        await expect(accountRow(page, '1907').getByText('限流中', { exact: true })).toBeVisible();
        await expect(accountRow(page, '5531').getByText('需要重新登录', { exact: true })).toBeVisible();
        await expect(accountRow(page, '0274').getByText('已受限', { exact: true })).toBeVisible();

        expectCleanPage(pageErrorLog);
    });

    test('disables and re-enables an account immediately, without a confirmation', async ({ page, pageErrorLog }) => {
        await page.goto('/settings/telegram');

        const row = accountRow(page, '8842');

        await row.getByRole('button', { name: '停用' }).click();

        await expect(row.getByText('已停用', { exact: true })).toBeVisible();
        await expect(page.getByRole('dialog')).toHaveCount(0);

        await row.getByRole('button', { name: '启用' }).click();

        await expect(row.getByText('正常', { exact: true })).toBeVisible();

        expectCleanPage(pageErrorLog);
    });

    test('removes a restricted account at once and offers no way to use it', async ({ page, pageErrorLog }) => {
        await page.goto('/settings/telegram');

        const row = accountRow(page, '0274');

        await expect(row.getByRole('button', { name: '停用' })).toHaveCount(0);

        await row.getByRole('button', { name: '移除账号' }).click();

        await expect(accountRow(page, '0274')).toHaveCount(0);
        await expect(accountRow(page, '8842')).toBeVisible();

        expectCleanPage(pageErrorLog);
    });

    test('points a signed-out account at the server login command', async ({ page, pageErrorLog }) => {
        await page.goto('/settings/telegram');

        await accountRow(page, '5531').getByRole('button', { name: '重新登录' }).click();

        await expect(page.getByText('growth-tglogin')).toBeVisible();

        expectCleanPage(pageErrorLog);
    });
});
