import { fireEvent, render, screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';

import type { FileManagerBulkAction, FileNode } from './file-manager-types';

import { FileManager } from './file-manager';

const file = (path: string, overrides: Partial<FileNode> = {}): FileNode => ({
    id: path,
    isDir: false,
    name: path.split('/').pop() ?? path,
    path,
    size: 10,
    ...overrides,
});

const dir = (path: string): FileNode => file(path, { isDir: true, size: 0 });

const renderTree = (onSelect: (files: FileNode[]) => void, onMoveItems?: (sources: FileNode[], to: string) => void) => {
    const bulkActions: FileManagerBulkAction[] = [{ icon: undefined, id: 'delete', label: 'Delete', onSelect }];

    render(
        <FileManager
            bulkActions={bulkActions}
            files={[dir('docs'), file('docs/a.txt'), file('docs/b.txt'), dir('dest'), file('loose.txt')]}
            onMoveItems={onMoveItems}
        />,
    );

    fireEvent.click(screen.getByRole('button', { name: '全部展开' }));
};

const rowFor = (path: string): HTMLElement =>
    document.querySelector<HTMLElement>(`[role="treeitem"][data-path="${path}"]`)!;

const stubDataTransfer = () => {
    const store = new Map<string, string>();

    return {
        dropEffect: '',
        effectAllowed: '',
        files: [] as unknown as FileList,
        getData: (type: string) => store.get(type) ?? '',
        setData: (type: string, value: string) => void store.set(type, value),
        setDragImage: () => {},
        get types() {
            return [...store.keys()];
        },
    };
};

const barText = () => screen.getByText(/已选/).textContent;

describe('bulk actions on a partially selected directory', () => {
    it('hands the action the ticked descendants instead of the directory', () => {
        const onSelect = vi.fn();

        renderTree(onSelect);

        fireEvent.click(screen.getByRole('checkbox', { name: '选择 docs' }));
        fireEvent.click(screen.getByRole('checkbox', { name: '选择 a.txt' }));

        expect(barText()).toMatch(/^已选 1 项/);

        fireEvent.click(screen.getByRole('button', { name: 'Delete' }));

        expect(onSelect).toHaveBeenCalledTimes(1);
        expect(onSelect.mock.calls[0]![0].map((entry: FileNode) => entry.path)).toEqual(['docs/b.txt']);
    });

    it('lets the directory stand in for its content once every row is ticked', () => {
        const onSelect = vi.fn();

        renderTree(onSelect);

        fireEvent.click(screen.getByRole('checkbox', { name: '选择 docs' }));

        expect(barText()).toMatch(/^已选 3 项/);

        fireEvent.click(screen.getByRole('button', { name: 'Delete' }));

        expect(onSelect.mock.calls[0]![0].map((entry: FileNode) => entry.path)).toEqual(['docs']);
    });

    it('keeps the bar reachable and its actions inert once the whole content is unticked', () => {
        const onSelect = vi.fn();

        renderTree(onSelect);

        fireEvent.click(screen.getByRole('checkbox', { name: '选择 docs' }));
        fireEvent.click(screen.getByRole('checkbox', { name: '选择 a.txt' }));
        fireEvent.click(screen.getByRole('checkbox', { name: '选择 b.txt' }));

        expect(barText()).toMatch(/^已选 0 项/);
        expect(screen.getByRole('button', { name: '取消' })).toBeEnabled();
        expect(screen.getByRole('button', { name: 'Delete' })).toBeDisabled();
    });
});

describe('dragging a partially selected directory', () => {
    it('moves the grabbed folder whole rather than the descendants that stayed ticked', () => {
        const onMoveItems = vi.fn();

        renderTree(vi.fn(), onMoveItems);

        fireEvent.click(screen.getByRole('checkbox', { name: '选择 docs' }));
        fireEvent.click(screen.getByRole('checkbox', { name: '选择 a.txt' }));

        const dataTransfer = stubDataTransfer();

        fireEvent.dragStart(rowFor('docs'), { dataTransfer });
        fireEvent.dragEnter(rowFor('dest'), { dataTransfer });
        fireEvent.dragOver(rowFor('dest'), { dataTransfer });
        fireEvent.drop(rowFor('dest'), { dataTransfer });

        expect(onMoveItems).toHaveBeenCalledTimes(1);
        expect(onMoveItems.mock.calls[0]![0].map((entry: FileNode) => entry.path)).toEqual(['docs']);
        expect(onMoveItems.mock.calls[0]![1]).toBe('dest');
    });
});
