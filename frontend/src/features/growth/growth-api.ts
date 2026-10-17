import { api, unwrapApiResponse } from '@/lib/axios';

import type {
    GrowthAccount,
    GrowthAccountsResponse,
    GrowthEmailResult,
    GrowthJoinWarning,
    GrowthLead,
    GrowthModelsInput,
    GrowthModelsResponse,
    GrowthProfile,
    GrowthProfileInput,
    GrowthProfilePreset,
    GrowthProxyTestResult,
    GrowthRemoteLogin,
    GrowthRun,
    GrowthSchedule,
    GrowthScheduleInput,
    GrowthSchedulesResponse,
    GrowthSetting,
    GrowthSettingInput,
    GrowthSettingKind,
    GrowthSettingsResponse,
    GrowthSourcesAddResponse,
    GrowthSourcesResponse,
    GrowthTelegramLoginStep,
} from './growth-types';

import { growthMock } from './growth-mock';
import {
    fromWireLeads,
    fromWireRuns,
    fromWireSource,
    fromWireSourceAddResult,
    type WireLead,
    type WireRun,
    type WireSource,
    type WireSourceAddResult,
} from './growth-wire';

const isMock = import.meta.env.VITE_GROWTH_API
    ? import.meta.env.VITE_GROWTH_API === 'mock'
    : import.meta.env.DEV && import.meta.env.MODE !== 'test';

export interface RemoteLoginInput extends StartLoginInput {
    external_browser_ws: null | string;
    platform: 'facebook' | 'instagram' | 'tiktok';
}

// `account_id` signs an existing account in again; without it a new account is added.
export interface StartLoginInput {
    account_id?: number;
    proxy: null | string;
}

const getWireLeads = async (flowId: number | string) =>
    unwrapApiResponse(await api.get<{ leads: null | WireLead[] }>('/growth/leads/', { params: { flow_id: flowId } }))
        .leads ?? [];

const getWireRuns = async (flowId: number | string) =>
    unwrapApiResponse(await api.get<{ runs: null | WireRun[] }>('/growth/runs/', { params: { flow_id: flowId } }))
        .runs ?? [];

const getWireSources = async () =>
    unwrapApiResponse(
        await api.get<{ sources: null | WireSource[]; total: number; warnings: GrowthJoinWarning[] | null }>(
            '/growth/sources/',
        ),
    );

