import { CalendarClock, Plus } from 'lucide-react';
import { useState } from 'react';
import { toast } from 'sonner';

import { AppHeaderAction } from '@/components/layouts/app/app-header';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle } from '@/components/ui/sheet';
import { Spinner } from '@/components/ui/spinner';
import { Switch } from '@/components/ui/switch';
import { Textarea } from '@/components/ui/textarea';
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group';
import { getApiErrorMessage } from '@/lib/axios';
import { copy } from '@/lib/copy';

import type { GrowthSchedule, GrowthScheduleDays, GrowthScheduleInput } from '../growth-types';

import { growthApi } from '../growth-api';
import { useGrowthFlow } from '../growth-flow-provider';
import { GrowthProfileSelect } from '../profiles/growth-profile-select';
import { useGrowthPoll } from '../use-growth-poll';

const text = copy.schedules;

const DAYS: GrowthScheduleDays[] = ['daily', 'weekdays', 'weekly', 'once'];

// Monday first, as a week reads; the wire numbers Sunday 0.
const WEEK_ORDER = [1, 2, 3, 4, 5, 6, 0];

// The server takes HH:MM only; a time input whose step allows seconds reports HH:MM:SS.
const toClock = (value: string): string => value.slice(0, 5);

const browserTimeZone = (): string => Intl.DateTimeFormat().resolvedOptions().timeZone || 'UTC';

interface ScheduleDraft {
    date: string;
    days: GrowthScheduleDays;
    end: string;
    input: string;
    // Empty keeps what the schedule has, or takes the task's profile for a new one.
    profileId: string;
    start: string;
    weekdays: string[];
}

const EMPTY_DRAFT: ScheduleDraft = {
    date: '',
    days: 'daily',
    end: '18:00',
    input: '',
    profileId: '',
    start: '09:00',
    weekdays: [],
};

const toDraft = (schedule: GrowthSchedule): ScheduleDraft => ({
    date: schedule.date ?? '',
    days: schedule.days,
    end: schedule.end_time,
    input: schedule.input,
    profileId: schedule.profile_id ? String(schedule.profile_id) : '',
    start: schedule.start_time,
    weekdays: (schedule.weekdays ?? []).map(String),
});

export const validateScheduleDraft = (draft: ScheduleDraft): null | string => {
    if (!draft.start || !draft.end) {
        return text.timeRequired;
    }

    if (toClock(draft.start) === toClock(draft.end)) {
        return text.sameTime;
    }

    if (draft.days === 'weekly' && !draft.weekdays.length) {
        return text.weekdaysRequired;
    }

    if (draft.days === 'once' && !draft.date) {
        return text.dateRequired;
    }

    return null;
};

export const scheduleSummary = (
    schedule: Pick<GrowthSchedule, 'date' | 'days' | 'end_time' | 'start_time' | 'weekdays'>,
) => {
    const days =
        schedule.days === 'weekly'
            ? text.weekdayList(
                  WEEK_ORDER.filter((day) => schedule.weekdays?.includes(day)).map((day) => text.weekday[day] ?? ''),
              )
            : schedule.days === 'once'
              ? text.onceOn(schedule.date ?? '')
              : (text.days[schedule.days] ?? schedule.days);

    return `${days} ${schedule.start_time}–${schedule.end_time}`;
};

const formatInZone = (iso: string, timeZone: string): string =>
    new Intl.DateTimeFormat(text.locale, {
        day: 'numeric',
        hour: '2-digit',
        hour12: false,
        minute: '2-digit',
        month: 'numeric',
        timeZone,
    }).format(new Date(iso));

interface ScheduleFormProps {
    flowId: number;
    onDone: (saved: boolean) => void;
    schedule: GrowthSchedule | null;
}

// The header button of a flow page; the list is fetched only once the sheet opens.
export function FlowScheduleButton({ flowId }: { flowId: number }) {
    const [isOpen, setIsOpen] = useState(false);

    return (
        <>
            <AppHeaderAction
                className="shrink-0"
                icon={<CalendarClock />}
                label={text.button}
                onClick={() => setIsOpen(true)}
                variant="ghost"
            />
            <Sheet
                onOpenChange={setIsOpen}
                open={isOpen}
            >
                <SheetContent className="flex w-full flex-col gap-0 overflow-y-auto sm:max-w-md">
                    <SheetHeader>
                        <SheetTitle>{text.title}</SheetTitle>
                        <SheetDescription>{text.description}</SheetDescription>
                    </SheetHeader>
                    <div className="px-4 pb-6">{isOpen && <FlowSchedules flowId={flowId} />}</div>
                </SheetContent>
            </Sheet>
        </>
    );
}

