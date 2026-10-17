import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import { type ManagedUser, usersApi } from '@/features/users/users-api';

import SettingsUsers from './settings-users';

const user = (hash: string, mail: string, patch: Partial<ManagedUser> = {}): ManagedUser => ({
    created_at: '2026-10-01T00:00:00Z',
    hash,
    id: 1,
    mail,
    name: '',
    password_change_required: false,
    role: { id: 2, name: 'User' },
    role_id: 2,
    status: 'active',
    type: 'local',
    ...patch,
});

const admin = user('a'.repeat(32), 'admin@example.com', { role: { id: 1, name: 'Admin' }, role_id: 1 });
const sales = user('b'.repeat(32), 'sales@example.com');

vi.mock('@/components/layouts/app/app-header', () => ({
    AppHeader: ({ children }: { children: unknown }) => children,
    AppHeaderContent: ({ children }: { children: unknown }) => children,
    AppHeaderTitle: ({ children }: { children: unknown }) => children,
}));

vi.mock('@/providers/user-provider', () => ({
    useUser: () => ({
        authInfo: {
            privileges: ['users.view', 'users.create', 'users.edit', 'users.delete'],
            user: { hash: 'a'.repeat(32) },
        },
    }),
}));

vi.mock('@/features/users/users-api', async (importOriginal) => ({
    ...(await importOriginal<object>()),
    usersApi: {
        create: vi.fn(async (input: { mail: string }) => user('c'.repeat(32), input.mail)),
        delete: vi.fn(async () => {}),
        list: vi.fn(async () => [admin, sales]),
        resetPassword: vi.fn(async (target: ManagedUser) => target),
        setBlocked: vi.fn(async (target: ManagedUser) => ({ ...target, status: 'blocked' })),
    },
}));

describe('SettingsUsers', () => {
    beforeEach(() => vi.clearAllMocks());

    it('offers no actions on the signed-in user’s own row', async () => {
        render(<SettingsUsers />);

        const own = (await screen.findByText('admin@example.com')).closest('tr') as HTMLElement;
        const other = screen.getByText('sales@example.com').closest('tr') as HTMLElement;

        expect(within(own).queryByRole('button')).not.toBeInTheDocument();
        expect(within(other).getByRole('button', { name: '停用' })).toBeInTheDocument();
    });

    it('disables a user and shows the new status', async () => {
        render(<SettingsUsers />);

        fireEvent.click(await screen.findByRole('button', { name: '停用' }));

        await waitFor(() => expect(usersApi.setBlocked).toHaveBeenCalledWith(sales, true));
        expect(await screen.findByText('已停用')).toBeInTheDocument();
    });

    it('deletes a user only after the confirmation', async () => {
        render(<SettingsUsers />);

        fireEvent.click(await screen.findByRole('button', { name: '删除' }));

        expect(usersApi.delete).not.toHaveBeenCalled();

        const dialog = await screen.findByRole('dialog');
        fireEvent.click(within(dialog).getByRole('button', { name: /删除/ }));

        await waitFor(() => expect(usersApi.delete).toHaveBeenCalledWith(sales));
        await waitFor(() => expect(screen.queryByText(sales.mail)).not.toBeInTheDocument());
    });

    it('refuses a short password that misses a character class', async () => {
        render(<SettingsUsers />);

        fireEvent.click(await screen.findByRole('button', { name: '新建用户' }));
        fireEvent.change(screen.getByLabelText('邮箱（登录用）'), { target: { value: 'new@example.com' } });
        fireEvent.change(screen.getByLabelText('初始密码'), { target: { value: 'abcdefgh1' } });
        fireEvent.click(screen.getByRole('button', { name: '创建' }));

        expect(
            await screen.findByText(
                '密码需要超过 15 个字符，或至少 8 个字符并同时包含数字、小写字母、大写字母和特殊字符（!@#$&*）',
            ),
        ).toBeInTheDocument();
        expect(usersApi.create).not.toHaveBeenCalled();
    });

    it('creates a user with a policy-passing password', async () => {
        render(<SettingsUsers />);

        fireEvent.click(await screen.findByRole('button', { name: '新建用户' }));
        fireEvent.change(screen.getByLabelText('邮箱（登录用）'), { target: { value: ' new@example.com ' } });
        fireEvent.change(screen.getByLabelText('名字'), { target: { value: 'New' } });
        fireEvent.change(screen.getByLabelText('初始密码'), { target: { value: 'Abcdef1!' } });
        fireEvent.click(screen.getByRole('button', { name: '创建' }));

        await waitFor(() =>
            expect(usersApi.create).toHaveBeenCalledWith({
                mail: 'new@example.com',
                name: 'New',
                password: 'Abcdef1!',
                role_id: 2,
            }),
        );
        expect(await screen.findByText('new@example.com')).toBeInTheDocument();
    });
});
