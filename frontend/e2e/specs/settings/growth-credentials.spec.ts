import type { Locator, Page } from '@playwright/test';

import type { Cassette } from '../../mocks/cassette.ts';

import { expect, test } from '../../fixtures/test.ts';
import { expectCleanPage } from '../../helpers/errors.ts';
import { baseQueries, baseRest } from '../../mocks/cassettes/base.ts';

// The growth API answers from the bundle's in-memory sample data on the mock tier (VITE_GROWTH_API=mock):
// an LLM key (sk-••••••••a1b2) and a Tavily key are already saved, nothing else is.
const growthCassette = (): Cassette => ({ queries: baseQueries(), rest: baseRest() });

const card = (page: Page, title: string): Locator =>
    page.locator('[data-slot="card"]').filter({ has: page.locator('[data-slot="card-title"]', { hasText: title }) });

test.describe('growth keys and connections', { tag: '@settings' }, () => {
    test.use({ cassette: growthCassette() });

    test('shows saved keys masked and the unset ones as not set', async ({ page, pageErrorLog }) => {
        await page.goto('/settings/credentials');

        const llm = card(page, '大模型');

        await expect(llm.getByText('sk-••••••••a1b2')).toBeVisible();
        await expect(llm.getByText('必填', { exact: true })).toBeVisible();
        await expect(card(page, 'Telegram 接口凭据').getByText('未配置', { exact: true }).first()).toBeVisible();
        await expect(card(page, 'YouTube').getByText('可选', { exact: true })).toBeVisible();
        await expect(card(page, '邮件通知').getByText('未配置', { exact: true }).first()).toBeVisible();
        await expect(page.getByText('还没有填写任何密钥')).toHaveCount(0);

        expectCleanPage(pageErrorLog);
    });

    test('never shows a saved secret in clear text', async ({ page, pageErrorLog }) => {
        await page.goto('/settings/credentials');
        await expect(card(page, '大模型').getByText('sk-••••••••a1b2')).toBeVisible();

        await expect(page.locator('input[type="text"], input[type="password"]').filter({ hasText: /sk-/ })).toHaveCount(
            0,
        );
        await expect(page.getByText(/sk-[A-Za-z0-9]{8,}/)).toHaveCount(0);

        expectCleanPage(pageErrorLog);
    });

    test('rejects malformed Telegram credentials before saving', async ({ page, pageErrorLog }) => {
        await page.goto('/settings/credentials');

        const telegram = card(page, 'Telegram 接口凭据');

        await telegram.getByLabel('api_id').fill('not-a-number');
        await telegram.getByLabel('api_hash').fill('short');
        await telegram.getByRole('button', { name: '保存并测试' }).click();

        await expect(telegram.getByText('api_id 是一串纯数字')).toBeVisible();
        await expect(telegram.getByText('api_hash 应为 32 位字母和数字')).toBeVisible();
        await expect(telegram.getByText('未配置', { exact: true }).first()).toBeVisible();

        expectCleanPage(pageErrorLog);
    });

    test('saves Telegram credentials, shows them masked and lets the user clear them', async ({
        page,
        pageErrorLog,
    }) => {
        await page.goto('/settings/credentials');

        const telegram = card(page, 'Telegram 接口凭据');

        await telegram.getByLabel('api_id').fill('1234567');
        await telegram.getByLabel('api_hash').fill('0123456789abcdef0123456789abcdef');
        await telegram.getByRole('button', { name: '保存并测试' }).click();

        await expect(page.getByText('Telegram 接口凭据已保存并通过测试')).toBeVisible();
        await expect(telegram.getByText('已连接', { exact: true }).first()).toBeVisible();
        await expect(telegram.getByText('0123456789abcdef0123456789abcdef')).toHaveCount(0);
        await expect(page.getByText('必填项已就绪')).toBeVisible();

        await telegram.getByRole('button', { name: '清除' }).click();

        await expect(page.getByText('Telegram 接口凭据已清除')).toBeVisible();
        await expect(telegram.getByText('未配置', { exact: true }).first()).toBeVisible();
        await expect(page.getByText('必填项已就绪')).toHaveCount(0);

        expectCleanPage(pageErrorLog);
    });

    test('flags a key the provider rejects as invalid', async ({ page, pageErrorLog }) => {
        await page.goto('/settings/credentials');

        const youtube = card(page, 'YouTube');

        await youtube.getByLabel('API 密钥').fill('bad-key-value-123456');
        await youtube.getByRole('button', { name: '保存并测试' }).click();

        await expect(page.getByText(/YouTube已保存，但测试未通过/)).toBeVisible();
        await expect(youtube.getByText('无效', { exact: true }).first()).toBeVisible();

        expectCleanPage(pageErrorLog);
    });

    test('requires every email field before saving', async ({ page, pageErrorLog }) => {
        await page.goto('/settings/credentials');

        const email = card(page, '邮件通知');

        await email.getByLabel('发信服务器（SMTP）').fill('smtp.example.com');
        await email.getByLabel('端口').fill('70000');
        await email.getByRole('button', { name: '保存并测试' }).click();

        await expect(email.getByText('端口是 1 到 65535 之间的数字')).toBeVisible();
        await expect(email.getByText('请填写 登录用户名')).toBeVisible();

        expectCleanPage(pageErrorLog);
    });

    test('saves the email settings, then sends a test email to the chosen address', async ({ page, pageErrorLog }) => {
        await page.goto('/settings/credentials');

        const email = card(page, '邮件通知');

        await email.getByLabel('发信服务器（SMTP）').fill('smtp.example.com');
        await email.getByLabel('端口').fill('465');
        await email.getByLabel('登录用户名').fill('ops@example.com');
        await email.getByLabel('密码或授权码').fill('app-password-123456');
        await email.getByLabel('发件人地址').fill('ops@example.com');
        await email.getByLabel('接收名单的邮箱').fill('boss@example.com');
        await email.getByRole('button', { name: '保存并测试' }).click();

        await expect(page.getByText('邮件通知已保存并通过测试')).toBeVisible();
        await expect(email.getByText('app-password-123456')).toHaveCount(0);

        await email.getByRole('button', { name: '发送测试邮件' }).click();

        await expect(page.getByText('测试邮件已发送到 boss@example.com')).toBeVisible();

        expectCleanPage(pageErrorLog);
    });
});
