import { SEEDED_USER } from '../fixtures/auth.ts';
import { expect, test } from '../fixtures/test.ts';
import { expectCleanPage } from '../helpers/errors.ts';
import { loginFailCassette, loginJourneyCassette, smokeCassette } from '../mocks/cassettes/smoke.ts';

test.describe('smoke', { tag: '@smoke' }, () => {
    test.describe('unauthenticated', () => {
        test.use({ cassette: loginJourneyCassette, isAuthSeeded: false });

        test('redirects a protected route to /login with returnUrl', async ({ page, pageErrorLog }) => {
            await page.goto('/flows');

            await expect(page).toHaveURL(/\/login\?returnUrl=%2Fflows/);
            await expect(page.getByRole('button', { exact: true, name: '登录' })).toBeVisible();
            // The guest /info carries OAuth providers, so the login page renders its OAuth buttons.
            await expect(page.getByRole('button', { name: '使用 Google 登录' })).toBeVisible();
            expectCleanPage(pageErrorLog);
        });

        test('logs in through the form and lands on the flows list', async ({ page, pageErrorLog }) => {
            await page.goto('/flows');
            await page.getByLabel('账号').fill(SEEDED_USER.mail);
            await page.getByRole('textbox', { name: '密码' }).fill('e2e-password');
            await page.getByRole('button', { exact: true, name: '登录' }).click();

            await expect(page).toHaveURL(/\/flows$/);
            await expect(page.getByText(SEEDED_USER.mail)).toBeVisible();
            expectCleanPage(pageErrorLog);
        });
    });

    test.describe('rejected login', () => {
        test.use({ cassette: loginFailCassette, isAuthSeeded: false });

        test('surfaces the error and re-disables Sign in until a field changes', async ({ page, pageErrorLog }) => {
            await page.goto('/login');
            await page.getByLabel('账号').fill(SEEDED_USER.mail);
            await page.getByRole('textbox', { name: '密码' }).fill('wrong-password');
            await page.getByRole('button', { exact: true, name: '登录' }).click();

            await expect(page.getByText('Login failed. Please try again.').first()).toBeVisible();
            await expect(page).toHaveURL(/\/login/);
            // Disabled by FormSubmitButton's `requireValid` gate (`isSubmitted && !isValid`), not by
            // the 401: the typed values are still schema-valid, and only a change recomputes validity.
            await expect(page.getByRole('button', { exact: true, name: '登录' })).toBeDisabled();

            await page.getByRole('textbox', { name: '密码' }).fill('another-password');

            await expect(page.getByRole('button', { exact: true, name: '登录' })).toBeEnabled();
            // The 401 logs an expected browser console error, but the path must raise no
            // uncaught JS exception / unhandled rejection.
            expect(pageErrorLog.pageErrors).toEqual([]);
        });
    });

    test.describe('authenticated', () => {
        test.use({ cassette: smokeCassette });

        test('renders the flows list with a clean page and no unmatched calls', async ({ page, pageErrorLog }) => {
            await page.goto('/flows');

            await expect(page).toHaveURL(/\/flows$/);
            await expect(page.getByText(SEEDED_USER.mail)).toBeVisible();
            expectCleanPage(pageErrorLog);
        });
    });
});
