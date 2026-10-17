import { expect, test } from '../../fixtures/test.ts';
import { expectCleanPage } from '../../helpers/errors.ts';
import {
    flowTabsCassette,
    TABS_SCREENSHOT_NEWER_URL,
    TABS_SCREENSHOT_NEWEST_NAME,
    TABS_SCREENSHOT_NEWEST_URL,
    TABS_SCREENSHOT_URL,
} from '../../mocks/cassettes/flows.ts';
import { FLOW_DETAIL_TABS } from '../../routes.ts';

test.describe('flow detail tabs', { tag: '@flows' }, () => {
    test.use({ cassette: flowTabsCassette() });

    test('each tab renders its populated content', async ({ page, pageErrorLog }) => {
        await page.goto('/flows/5');
        await expect(page.locator('header').getByRole('button', { name: '收藏 / 取消收藏' })).toBeEnabled();

        for (const { name, ready } of FLOW_DETAIL_TABS) {
            // Pins that the click is what reveals the panel. Ordering this sweep so a tab is already
            // open when its turn comes (the flow auto-opens Assistant) makes its iteration a no-op.
            await expect(ready(page), `"${name}" must not be on screen before its own click`).not.toBeAttached();
            await page.getByRole('tab', { name }).click();
            await expect(ready(page)).toBeVisible();
        }

        expectCleanPage(pageErrorLog);
    });

    test('keeps both tab strips in place while a panel scrolls', async ({ page, pageErrorLog }) => {
        await page.setViewportSize({ height: 700, width: 1500 });
        await page.goto('/flows/5');
        await page.getByRole('tab', { name: '潜客' }).click();
        await expect(page.getByRole('row', { name: /阿杰 Jay/ })).toBeVisible();

        const strips = page.getByRole('tablist');
        const tops = async () => strips.evaluateAll((lists) => lists.map((list) => list.getBoundingClientRect().top));
        const before = await tops();

        await page.getByRole('row', { name: /阿杰 Jay/ }).hover();
        await page.mouse.wheel(0, 2000);

        await expect.poll(() => page.evaluate(() => document.scrollingElement?.scrollTop)).toBe(0);
        expect(await tops()).toEqual(before);

        expectCleanPage(pageErrorLog);
    });

    test('the screenshot image decodes from its REST endpoint', async ({ page, pageErrorLog }) => {
        await page.goto('/flows/5');
        await expect(page.locator('header').getByRole('button', { name: '收藏 / 取消收藏' })).toBeEnabled();
        await page.getByRole('tab', { name: '截图' }).click();

        const image = page.getByRole('img', { name: TABS_SCREENSHOT_NEWEST_NAME });
        await image.scrollIntoViewIfNeeded();

        // toBeVisible passes on an undecoded element; only the width proves the blob arrived.
        await expect
            .poll(async () => image.evaluate((element: HTMLImageElement) => element.naturalWidth))
            .toBeGreaterThan(0);

        expectCleanPage(pageErrorLog);
    });

    test('renders screenshots in the order the server sent, without re-sorting them', async ({
        page,
        pageErrorLog,
    }) => {
        await page.goto('/flows/5');
        await expect(page.locator('header').getByRole('button', { name: '收藏 / 取消收藏' })).toBeEnabled();
        await page.getByRole('tab', { name: '截图' }).click();

        const shots = page.getByRole('tabpanel', { name: '截图' }).getByRole('link');

        await expect(shots).toHaveText([TABS_SCREENSHOT_URL, TABS_SCREENSHOT_NEWER_URL, TABS_SCREENSHOT_NEWEST_URL]);

        expectCleanPage(pageErrorLog);
    });
});
