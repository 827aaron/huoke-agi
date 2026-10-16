import type { RemoteLoginInput, StartLoginInput } from './growth-api';
import type {
    GrowthAccount,
    GrowthEmailResult,
    GrowthLead,
    GrowthProfile,
    GrowthProfileInput,
    GrowthProxyTestResult,
    GrowthRemoteLogin,
    GrowthRun,
    GrowthSetting,
    GrowthSettingInput,
    GrowthSettingKind,
    GrowthSource,
    GrowthSourcesAddResponse,
    GrowthTelegramLoginStep,
} from './growth-types';

import { isTelegramLink } from './source-links';

// In-memory stand-in for /api/v1/growth, used until the backend serves it. State lives for the page session.

const now = Date.now();
const ago = (minutes: number) => new Date(now - minutes * 60_000).toISOString();

let profiles: GrowthProfile[] = [];

let accounts: GrowthAccount[] = [
    {
        available: true,
        consecutive_errors: 0,
        external_browser_ws: null,
        external_id: '+8613800008842',
        fingerprint: null,
        id: 1,
        kind: 'user',
        label: 'Growth Ops',
        last_error: null,
        last_rate_limited_at: ago(1440),
        last_success_at: ago(3),
        last_used_at: ago(3),
        platform: 'telegram',
        proxy: null,
        rate_limit_until: null,
        status: 'active',
    },
    {
        available: false,
        consecutive_errors: 0,
        external_browser_ws: null,
        external_id: '+8613900001907',
        fingerprint: null,
        id: 2,
        kind: 'user',
        label: 'Growth Ops 2',
        last_error: null,
        last_rate_limited_at: ago(4),
        last_success_at: ago(20),
        last_used_at: ago(20),
        platform: 'telegram',
        proxy: null,
        rate_limit_until: new Date(now + 6 * 60_000).toISOString(),
        status: 'rate_limited',
    },
    {
        available: false,
        consecutive_errors: 1,
        external_browser_ws: null,
        external_id: '+8613700005531',
        fingerprint: null,
        id: 3,
        kind: 'user',
        label: 'Backup',
        last_error: 'AUTH_KEY_UNREGISTERED',
        last_rate_limited_at: null,
        last_success_at: ago(4320),
        last_used_at: ago(4320),
        platform: 'telegram',
        proxy: null,
        rate_limit_until: null,
        status: 'needs_login',
    },
    {
        available: true,
        consecutive_errors: 0,
        external_browser_ws: null,
        external_id: 'fb_100089331207',
        fingerprint: { language: 'zh-TW', os: 'Windows 11', timezone: 'Asia/Taipei' },
        id: 4,
        kind: 'browser_session',
        label: '星穹小编',
        last_error: null,
        last_rate_limited_at: null,
        last_success_at: ago(35),
        last_used_at: ago(35),
        platform: 'facebook',
        proxy: '203.0.113.24:8080',
        rate_limit_until: null,
        status: 'active',
    },
    {
        available: false,
        consecutive_errors: 0,
        external_browser_ws: 'ws://127.0.0.1:53201/devtools/browser/8c1f',
        external_id: 'ig_starera_sea',
        fingerprint: null,
        id: 5,
        kind: 'browser_session',
        label: 'starera.sea',
        last_error: null,
        last_rate_limited_at: null,
        last_success_at: null,
        last_used_at: null,
        platform: 'instagram',
        proxy: null,
        rate_limit_until: null,
        status: 'needs_login',
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
        notify_error: null,
        notify_status: null,
        round: 1,
        sources: { joined: 2, not_joined: 1, pending: 1, unjoinable: 1 },
        started_at: ago(45),
        status: 'running',
        target: 50,
        task_id: 11,
        watch: 2,
    },
];

const providersOf: Record<GrowthSettingKind, string[]> = {
    email: [],
    llm: ['openai', 'anthropic', 'gemini', 'deepseek', 'glm', 'kimi', 'qwen', 'minimax', 'mistral', 'xai', 'custom'],
    proxy: [],
    telegram: [],
    web_search: ['tavily', 'google', 'perplexity', 'firecrawl', 'traversaal'],
    youtube: [],
};

const SECRET_FIELDS = new Set(['api_hash', 'api_key', 'api_keys', 'password']);

const emptySetting = (kind: GrowthSettingKind): GrowthSetting => ({
    fields: {},
    kind,
    provider: null,
    providers: providersOf[kind],
    source: 'none',
    status: 'untested',
    status_reason: null,
    tested_at: null,
    updated_at: null,
});

