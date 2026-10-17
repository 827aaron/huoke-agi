import type { Page } from '@playwright/test';

import type { VersionInfoFragmentFragment } from '@/graphql/types';

import { UpdateState } from '@/graphql/types';

import type { Cassette } from '../../mocks/cassette.ts';

import { infoEntryFor, seedAuthenticatedAs } from '../../fixtures/auth.ts';
import { expect, test } from '../../fixtures/test.ts';
import { expectCleanPage } from '../../helpers/errors.ts';
import { entity, mergeCassettes } from '../../mocks/cassette.ts';
import { baseQueries, baseRest } from '../../mocks/cassettes/base.ts';

const versionInfo = (fields: Partial<VersionInfoFragmentFragment> = {}) => ({
    data: {
        versionInfo: entity('VersionInfo', {
            build: 'e2e00000',
            checkedAt: null,
            current: 'e2e',
            failedAt: null,
            latest: null,
            state: UpdateState.Disabled,
            strategy: 'preview',
            ...fields,
        }),
    },
});

const sidebarCassette = (overrides: Cassette = {}): Cassette =>
    mergeCassettes({ queries: baseQueries(), rest: baseRest() }, overrides);

const root = (page: Page) => page.locator('[data-side][data-state][data-variant]');
const header = (page: Page) => page.getByRole('button', { name: /客来 AGI.*e2e/ });

test.describe('sidebar header', { tag: '@cross' }, () => {
    test.describe('a build with nothing to act on', () => {
        test.use({ cassette: sidebarCassette() });

        test('carries the product name and the running version', async ({ page, pageErrorLog }) => {
            await page.goto('/flows');

            await expect(header(page)).toBeVisible();
            await expect(header(page)).toContainText('客来 AGI');
            await expect(header(page)).toContainText('e2e');
            expectCleanPage(pageErrorLog);
        });

        test('raises no indicator, and the panel says so', async ({ page }) => {
            await page.goto('/flows');
            await header(page).click();

            const panel = page.getByRole('menu');

            await expect(panel).toContainText('已关闭更新检查');
            await expect(page.locator('[data-slot^="version-indicator"]')).toHaveCount(0);
        });

        test('closes the panel on Escape', async ({ page }) => {
            await page.goto('/flows');
            await header(page).click();
            await expect(page.getByRole('menu')).toBeVisible();

            await page.keyboard.press('Escape');

            await expect(page.getByRole('menu')).toHaveCount(0);
        });

        test('explains the update channel when the badge is hovered', async ({ page }) => {
            await page.goto('/flows');
            await header(page).click();

            await page.getByLabel('更新渠道：preview').hover();

            await expect(page.getByRole('tooltip')).toContainText('preview 渠道');
        });
    });

    test.describe('a build with an update offered', () => {
        test.use({
            cassette: sidebarCassette({
                queries: { versionInfo: [versionInfo({ latest: '2.4.0', state: UpdateState.UpdateAvailable })] },
            }),
        });

        test('raises the indicator and names the new version', async ({ page, pageErrorLog }) => {
            await page.goto('/flows');

            await expect(page.locator('[data-slot="version-indicator-update"]')).toBeVisible();

            await header(page).click();

            await expect(page.getByRole('menu')).toContainText('新版本 v2.4.0 可用');
            await expect(page.getByRole('menuitem')).toHaveCount(0);
            expectCleanPage(pageErrorLog);
        });

        test('opens from the keyboard while the sidebar is collapsed', async ({ page }) => {
            await page.goto('/flows');
            await expect(root(page)).toHaveAttribute('data-state', 'expanded');
            await page.keyboard.press('ControlOrMeta+b');
            await expect(root(page)).toHaveAttribute('data-state', 'collapsed');

            await page.keyboard.press('Tab');
            await expect(header(page)).toBeFocused();

            await page.keyboard.press('Enter');

            await expect(page.getByRole('menu')).toContainText('新版本 v2.4.0 可用');
        });
    });
});

test.describe('sidebar collapse', { tag: '@cross' }, () => {
    test.use({ cassette: sidebarCassette() });

    test('collapses and expands from the trigger', async ({ page, pageErrorLog }) => {
        await page.goto('/flows');
        await expect(root(page)).toHaveAttribute('data-state', 'expanded');

        const trigger = page.getByRole('button', { name: '展开/收起侧边栏' }).first();

        await trigger.click();
        await expect(root(page)).toHaveAttribute('data-state', 'collapsed');
        await expect(root(page)).toHaveAttribute('data-collapsible', 'icon');

        await trigger.click();
        await expect(root(page)).toHaveAttribute('data-state', 'expanded');
        expectCleanPage(pageErrorLog);
    });

    test('collapses from the keyboard shortcut', async ({ page }) => {
        await page.goto('/flows');
        await expect(root(page)).toHaveAttribute('data-state', 'expanded');

        await page.keyboard.press('ControlOrMeta+b');

        await expect(root(page)).toHaveAttribute('data-state', 'collapsed');
    });

    test('remembers the collapsed state across a reload', async ({ context, page }) => {
        await page.goto('/flows');
        await page.getByRole('button', { name: '展开/收起侧边栏' }).first().click();
        await expect(root(page)).toHaveAttribute('data-state', 'collapsed');

        const cookie = (await context.cookies()).find((entry) => entry.name === 'sidebar:state');

        expect(cookie?.value).toBe('false');

        await page.reload();

        await expect(root(page)).toHaveAttribute('data-state', 'collapsed');
    });
});

const GH_USER = { mail: 'gh@example.com', name: 'gh', provider: 'github', type: 'oauth' } as const;
const ANON_USER = { mail: 'x@example.com', name: 'x', type: 'oauth' } as const;

test.describe('sidebar account badge', { tag: '@cross' }, () => {
    const openMenu = async (page: Page, mail: string) => {
        await page.goto('/flows');
        await page.getByRole('button', { name: new RegExp(mail) }).click();
    };

    test.describe('a local account', () => {
        test.use({ cassette: sidebarCassette() });

        test('is named local', async ({ page, pageErrorLog }) => {
            await openMenu(page, 'admin@pentagi.com');

            await expect(page.getByRole('menu').getByText('本地账号')).toBeVisible();
            expectCleanPage(pageErrorLog);
        });
    });

    test.describe('a federated account the record names', () => {
        test.use({
            cassette: sidebarCassette({
                rest: { 'GET /api/v1/info': [infoEntryFor(GH_USER)] },
            }),
            isAuthSeeded: false,
        });

        test('is named by its provider', async ({ page }) => {
            await seedAuthenticatedAs(page, GH_USER);
            await openMenu(page, 'gh@example.com');

            await expect(page.getByRole('menu').getByText('GitHub')).toBeVisible();
        });
    });

    test.describe('a federated account the record does not name', () => {
        test.use({
            cassette: sidebarCassette({ rest: { 'GET /api/v1/info': [infoEntryFor(ANON_USER)] } }),
            isAuthSeeded: false,
        });

        test('falls back to the kind of account', async ({ page }) => {
            await seedAuthenticatedAs(page, ANON_USER);
            await openMenu(page, 'x@example.com');

            await expect(page.getByRole('menu').getByText('OAuth')).toBeVisible();
        });
    });
});
