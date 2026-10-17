import { beforeEach, describe, expect, it, vi } from 'vitest';

import { api } from '@/lib/axios';

import { type ManagedUser, usersApi } from './users-api';

vi.mock('@/lib/axios', () => ({
    api: {
        post: vi.fn(async () => ({ data: {}, status: 'success' })),
        put: vi.fn(async () => ({ data: {}, status: 'success' })),
    },
    unwrapApiResponse: (response: { data: unknown }) => response.data,
}));

const sales: ManagedUser = {
    created_at: '2026-10-01T00:00:00Z',
    hash: 'b'.repeat(32),
    id: 4,
    mail: 'sales@example.com',
    name: 'Sales',
    password_change_required: false,
    role: { id: 2, name: 'User' },
    role_id: 2,
    status: 'active',
    type: 'local',
};

describe('usersApi', () => {
    beforeEach(() => vi.clearAllMocks());

    it('creates an active local user who must change the password at first sign-in', async () => {
        await usersApi.create({ mail: 'new@example.com', name: 'New', password: 'Abcdef1!', role_id: 1 });

        expect(api.post).toHaveBeenCalledWith('/users/', {
            mail: 'new@example.com',
            name: 'New',
            password: 'Abcdef1!',
            password_change_required: true,
            role_id: 1,
            status: 'active',
            type: 'local',
        });
    });

    it('sends the whole user with the new status and no password when disabling', async () => {
        await usersApi.setBlocked(sales, true);

        expect(api.put).toHaveBeenCalledWith(`/users/${'b'.repeat(32)}`, {
            hash: 'b'.repeat(32),
            mail: 'sales@example.com',
            name: 'Sales',
            password: '',
            role_id: 2,
            status: 'blocked',
            type: 'local',
        });
    });
});
