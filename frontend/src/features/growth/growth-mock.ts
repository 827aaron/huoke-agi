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

import { isTelegramLink } from './source-links';

// In-memory stand-in for /api/v1/growth, used until the backend serves it. State lives for the page session.

const now = Date.now();
const ago = (minutes: number) => new Date(now - minutes * 60_000).toISOString();

let profiles: GrowthProfile[] = [];

let accounts: GrowthAccount[] = [
    {
        display_name: 'Growth Ops',
        id: 1,
        last_flood_wait_at: ago(1440),
        last_success_at: ago(3),
        phone_last4: '8842',
        platform: 'telegram',
        resume_at: null,
        status: 'connected',
        today_joins: 3,
        today_reads: 1240,
    },
    {
        display_name: 'Growth Ops 2',
        id: 2,
        last_flood_wait_at: ago(4),
        last_success_at: ago(20),
        phone_last4: '1907',
        platform: 'telegram',
        resume_at: new Date(now + 6 * 60_000).toISOString(),
        status: 'flood_wait',
        today_joins: 1,
        today_reads: 860,
    },
    {
        display_name: 'Backup',
        id: 3,
        last_flood_wait_at: null,
        last_success_at: ago(4320),
        phone_last4: '5531',
        platform: 'telegram',
        resume_at: null,
        status: 'signed_out',
        today_joins: 0,
        today_reads: 0,
    },
    {
        display_name: 'Old SIM',
        id: 4,
        last_flood_wait_at: ago(10080),
        last_success_at: ago(8640),
        phone_last4: '0274',
        platform: 'telegram',
        resume_at: null,
        status: 'restricted',
        today_joins: 0,
        today_reads: 0,
    },
];

const sources: GrowthSource[] = [
    {
        created_at: ago(600),
        id: 1,
        join_status: 'joined',
        kind: 'group',
        last_read_at: ago(12),
        leads_count: 4,
        link: 'https://t.me/slg_players_sea',
        members_count: 18240,
        origin: 'ai',
        platform: 'telegram',
        source_id: '-1001820000001',
        title: 'SLG Players SEA',
    },
    {
        created_at: ago(560),
        id: 2,
        join_status: 'joined',
        kind: 'group',
        last_read_at: ago(30),
        leads_count: 2,
        link: 'https://t.me/mmorpg_tw_chat',
        members_count: 6120,
        origin: 'ai',
        platform: 'telegram',
        source_id: '-1001820000002',
        title: 'MMORPG 台灣玩家交流',
    },
    {
        created_at: ago(300),
        id: 3,
        join_status: 'pending_approval',
        kind: 'group',
        last_read_at: null,
        leads_count: 0,
        link: 'https://t.me/+k3QxR8sLmA1hNjVl',
        members_count: null,
        origin: 'manual',
        platform: 'telegram',
        source_id: '-1001820000003',
        title: '手游退坑互助群',
    },
    {
        created_at: ago(240),
        id: 4,
        join_status: 'not_joined',
        kind: 'channel',
        last_read_at: null,
        leads_count: 0,
        link: 'https://t.me/gamers_mena',
        members_count: 40210,
        origin: 'ai',
        platform: 'telegram',
        source_id: '-1001820000004',
        title: 'Gamers MENA',
    },
    {
        created_at: ago(200),
        id: 5,
        join_status: 'unjoinable',
        kind: 'group',
        last_read_at: null,
        leads_count: 0,
        link: 'https://t.me/closed_guild_vn',
        members_count: null,
        origin: 'ai',
        platform: 'telegram',
        source_id: '-1001820000005',
        title: 'Closed Guild VN',
    },
];