export function FlowSchedules({ flowId }: { flowId: number }) {
    const { data, error, isLoading, refetch } = useGrowthPoll(
        `schedules:${flowId}`,
        () => growthApi.listSchedules(flowId),
        null,
    );
    const [editing, setEditing] = useState<'new' | GrowthSchedule | null>(null);
    const [busyId, setBusyId] = useState<null | number>(null);

    const act = async (id: number, action: () => Promise<unknown>, success?: string) => {
        setBusyId(id);

        try {
            await action();

            if (success) {
                toast.success(success);
            }

            await refetch();
        } catch (actionError) {
            toast.error(getApiErrorMessage(actionError, copy.common.saveFailed));
        } finally {
            setBusyId(null);
        }
    };

    const toggle = (schedule: GrowthSchedule, enabled: boolean) =>
        act(schedule.id, () =>
            growthApi.updateSchedule(schedule.id, {
                ...(schedule.date && { date: schedule.date }),
                days: schedule.days,
                enabled,
                end_time: schedule.end_time,
                flow_id: schedule.flow_id,
                start_time: schedule.start_time,
                timezone: schedule.timezone,
                ...(schedule.weekdays && { weekdays: schedule.weekdays }),
            }),
        );

    if (isLoading) {
        return (
            <div className="flex justify-center py-6">
                <Spinner variant="circle" />
            </div>
        );
    }

    if (error && !data) {
        return <p className="text-destructive text-sm">{text.loadFailed}</p>;
    }

    const schedules = data ?? [];

    return (
        <div className="flex flex-col gap-3">
            {!schedules.length && editing === null && <p className="text-muted-foreground text-sm">{text.empty}</p>}

            {schedules.map((schedule) =>
                editing !== 'new' && editing?.id === schedule.id ? (
                    <ScheduleForm
                        flowId={flowId}
                        key={schedule.id}
                        onDone={(saved) => {
                            setEditing(null);

                            if (saved) {
                                void refetch();
                            }
                        }}
                        schedule={schedule}
                    />
                ) : (
                    <div
                        className="flex flex-col gap-1 rounded-md border p-3 text-sm"
                        data-slot="schedule-item"
                        key={schedule.id}
                    >
                        <div className="flex items-center justify-between gap-2">
                            <span className="font-medium">{scheduleSummary(schedule)}</span>
                            <Switch
                                aria-label={text.enabled}
                                checked={schedule.enabled}
                                disabled={busyId === schedule.id}
                                onCheckedChange={(enabled) => toggle(schedule, enabled)}
                            />
                        </div>
                        <p className="text-muted-foreground text-xs">
                            {schedule.enabled && schedule.next_start_at
                                ? text.nextStart(formatInZone(schedule.next_start_at, schedule.timezone))
                                : !schedule.enabled
                                  ? text.paused
                                  : text.timezone(schedule.timezone)}
                        </p>
                        {schedule.input && (
                            <p className="text-muted-foreground line-clamp-2 text-xs">{schedule.input}</p>
                        )}
                        {schedule.last_run && (
                            <p className="text-muted-foreground text-xs">
                                {text.lastRun(schedule.last_run.delivered_count, schedule.last_run.target)}
                            </p>
                        )}
                        {schedule.last_error && (
                            <p className="text-destructive text-xs">{text.lastError(schedule.last_error)}</p>
                        )}
                        <div className="flex gap-1">
                            <Button
                                className="h-auto p-0"
                                disabled={busyId === schedule.id}
                                onClick={() => setEditing(schedule)}
                                size="sm"
                                variant="link"
                            >
                                {text.edit}
                            </Button>
                            <Button
                                className="text-destructive ml-3 h-auto p-0"
                                disabled={busyId === schedule.id}
                                onClick={() =>
                                    act(schedule.id, () => growthApi.deleteSchedule(schedule.id), text.deleted)
                                }
                                size="sm"
                                variant="link"
                            >
                                {text.delete}
                            </Button>
                        </div>
                    </div>
                ),
            )}

            {editing === 'new' ? (
                <ScheduleForm
                    flowId={flowId}
                    onDone={(saved) => {
                        setEditing(null);

                        if (saved) {
                            void refetch();
                        }
                    }}
                    schedule={null}
                />
            ) : (
                <Button
                    className="self-start"
                    onClick={() => setEditing('new')}
                    size="sm"
                    variant="outline"
                >
                    <Plus />
                    {text.add}
                </Button>
            )}
        </div>
    );
}