let settings: GrowthSetting[] = [
    {
        ...emptySetting('llm'),
        fields: { api_key: '••••a1b2' },
        provider: 'openai',
        source: 'database',
        status: 'connected',
        tested_at: ago(90),
        updated_at: ago(600),
    },
    emptySetting('telegram'),
    {
        ...emptySetting('web_search'),
        fields: { api_keys: '••••9f3c\n••••07de' },
        provider: 'tavily',
        source: 'env',
        status: 'connected',
        tested_at: ago(300),
    },
    {
        ...emptySetting('youtube'),
        fields: { api_keys: '••••Kx2q\n••••p8Ld' },
        source: 'database',
        status: 'connected',
        tested_at: ago(240),
        updated_at: ago(3600),
    },
    emptySetting('proxy'),
    emptySetting('email'),
];

// The masking rule the server applies to secrets: only the last 4 characters survive.
const mask = (field: string, value: string) =>
    SECRET_FIELDS.has(field)
        ? value
              .split('\n')
              .map((line) => `••••${line.slice(-4)}`)
              .join('\n')
        : value;

// Any value is accepted except one containing "bad", so both outcomes can be tried by hand.
const testValues = (values: Record<string, string>): null | string =>
    Object.values(values).some((value) => value.includes('bad')) ? 'rejected' : null;

const storedValues = new Map<GrowthSettingKind, Record<string, string>>();

const findSetting = (kind: GrowthSettingKind) => settings.find((item) => item.kind === kind) ?? emptySetting(kind);

const replaceSetting = (next: GrowthSetting) => {
    settings = settings.map((item) => (item.kind === next.kind ? next : item));

    return next;
};

const sendMail = (): Promise<GrowthEmailResult> => {
    const email = findSetting('email');

    if (email.source === 'none') {
        return Promise.reject(new Error('mock: email is not configured'));
    }

    const to = (email.fields.notify_emails || email.fields.username || '')
        .split(',')
        .map((address) => address.trim())
        .filter(Boolean);

    return delay({ sent: true, to });
};

// Drops the credentials from a proxy URL the way the server reports it: host:port only.
const proxyHost = (proxy: null | string | undefined): null | string => {
    if (!proxy) {
        return null;
    }

    try {
        const url = new URL(proxy);

        return `${url.hostname}:${url.port}`;
    } catch {
        return proxy;
    }
};

const telegramLogins = new Map<string, StartLoginInput & { phone: string }>();
const remoteLogins = new Map<string, { input: RemoteLoginInput }>();

const upsertAccount = (
    accountId: number | undefined,
    fields: Pick<GrowthAccount, 'external_id' | 'kind' | 'label' | 'platform' | 'proxy'>,
    externalBrowserWs?: null | string,
): GrowthAccount => {
    const existing = accounts.find((account) => account.id === accountId);
    const account: GrowthAccount = {
        available: true,
        consecutive_errors: 0,
        external_browser_ws: existing?.external_browser_ws ?? null,
        fingerprint: existing?.fingerprint ?? null,
        id: existing?.id ?? Math.max(0, ...accounts.map(({ id }) => id)) + 1,
        last_error: null,
        last_rate_limited_at: existing?.last_rate_limited_at ?? null,
        last_success_at: new Date().toISOString(),
        last_used_at: new Date().toISOString(),
        rate_limit_until: null,
        status: 'active',
        ...fields,
        ...(existing ? { external_id: existing.external_id, label: existing.label } : {}),
        proxy: fields.proxy ?? existing?.proxy ?? null,
        ...(externalBrowserWs !== undefined ? { external_browser_ws: externalBrowserWs || null } : {}),
    };

    accounts = existing ? accounts.map((item) => (item.id === account.id ? account : item)) : [...accounts, account];

    return account;
};

