import type { Page } from '@playwright/test';

import { expect, test } from '../../fixtures/test.ts';
import { clipboardWrites, recordClipboardWrites } from '../../helpers/clipboard.ts';
import { expectCleanPage } from '../../helpers/errors.ts';
import {
    flowTabsCassette,
    TABS_FILE_NAME,
    TABS_FILE_PATH,
    TABS_RESOURCE_FILE_NAME,
    TABS_RESOURCE_FILE_PATH,
} from '../../mocks/cassettes/flows.ts';

const DELETED = 'flow-file-deleted';
const STORED_NAME = 'stored-report.txt';
const PROMOTE_PATH = 'library/keep-this.txt';
const ATTACHED_RESOURCE = { id: 9, name: 'lib-notes.txt' };
const NESTED_FLOW_FILE = { name: 'nested-report.txt', path: 'uploads/sub/nested-report.txt' };
const NESTED_FLOW_FILE_DEFAULT = 'sub/nested-report.txt';
const flowFilesWithNested = {
    flowFiles: [
        {
            __typename: 'FlowFile' as const,
            flowId: '5',
            id: '1',
            isDir: false,
            modifiedAt: '2026-01-15T09:00:00Z',
            name: TABS_FILE_NAME,
            path: TABS_FILE_PATH,
            size: 2048,
        },
        {
            __typename: 'FlowFile' as const,
            flowId: '5',
            id: '3',
            isDir: false,
            modifiedAt: '2026-01-15T09:00:00Z',
            name: NESTED_FLOW_FILE.name,
            path: NESTED_FLOW_FILE.path,
            size: 128,
        },
    ],
};
const CONTAINER_FILE = { name: 'evidence.log', path: '/work/evidence.log' };

const promoteRequest = (page: Page) =>
    page.waitForRequest(
        (candidate) =>
            candidate.method() === 'POST' && new URL(candidate.url()).pathname === '/api/v1/flows/5/files/to-resources',
    );

const openRowMenu = async (page: Page, name: string) => {
    await page
        .getByRole('treeitem', { name: new RegExp(name) })
        .getByRole('button', { name: '行操作' })
        .click();
};

const openFilesTab = async (page: Page) => {
    await page.goto('/flows/5');
    await expect(page.locator('header').getByRole('button', { name: '收藏 / 取消收藏' })).toBeEnabled();
    await page.getByRole('tab', { name: '文件' }).click();
    await expect(page.getByRole('treeitem', { name: new RegExp(TABS_FILE_NAME) })).toBeVisible();
};

