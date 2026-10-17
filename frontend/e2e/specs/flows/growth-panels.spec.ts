import { expect, test } from '../../fixtures/test.ts';
import { expectCleanPage } from '../../helpers/errors.ts';
import { flowTabsCassette } from '../../mocks/cassettes/flows.ts';

// The mock tier answers /api/v1/growth from the bundle's in-memory sample data: six Telegram leads (four
// qualified, two on the watch list) and a pool of sources.
test.describe('growth prospect and source panels', { tag: '@flows' }, () => {
    test.use({ cassette: flowTabsCassette() });

    test('lists the prospects and narrows them to the watch list', async ({ page, pageErrorLog }) => {
        await page.goto('/flows/5');
        await page.getByRole('tab', { name: '潜客' }).click();

        for (const name of ['阿杰 Jay', 'Mina', '老K', 'Arif', '小鱼', 'Thanh']) {
            await expect(page.getByRole('row', { name: new RegExp(name) })).toBeVisible();
        }

        await page.getByRole('combobox').first().click();
        await page.getByRole('option', { name: '待观察' }).click();

        await expect(page.getByRole('row', { name: /小鱼/ })).toBeVisible();
        await expect(page.getByRole('row', { name: /Thanh/ })).toBeVisible();
        await expect(page.getByRole('row', { name: /阿杰 Jay/ })).toHaveCount(0);
        await expect(page.getByRole('row', { name: /Arif/ })).toHaveCount(0);

        expectCleanPage(pageErrorLog);
    });

    test('opens a prospect with its score breakdown and original messages', async ({ page, pageErrorLog }) => {
        await page.goto('/flows/5');
        await page.getByRole('tab', { name: '潜客' }).click();
        await page.getByRole('row', { name: /阿杰 Jay/ }).click();

        const sheet = page.getByRole('dialog');

        await expect(sheet.getByText('打分明细')).toBeVisible();
        await expect(sheet.getByText('这游戏越来越氪了，想换个公平点的 SLG')).toBeVisible();
        await expect(sheet.getByText('有没有新开的 SLG 服，想重新开始')).toBeVisible();

        expectCleanPage(pageErrorLog);
    });

    test('adds pasted group links to the source pool and reports each bad link', async ({ page, pageErrorLog }) => {
        await page.goto('/flows/5');
        await page.getByRole('tab', { name: '来源池' }).click();

        await page
            .getByPlaceholder(/粘贴 Telegram 群\/频道或论坛、网页链接/)
            .fill('https://t.me/e2e_new_group\nnot-a-link');
        await page.getByRole('button', { name: '添加到来源池' }).click();

        await expect(page.getByText('已添加 1 个，将在下一轮加入并读取')).toBeVisible();
        await expect(page.getByText('not-a-link 不是有效的 Telegram 链接')).toBeVisible();
        await expect(page.getByRole('row', { name: /e2e_new_group/ })).toBeVisible();

        expectCleanPage(pageErrorLog);
    });
});
