import type { Page } from '@playwright/test';

import type { Cassette } from '../../mocks/cassette.ts';

import { expect, test } from '../../fixtures/test.ts';
import { expectCleanPage } from '../../helpers/errors.ts';
import { baseQueries, baseRest } from '../../mocks/cassettes/base.ts';

// The mock tier answers /api/v1/growth from the bundle's in-memory sample data, which starts with no profile.
const growthCassette = (): Cassette => ({ queries: baseQueries(), rest: baseRest() });

const openProfile = async (page: Page) => {
    await page.goto('/settings/business-profile');
    await expect(page.getByText('还没有用户画像')).toBeVisible();
    await expect(page.getByLabel('需要什么')).toBeVisible();
};

test.describe('growth user profiles', { tag: '@settings' }, () => {
    test.use({ cassette: growthCassette() });

    test('starts blank and refuses a save with nothing to look for', async ({ page, pageErrorLog }) => {
        await openProfile(page);

        await expect(page.getByRole('combobox')).toHaveCount(0);
        await expect(page.getByLabel('需要什么')).toHaveValue('');
        await expect(page.getByLabel('排除什么')).toHaveValue('');
        await expect(page.getByLabel('Jev 分数')).toHaveValue('');

        await page.getByRole('button', { name: '保存' }).click();

        await expect(page.getByText('请至少填写需要什么、排除什么，或添加一段')).toBeVisible();
        await expect(page.getByText('用户画像已保存')).toHaveCount(0);

        expectCleanPage(pageErrorLog);
    });

    test('fills the example prompts and rejects a scoring line that cannot qualify anyone', async ({
        page,
        pageErrorLog,
    }) => {
        await openProfile(page);

        await page.getByRole('button', { name: '填入示例' }).click();

        await expect(page.getByLabel('需要什么')).toHaveValue(/Offer: A licensed/);
        await expect(page.getByLabel('排除什么')).toHaveValue(/bots and spam/);
        await expect(page.getByLabel('Jev 分数')).toHaveValue(/qualified >= 8/);
        await expect(page.getByText('qualified >= 7、watchlist >= 4')).toBeVisible();

        await page.getByLabel('Jev 分数').fill('player = 3\nqualified >= 9\nwatchlist >= 1');
        await page.getByRole('button', { name: '保存' }).click();

        await expect(page.getByText('各项分数加起来低于合格线，没有潜客能合格')).toBeVisible();
        await expect(page.getByText('用户画像已保存')).toHaveCount(0);

        expectCleanPage(pageErrorLog);
    });

    test('saves a profile, adds a prompt, and copies, defaults and deletes profiles', async ({
        page,
        pageErrorLog,
    }) => {
        await openProfile(page);

        await page.getByLabel('画像名称').fill('泰国玩家');
        await page.getByLabel('需要什么').fill('找泰国的 MOBA 玩家');
        await page.getByRole('button', { name: '添加一项' }).click();
        await page.getByLabel('标题 1').fill('预算');
        await page.getByLabel('内容 1').fill('只找愿意付费的');
        await page.getByRole('button', { name: '保存' }).click();

        await expect(page.getByText('用户画像已保存，下一轮起生效。')).toBeVisible();
        await expect(page.getByRole('button', { name: /泰国玩家.*默认/ })).toBeVisible();
        await expect(page.getByLabel('需要什么')).toHaveValue('找泰国的 MOBA 玩家');
        await expect(page.getByLabel('标题 1')).toHaveValue('预算');

        await page.getByLabel('需要什么').fill('改过还没保存');

        await expect(page.getByText('有未保存的修改')).toBeVisible();

        await page.getByRole('button', { name: '复制' }).click();
        await expect(page.getByRole('button', { name: '泰国玩家（副本）' })).toBeVisible();
        await expect(page.getByLabel('画像名称')).toHaveValue('泰国玩家（副本）');

        await page.getByRole('button', { name: '设为默认' }).click();
        await expect(page.getByRole('button', { name: /泰国玩家（副本）.*默认/ })).toBeVisible();

        await page.getByRole('button', { exact: true, name: '删除' }).click();
        await page.getByRole('dialog').getByRole('button', { exact: true, name: '删除' }).click();
        await expect(page.getByText('已删除画像')).toBeVisible();
        await expect(page.getByRole('button', { name: /泰国玩家.*默认/ })).toBeVisible();
        await expect(page.getByRole('button', { name: /副本/ })).toHaveCount(0);

        expectCleanPage(pageErrorLog);
    });
});