function ScheduleForm({ flowId, onDone, schedule }: ScheduleFormProps) {
    const { flowProfile } = useGrowthFlow();
    const [draft, setDraft] = useState<ScheduleDraft>(() =>
        schedule
            ? toDraft(schedule)
            : { ...EMPTY_DRAFT, profileId: flowProfile?.profile_id ? String(flowProfile.profile_id) : '' },
    );
    const [error, setError] = useState<null | string>(null);
    const [isSaving, setIsSaving] = useState(false);

    const timezone = schedule?.timezone ?? browserTimeZone();
    const update = (patch: Partial<ScheduleDraft>) => setDraft((previous) => ({ ...previous, ...patch }));

    const handleSave = async () => {
        const invalid = validateScheduleDraft(draft);

        setError(invalid);

        if (invalid) {
            return;
        }

        const input: GrowthScheduleInput = {
            days: draft.days,
            end_time: toClock(draft.end),
            flow_id: flowId,
            input: draft.input.trim(),
            ...(draft.profileId && { profile_id: Number(draft.profileId) }),
            start_time: toClock(draft.start),
            timezone,
            ...(draft.days === 'weekly' && { weekdays: draft.weekdays.map(Number).sort((a, b) => a - b) }),
            ...(draft.days === 'once' && { date: draft.date }),
        };

        setIsSaving(true);

        try {
            await (schedule ? growthApi.updateSchedule(schedule.id, input) : growthApi.createSchedule(input));
            toast.success(text.saved);
            onDone(true);
        } catch (saveError) {
            setError(getApiErrorMessage(saveError, copy.common.saveFailed));
        } finally {
            setIsSaving(false);
        }
    };

    return (
        <div className="flex flex-col gap-4 rounded-md border p-3">
            <div className="flex flex-col gap-1.5">
                <Label htmlFor="schedule-days">{text.daysLabel}</Label>
                <Select
                    onValueChange={(value) => update({ days: value as GrowthScheduleDays })}
                    value={draft.days}
                >
                    <SelectTrigger
                        className="w-full"
                        id="schedule-days"
                    >
                        <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                        {DAYS.map((days) => (
                            <SelectItem
                                key={days}
                                value={days}
                            >
                                {text.days[days]}
                            </SelectItem>
                        ))}
                    </SelectContent>
                </Select>
            </div>

            {draft.days === 'weekly' && (
                <ToggleGroup
                    className="flex-wrap justify-start"
                    onValueChange={(weekdays: string[]) => update({ weekdays })}
                    type="multiple"
                    value={draft.weekdays}
                    variant="outline"
                >
                    {WEEK_ORDER.map((day) => (
                        <ToggleGroupItem
                            className="data-[state=on]:bg-primary data-[state=on]:text-primary-foreground px-3"
                            key={day}
                            value={String(day)}
                        >
                            {text.weekday[day]}
                        </ToggleGroupItem>
                    ))}
                </ToggleGroup>
            )}

            {draft.days === 'once' && (
                <div className="flex flex-col gap-1.5">
                    <Label htmlFor="schedule-date">{text.date}</Label>
                    <Input
                        className="w-44"
                        id="schedule-date"
                        onChange={(event) => update({ date: event.target.value })}
                        type="date"
                        value={draft.date}
                    />
                </div>
            )}

            <div className="flex flex-wrap gap-3">
                <div className="flex flex-col gap-1.5">
                    <Label htmlFor="schedule-start">{text.start}</Label>
                    <Input
                        className="w-32"
                        id="schedule-start"
                        onChange={(event) => update({ start: event.target.value })}
                        type="time"
                        value={draft.start}
                    />
                </div>
                <div className="flex flex-col gap-1.5">
                    <Label htmlFor="schedule-end">{text.end}</Label>
                    <Input
                        className="w-32"
                        id="schedule-end"
                        onChange={(event) => update({ end: event.target.value })}
                        type="time"
                        value={draft.end}
                    />
                </div>
            </div>
            <p className="text-muted-foreground text-xs">
                {text.timezone(timezone)}
                {draft.start && draft.end && draft.end < draft.start && ` · ${text.overnight}`}
            </p>

            <GrowthProfileSelect
                id="schedule-profile"
                onChange={(profileId) => update({ profileId })}
                value={draft.profileId}
            />

            <div className="flex flex-col gap-1.5">
                <Label htmlFor="schedule-input">{text.input}</Label>
                <Textarea
                    id="schedule-input"
                    onChange={(event) => update({ input: event.target.value })}
                    placeholder={text.inputPlaceholder}
                    rows={2}
                    value={draft.input}
                />
            </div>

            {error && <p className="text-destructive text-sm">{error}</p>}

            <div className="flex gap-2">
                <Button
                    disabled={isSaving}
                    onClick={handleSave}
                    size="sm"
                >
                    {isSaving && <Spinner variant="circle" />}
                    {text.save}
                </Button>
                <Button
                    disabled={isSaving}
                    onClick={() => onDone(false)}
                    size="sm"
                    variant="ghost"
                >
                    {text.cancel}
                </Button>
            </div>
        </div>
    );
}