const finishTelegramLogin = (loginId: string, input: StartLoginInput & { phone: string }) => {
    telegramLogins.delete(loginId);

    return upsertAccount(input.account_id, {
        external_id: input.phone,
        kind: 'user',
        label: 'Telegram 账号',
        platform: 'telegram',
        proxy: proxyHost(input.proxy),
    });
};

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
    cancelRemoteLogin: async (_sessionId: string) => {
        await delay(null);
    },
    clearSetting: async (kind: GrowthSettingKind) => {
        replaceSetting(emptySetting(kind));
        storedValues.delete(kind);
        await delay(null);
    },
    createProfile: (input: GrowthProfileInput): Promise<GrowthProfile> => {
        const profile = { ...input, id: 1, updated_at: new Date().toISOString() };
        profiles = [profile];

        return delay(profile);
    },
    finishRemoteLogin: (sessionId: string): Promise<GrowthAccount> => {
        const pending = remoteLogins.get(sessionId);

        if (!pending) {
            return Promise.reject(new Error('mock: unknown remote login'));
        }

        remoteLogins.delete(sessionId);

        return delay(
            upsertAccount(
                pending.input.account_id,
                {
                    external_id: `${pending.input.platform}_user_${sessionId.slice(-4)}`,
                    kind: 'browser_session',
                    label: `${pending.input.platform === 'facebook' ? 'Facebook' : 'Instagram'} 账号`,
                    platform: pending.input.platform,
                    proxy: proxyHost(pending.input.proxy),
                },
                pending.input.external_browser_ws,
            ),
        );
    },
    listAccounts: () => delay(accounts),
    // Every flow sees the same sample leads so any flow opened in development has something to show.
    listLeads: () => delay(leads),
    listProfiles: () => delay(profiles),
    listRuns: (flowId: number | string) => delay(runs.map((run) => ({ ...run, flow_id: Number(flowId) }))),
    listSettings: () => delay(settings),
    listSources: () => delay(sources),
    saveSetting: (kind: GrowthSettingKind, input: GrowthSettingInput): Promise<GrowthSetting> => {
        const current = findSetting(kind);
        const kept = current.provider === input.provider ? (storedValues.get(kind) ?? {}) : {};
        const values = { ...kept, ...Object.fromEntries(Object.entries(input.fields).filter(([, value]) => value)) };

        storedValues.set(kind, values);

        return delay(
            replaceSetting({
                ...current,
                fields: Object.fromEntries(Object.entries(values).map(([field, value]) => [field, mask(field, value)])),
                provider: input.provider,
                source: 'database',
                status: 'untested',
                status_reason: null,
                updated_at: new Date().toISOString(),
            }),
        );
    },
    sendLeadsEmail: (_flowId: number | string, _taskId: number) => sendMail(),
    setAccountDisabled: (id: number, isDisabled: boolean): Promise<GrowthAccount> => {
        accounts = accounts.map((account) =>
            account.id === id
                ? {
                      ...account,
                      available: !isDisabled,
                      rate_limit_until: null,
                      status: isDisabled ? 'disabled' : 'active',
                  }
                : account,
        );

        return delay(accounts.find((account) => account.id === id) as GrowthAccount);
    },
    setAccountProxy: (id: number, proxy: null | string): Promise<GrowthAccount> => {
        accounts = accounts.map((account) => (account.id === id ? { ...account, proxy: proxyHost(proxy) } : account));

        return delay(accounts.find((account) => account.id === id) as GrowthAccount);
    },
    startRemoteLogin: (input: RemoteLoginInput): Promise<GrowthRemoteLogin> => {
        const sessionId = `rl-${Date.now()}`;

        remoteLogins.set(sessionId, { input });

        return delay({
            fingerprint: input.external_browser_ws
                ? null
                : { language: 'en-US', os: 'macOS 14', timezone: 'Asia/Singapore' },
            session_id: sessionId,
            view_url: null,
        });
    },
    startTelegramLogin: (input: StartLoginInput & { phone: string }): Promise<GrowthTelegramLoginStep> => {
        const loginId = `tg-${Date.now()}`;

        telegramLogins.set(loginId, input);

        return delay({ account: null, login_id: loginId, next: 'code' });
    },
    // Code 00000 is refused; a phone ending in 2 asks for a two-step password, so every step can be tried.
    submitTelegramCode: (loginId: string, code: string): Promise<GrowthTelegramLoginStep> => {
        const input = telegramLogins.get(loginId);

        if (!input || code === '00000') {
            return Promise.reject(new Error('mock: wrong code'));
        }

        if (input.phone.endsWith('2')) {
            return delay({ account: null, login_id: loginId, next: 'password' });
        }

        return delay({ account: finishTelegramLogin(loginId, input), login_id: loginId, next: 'done' });
    },
    submitTelegramPassword: (loginId: string, _password: string): Promise<GrowthTelegramLoginStep> => {
        const input = telegramLogins.get(loginId);

        if (!input) {
            return Promise.reject(new Error('mock: unknown login'));
        }

        return delay({ account: finishTelegramLogin(loginId, input), login_id: loginId, next: 'done' });
    },
    testAccountProxy: (_id: number, proxy?: string): Promise<GrowthProxyTestResult> =>
        delay(proxy?.includes('bad') ? { error: 'connection refused', ok: false } : { error: null, ok: true }),
    testEmail: () => sendMail(),
    testSetting: (kind: GrowthSettingKind): Promise<GrowthSetting> => {
        const current = findSetting(kind);
        const reason = current.source === 'env' ? null : testValues(storedValues.get(kind) ?? {});

        return delay(
            replaceSetting({
                ...current,
                status: reason ? 'invalid' : 'connected',
                status_reason: reason,
                tested_at: new Date().toISOString(),
            }),
        );
    },
    updateProfile: (id: number, input: GrowthProfileInput): Promise<GrowthProfile> => {
        const profile = { ...input, id, updated_at: new Date().toISOString() };
        profiles = [profile];

        return delay(profile);
    },
};
