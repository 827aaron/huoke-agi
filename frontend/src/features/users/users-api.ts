import type { User } from '@/models/user';

import { api, unwrapApiResponse } from '@/lib/axios';

// Roles seeded by the first migration; only an administrator holds users.* privileges.
export const ROLE_ADMIN = 1;
export const ROLE_USER = 2;

export interface ManagedUser extends User {
    role: { id: number; name: string };
}

export interface NewUserInput {
    mail: string;
    name: string;
    password: string;
    role_id: number;
}

// The server takes the whole user on update but changes only its name, status and, when given, password.
const updateBody = (user: ManagedUser, patch: Partial<Pick<User, 'name' | 'status'>> & { password?: string }) => ({
    hash: user.hash,
    mail: user.mail,
    name: patch.name ?? user.name,
    password: patch.password ?? '',
    role_id: user.role_id,
    status: patch.status ?? user.status,
    type: user.type,
});

export const usersApi = {
    // Created active and asked to change the password at first sign-in; a `created` user cannot sign in.
    create: async (input: NewUserInput) =>
        unwrapApiResponse(
            await api.post<ManagedUser>('/users/', {
                ...input,
                password_change_required: true,
                status: 'active',
                type: 'local',
            }),
        ),
    delete: async (user: ManagedUser) => {
        await api.delete(`/users/${user.hash}`);
    },
    list: async () =>
        unwrapApiResponse(
            await api.get<{ total: number; users: ManagedUser[] }>('/users/', {
                params: { page: 1, pageSize: -1, type: 'init' },
            }),
        ).users,
    resetPassword: async (user: ManagedUser, password: string) =>
        unwrapApiResponse(await api.put<ManagedUser>(`/users/${user.hash}`, updateBody(user, { password }))),
    setBlocked: async (user: ManagedUser, isBlocked: boolean) =>
        unwrapApiResponse(
            await api.put<ManagedUser>(
                `/users/${user.hash}`,
                updateBody(user, { status: isBlocked ? 'blocked' : 'active' }),
            ),
        ),
};
