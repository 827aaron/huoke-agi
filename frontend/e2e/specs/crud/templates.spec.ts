import type { ResultOf } from '@graphql-typed-document-node/core';

import type {
    CreateFlowTemplateDocument,
    DeleteFlowTemplateDocument,
    UpdateFlowTemplateDocument,
} from '@/graphql/types';

import { ResultType } from '@/graphql/types';

import { expect, test } from '../../fixtures/test.ts';
import { typeIntoEditor } from '../../helpers/editor.ts';
import { expectCleanPage } from '../../helpers/errors.ts';
import { makeTemplate, TEMPLATE_SEED, templatesCassette } from '../../mocks/cassettes/templates.ts';

test.describe('templates crud', { tag: '@crud' }, () => {
    test.describe('create', () => {
        const NEW_TITLE = 'E2E Created Template';
        const NEW_TEXT = 'E2E created template body';
        const createdTemplate = makeTemplate('12', NEW_TITLE, NEW_TEXT);
        const created: ResultOf<typeof CreateFlowTemplateDocument> = { createFlowTemplate: createdTemplate };

        test.use({
            cassette: templatesCassette({
                mutations: {
                    createFlowTemplate: [
                        {
                            data: created,
                            setFlag: 'template-created',
                            variables: { input: { text: NEW_TEXT, title: NEW_TITLE } },
                        },
                    ],
                },
                subscriptions: {
                    flowTemplateCreated: [
                        {
                            frames: [
                                {
                                    payload: { data: { flowTemplateCreated: createdTemplate } },
                                    whenFlag: 'template-created',
                                },
                            ],
                        },
                    ],
                },
            }),
        });

        test('creates a template and the new row arrives via the created frame', async ({ page, pageErrorLog }) => {
            await page.goto('/templates');

            await expect(page.getByRole('row', { name: /E2E Seed Template/ })).toBeVisible();

            await page.getByRole('button', { name: '新建模板' }).click();

            await expect(page).toHaveURL(/\/templates\/new$/);

            await page.getByLabel('标题').fill(NEW_TITLE);
            await typeIntoEditor(page, '模板内容', NEW_TEXT);
            await page.getByRole('button', { name: '创建' }).click();

            await expect(page).toHaveURL(/\/templates$/);
            await expect(page.getByRole('row', { name: /E2E Created Template/ })).toBeVisible();
            await expect(page.getByRole('row', { name: /E2E Seed Template/ })).toBeVisible();
            expectCleanPage(pageErrorLog);
        });
    });

    test.describe('rename', () => {
        const renamed: ResultOf<typeof UpdateFlowTemplateDocument> = {
            updateFlowTemplate: makeTemplate(TEMPLATE_SEED.id, 'E2E Renamed Template', TEMPLATE_SEED.text),
        };

        test.use({
            cassette: templatesCassette({
                mutations: {
                    updateFlowTemplate: [
                        {
                            data: renamed,
                            variables: {
                                input: { text: TEMPLATE_SEED.text, title: 'E2E Renamed Template' },
                                templateId: TEMPLATE_SEED.id,
                            },
                        },
                    ],
                },
            }),
        });

        test('renames inline from the row menu and the row shows the new title', async ({ page, pageErrorLog }) => {
            await page.goto('/templates');

            const row = page.getByRole('row', { name: /E2E Seed Template/ });

            await row.hover();
            await row.getByRole('button', { name: '打开菜单' }).click();
            await page.getByRole('menuitem', { name: '重命名' }).click();
            await page.getByPlaceholder('模板标题').fill('E2E Renamed Template');
            await page.getByPlaceholder('模板标题').press('Enter');

            await expect(page.getByText('模板已重命名')).toBeVisible();
            await expect(page.getByRole('row', { name: /E2E Renamed Template/ })).toBeVisible();
            await expect(page.getByRole('row', { name: /E2E Seed Template/ })).toBeHidden();
            expectCleanPage(pageErrorLog);
        });
    });

    test.describe('delete', () => {
        const deleted: ResultOf<typeof DeleteFlowTemplateDocument> = { deleteFlowTemplate: ResultType.Success };

        test.use({
            cassette: templatesCassette({
                mutations: {
                    deleteFlowTemplate: [
                        {
                            data: deleted,
                            setFlag: 'template-deleted',
                            variables: { templateId: TEMPLATE_SEED.id },
                        },
                    ],
                },
                subscriptions: {
                    flowTemplateDeleted: [
                        {
                            frames: [
                                {
                                    payload: { data: { flowTemplateDeleted: TEMPLATE_SEED } },
                                    whenFlag: 'template-deleted',
                                },
                            ],
                        },
                    ],
                },
            }),
        });

        test('deletes via the confirm dialog and the row drops off the list', async ({ page, pageErrorLog }) => {
            await page.goto('/templates');

            const row = page.getByRole('row', { name: /E2E Seed Template/ });

            await row.hover();
            await row.getByRole('button', { name: '打开菜单' }).click();
            await page.getByRole('menuitem', { name: '删除' }).click();

            const dialog = page.getByRole('dialog');

            await expect(dialog.getByRole('heading', { name: '删除模板' })).toBeVisible();
            await dialog.getByRole('button', { name: '删除' }).click();

            await expect(page.getByRole('row', { name: /E2E Seed Template/ })).toBeHidden();
            await expect(page.getByText('还没有模板')).toBeVisible();
            expectCleanPage(pageErrorLog);
        });
    });
});
