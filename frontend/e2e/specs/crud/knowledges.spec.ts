import type { ResultOf } from '@graphql-typed-document-node/core';
import type { Page } from '@playwright/test';

import type {
    CreateKnowledgeDocumentDocument,
    DeleteKnowledgeDocumentDocument,
    KnowledgeDocumentDocument,
    RenameKnowledgeDocumentDocument,
} from '@/graphql/types';

import { ResultType } from '@/graphql/types';

import { expect, test } from '../../fixtures/test.ts';
import { typeIntoEditor } from '../../helpers/editor.ts';
import { expectCleanPage } from '../../helpers/errors.ts';
import {
    KNOWLEDGE_DOC,
    knowledgeDetailCassette,
    knowledgesCassette,
    makeKnowledge,
} from '../../mocks/cassettes/knowledges.ts';

const openNewKnowledgeForm = async (page: Page) => {
    await page.goto('/knowledges');
    await page.getByRole('button', { name: '新建知识' }).click();

    await expect(page).toHaveURL(/\/knowledges\/new$/);
    await expect(page.getByRole('combobox', { name: '回答类型' })).toBeVisible();
};

test.describe('knowledges crud', { tag: '@crud' }, () => {
    test.describe('create validation', () => {
        test.use({ cassette: knowledgesCassette() });

        test('blocks submit until the answer type is picked', async ({ page, pageErrorLog }) => {
            await openNewKnowledgeForm(page);
            await page.getByRole('textbox', { name: '问题' }).fill('What is the E2E answer?');
            await typeIntoEditor(page, '内容', 'E2E knowledge content');
            await page.getByRole('button', { name: '创建' }).click();

            await expect(page.getByText('请选择回答类型')).toBeVisible();
            await expect(page).toHaveURL(/\/knowledges\/new$/);
            expectCleanPage(pageErrorLog);
        });
    });

    test.describe('create', () => {
        const CREATED_DOC = makeKnowledge('301', 'What is the E2E answer?');
        const created: ResultOf<typeof CreateKnowledgeDocumentDocument> = { createKnowledgeDocument: CREATED_DOC };
        const detail: ResultOf<typeof KnowledgeDocumentDocument> = { knowledgeDocument: CREATED_DOC };

        test.use({
            cassette: knowledgesCassette({
                mutations: {
                    createKnowledgeDocument: [
                        {
                            data: created,
                            variables: {
                                input: {
                                    answerType: 'other',
                                    content: 'E2E knowledge content',
                                    docType: 'answer',
                                    question: 'What is the E2E answer?',
                                },
                            },
                        },
                    ],
                },
                queries: { knowledgeDocument: [{ data: detail, variables: { id: '301' } }] },
            }),
        });

        test('creates a document and lands on its detail page', async ({ page, pageErrorLog }) => {
            await openNewKnowledgeForm(page);
            await page.getByRole('textbox', { name: '问题' }).fill('What is the E2E answer?');
            await typeIntoEditor(page, '内容', 'E2E knowledge content');
            await page.getByRole('combobox', { name: '回答类型' }).click();
            await page.getByRole('option', { name: '其他' }).click();
            await page.getByRole('button', { name: '创建' }).click();

            await expect(page).toHaveURL(/\/knowledges\/301$/);
            await expect(page.getByRole('textbox', { name: '问题' })).toHaveValue('What is the E2E answer?');
            expectCleanPage(pageErrorLog);
        });
    });

    test.describe('anonymize', () => {
        const RAW = 'Contact admin@example.com about host 10.0.0.7';
        const SCRUBBED = 'Contact <email> about host <ip>';

        test.describe('the anonymizer finds something', () => {
            test.use({
                cassette: knowledgesCassette({
                    mutations: { anonymizeText: [{ data: { anonymizeText: SCRUBBED } as never }] },
                }),
            });

            test('replaces the body with what the server returned', async ({ page, pageErrorLog }) => {
                await openNewKnowledgeForm(page);
                await typeIntoEditor(page, '内容', RAW);

                const request = page.waitForRequest(
                    (candidate) =>
                        candidate.method() === 'POST' && candidate.postDataJSON()?.operationName === 'anonymizeText',
                );

                await page.getByRole('button', { name: '脱敏' }).click();

                expect((await request).postDataJSON().variables.text).toContain('admin@example.com');
                await expect(page.getByRole('textbox', { name: '内容' })).toContainText(SCRUBBED);
                expectCleanPage(pageErrorLog);
            });
        });

        test.describe('the anonymizer finds nothing', () => {
            const PLAIN = 'Nothing sensitive in this body at all';

            test.use({
                cassette: knowledgesCassette({
                    mutations: { anonymizeText: [{ data: { anonymizeText: PLAIN } as never }] },
                }),
            });

            test('leaves the body alone and says so', async ({ page }) => {
                await openNewKnowledgeForm(page);
                await typeIntoEditor(page, '内容', PLAIN);
                await page.getByRole('button', { name: '脱敏' }).click();

                await expect(page.getByText('没有发现敏感信息')).toBeVisible();
                await expect(page.getByRole('textbox', { name: '内容' })).toContainText(PLAIN);
            });
        });
    });

    test.describe('rename', () => {
        const RENAMED = 'E2E Seed Question, renamed';
        const renamed: ResultOf<typeof RenameKnowledgeDocumentDocument> = {
            renameKnowledgeDocument: { ...KNOWLEDGE_DOC, question: RENAMED },
        };

        test.use({
            cassette: knowledgesCassette({
                mutations: {
                    renameKnowledgeDocument: [
                        {
                            data: renamed,
                            setFlag: 'knowledge-renamed',
                            variables: { id: '7', question: RENAMED },
                        },
                    ],
                },
                subscriptions: {
                    knowledgeDocumentUpdated: [
                        {
                            frames: [
                                {
                                    payload: {
                                        data: { knowledgeDocumentUpdated: { ...KNOWLEDGE_DOC, question: RENAMED } },
                                    },
                                    whenFlag: 'knowledge-renamed',
                                },
                            ],
                        },
                    ],
                },
            }),
        });

        test('renames in the row itself and the list keeps the new question', async ({ page, pageErrorLog }) => {
            await page.goto('/knowledges');

            const row = page.getByRole('row', { name: /E2E Seed Question/ });

            await row.hover();
            await row.getByRole('button', { name: '打开菜单' }).click();
            await page.getByRole('menuitem', { name: '重命名' }).click();

            const editor = page.getByRole('row').getByRole('textbox');

            await expect(editor).toBeFocused();
            await editor.fill(RENAMED);
            await editor.press('Enter');

            await expect(page.getByRole('row', { name: new RegExp(RENAMED) })).toBeVisible();
            await expect(page.getByRole('row').getByRole('textbox')).toHaveCount(0);
            expectCleanPage(pageErrorLog);
        });
    });

    test.describe('delete', () => {
        const deleted: ResultOf<typeof DeleteKnowledgeDocumentDocument> = {
            deleteKnowledgeDocument: ResultType.Success,
        };

        test.use({
            cassette: knowledgesCassette({
                mutations: {
                    deleteKnowledgeDocument: [{ data: deleted, setFlag: 'knowledge-deleted', variables: { id: '7' } }],
                },
                subscriptions: {
                    knowledgeDocumentDeleted: [
                        {
                            frames: [
                                {
                                    payload: { data: { knowledgeDocumentDeleted: KNOWLEDGE_DOC } },
                                    whenFlag: 'knowledge-deleted',
                                },
                            ],
                        },
                    ],
                },
            }),
        });

        test('deletes from the row menu and drops off the list', async ({ page, pageErrorLog }) => {
            await page.goto('/knowledges');

            const row = page.getByRole('row', { name: /E2E Seed Question/ });

            await row.hover();
            await row.getByRole('button', { name: '打开菜单' }).click();
            await page.getByRole('menuitem', { name: '删除' }).click();

            const dialog = page.getByRole('dialog');

            await expect(dialog.getByRole('heading', { name: '删除知识文档' })).toBeVisible();
            await dialog.getByRole('button', { name: '删除' }).click();

            await expect(page.getByRole('row', { name: /E2E Seed Question/ })).toBeHidden();
            await expect(page.getByText('还没有知识文档')).toBeVisible();
            expectCleanPage(pageErrorLog);
        });
    });

    // A real load failure on the detail route used to be mislabelled "not found" and bounced to
    // the list with no way back in; it must keep the user on the route behind Retry instead.
    test.describe('detail load failure', () => {
        test.use({
            cassette: knowledgesCassette({
                queries: { knowledgeDocument: [{ errors: [{ message: 'e2e induced load failure' }] }] },
            }),
        });

        test('shows an in-page error with Retry, not a bounce to the list', async ({ page }) => {
            await page.goto(`/knowledges/${KNOWLEDGE_DOC.id}`);

            await expect(page.getByRole('button', { name: '重试' })).toBeVisible();
            await expect(page).toHaveURL(new RegExp(`/knowledges/${KNOWLEDGE_DOC.id}$`));
        });
    });

    test.describe('detail not found', () => {
        test.use({
            cassette: knowledgesCassette({
                // Mirrors the backend error presenter's answer to a missing row (graphql_errors.go).
                queries: {
                    knowledgeDocument: [{ errors: [{ extensions: { code: 'NOT_FOUND' }, message: 'not found' }] }],
                },
            }),
        });

        test('says the document is gone and returns to the list', async ({ page }) => {
            await page.goto(`/knowledges/${KNOWLEDGE_DOC.id}`);

            await expect(page.getByText('找不到知识文档')).toBeVisible();
            await expect(page).toHaveURL(/\/knowledges$/);
        });
    });

    // Mirrors the backend error presenter's answer to a denied permission (graphql_errors.go).
    test.describe('detail authz denial', () => {
        test.use({
            cassette: knowledgesCassette({
                queries: {
                    knowledgeDocument: [{ errors: [{ extensions: { code: 'FORBIDDEN' }, message: 'not permitted' }] }],
                },
            }),
        });

        test('does not bounce a permission denial to the list', async ({ page }) => {
            await page.goto(`/knowledges/${KNOWLEDGE_DOC.id}`);

            await expect(page.getByRole('button', { name: '重试' })).toBeVisible();
            await expect(page).toHaveURL(new RegExp(`/knowledges/${KNOWLEDGE_DOC.id}$`));
        });
    });

    test.describe('save', () => {
        test.use({
            cassette: knowledgesCassette({
                mutations: {
                    updateKnowledgeDocument: [{ data: { updateKnowledgeDocument: KNOWLEDGE_DOC } as never }],
                },
                queries: {
                    knowledgeDocument: [
                        { data: { knowledgeDocument: KNOWLEDGE_DOC }, variables: { id: KNOWLEDGE_DOC.id } },
                    ],
                },
            }),
        });

        // The write half of the editor: until now the suite proved the document loads and round-trips
        // in memory, never that Save puts the edited bytes on the wire.
        test('Save sends the edited content and keeps the document id', async ({ page, pageErrorLog }) => {
            await page.goto(`/knowledges/${KNOWLEDGE_DOC.id}`);

            await typeIntoEditor(page, '内容', 'E2E-SAVE-MARK');

            const request = page.waitForRequest(
                (candidate) =>
                    candidate.method() === 'POST' &&
                    candidate.postDataJSON()?.operationName === 'updateKnowledgeDocument',
            );

            await page.getByRole('button', { exact: true, name: '保存' }).click();

            const { variables } = (await request).postDataJSON();

            expect(variables.id).toBe(KNOWLEDGE_DOC.id);
            expect(variables.input.content).toContain('E2E-SAVE-MARK');
            expectCleanPage(pageErrorLog);
        });
    });

    test.describe('detail header', () => {
        test.use({ cassette: knowledgeDetailCassette() });

        test('keeps the pager left of save and the actions menu', async ({ page }) => {
            await page.goto(`/knowledges/${KNOWLEDGE_DOC.id}`);
            await expect(page.getByRole('button', { name: '知识操作' })).toBeVisible();

            const labels = await page
                .locator('header button')
                .evaluateAll((buttons) =>
                    buttons.map((button) => button.getAttribute('aria-label') ?? button.textContent),
                );
            const positionOf = (label: string) => labels.findIndex((candidate) => (candidate ?? '').includes(label));

            // findIndex returns -1 for an absent label, and -1 < any real index, so the ordering below
            // passes vacuously when a button is missing. Require presence first.
            for (const label of ['保存', '上一个', '下一个', '知识操作']) {
                expect(positionOf(label), `header is missing the "${label}" button`).toBeGreaterThanOrEqual(0);
            }

            expect(positionOf('上一个')).toBeLessThan(positionOf('保存'));
            expect(positionOf('下一个')).toBeLessThan(positionOf('保存'));
            expect(positionOf('保存')).toBeLessThan(positionOf('知识操作'));
        });
    });
});
