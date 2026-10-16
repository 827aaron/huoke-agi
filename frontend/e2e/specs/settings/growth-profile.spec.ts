import type { Cassette } from '../../mocks/cassette.ts';

import { expect, test } from '../../fixtures/test.ts';
import { expectCleanPage } from '../../helpers/errors.ts';
import { baseQueries, baseRest } from '../../mocks/cassettes/base.ts';

// The mock tier answers /api/v1/growth from the bundle's in-memory sample data, which starts with no profile.
const growthCassette = (): Cassette => ({ queries: baseQueries(), rest: baseRest() });

test.describe('growth business profile', { tag: '@settings' }, () => {
    test.use({ cassette: growthCassette() });

    test('prefills the default exclusions and flags every missing required field', async ({ page, pageErrorLog }) => {
        await page.goto('/settings/business-profile');

        await expect(page.getByText('还没有业务档案')).toBeVisible();
        await expect(page.getByLabel('排除项')).toHaveValue('卖号\n代练\n竞品官方\n机器人');

        await page.getByRole('button', { name: '保存' }).click();

        await expect(page.getByText('请填写业务名称')).toBeVisible();
        await expect(page.getByText('请填写游戏 / 产品名称')).toBeVisible();
        await expect(page.getByText('请选择游戏类型')).toBeVisible();
        await expect(page.getByText('请填写你想让玩家知道什么')).toBeVisible();
        await expect(page.getByText('请填写目标地区')).toBeVisible();
        await expect(page.getByText('请选择目标语言')).toBeVisible();
        await expect(page.getByText('业务档案已保存')).toHaveCount(0);

        expectCleanPage(pageErrorLog);
    });

    test('rejects a round target outside 1 to 1000', async ({ page, pageErrorLog }) => {
        await page.goto('/settings/business-profile');

        await page.getByLabel('默认每轮目标数').fill('1001');
        await page.getByRole('button', { name: '保存' }).click();

        await expect(page.getByText('目标数请输入 1–1000 之间的整数')).toBeVisible();

        await page.getByLabel('默认每轮目标数').fill('0');

        await expect(page.getByText('目标数请输入 1–1000 之间的整数')).toBeVisible();

        await page.getByLabel('默认每轮目标数').fill('50');

        await expect(page.getByText('目标数请输入 1–1000 之间的整数')).toHaveCount(0);

        expectCleanPage(pageErrorLog);
    });

    test('saves a complete profile', async ({ page, pageErrorLog }) => {
        await page.goto('/settings/business-profile');

        await page.getByLabel('业务名称').fill('星穹纪元');
        await page.getByLabel('游戏 / 产品名称').fill('星穹纪元, Star Era');
        await page.getByRole('combobox').click();
        await page.getByRole('option', { name: 'MMORPG' }).click();
        await page.getByLabel('你想让玩家知道什么').fill('新服开启，老玩家回归送礼包');
        await page.getByLabel('目标地区').fill('东南亚、台湾');
        await page.getByRole('button', { name: '中文' }).click();
        await page.getByRole('button', { name: 'English' }).click();
        await page.getByLabel('默认每轮目标数').fill('30');
        await page.getByRole('button', { name: '保存' }).click();

        await expect(page.getByText('业务档案已保存，下一轮起生效。')).toBeVisible();
        await expect(page.getByText('请填写业务名称')).toHaveCount(0);
        await expect(page.getByLabel('业务名称')).toHaveValue('星穹纪元');
        await expect(page.getByLabel('默认每轮目标数')).toHaveValue('30');

        expectCleanPage(pageErrorLog);
    });
});