test.describe('flow files', { tag: '@flows' }, () => {
    test.describe('upload', () => {
        test.use({
            cassette: flowTabsCassette({
                rest: {
                    'POST /api/v1/flows/5/files/': [
                        {
                            body: {
                                data: { files: [{ isDir: false, name: STORED_NAME, path: `uploads/${STORED_NAME}` }] },
                                status: 'success',
                            },
                        },
                    ],
                },
            }),
        });

        test('sends the picked file and reports the name the server stored', async ({ page, pageErrorLog }) => {
            await openFilesTab(page);

            const request = page.waitForRequest(
                (candidate) =>
                    candidate.method() === 'POST' && new URL(candidate.url()).pathname === '/api/v1/flows/5/files/',
            );

            await page.locator('input[name="flow-file-upload"]').setInputFiles({
                buffer: Buffer.from('e2e upload body'),
                mimeType: 'text/plain',
                name: 'picked-report.txt',
            });

            expect((await request).postData(), 'the picked file travels as multipart form data').toContain(
                'filename="picked-report.txt"',
            );

            await expect(page.getByText(`已可在 /work/uploads/${STORED_NAME} 使用`)).toBeVisible();
            expectCleanPage(pageErrorLog);
        });
    });

    test.describe('delete', () => {
        test.use({
            cassette: flowTabsCassette({
                rest: {
                    'DELETE /api/v1/flows/5/files/': [
                        {
                            body: { data: { files: [] }, status: 'success' },
                            querySubset: { 'paths[]': TABS_FILE_PATH },
                            setFlag: DELETED,
                        },
                    ],
                },
                subscriptions: {
                    flowFileDeleted: [
                        {
                            frames: [
                                {
                                    payload: {
                                        data: {
                                            flowFileDeleted: {
                                                __typename: 'FlowFile',
                                                flowId: '5',
                                                id: '1',
                                                isDir: false,
                                                modifiedAt: '2026-01-15T09:00:00Z',
                                                name: TABS_FILE_NAME,
                                                path: TABS_FILE_PATH,
                                                size: 2048,
                                            },
                                        },
                                    },
                                    whenFlag: DELETED,
                                },
                            ],
                            variables: { flowId: '5' },
                        },
                    ],
                },
            }),
        });

        test('asks for the one path the row owns, and the row leaves on the event', async ({ page, pageErrorLog }) => {
            await openFilesTab(page);

            const row = page.getByRole('treeitem', { name: new RegExp(TABS_FILE_NAME) });

            await row.getByRole('button', { name: '行操作' }).click();
            await page.getByRole('menuitem', { name: '删除' }).click();

            const dialog = page.getByRole('dialog');

            await expect(dialog.getByRole('heading', { name: '删除文件' })).toBeVisible();

            const request = page.waitForRequest(
                (candidate) =>
                    candidate.method() === 'DELETE' && new URL(candidate.url()).pathname === '/api/v1/flows/5/files/',
            );

            await dialog.getByRole('button', { exact: true, name: '删除' }).click();

            expect(new URL((await request).url()).searchParams.getAll('paths[]')).toEqual([TABS_FILE_PATH]);

            await expect(page.getByRole('dialog')).toHaveCount(0);
            await expect(row).toBeHidden();
            await expect(page.getByRole('treeitem', { name: new RegExp(TABS_RESOURCE_FILE_NAME) })).toBeVisible();
            expectCleanPage(pageErrorLog);
        });
    });

    test.describe('bulk delete', () => {
        test.use({
            cassette: flowTabsCassette({
                rest: {
                    'DELETE /api/v1/flows/5/files/': [
                        {
                            body: { data: { files: [] }, status: 'success' },
                            querySubset: { 'paths[]': [TABS_FILE_PATH, TABS_RESOURCE_FILE_PATH] },
                        },
                    ],
                },
            }),
        });

        test('sends every checked path in one request', async ({ page, pageErrorLog }) => {
            await openFilesTab(page);

            await page.getByRole('checkbox', { name: `选择 ${TABS_FILE_NAME}` }).click();
            await page.getByRole('checkbox', { name: `选择 ${TABS_RESOURCE_FILE_NAME}` }).click();

            const request = page.waitForRequest(
                (candidate) =>
                    candidate.method() === 'DELETE' && new URL(candidate.url()).pathname === '/api/v1/flows/5/files/',
            );

            await page.getByRole('button', { exact: true, name: '删除' }).click();
            await page.getByRole('dialog').getByRole('button', { exact: true, name: '删除' }).click();

            expect(new URL((await request).url()).searchParams.getAll('paths[]')).toEqual([
                TABS_FILE_PATH,
                TABS_RESOURCE_FILE_PATH,
            ]);
            await expect(page.getByText('已删除 2 项')).toBeVisible();
            expectCleanPage(pageErrorLog);
        });
    });

    test.describe('save as resource', () => {
        test.use({
            cassette: flowTabsCassette({
                queries: { flowFiles: [{ data: flowFilesWithNested, variables: { flowId: '5' } }] },
                rest: {
                    'POST /api/v1/flows/5/files/to-resources': [
                        {
                            body: { data: { items: [], total: 0 }, status: 'success' },
                            bodySubset: { destination: PROMOTE_PATH, force: false, sources: [NESTED_FLOW_FILE.path] },
                        },
                    ],
                },
            }),
        });

        test('promotes the file to the path the operator typed', async ({ page, pageErrorLog }) => {
            await openFilesTab(page);
            await page.getByRole('button', { name: '全部展开' }).click();
            await openRowMenu(page, NESTED_FLOW_FILE.name);
            await page.getByRole('menuitem', { name: '保存为资源' }).click();

            const dialog = page.getByRole('dialog');

            await expect(dialog.getByLabel('目标路径')).toHaveValue(NESTED_FLOW_FILE_DEFAULT);
            await dialog.getByLabel('目标路径').fill(PROMOTE_PATH);

            const request = promoteRequest(page);

            await dialog.getByRole('button', { exact: true, name: '保存' }).click();

            expect((await request).postDataJSON()).toMatchObject({
                destination: PROMOTE_PATH,
                force: false,
                sources: [NESTED_FLOW_FILE.path],
            });
            await expect(page.getByText(`已保存到资源库的 ${PROMOTE_PATH}`)).toBeVisible();
            expectCleanPage(pageErrorLog);
        });
    });

    test.describe('save as resource over an existing one', () => {
        test.use({
            cassette: flowTabsCassette({
                rest: {
                    'GET /api/v1/resources/': [
                        {
                            body: {
                                data: {
                                    items: [
                                        {
                                            created_at: '2026-01-15T09:00:00Z',
                                            id: 9,
                                            is_dir: false,
                                            name: PROMOTE_PATH.split('/').pop(),
                                            path: PROMOTE_PATH,
                                            size: 11,
                                            updated_at: '2026-01-15T09:00:00Z',
                                            user_id: 1,
                                        },
                                    ],
                                    total: 1,
                                },
                                status: 'success',
                            },
                        },
                    ],
                    'POST /api/v1/flows/5/files/to-resources': [
                        {
                            body: { data: { items: [], total: 0 }, status: 'success' },
                            bodySubset: { destination: PROMOTE_PATH, force: true, sources: [TABS_FILE_PATH] },
                        },
                    ],
                },
            }),
        });

        test('asks before overwriting, and only then sends force', async ({ page, pageErrorLog }) => {
            await openFilesTab(page);
            await openRowMenu(page, TABS_FILE_NAME);
            await page.getByRole('menuitem', { name: '保存为资源' }).click();

            const dialog = page.getByRole('dialog');
            let sent = 0;

            page.on('request', (candidate) => {
                if (new URL(candidate.url()).pathname === '/api/v1/flows/5/files/to-resources') {
                    sent += 1;
                }
            });

            await dialog.getByLabel('目标路径').fill(PROMOTE_PATH);
            await dialog.getByRole('button', { exact: true, name: '保存' }).click();

            await expect(page.getByRole('heading', { name: '替换已有项目？' })).toBeVisible();
            expect(sent, 'no write before the answer').toBe(0);

            const request = promoteRequest(page);

            await page.getByRole('button', { exact: true, name: '替换' }).click();

            expect((await request).postDataJSON()).toMatchObject({
                destination: PROMOTE_PATH,
                force: true,
                sources: [TABS_FILE_PATH],
            });
            expectCleanPage(pageErrorLog);
        });
    });

    test.describe('attach resources', () => {
        test.use({
            cassette: flowTabsCassette({
                rest: {
                    'GET /api/v1/resources/': [
                        {
                            body: {
                                data: {
                                    items: [
                                        {
                                            created_at: '2026-01-15T09:00:00Z',
                                            id: ATTACHED_RESOURCE.id,
                                            is_dir: false,
                                            name: ATTACHED_RESOURCE.name,
                                            path: ATTACHED_RESOURCE.name,
                                            size: 64,
                                            updated_at: '2026-01-15T09:00:00Z',
                                            user_id: 1,
                                        },
                                    ],
                                    total: 1,
                                },
                                status: 'success',
                            },
                        },
                    ],
                    'POST /api/v1/flows/5/files/resources': [
                        {
                            body: { data: { files: [] }, status: 'success' },
                            bodySubset: { force: false, ids: [ATTACHED_RESOURCE.id] },
                        },
                    ],
                },
            }),
        });

        test('copies the picked library entry into the flow', async ({ page, pageErrorLog }) => {
            await openFilesTab(page);
            await page.getByRole('button', { name: '添加资源' }).click();

            const dialog = page.getByRole('dialog');

            await expect(dialog.getByRole('heading', { name: '添加资源' })).toBeVisible();
            await dialog.getByRole('checkbox', { name: `选择 ${ATTACHED_RESOURCE.name}` }).click();

            const request = page.waitForRequest(
                (candidate) =>
                    candidate.method() === 'POST' &&
                    new URL(candidate.url()).pathname === '/api/v1/flows/5/files/resources',
            );

            await dialog.getByRole('button', { exact: true, name: '添加 1 项' }).click();

            expect((await request).postDataJSON()).toMatchObject({ force: false, ids: [ATTACHED_RESOURCE.id] });
            await expect(page.getByText('已复制 1 项到 /work/resources')).toBeVisible();
            expectCleanPage(pageErrorLog);
        });
    });

    test.describe('copy paths', () => {
        test.use({ cassette: flowTabsCassette() });

        test('puts the row path on the clipboard', async ({ page, pageErrorLog }) => {
            await recordClipboardWrites(page);
            await openFilesTab(page);
            await openRowMenu(page, TABS_FILE_NAME);
            await page.getByRole('menuitem', { name: '复制路径' }).click();

            await expect(page.getByText('路径已复制到剪贴板')).toBeVisible();
            expect(await clipboardWrites(page)).toEqual([TABS_FILE_PATH]);
            expectCleanPage(pageErrorLog);
        });

        test('puts every checked path on the clipboard, one per line', async ({ page, pageErrorLog }) => {
            await recordClipboardWrites(page);
            await openFilesTab(page);
            await page.getByRole('checkbox', { name: `选择 ${TABS_FILE_NAME}` }).click();
            await page.getByRole('checkbox', { name: `选择 ${TABS_RESOURCE_FILE_NAME}` }).click();
            await page.getByRole('button', { name: '更多操作' }).click();
            await page.getByRole('menuitem', { name: '复制路径' }).click();

            await expect(page.getByText('已复制 2 项路径到剪贴板')).toBeVisible();
            expect(await clipboardWrites(page)).toEqual([`${TABS_FILE_PATH}\n${TABS_RESOURCE_FILE_PATH}`]);
            expectCleanPage(pageErrorLog);
        });
    });

    test.describe('pull from container', () => {
        test.use({
            cassette: flowTabsCassette({
                rest: {
                    'GET /api/v1/flows/5/files/container': [
                        {
                            body: {
                                data: {
                                    files: [
                                        {
                                            id: 'c1',
                                            is_dir: false,
                                            modified_at: '2026-01-15T09:00:00Z',
                                            name: CONTAINER_FILE.name,
                                            path: CONTAINER_FILE.path,
                                            size: 128,
                                        },
                                    ],
                                    path: '/work',
                                    total: 1,
                                },
                                status: 'success',
                            },
                        },
                    ],
                    'POST /api/v1/flows/5/files/pull': [
                        {
                            body: { data: { files: [] }, status: 'success' },
                            bodySubset: { force: false, paths: [CONTAINER_FILE.path] },
                        },
                    ],
                },
            }),
        });

        test('lists the sandbox and pulls the checked entry into the cache', async ({ page, pageErrorLog }) => {
            await openFilesTab(page);

            const listing = page.waitForRequest(
                (candidate) =>
                    candidate.method() === 'GET' &&
                    new URL(candidate.url()).pathname === '/api/v1/flows/5/files/container',
            );

            await page.getByRole('button', { name: '从运行环境拉取' }).click();

            const dialog = page.getByRole('dialog');

            await expect(dialog.getByRole('heading', { name: '从运行环境拉取' })).toBeVisible();
            expect(new URL((await listing).url()).searchParams.getAll('paths[]')).toEqual(['/work']);

            await dialog.getByRole('checkbox', { name: `选择 ${CONTAINER_FILE.name}` }).click();

            const request = page.waitForRequest(
                (candidate) =>
                    candidate.method() === 'POST' && new URL(candidate.url()).pathname === '/api/v1/flows/5/files/pull',
            );

            await dialog.getByRole('button', { exact: true, name: '拉取 1 项' }).click();

            expect((await request).postDataJSON()).toMatchObject({ force: false, paths: [CONTAINER_FILE.path] });
            await expect(page.getByText('已保存到本地缓存的 container/ 下')).toBeVisible();
            expectCleanPage(pageErrorLog);
        });
    });
});
