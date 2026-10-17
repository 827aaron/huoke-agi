import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import type { GrowthLead } from '../growth-types';

import { growthApi } from '../growth-api';
import LeadFormDialog from './lead-form-dialog';

vi.mock('../growth-api', () => ({
    growthApi: { createLead: vi.fn(async () => ({})), updateLead: vi.fn(async () => ({})) },
}));

const jay = {
    contacts: [
        { type: 'line', value: 'jay88' },
        { type: 'phone', value: '+60 12 345 6789' },
    ],
    display_name: 'Jay',
    id: 21,
    note: '',
    tier: 'watch',
} as GrowthLead;

const renderForm = (lead?: GrowthLead) => {
    const onClose = vi.fn();
    const onSaved = vi.fn(async () => {});

    render(
        <LeadFormDialog
            flowId={5}
            lead={lead}
            onClose={onClose}
            onSaved={onSaved}
        />,
    );

    return { onClose, onSaved };
};

describe('LeadFormDialog', () => {
    beforeEach(() => vi.clearAllMocks());

    it('adds a prospect by hand with its contacts and note', async () => {
        const { onClose, onSaved } = renderForm();
        const save = screen.getByRole('button', { name: '保存' });

        expect(save).toBeDisabled();

        fireEvent.change(screen.getByLabelText('平台账号'), { target: { value: ' @mina_th ' } });
        fireEvent.change(screen.getByLabelText('名称（可选）'), { target: { value: 'Mina' } });
        fireEvent.click(screen.getByRole('button', { name: '添加联系方式' }));
        fireEvent.change(screen.getByLabelText('号码或账号 1'), { target: { value: ' 0812345678 ' } });
        fireEvent.click(screen.getByRole('button', { name: '添加联系方式' }));
        fireEvent.change(screen.getByLabelText('备注'), { target: { value: 'met at the expo' } });
        fireEvent.click(save);

        await waitFor(() => expect(onClose).toHaveBeenCalledTimes(1));
        expect(onSaved).toHaveBeenCalledTimes(1);
        expect(growthApi.createLead).toHaveBeenCalledWith({
            contacts: [{ type: 'phone', value: '0812345678' }],
            display_name: 'Mina',
            flow_id: 5,
            handle: '@mina_th',
            note: 'met at the expo',
            platform: 'telegram',
            tier: 'qualified',
        });
    });

    it('edits the contacts and note of a found prospect and keeps its tier', async () => {
        const { onClose } = renderForm(jay);

        expect(screen.queryByLabelText('平台账号')).not.toBeInTheDocument();

        fireEvent.click(screen.getByRole('button', { name: '删除这条联系方式 1' }));
        fireEvent.change(screen.getByLabelText('备注'), { target: { value: 'prefers phone' } });
        fireEvent.click(screen.getByRole('button', { name: '保存' }));

        await waitFor(() => expect(onClose).toHaveBeenCalledTimes(1));
        expect(growthApi.updateLead).toHaveBeenCalledWith(21, {
            contacts: [{ type: 'phone', value: '+60 12 345 6789' }],
            note: 'prefers phone',
            tier: 'watch',
        });
        expect(growthApi.createLead).not.toHaveBeenCalled();
    });
});
