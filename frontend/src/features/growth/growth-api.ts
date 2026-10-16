import { api, unwrapApiResponse } from '@/lib/axios';

import type {
    GrowthAccount,
    GrowthEmailResult,
    GrowthLead,
    GrowthProfile,
    GrowthProfileInput,
    GrowthRun,
    GrowthSetting,
    GrowthSettingInput,
    GrowthSource,
    GrowthSourcesAddResponse,
} from './growth-types';

import { growthMock } from './growth-mock';

const isMock = import.meta.env.VITE_GROWTH_API
    ? import.meta.env.VITE_GROWTH_API === 'mock'
    : import.meta.env.DEV && import.meta.env.MODE !== 'test';

const live = {
    addSources: async (links: string[]) =>
        unwrapApiResponse(await api.post<GrowthSourcesAddResponse>('/growth/sources', { links })),
    clearSetting: async (id: string) => {
        await api.delete(`/growth/settings/${encodeURIComponent(id)}`);
    },
    createProfile: async (input: GrowthProfileInput) =>
        unwrapApiResponse(await api.post<GrowthProfile>('/growth/profiles', input)),
    listAccounts: async () => unwrapApiResponse(await api.get<GrowthAccount[]>('/growth/accounts')),
    listLeads: async (flowId: number | string) =>
        unwrapApiResponse(await api.get<GrowthLead[]>('/growth/leads', { params: { flow_id: flowId } })),
    listProfiles: async () => unwrapApiResponse(await api.get<GrowthProfile[]>('/growth/profiles')),
    listRuns: async (flowId: number | string) =>
        unwrapApiResponse(await api.get<GrowthRun[]>('/growth/runs', { params: { flow_id: flowId } })),
    listSettings: async () => unwrapApiResponse(await api.get<GrowthSetting[]>('/growth/settings')),
    listSources: async () => unwrapApiResponse(await api.get<GrowthSource[]>('/growth/sources')),
    // Also deletes the account's session from the server.
    removeAccount: async (id: number) => {
        await api.delete(`/growth/accounts/${id}`);
    },
    saveSetting: async (id: string, input: GrowthSettingInput) =>
        unwrapApiResponse(await api.put<GrowthSetting>(`/growth/settings/${encodeURIComponent(id)}`, input)),
    // Mails the lead list of one round to every address in notify_emails, the same email a finished round sends.
    sendLeadsEmail: async (flowId: number | string, taskId: number) =>
        unwrapApiResponse(
            await api.post<GrowthEmailResult>('/growth/leads/email', { flow_id: Number(flowId), task_id: taskId }),
        ),
    // Re-enabling returns the account to `signed_out` or `connected`, whichever its session allows.
    setAccountDisabled: async (id: number, isDisabled: boolean) =>
        unwrapApiResponse(await api.post<GrowthAccount>(`/growth/accounts/${id}/${isDisabled ? 'disable' : 'enable'}`)),
    testEmail: async () => unwrapApiResponse(await api.post<GrowthEmailResult>('/growth/settings/test-email')),
    testSetting: async (id: string) =>
        unwrapApiResponse(await api.post<GrowthSetting>(`/growth/settings/${encodeURIComponent(id)}/test`)),
    updateProfile: async (id: number, input: GrowthProfileInput) =>
        unwrapApiResponse(await api.put<GrowthProfile>(`/growth/profiles/${id}`, input)),
};

export const growthApi: typeof live = isMock ? growthMock : live;
