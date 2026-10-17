import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import type { GrowthSchedule } from '../growth-types';

import { growthApi } from '../growth-api';
import { FlowSchedules, scheduleSummary } from './flow-schedules';

const weekly: GrowthSchedule = {
    date: null,
    days: 'weekly',
    enabled: true,
    end_time: '02:00',
    flow_id: 5,
    id: 9,
    input: '找 50 个泰国的 MOBA 玩家',
    last_error: null,
    last_run: { deadline_at: null, delivered_count: 32, run_id: 3, status: 'delivered', target: 50 },
    next_start_at: '2026-10-19T13:00:00Z',
    start_time: '20:00',
    timezone: 'Asia/Bangkok',
    weekdays: [5, 1],
};

vi.mock('../growth-api', () => ({
    growthApi: {
        createSchedule: vi.fn(async () => weekly),
        deleteSchedule: vi.fn(async () => {}),
        listSchedules: vi.fn(async () => [weekly]),
        updateSchedule: vi.fn(async () => weekly),
    },
}));

describe('FlowSchedules', () => {
    beforeEach(() => {
        vi.clearAllMocks();
        vi.spyOn(Intl.DateTimeFormat.prototype, 'resolvedOptions').mockReturnValue({
            ...new Intl.DateTimeFormat('en-US', { timeZone: 'UTC' }).resolvedOptions(),
            timeZone: 'Asia/Riyadh',
        });
    });

    afterEach(() => vi.restoreAllMocks());

    it('lists a schedule with its days, next start in its own zone and last delivery', async () => {
        render(<FlowSchedules flowId={5} />);

        expect(await screen.findByText('每周一、周五 20:00–02:00')).toBeInTheDocument();
        expect(screen.getByText('下次开始：10/19 20:00')).toBeInTheDocument();
        expect(screen.getByText('上次交付 32 人（目标 50）')).toBeInTheDocument();
    });

    it('saves edited days in order and keeps the schedule’s own zone', async () => {
        render(<FlowSchedules flowId={5} />);

        fireEvent.click(await screen.findByRole('button', { name: '修改' }));
        fireEvent.click(screen.getByRole('button', { name: '周三' }));

        expect(screen.getByText('按 Asia/Bangkok 时间 · 结束时间早于开始时间，会在第二天结束')).toBeInTheDocument();

        fireEvent.click(screen.getByRole('button', { name: '保存' }));

        await waitFor(() =>
            expect(growthApi.updateSchedule).toHaveBeenCalledWith(9, {
                days: 'weekly',
                end_time: '02:00',
                flow_id: 5,
                input: '找 50 个泰国的 MOBA 玩家',
                start_time: '20:00',
                timezone: 'Asia/Bangkok',
                weekdays: [1, 3, 5],
            }),
        );
    });

    it('creates a daily schedule in the browser’s zone', async () => {
        render(<FlowSchedules flowId={5} />);

        fireEvent.click(await screen.findByRole('button', { name: '添加定时' }));
        fireEvent.click(screen.getByRole('button', { name: '保存' }));

        await waitFor(() =>
            expect(growthApi.createSchedule).toHaveBeenCalledWith({
                days: 'daily',
                end_time: '18:00',
                flow_id: 5,
                input: '',
                start_time: '09:00',
                timezone: 'Asia/Riyadh',
            }),
        );
    });

    it('sends times with seconds as HH:MM', async () => {
        render(<FlowSchedules flowId={5} />);

        fireEvent.click(await screen.findByRole('button', { name: '添加定时' }));
        fireEvent.change(screen.getByLabelText('开始时间'), { target: { value: '09:30:00' } });
        fireEvent.change(screen.getByLabelText('结束时间'), { target: { value: '17:45:00' } });
        fireEvent.click(screen.getByRole('button', { name: '保存' }));

        await waitFor(() =>
            expect(growthApi.createSchedule).toHaveBeenCalledWith(
                expect.objectContaining({ end_time: '17:45', start_time: '09:30' }),
            ),
        );
    });

    it('refuses equal start and end times', async () => {
        render(<FlowSchedules flowId={5} />);

        fireEvent.click(await screen.findByRole('button', { name: '添加定时' }));
        fireEvent.change(screen.getByLabelText('结束时间'), { target: { value: '09:00' } });
        fireEvent.click(screen.getByRole('button', { name: '保存' }));

        expect(screen.getByText('开始和结束时间不能相同')).toBeInTheDocument();
        expect(growthApi.createSchedule).not.toHaveBeenCalled();
    });

    it('pauses a schedule without resending its instructions', async () => {
        render(<FlowSchedules flowId={5} />);

        fireEvent.click(await screen.findByRole('switch', { name: '启用' }));

        await waitFor(() =>
            expect(growthApi.updateSchedule).toHaveBeenCalledWith(9, {
                days: 'weekly',
                enabled: false,
                end_time: '02:00',
                flow_id: 5,
                start_time: '20:00',
                timezone: 'Asia/Bangkok',
                weekdays: [5, 1],
            }),
        );
    });
});

describe('scheduleSummary', () => {
    it('names a one-off date', () => {
        expect(
            scheduleSummary({
                date: '2026-11-01',
                days: 'once',
                end_time: '18:00',
                start_time: '09:00',
                weekdays: null,
            }),
        ).toBe('2026-11-01 一次 09:00–18:00');
    });
});