const lead = (
    id: number,
    name: string,
    username: null | string,
    score: number,
    signals: GrowthLead['signals'],
    source: GrowthSource,
    excerpt: string,
    activeMinutesAgo: number,
    delivered: boolean,
): GrowthLead => {
    const points: Record<string, number> = { genre_match: 3, paying: 2, player: 3, recent: 2, switching: 3 };

    return {
        delivered_at: delivered ? ago(5) : null,
        delivered_task_id: delivered ? 11 : null,
        display_name: name,
        evidence: [
            {
                excerpt,
                id: id * 10,
                message_id: String(4000 + id),
                message_url: `${source.link}/${4000 + id}`,
                posted_at: ago(activeMinutesAgo),
                reason: '主动表达对当前游戏不满并在找新游戏',
                source_id: source.id,
                source_title: source.title,
            },
            {
                excerpt: '有没有新开的 SLG 服，想重新开始',
                id: id * 10 + 1,
                message_id: String(3900 + id),
                message_url: `${source.link}/${3900 + id}`,
                posted_at: ago(activeMinutesAgo + 2880),
                reason: '在问同类型新服',
                source_id: source.id,
                source_title: source.title,
            },
        ],
        first_flow_id: 1,
        first_flow_title: '找 50 个 SLG 玩家',
        first_round: 1,
        first_task_id: 11,
        id,
        last_active_at: ago(activeMinutesAgo),
        platform: 'telegram',
        platform_user_id: String(700000 + id),
        profile:
            id % 2 === 0
                ? { bio: 'SLG 老玩家 / 公会会长', languages: ['中文'], messages_30d: 46, related_groups: 3 }
                : null,
        score,
        score_breakdown: signals
            .filter((signal) => points[signal])
            .map((signal) => ({ points: points[signal] ?? 0, signal })),
        signals,
        source_id: source.id,
        source_title: source.title,
        tier: score >= 7 ? 'qualified' : 'watch',
        username,
    };
};

const [sea, tw] = sources as [GrowthSource, GrowthSource];

const leads: GrowthLead[] = [
    lead(
        1,
        '阿杰 Jay',
        'jay_slg',
        10,
        ['player', 'switching', 'paying', 'recent'],
        sea,
        '这游戏越来越氪了，想换个公平点的 SLG',
        90,
        true,
    ),
    lead(
        2,
        'Mina',
        'mina_plays',
        9,
        ['player', 'switching', 'genre_match'],
        sea,
        'Any new SLG worth trying? Bored of my current one',
        300,
        true,
    ),
    lead(
        3,
        '老K',
        null,
        8,
        ['player', 'paying', 'recent', 'competitor'],
        tw,
        '在某某世界充了不少，最近版本太烂准备退坑',
        1440,
        false,
    ),
    lead(
        4,
        'Arif',
        'arif_gaming',
        7,
        ['player', 'switching', 'recent'],
        sea,
        'Looking for a guild in a new server',
        2000,
        false,
    ),
    lead(5, '小鱼', 'xiaoyu88', 5, ['player', 'recent'], tw, '最近在玩什么好玩的？', 4000, false),
    lead(6, 'Thanh', null, 4, ['player', 'genre_match'], sea, 'SLG nào đang hot vậy mọi người', 9000, false),
];

const runs: GrowthRun[] = [
    {
        current_action: '正在读取：SLG Players SEA',
        delivered: 2,
        error: null,
        finished_at: null,
        flood_wait_seconds: null,
        flow_id: 1,
        found: 4,
        round: 1,
        sources: { joined: 2, not_joined: 1, pending: 1, unjoinable: 1 },
        started_at: ago(45),
        status: 'running',
        target: 50,
        task_id: 11,
        watch: 2,
    },
];

let settings: GrowthSetting[] = [
    {
        id: 'llm',
        kind: 'llm',
        masked: { api_key: 'sk-••••••••a1b2' },
        provider: 'openai',
        reason_code: null,
        status: 'connected',
        tested_at: ago(90),
    },
    {
        id: 'web_search:tavily',
        kind: 'web_search',
        masked: { api_key: 'tvl••••••••9f3c' },
        provider: 'tavily',
        reason_code: null,
        status: 'connected',
        tested_at: ago(300),
    },
];

const PLAIN_FIELDS = new Set(['notify_emails', 'smtp_from', 'smtp_host', 'smtp_port', 'smtp_user']);

// The masking rule the server applies: the first 3 and last 4 characters, only the last 2 below 12. Mail
// server addresses are not secret and come back as entered.
const mask = (field: string, value: string) =>
    PLAIN_FIELDS.has(field)
        ? value
        : value.length < 12
          ? `••••••${value.slice(-2)}`
          : `${value.slice(0, 3)}••••••••${value.slice(-4)}`;

const notifyEmails = () =>
    (storedValues.get('email')?.notify_emails ?? '')
        .split(',')
        .map((address) => address.trim())
        .filter(Boolean);