const live = {
    addSources: async (links: string[]): Promise<GrowthSourcesAddResponse> => ({
        results: unwrapApiResponse(
            await api.post<{ results: WireSourceAddResult[] }>('/growth/sources/', { links }),
        ).results.map(fromWireSourceAddResult),
    }),
    // Leaves the account as it was (`needs_login` for a re-login, nothing for a new one).
    cancelRemoteLogin: async (sessionId: string) => {
        await api.delete(`/growth/accounts/remote-login/${sessionId}`);
    },
    clearSetting: async (kind: GrowthSettingKind) => {
        await api.delete(`/growth/settings/${kind}`);
    },
    createProfile: async (input: GrowthProfileInput) =>
        unwrapApiResponse(await api.post<GrowthProfile>('/growth/profiles', input)),
    createSchedule: async (input: GrowthScheduleInput) =>
        unwrapApiResponse(await api.post<GrowthSchedule>('/growth/schedules', input)),
    deleteSchedule: async (id: number) => {
        await api.delete(`/growth/schedules/${id}`);
    },
    finishRemoteLogin: async (sessionId: string) =>
        unwrapApiResponse(await api.post<GrowthAccount>(`/growth/accounts/remote-login/${sessionId}/finish`)),
    listAccounts: async () => unwrapApiResponse(await api.get<GrowthAccountsResponse>('/growth/accounts/')).accounts,
    listLeads: async (flowId: number | string): Promise<GrowthLead[]> => {
        const [leads, runs, sources] = await Promise.all([getWireLeads(flowId), getWireRuns(flowId), getWireSources()]);

        return fromWireLeads(leads, fromWireRuns(runs, leads), runs, (sources.sources ?? []).map(fromWireSource));
    },
    // Answers 502 Growth.ModelList when the relay does not list its models; the model is then typed by name.
    listModels: async (kind: GrowthSettingKind, input: GrowthModelsInput) =>
        unwrapApiResponse(await api.post<GrowthModelsResponse>(`/growth/settings/${kind}/models`, input)).models,
    listProfilePresets: async () => unwrapApiResponse(await api.get<GrowthProfilePreset[]>('/growth/profiles/presets')),
    listProfiles: async () => unwrapApiResponse(await api.get<GrowthProfile[]>('/growth/profiles')),
    listRuns: async (flowId: number | string): Promise<GrowthRun[]> => {
        const [runs, leads] = await Promise.all([getWireRuns(flowId), getWireLeads(flowId)]);

        return fromWireRuns(runs, leads);
    },
    listSchedules: async (flowId: number | string) =>
        unwrapApiResponse(await api.get<GrowthSchedulesResponse>('/growth/schedules', { params: { flow_id: flowId } }))
            .schedules,
    listSettings: async () => unwrapApiResponse(await api.get<GrowthSettingsResponse>('/growth/settings/')).settings,
    listSources: async (): Promise<GrowthSourcesResponse> => {
        const { sources, total, warnings } = await getWireSources();

        return { sources: (sources ?? []).map(fromWireSource), total, warnings };
    },
    // Answers 503 Growth.NoSecretKey while the server has no key to encrypt secrets with.
    saveSetting: async (kind: GrowthSettingKind, input: GrowthSettingInput) =>
        unwrapApiResponse(await api.put<GrowthSetting>(`/growth/settings/${kind}`, input)),
    // Mails the lead list of one round to the notification addresses, the same email a finished round sends.
    sendLeadsEmail: async (flowId: number | string, taskId: number) =>
        unwrapApiResponse(
            await api.post<GrowthEmailResult>('/growth/leads/email', { flow_id: Number(flowId), task_id: taskId }),
        ),
    // Re-enabling returns the account to `needs_login` or `active`, whichever its session allows.
    setAccountDisabled: async (id: number, isDisabled: boolean) =>
        unwrapApiResponse(
            await api.put<GrowthAccount>(`/growth/accounts/${id}`, { status: isDisabled ? 'disabled' : 'active' }),
        ),
    // An empty string clears the account's own proxy, so it falls back to the default one.
    setAccountProxy: async (id: number, proxyUrl: null | string) =>
        unwrapApiResponse(await api.put<GrowthAccount>(`/growth/accounts/${id}`, { proxy: proxyUrl ?? '' })),
    startRemoteLogin: async (input: RemoteLoginInput) =>
        unwrapApiResponse(await api.post<GrowthRemoteLogin>('/growth/accounts/remote-login', input)),
    startTelegramLogin: async (input: StartLoginInput & { phone: string }) =>
        unwrapApiResponse(await api.post<GrowthTelegramLoginStep>('/growth/accounts/telegram/login', input)),
    submitTelegramCode: async (loginId: string, code: string) =>
        unwrapApiResponse(
            await api.post<GrowthTelegramLoginStep>(`/growth/accounts/telegram/login/${loginId}/code`, { code }),
        ),
    submitTelegramPassword: async (loginId: string, password: string) =>
        unwrapApiResponse(
            await api.post<GrowthTelegramLoginStep>(`/growth/accounts/telegram/login/${loginId}/password`, {
                password,
            }),
        ),
    testAccountProxy: async (id: number) =>
        unwrapApiResponse(await api.post<GrowthProxyTestResult>(`/growth/accounts/${id}/test-proxy`)),
    // Without `to` the server mails the notification addresses.
    testEmail: async (language: string) =>
        unwrapApiResponse(await api.post<GrowthEmailResult>('/growth/settings/test-email', { language })),
    testSetting: async (kind: GrowthSettingKind) =>
        unwrapApiResponse(await api.post<GrowthSetting>(`/growth/settings/${kind}/test`)),
    updateProfile: async (id: number, input: GrowthProfileInput) =>
        unwrapApiResponse(await api.put<GrowthProfile>(`/growth/profiles/${id}`, input)),
    updateSchedule: async (id: number, input: GrowthScheduleInput) =>
        unwrapApiResponse(await api.put<GrowthSchedule>(`/growth/schedules/${id}`, input)),
};

export const growthApi: typeof live = isMock ? growthMock : live;