const sendMail = (): Promise<GrowthEmailResult> =>
    settings.some((item) => item.kind === 'email' && item.status === 'connected')
        ? delay({ sent_to: notifyEmails() })
        : Promise.reject(new Error('mock: email is not configured'));

// Any value is accepted except one containing "bad", so both outcomes can be tried by hand.
const testValues = (values: Record<string, string>): GrowthSetting['reason_code'] =>
    Object.values(values).some((value) => value.includes('bad')) ? 'rejected' : null;

const storedValues = new Map<string, Record<string, string>>();

const delay = <T>(value: T): Promise<T> =>
    new Promise((resolve) => setTimeout(() => resolve(structuredClone(value)), 150));

export const growthMock = {
    addSources: (links: string[]): Promise<GrowthSourcesAddResponse> => {
        const results = links.map((link) => {
            if (!isTelegramLink(link)) {
                return { link, outcome: 'invalid' as const };
            }

            const existing = sources.find((source) => source.link === link);

            if (existing) {
                return { link, outcome: 'existing' as const, source: existing };
            }

            const source: GrowthSource = {
                created_at: new Date().toISOString(),
                id: Math.max(0, ...sources.map(({ id }) => id)) + 1,
                join_status: 'not_joined',
                kind: 'group',
                last_read_at: null,
                leads_count: 0,
                link,
                members_count: null,
                origin: 'manual',
                platform: 'telegram',
                source_id: '',
                title: link.replace(/^https?:\/\//, ''),
            };

            sources.push(source);

            return { link, outcome: 'added' as const, source };
        });

        return delay({ results });
    },
    clearSetting: async (id: string) => {
        settings = settings.filter((item) => item.id !== id);
        storedValues.delete(id);
        await delay(null);
    },
    createProfile: (input: GrowthProfileInput): Promise<GrowthProfile> => {
        const profile = { ...input, id: 1, updated_at: new Date().toISOString() };
        profiles = [profile];

        return delay(profile);
    },
    listAccounts: () => delay(accounts),
    // Every flow sees the same sample leads so any flow opened in development has something to show.
    listLeads: () => delay(leads),
    listProfiles: () => delay(profiles),
    listRuns: (flowId: number | string) => delay(runs.map((run) => ({ ...run, flow_id: Number(flowId) }))),
    listSettings: () => delay(settings),
    listSources: () => delay(sources),
    removeAccount: (id: number) => {
        accounts = accounts.filter((account) => account.id !== id);

        return delay(undefined);
    },
    saveSetting: (id: string, input: GrowthSettingInput): Promise<GrowthSetting> => {
        const reason = testValues(input.values);
        const saved: GrowthSetting = {
            id,
            kind: input.kind,
            masked: Object.fromEntries(
                Object.entries(input.values).map(([field, value]) => [field, mask(field, value)]),
            ),
            provider: input.provider,
            reason_code: reason,
            status: reason ? 'invalid' : 'connected',
            tested_at: new Date().toISOString(),
        };

        storedValues.set(id, input.values);
        settings = [...settings.filter((item) => item.id !== id), saved];

        return delay(saved);
    },
    sendLeadsEmail: (_flowId: number | string, _taskId: number) => sendMail(),
    setAccountDisabled: (id: number, isDisabled: boolean): Promise<GrowthAccount> => {
        accounts = accounts.map((account) =>
            account.id === id
                ? { ...account, resume_at: null, status: isDisabled ? 'disabled' : 'connected' }
                : account,
        );

        return delay(accounts.find((account) => account.id === id) as GrowthAccount);
    },
    testEmail: () => sendMail(),
    testSetting: (id: string): Promise<GrowthSetting> => {
        const current = settings.find((item) => item.id === id);

        if (!current) {
            return Promise.reject(new Error(`mock: no setting ${id}`));
        }

        const reason = testValues(storedValues.get(id) ?? {});
        const tested: GrowthSetting = {
            ...current,
            reason_code: reason,
            status: reason ? 'invalid' : 'connected',
            tested_at: new Date().toISOString(),
        };

        settings = settings.map((item) => (item.id === id ? tested : item));

        return delay(tested);
    },
    updateProfile: (id: number, input: GrowthProfileInput): Promise<GrowthProfile> => {
        const profile = { ...input, id, updated_at: new Date().toISOString() };
        profiles = [profile];

        return delay(profile);
    },
};
