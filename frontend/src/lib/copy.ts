// The UI has no i18n runtime yet: every customer-facing string of the growth product lives here in two
// dictionaries with the same keys, and `copy` is the one the app renders. It sits in lib/ rather than
// features/growth/ because components/shared (the version panel) may not import a feature.

import { format, formatDistanceToNow } from 'date-fns';
import { enUS, zhCN } from 'date-fns/locale';

const zh = {
    agent: {
        adviser: '顾问',
        assistant: '助手',
        coder: '程序员',
        enricher: '补全员',
        generator: '规划员',
        installer: '安装员',
        memorist: '记忆员',
        pentester: '潜客研究员',
        primary_agent: '主控',
        refiner: '调整员',
        reflector: '反思员',
        reporter: '汇报员',
        searcher: '搜索员',
        summarizer: '摘要员',
        tool_call_fixer: '工具修复员',
    } as Record<string, string>,
    app: {
        name: '客来 AGI',
        upToDate: '已是最新版本',
        upToDateTooltip: (label: string) => `客来 AGI ${label} 已是最新版本`,
    },
    automation: {
        firstStartHint: '第一次启动会准备运行环境，可能需要几分钟',
        working: '智能体正在工作…点击“停止”可中断',
    },
    common: {
        allPlatforms: '全部平台',
        dash: '—',
        date: (date: Date) => format(date, 'yyyy-MM-dd', { locale: zhCN }),
        networkError: '网络异常，请稍后重试。',
        platform: '平台',
        relative: (date: Date) => formatDistanceToNow(date, { addSuffix: true, locale: zhCN }),
        saveFailed: '保存失败，请重试。',
    },
    credentials: {
        cancel: '取消',
        card: {
            email: {
                purpose:
                    '每轮找人完成后，把潜客名单作为附件发到你指定的邮箱。不填也能用，只是不发邮件，名单在页面上查看。',
                title: '邮件通知',
            },
            embedding: {
                purpose: '默认由系统统一提供，一般无需设置。留空即使用系统默认的。',
                title: '向量模型（用于记忆和知识库）',
            },
            llm: { purpose: '智能体的“大脑”，没有它无法工作。', title: '大模型' },
            proxy: {
                purpose: '所有账号默认走这个代理出网。不填也能用，直接连接。单个账号可以在账号连接页另设代理。',
                title: '默认代理',
            },
            telegram: {
                purpose:
                    '让系统能以你的账号登录 Telegram，加入公开群、阅读群内公开发言。不填也能用，只是不找 Telegram 上的人。',
                title: 'Telegram 接口凭据',
            },
            web_search: {
                purpose:
                    '让智能体在网上查竞品、找论坛和公开讨论。不填也能用（走免费的 DuckDuckGo 和网页抓取，结果较弱）；填 Tavily 的 key 后搜索更准更稳（推荐）。',
                title: '网页搜索',
            },
            youtube: {
                purpose: '读取公开视频的评论区，从中发现讨论游戏的玩家。不填也能用，只是不找 YouTube 上的人。',
                title: 'YouTube',
            },
        } as Record<string, { purpose: string; title: string }>,
        clear: '清除',
        cleared: (card: string) => `${card}已清除`,
        description:
            '智能体工作需要几项外部服务的密钥。在这里填写就行，不用改任何配置文件。密钥只保存在你自己的服务器上，保存后只显示一部分。',
        embeddingAdvanced: '高级：向量模型',
        emptyHint: '还没有填写任何密钥。先填写“大模型”和“Telegram 接口凭据”，智能体就能开始工作。',
        envSource: '服务器预设',
        envSourceNote: '这一项由服务器预设，在这里保存会覆盖它。',
        field: {
            api_hash: { label: 'api_hash', placeholder: '32 位字母数字' },
            api_id: { label: 'api_id', placeholder: '纯数字，例如 1234567' },
            api_key: { label: 'API 密钥', placeholder: '粘贴服务商给你的密钥' },
            api_keys: { label: 'API 密钥', placeholder: '每行一个密钥' },
            cx: { label: '搜索引擎 ID', placeholder: '' },
            from: { label: '发件人地址', placeholder: '例如 leads@example.com' },
            host: { label: '发信服务器（SMTP）', placeholder: '例如 smtp.qq.com' },
            model: { label: '模型名称', placeholder: '例如 gpt-4o' },
            notify_emails: { label: '接收名单的邮箱', placeholder: '多个邮箱用逗号分隔，留空则发到登录邮箱' },
            password: { label: '密码或授权码', placeholder: '邮箱的 SMTP 授权码' },
            port: { label: '端口', placeholder: '通常是 465 或 587' },
            protocol: { label: 'API 协议', placeholder: '' },
            server_url: { label: '接口地址', placeholder: 'https://api.example.com/v1' },
            tls: { label: '加密方式', placeholder: '' },
            url: { label: '代理地址', placeholder: 'http://user:pass@host:port 或 socks5://host:port' },
            username: { label: '登录用户名', placeholder: '一般就是邮箱地址' },
        } as Record<string, { label: string; placeholder: string }>,
        fieldHelp: {
            'web_search.api_keys': '可填多个，用完一个自动换下一个。',
            'youtube.api_keys':
                '可填多个，用完一个自动换下一个。注意：同一个 Google 项目里的多个 key 共用额度，多 key 不增加额度；建议向 Google 免费申请提额。',
        } as Record<string, string>,
        fieldInvalid: {
            api_hash: 'api_hash 应为 32 位字母和数字',
            api_id: 'api_id 是一串纯数字',
            from: '请填写正确的邮箱地址',
            notify_emails: '请填写正确的邮箱，多个用逗号分隔',
            port: '端口是 1 到 65535 之间的数字',
            url: '代理地址格式应为 http://host:port 或 socks5://host:port',
        } as Record<string, string>,
        fieldRequired: (field: string) => `请填写 ${field}`,
        globalInvalid: (card: string) => `${card}的密钥已失效，智能体可能无法继续工作。`,
        globalInvalidLink: '去更新',
        guide: {
            anthropic: '登录 Anthropic Console（console.anthropic.com），进入 API Keys 页面创建密钥。',
            custom: '向你的接口提供方索取密钥和服务地址，接口需兼容 OpenAI 的格式。',
            deepseek: '登录 DeepSeek 开放平台（platform.deepseek.com），进入 API keys 页面。',
            email: '在邮箱的设置里开启 SMTP 服务（QQ、163 等邮箱会给你一个“授权码”，用它代替登录密码），再把服务器地址、端口和账号填到这里。',
            firecrawl: '登录 Firecrawl（firecrawl.dev），在 Dashboard 里复制 API Key。',
            gemini: '登录 Google AI Studio（aistudio.google.com），点“Get API key”。',
            glm: '登录智谱开放平台（open.bigmodel.cn），进入“API Keys”页面。',
            google: '在 Google Cloud 控制台开通 Custom Search API 并创建 API 密钥，再到 Programmable Search Engine（programmablesearchengine.google.com）创建搜索引擎，复制它的“搜索引擎 ID”。',
            kimi: '登录 Moonshot AI 开放平台（platform.moonshot.cn），进入“API Key 管理”。',
            minimax: '登录 MiniMax 开放平台（platform.minimaxi.com），进入“接口密钥”页面。',
            mistral: '登录 Mistral La Plateforme（console.mistral.ai），进入 API Keys。',
            openai: '登录 OpenAI Platform（platform.openai.com），进入 API keys 页面，点“Create new secret key”。',
            perplexity: '登录 Perplexity 设置页的 API 板块（perplexity.ai/settings/api）生成密钥。',
            proxy: '向你的代理服务商要一个 HTTP 或 SOCKS5 地址填在这里。不填则直连。',
            qwen: '登录阿里云百炼（bailian.console.aliyun.com），进入“API-KEY”页面。',
            tavily: '登录 Tavily（tavily.com），在控制台首页复制 API Key，新账号有免费额度。',
            telegram:
                '用你的 Telegram 手机号登录 Telegram 官方网站 my.telegram.org，进入“API development tools”，填写应用名称（随意，例如 KeLai）后提交，页面会显示 api_id 和 api_hash。',
            traversaal: '登录 Traversaal Ares（api.traversaal.ai）申请 API Key。',
            xai: '登录 xAI Console（console.x.ai），创建 API key。',
            youtube:
                '登录 Google Cloud 控制台（console.cloud.google.com），新建或选择一个项目，在“API 和服务”里启用“YouTube Data API v3”，再进入“API 和服务 → 凭据”页，点“创建凭据 → API 密钥”。',
        } as Record<string, string>,
        howToApply: '怎么申请',
        keepSaved: '留空则保留已保存的值',
        llmMode: {
            catalog: '第三方模型提供商',
            catalogHint: '从内置目录中选择 OpenAI、Anthropic、Kimi 等提供商，填入其 API 密钥即可使用。',
            custom: '自定义模型 API',
            customHint:
                '适用于中转站、自建网关或任何 OpenAI / Anthropic 格式的接口，需填写接口地址、协议、模型名称和 API 密钥。',
            overrides: '自定义设置',
            overridesHint: '留空则使用该服务商的默认模型和官方地址。',
            rolesLink: '按角色分配模型',
        },
        llmTopUpNote: '新注册的账号请先在服务商后台充值，否则测试会提示额度不足。',
        loadFailed: '密钥状态加载失败，请刷新重试。',
        noSecretKey:
            '服务器还没有设置加密密钥，密钥暂时无法保存。请让部署这套系统的管理员在服务器上设置 GROWTH_SECRET_KEY 后重试。',
        optional: '可选',
        protocol: {
            anthropic_messages: 'Anthropic Messages',
            openai_chat: 'OpenAI Chat Completions',
        } as Record<string, string>,
        provider: {
            embedding: '向量服务',
            llm: '服务商',
            web_search: '搜索服务',
        } as Record<string, string>,
        providerLabel: {
            anthropic: 'Anthropic (Claude)',
            custom: '自定义（OpenAI 兼容接口）',
            deepseek: 'DeepSeek',
            firecrawl: 'Firecrawl',
            gemini: 'Google Gemini',
            glm: '智谱 GLM',
            google: 'Google 自定义搜索（仅限已有旧账号，Google 已不接受新客户）',
            googleai: 'Google AI',
            huggingface: 'Hugging Face',
            jina: 'Jina',
            kimi: '月之暗面 Kimi',
            minimax: 'MiniMax',
            mistral: 'Mistral',
            ollama: 'Ollama',
            openai: 'OpenAI',
            perplexity: 'Perplexity',
            qwen: '通义千问',
            tavily: 'Tavily',
            traversaal: 'Traversaal',
            voyageai: 'Voyage AI',
            xai: 'xAI',
        } as Record<string, string>,
        ready: '必填项已就绪，可以去新建获客项目了。',
        readyLink: '去找客户',
        reason: {
            format: '格式不对，请检查是否多复制或少复制了字符',
            permission: '这个密钥没有开通所需的服务，请按下面的指引开通',
            quota: '服务商提示账户余额或额度不足，请到服务商后台充值',
            rejected: '服务商不认这个密钥，可能复制不完整或已被撤销',
            timeout: '测试超时，请稍后再试',
            unreachable: '无法连接到服务商，请检查服务器网络或服务地址',
        } as Record<string, string>,
        replace: '更换密钥',
        required: '必填',
        saveAndTest: '保存并测试',
        saved: (card: string) => `${card}已保存并通过测试`,
        savedButFailed: (card: string, reason: string) => `${card}已保存，但测试未通过：${reason}`,
        status: {
            connected: '已连接',
            invalid: '无效',
            unset: '未配置',
            untested: '未测试',
        },
        statusLine: {
            connected: (time: string) => `已连接，最近一次测试：${time}通过`,
            invalid: (reason: string) => `无效：${reason}。请检查后重新填写。`,
            unsetOptional: '还没有填写。不填也能用，填了效果更好。',
            unsetRequired: '还没有填写。没有它智能体无法工作。',
            unsetSearch: '未填写，正在用免费的 DuckDuckGo 和网页抓取。',
            untested: '已保存，还没有测试。',
        },
        telegramNextStep: '下一步：去登录 Telegram 账号',
        telegramNote: '这两项只是应用凭据，不等于账号登录；填好后到“账号连接”页用手机号登录。',
        test: '测试连接',
        testEmail: '发送测试邮件',
        testEmailFailed: '测试邮件没有发出去，请检查发信设置。',
        testEmailSent: (to: string) => `测试邮件已发送到 ${to}`,
        testing: '测试中…',
        title: '密钥与连接',
        tlsOption: {
            none: '不加密',
            starttls: 'STARTTLS（常用于 587 端口）',
            tls: 'SSL/TLS（常用于 465 端口）',
        } as Record<string, string>,
        unknownReason: '测试未通过',
    },
    delivery: {
        evidence: '每位都附有原话证据和消息链接',
        qualified: (n: number, watch: number) => `合格 ${n} 位 · 待观察 ${watch} 位（未交付，仍在潜客标签里）`,
        sources: (n: number) => `来自 ${n} 个群`,
        title: (n: number) => `本轮交付 ${n} 位潜客`,
    },
    export: {
        allDelivered: '全部已交付',
        allQualified: '全部合格潜客',
        button: '导出 CSV',
        currentRoundDelivered: '本轮已交付',
        failed: '导出失败，请重试',
        fileName: (business: string, stamp: string) => `潜客_${business}_${stamp}.csv`,
        filtered: '当前筛选结果',
        header: {
            bio: '画像简介',
            deliveredAt: '交付时间',
            evidenceText: (n: number) => `证据 ${n} 原话`,
            evidenceTime: (n: number) => `证据 ${n} 时间`,
            evidenceUrl: (n: number) => `证据 ${n} 消息链接`,
            foundIn: '发现项目',
            foundRound: '发现轮次',
            lastActive: '最近活跃',
            name: '玩家昵称',
            platform: '平台',
            score: '评分',
            signals: '信号',
            source: '来源群',
            sourceUrl: '来源群链接',
            status: '状态',
            username: '用户名',
        },
        nothing: '当前没有可导出的潜客',
        unnamedBusiness: '未命名业务',
    },
    flowTabs: {
        agents: '智能体',
        assistant: '助手',
        automation: '自动执行',
        dashboard: '概览',
        files: '文件',
        leads: '潜客',
        screenshots: '截图',
        sources: '来源池',
        tasks: '任务',
        terminal: '终端',
        tools: '搜索记录',
        vectorStores: '记忆库',
    },
    helpKeys: {
        loadFailed: '申请指南加载失败，请刷新重试。',
        title: '密钥申请指南',
    },
    leads: {
        column: {
            evidence: '证据',
            foundIn: '发现于',
            lastActive: '最近活跃',
            player: '玩家',
            score: '评分',
            signals: '信号',
            source: '来源群',
            status: '状态',
        },
        detail: {
            breakdown: '打分明细',
            evidence: '证据',
            openInTelegram: '在 Telegram 打开',
            profile: '画像补全',
            profileActivity30d: (n: number) => `近 30 天发言 ${n} 条`,
            profileGroups: (n: number) => `所在相关群 ${n} 个`,
            profileLanguages: (languages: string[]) => `常用语言：${languages.join('、')}`,
        },
        email: {
            failed: '发送失败，请重试。',
            notConfigured: '还没有设置邮件通知，设置好后就能把名单发到邮箱。',
            notConfiguredLink: '去设置',
            send: '发送到邮箱',
            sent: (to: string) => `名单已发送到 ${to}`,
        },
        empty: '还没有找到潜客。智能体开始工作后，找到的人会实时出现在这里。',
        emptyFiltered: '没有符合条件的潜客，试试放宽筛选。',
        evidenceCount: (n: number) => `${n} 条`,
        filter: {
            all: '全部',
            allSources: '全部来源',
            delivered: '已交付',
            flags: '标记',
            notDelivered: '未交付',
            qualified: '合格',
            watch: '待观察',
        },
        flag: {
            competitor: '提到竞品',
            paying: '付费迹象',
            recent: '7 天内活跃',
        },
        foundIn: (project: string, round: number) => `${project} · 第 ${round} 轮`,
        loadFailed: '潜客列表加载失败，请刷新重试。',
        searchPlaceholder: '搜索昵称、用户名、原话…',
        signal: {
            competitor: '提到竞品',
            genre_match: '游戏类型匹配',
            paying: '有付费迹象',
            player: '是玩家',
            recent: '近期活跃',
            switching: '想换游戏',
        } as Record<string, string>,
        status: {
            delivered: (date: string) => `已交付 ${date}`,
            notDelivered: '未交付',
        },
        tier: {
            qualified: '合格',
            watch: '待观察',
        },
        total: (n: number) => `共 ${n} 位潜客`,
    },
    nav: {
        dashboard: '概览',
        favoriteFlows: '收藏的项目',
        flows: '获客项目',
        knowledges: '知识库',
        newFlow: '新建获客项目',
        recentFlows: '最近的项目',
        resources: '资源',
        settings: '设置',
        templates: '模板',
    },
    newFlow: {
        assistantHint: '和智能体聊天，一边问一边找。',
        automationHint: '说一句话，智能体自己找群、读消息、交付名单。',
        chips: ['给我找 50 个客户', '找 30 个想退坑 / 想换游戏的玩家', '找 20 个在问同类游戏的新玩家'],
        missingAccount: '还没有连接 Telegram 账号，智能体暂时无法加群和读消息。',
        missingAccountLink: '去连接',
        missingProfile: '还没有业务档案，智能体不知道你卖什么、找谁。',
        missingProfileLink: '去填写',
        pageTitle: '新建获客项目',
        placeholder: '例如：给我找 50 个想换游戏的玩家',
        subtitle: '说一句你想找什么样的客户',
        title: '开始找客户',
    },
    onboarding: {
        stepAccount: '连接 Telegram 账号',
        stepCredentials: '填写密钥与连接',
        stepFlow: '去找客户',
        stepProfile: '填写业务档案',
        title: '四步开始',
    },
    profile: {
        aiHint: '这些内容会自动带给 AI，不用改任何模板。',
        defaultExcludes: ['卖号', '代练', '竞品官方', '机器人'],
        description: '告诉智能体你在做什么、想找谁。填一次，之后每次只要说一句“找 50 个客户”。',
        emptyHint: '还没有业务档案。填写后，智能体才知道该找什么样的玩家。',
        field: {
            competitors: {
                help: '可选。提到竞品、想从竞品离开的玩家会被标记',
                label: '竞品名称',
                placeholder: '例如：某某传奇、某某世界',
            },
            defaultTarget: {
                help: '说“再找一些”时使用；一句话里说了具体数字则以那句为准',
                label: '默认每轮目标数',
                placeholder: '50',
            },
            excludes: {
                help: '可增删。命中的人不会入库',
                label: '排除规则',
                placeholder: '例如：代练、卖号、竞品员工',
            },
            games: {
                help: '智能体会用这些名字搜索玩家讨论',
                label: '游戏 / 产品名称',
                placeholder: '例如：星穹纪元、Star Era（可填多个，用回车分隔）',
            },
            genre: {
                help: '用来判断玩家玩的游戏是否对口：完全匹配加分，不相关会被淘汰',
                label: '游戏类型',
                placeholder: '选择游戏类型',
            },
            languages: { help: '决定去哪些语言的群里找', label: '目标语言', placeholder: '选择语言' },
            name: { help: '方便你在多个业务之间区分', label: '业务名称', placeholder: '例如：星穹纪元' },
            notes: {
                help: '可选。写成一段话就行',
                label: '补充说明',
                placeholder: '任何想让 AI 知道的事，例如游戏特色、常去的社区、忌讳',
            },
            pitch: {
                help: '一两句话即可，决定要找“什么需求”的玩家',
                label: '你想让玩家知道什么',
                placeholder: '例如：新服开启，老玩家回归送礼包',
            },
            regions: { help: '可填多个', label: '目标地区', placeholder: '例如：东南亚、台湾、中东' },
            signals: {
                help: '可选。智能体会自己扩展关键词，这里填你已知的',
                label: '需求信号词',
                placeholder: '例如：想换游戏、没意思了、求推荐、退坑',
            },
        },
        genres: ['MMORPG', 'SLG', '卡牌', 'FPS', 'MOBA', '休闲', '其他'],
        languages: {
            ar: '阿拉伯语',
            en: '英语',
            id: '印尼语',
            ja: '日语',
            ko: '韩语',
            ms: '马来语',
            th: '泰语',
            vi: '越南语',
            zh: '中文',
        } as Record<string, string>,
        otherLanguage: '其他',
        otherLanguagePlaceholder: '填写其他语言，多个用逗号分隔，例如：泰米尔语、波斯语',
        required: {
            games: '请填写游戏 / 产品名称',
            genre: '请选择游戏类型',
            languages: '请至少选择一种目标语言',
            name: '请填写业务名称',
            pitch: '请填写你想让玩家知道什么',
            regions: '请填写目标地区',
        },
        save: '保存',
        saved: '业务档案已保存，下一轮起生效。',
        targetRange: '目标数请输入 1–1000 之间的整数',
        title: '业务档案',
    },
    progress: {
        delivered: '已交付',
        deliveredLine: (n: number, t: number) => ({ count: n, prefix: '本轮已交付', suffix: ` / ${t} 位潜客` }),
        emailFailed: (error: string) => `名单邮件没有发出去：${error}`,
        emailSent: '名单已发到邮箱',
        exportCsv: '导出 CSV',
        failed: (reason: string, n: number) => `本轮中断：${reason}，已入库的 ${n} 位潜客保留。`,
        foundLine: (n: number, t: number) => ({ count: n, prefix: '本轮已找到', suffix: ` / ${t} 位潜客` }),
        missed: '目标未达成，原因见汇报',
        more: (n: number) => `再找 ${n} 个`,
        retry: '重试',
        retryText: '继续完成上一轮的目标',
        running: '进行中',
        sources: (s: { joined: number; not_joined: number; pending: number; unjoinable: number }) =>
            `来源 已加入 ${s.joined} · 待审批 ${s.pending} · 未加入 ${s.not_joined} · 不可加入 ${s.unjoinable}`,
        viewLeads: '查看名单',
        viewReport: '查看汇报',
        watch: (n: number) => `待观察 ${n}`,
    },
    quickActions: {
        changeDirection: '换个方向找',
        changeDirectionText: '换个方向找一些新的群和玩家',
        more: (n: number) => `再找 ${n} 个`,
        moreText: (n: number) => `再找 ${n} 个`,
        watchOnly: '只看待观察',
        watchOnlyText: '把待观察的潜客再核实一遍，能确认的升为合格',
    },
    settingsNav: {
        account: '账号',
        accounts: '账号连接',
        apiTokens: 'API 令牌',
        credentials: '密钥与连接',
        growthProfile: '业务档案',
        prompts: '提示词',
        providers: '模型服务',
        settings: '设置',
    },
    sources: {
        add: '添加到来源池',
        addedCount: (n: number) => `已添加 ${n} 个，将在下一轮加入并读取`,
        column: {
            joinStatus: '加入状态',
            lastRead: '最后读取',
            leads: '产出潜客',
            members: '成员数',
            origin: '来源方式',
            source: '群 / 频道',
        },
        empty: '来源池还是空的。智能体找到的群会自动出现在这里，你也可以在上面粘贴自己的群链接。',
        emptyFiltered: '没有符合条件的来源。',
        existing: (link: string) => `${link} 已在来源池里`,
        filter: {
            allOrigins: '全部来源方式',
            allStatuses: '全部加入状态',
        },
        invalid: (link: string) => `${link} 不是有效的 Telegram 链接`,
        joinStatus: {
            joined: '已加入',
            not_joined: '未加入',
            pending_approval: '已申请待审批',
            unjoinable: '不可加入',
        },
        joinStatusHint: {
            pending_approval: '已发出入群申请，等待群管理员通过，系统会定期复查',
            unjoinable: '群已关闭、被封或需要邀请，系统不会再尝试',
        } as Record<string, string>,
        loadFailed: '来源池加载失败，请刷新重试。',
        origin: {
            ai: 'AI 发现',
            manual: '手工添加',
        },
        placeholder: '粘贴 Telegram 群或频道链接，一行一个，例如 https://t.me/xxxx 或 https://t.me/+邀请码',
        searchPlaceholder: '搜索群名或链接…',
        tooMany: '一次最多添加 100 个链接，请分批添加。',
        unresolved: (link: string) => `${link} 暂时无法解析，请稍后重试`,
    },
    status: {
        created: '已创建',
        failed: '失败',
        finished: '已完成',
        running: '进行中',
        waiting: '等待处理',
    } as Record<string, string>,
    telegram: {
        cliHint: '请在服务器上运行登录命令 growth-tglogin 完成登录，完成后账号会出现在上面的列表里。',
        credentialsMissing: '请先在密钥与连接里填写 Telegram 的 api_id 和 api_hash',
        credentialsMissingLink: '去填写',
        description: '系统用这个账号加入公开群、阅读群内公开发言。请使用你授权的账号，登录信息只保存在你的服务器上。',
        fingerprint: {
            advanced: '指纹浏览器接入（高级）',
            auto: (os: string, language: string, timezone: string) => `自动指纹：${os} · ${language} · ${timezone}`,
            externalHint: '填了就用你自己的 AdsPower/比特浏览器窗口，代理和指纹由它管。',
            externalPlaceholder: 'ws://127.0.0.1:xxxxx/devtools/browser/…',
            externalWs: '指纹浏览器连接地址',
            usingExternal: '使用你自己的指纹浏览器',
        },
        floodWait: (n: number) => `Telegram 要求稍等 ${n} 秒，系统会自动等待后继续`,
        health: {
            accountName: (name: string, last4: string) => `${name}（…${last4}）`,
            actionFailed: '操作失败，请重试。',
            add: '添加账号',
            added: (a: string, n: number) => `账号 ${a} 已添加，当前共 ${n} 个可用账号。`,
            allLimited: (time: string) => `所有账号都在限流中，最早 ${time} 恢复，智能体会自动继续。`,
            column: {
                account: '账号',
                actions: '操作',
                lastSuccess: '上次成功',
                limit: '限流 / 恢复',
                platform: '平台',
                status: '状态',
            },
            disable: '停用',
            disabled: (a: string) => `账号 ${a} 已停用。`,
            empty: '还没有添加账号。',
            enable: '启用',
            enabled: (a: string) => `账号 ${a} 已启用。`,
            hint: {
                active: '可正常使用',
                disabled: '已停用，智能体不会使用这个账号',
                needs_login: '登录已失效（密码修改、在别处退出或会话过期），需要重新登录',
                rate_limited: (time: string) => `Telegram 要求等待，到 ${time} 自动恢复，期间智能体改用其他账号`,
            },
            limitedNoBackup: (a: string, time: string) => `账号 ${a} 被限流，暂无其他可用账号，将在 ${time} 自动继续。`,
            limitedSwitched: (a: string, b: string) => `账号 ${a} 被限流，已自动切换到账号 ${b}，工作不受影响。`,
            limitedUntil: (time: string, minutes: number) => `限流至 ${time}（还剩 ${minutes} 分钟）`,
            noneUsable: '没有可用的 Telegram 账号，智能体已暂停。请重新登录或添加账号。',
            note: '账号多一个，系统就多一份保险。某个账号被限流或掉线时，智能体会自动改用其他健康的账号继续工作。',
            onlyOne: '目前只有 1 个 Telegram 账号。多添加一个账号，被限流时智能体可以无缝切换。',
            platform: {
                facebook: 'Facebook',
                instagram: 'Instagram',
                telegram: 'Telegram',
                web: '网页 / 论坛',
                youtube: 'YouTube',
            } as Record<string, string>,
            recovered: (a: string) => `账号 ${a} 已恢复，重新加入可用账号。`,
            recoveredAt: (relative: string) => `${relative}恢复`,
            relogin: '重新登录',
            reloginLink: '去重新登录',
            remoteBrowserPending: '远程浏览器登录还在接入中，接好后点这里即可登录。',
            signedOutNoBackup: (a: string) =>
                `账号 ${a} 登录已失效，没有其他可用账号，进行中的项目已暂停。请重新登录。`,
            signedOutSwitched: (a: string, b: string) => `账号 ${a} 登录已失效，已改用账号 ${b}。请尽快重新登录 ${a}。`,
            status: {
                active: '正常',
                disabled: '已停用',
                needs_login: '需要重新登录',
                rate_limited: '限流中',
            },
            title: '账号健康',
        },
        loadFailed: '账号状态加载失败，请刷新重试。',
        login: {
            back: '上一步',
            code: '验证码',
            codeHint: (phone: string) => `验证码已发到 ${phone} 的 Telegram 应用里（不是短信）。`,
            codePlaceholder: '5 位数字',
            done: (name: string) => `已连接：${name}`,
            failed: '登录失败，请重试。',
            finish: '完成',
            password: '二步验证密码',
            passwordHint: '这个账号开启了二步验证，请输入你在 Telegram 里设置的密码。',
            phone: '手机号',
            phonePlaceholder: '带国家区号，例如 +8613800000000',
            sendCode: '发送验证码',
            signIn: '登录',
            step: (n: number) => `第 ${n} 步，共 3 步`,
            title: '添加 Telegram 账号',
            titleRelogin: '重新登录 Telegram 账号',
        },
        proxy: {
            column: '代理',
            direct: '直连',
            edit: '代理',
            field: '代理（可选）',
            global: '默认代理',
            hint: '不填则使用密钥与连接里的默认代理；默认代理也没填就直连。',
            placeholder: 'http://user:pass@host:port 或 socks5://host:port',
            save: '保存',
            saved: '代理已保存',
            test: '测试代理',
            testFailed: (error: string) => `代理连不上：${error}`,
            testOk: (latencyMs: null | number) =>
                latencyMs === null ? '代理可用' : `代理可用，延迟 ${latencyMs} 毫秒`,
            title: (name: string) => `${name} 的代理`,
        },
        remote: {
            done: '我已登录完成',
            failed: '还没检测到登录成功，请在窗口里完成登录后再点一次。',
            hint: '下面是一个在服务器上打开的浏览器窗口。在里面像平时一样登录你的账号（含验证码），登录完成后点“我已登录完成”。关掉窗口不会保存登录。',
            loading: '正在打开浏览器窗口…',
            placeholder: '浏览器服务还没接上，这里会显示登录页面。',
            title: (platform: string) => `登录 ${platform}`,
            unavailable: '暂时无法打开远程浏览器，请稍后重试。',
        },
        remoteLogin: '远程登录',
        sections: {
            facebook: '通过远程浏览器登录。系统用这个账号浏览公开主页和群组。',
            instagram: '通过远程浏览器登录。系统用这个账号浏览公开主页和评论。',
            telegram: '用手机号和验证码登录。需要先在密钥与连接里填写 api_id 和 api_hash。',
            web: '不需要账号。智能体直接读取公开网页和论坛帖子。',
            youtube: '不需要登录账号，使用密钥与连接里填写的 YouTube API 密钥读取公开视频的评论。',
        } as Record<string, string>,
        title: '账号连接',
        youtubeKey: { connected: 'API 密钥可用', link: '去密钥与连接', missing: '还没有填写 YouTube API 密钥' },
    },
    templates: {
        presets: [
            {
                text: `给我找 {{TARGET_COUNT}} 个想换游戏的 {{GENRE}} 玩家

要求：
1. 从业务档案里的游戏名、竞品名和需求信号词出发扩展关键词
2. 搜索相关的公开 Telegram 群组和频道，能加入的先加入
3. 阅读最近的群消息，找出抱怨现在的游戏、问新游戏推荐、说要退坑的玩家
4. 每位潜客都要有原话证据和消息链接，排除卖号、代练、竞品官方和机器人
5. 达到目标数后交付名单`,
                title: '找想换游戏的玩家',
            },
            {
                text: `找 {{TARGET_COUNT}} 个正在讨论 {{COMPETITOR}} 的玩家

要求：
1. 找出 {{COMPETITOR}} 的玩家群、交易群和攻略频道
2. 重点关注对 {{COMPETITOR}} 不满、提到氪金太重、版本太烂、想离开的发言
3. 有付费迹象（提到充值、月卡、礼包）的玩家优先
4. 每位潜客附原话和消息链接，标记“提到竞品”
5. 达到目标数后交付名单`,
                title: '竞品玩家监控',
            },
            {
                text: `找 {{TARGET_COUNT}} 个 {{REGION}} 地区、说 {{LANGUAGE}} 的 {{GENRE}} 新玩家

要求：
1. 用 {{LANGUAGE}} 扩展关键词，搜索 {{REGION}} 本地的游戏群和频道
2. 找出在问“有什么好玩的 {{GENRE}}”、求推荐、刚入坑的玩家
3. 只保留 30 天内有发言的人
4. 每位潜客附原话和消息链接
5. 达到目标数后交付名单`,
                title: '新地区新玩家',
            },
            {
                text: `把这些群加入来源池并从中找潜客：
{{GROUP_LINKS}}

要求：
1. 先加入上面的群，加入不了的记下原因
2. 读取每个群最近的消息，找出有购买意向的玩家
3. 按业务档案打分，合格的入库，待观察的保留
4. 达到 {{TARGET_COUNT}} 个后交付名单`,
                title: '从指定群里找人',
            },
            {
                text: `给我找 {{TARGET_COUNT}} 个有付费迹象的老玩家

要求：
1. 搜索 {{GENRE}} 老玩家聚集的群（公会群、区服群、交易群）
2. 找出提到充值、月卡、礼包、抽卡花费的玩家
3. 优先 7 天内活跃、游戏类型完全匹配的人
4. 每位潜客附原话和消息链接，排除卖号和代练
5. 达到目标数后交付名单`,
                title: '高付费意愿玩家',
            },
        ],
        presetsTitle: '预设模板',
    },
    tool: {
        growth_add_sources: '添加来源',
        growth_deliver_leads: '交付名单',
        growth_get_progress: '查看进度',
        growth_join_source: '加入群组',
        growth_list_leads: '查看潜客',
        growth_search_sources: '搜索群组',
        growth_sync_source: '读取群消息',
        growth_upsert_leads: '写入潜客',
    } as Record<string, string>,
};

type Copy = typeof zh;

const en: Copy = {
    agent: {
        adviser: 'Adviser',
        assistant: 'Assistant',
        coder: 'Coder',
        enricher: 'Enricher',
        generator: 'Planner',
        installer: 'Installer',
        memorist: 'Memory keeper',
        pentester: 'Prospect researcher',
        primary_agent: 'Coordinator',
        refiner: 'Re-planner',
        reflector: 'Reflector',
        reporter: 'Reporter',
        searcher: 'Searcher',
        summarizer: 'Summarizer',
        tool_call_fixer: 'Tool fixer',
    },
    app: {
        name: 'KeLai AGI',
        upToDate: 'Up to date',
        upToDateTooltip: (label) => `KeLai AGI ${label} is up to date.`,
    },
    automation: {
        firstStartHint: 'The first start prepares the runtime and may take a few minutes',
        working: 'The agent is working... Click Stop to interrupt',
    },
    common: {
        allPlatforms: 'All platforms',
        dash: '—',
        date: (date) => format(date, 'd MMM yyyy', { locale: enUS }),
        networkError: 'Network error, please retry.',
        platform: 'Platform',
        relative: (date) => formatDistanceToNow(date, { addSuffix: true, locale: enUS }),
        saveFailed: 'Save failed, please retry.',
    },
    credentials: {
        cancel: 'Cancel',
        card: {
            email: {
                purpose:
                    'After each round, the lead list is emailed as an attachment to the addresses you choose. Without it, lists are delivered on the page only.',
                title: 'Email notifications',
            },
            embedding: {
                purpose:
                    'Provided by the system by default; usually nothing to set here. Leave it empty to keep the default.',
                title: 'Embedding model (memory and knowledge base)',
            },
            llm: { purpose: 'The agent’s brain; nothing works without it.', title: 'Language model' },
            proxy: {
                purpose:
                    'Every account goes online through this proxy by default. Leave empty to connect directly. An account can override it on the Accounts page.',
                title: 'Default proxy',
            },
            telegram: {
                purpose:
                    'Lets the system sign in to Telegram as you to join public groups and read public messages. Optional; without it Telegram is skipped.',
                title: 'Telegram API credentials',
            },
            web_search: {
                purpose:
                    'Lets the agent look up competitors, forums and public discussions. Works without a key (free DuckDuckGo and page fetching, weaker results); a Tavily key (recommended) makes search more accurate and stable.',
                title: 'Web search',
            },
            youtube: {
                purpose:
                    'Reads comments under public videos to find players talking about games. Without it the agent skips YouTube.',
                title: 'YouTube Data API',
            },
        },
        clear: 'Clear',
        cleared: (card) => `${card} cleared`,
        description:
            'The agent needs keys for a few outside services. Enter them here, no config files needed. Keys stay on your own server and are shown masked after saving.',
        embeddingAdvanced: 'Advanced: embedding model',
        emptyHint: 'No keys yet. Fill in “Language model” and “Telegram API credentials” to get started.',
        envSource: 'Preset on server',
        envSourceNote: 'This item is preset on the server; saving here overrides it.',
        field: {
            api_hash: { label: 'api_hash', placeholder: '32 letters and digits' },
            api_id: { label: 'api_id', placeholder: 'Digits only, e.g. 1234567' },
            api_key: { label: 'API key', placeholder: 'Paste the key from the provider' },
            api_keys: { label: 'API keys', placeholder: 'One key per line' },
            cx: { label: 'Search engine ID', placeholder: '' },
            from: { label: 'From address', placeholder: 'e.g. leads@example.com' },
            host: { label: 'SMTP server', placeholder: 'e.g. smtp.gmail.com' },
            model: { label: 'Model name', placeholder: 'e.g. gpt-4o' },
            notify_emails: {
                label: 'Send lists to',
                placeholder: 'Separate addresses with commas; empty sends to the sign-in email',
            },
            password: { label: 'Password or app password', placeholder: 'The SMTP password of the mailbox' },
            port: { label: 'Port', placeholder: 'Usually 465 or 587' },
            protocol: { label: 'API protocol', placeholder: '' },
            server_url: { label: 'Endpoint', placeholder: 'https://api.example.com/v1' },
            tls: { label: 'Encryption', placeholder: '' },
            url: { label: 'Proxy URL', placeholder: 'http://user:pass@host:port or socks5://host:port' },
            username: { label: 'Username', placeholder: 'Usually the email address' },
        },
        fieldHelp: {
            'web_search.api_keys': 'Several keys are fine; when one runs out the next is used.',
            'youtube.api_keys':
                'Several keys are fine; when one runs out the next is used. Keys from the same Google project share one quota, so more keys add no quota; ask Google for a free quota increase instead.',
        },
        fieldInvalid: {
            api_hash: 'api_hash is 32 letters and digits',
            api_id: 'api_id is digits only',
            from: 'Enter a valid email address',
            notify_emails: 'Enter valid addresses separated by commas',
            port: 'The port is a number from 1 to 65535',
            url: 'Use http://host:port or socks5://host:port',
        },
        fieldRequired: (field) => `Please enter ${field}`,
        globalInvalid: (card) => `The ${card} key no longer works; the agent may not be able to continue.`,
        globalInvalidLink: 'Update',
        guide: {
            anthropic: 'Sign in to the Anthropic Console (console.anthropic.com) and create a key under API Keys.',
            custom: 'Ask your provider for a key and server URL; the API must be OpenAI-compatible.',
            deepseek: 'Sign in to the DeepSeek platform (platform.deepseek.com) and open API keys.',
            email: 'Turn on SMTP in your mailbox settings (Gmail and others give you an app password to use instead of the sign-in password), then enter the server, port and account here.',
            firecrawl: 'Sign in to Firecrawl (firecrawl.dev) and copy the API key from the Dashboard.',
            gemini: 'Sign in to Google AI Studio (aistudio.google.com) and click “Get API key”.',
            glm: 'Sign in to the Zhipu platform (open.bigmodel.cn) and open API Keys.',
            google: 'Enable the Custom Search API in Google Cloud and create an API key, then create a search engine at programmablesearchengine.google.com and copy its search engine ID.',
            kimi: 'Sign in to the Moonshot AI platform (platform.moonshot.cn) and open API Key management.',
            minimax: 'Sign in to the MiniMax platform (platform.minimaxi.com) and open the API keys page.',
            mistral: 'Sign in to Mistral La Plateforme (console.mistral.ai) and open API Keys.',
            openai: 'Sign in to the OpenAI Platform (platform.openai.com), open API keys and click “Create new secret key”.',
            perplexity: 'Generate a key in the API section of Perplexity settings (perplexity.ai/settings/api).',
            proxy: 'Ask your proxy provider for an HTTP or SOCKS5 address. Leave empty to connect directly.',
            qwen: 'Sign in to Alibaba Cloud Bailian (bailian.console.aliyun.com) and open API-KEY.',
            tavily: 'Sign in to Tavily (tavily.com) and copy the API key from the dashboard; new accounts get free credits.',
            telegram:
                'Sign in at my.telegram.org with your Telegram phone number, open “API development tools”, enter any app name (e.g. KeLai) and submit; the page shows api_id and api_hash.',
            traversaal: 'Request an API key at Traversaal Ares (api.traversaal.ai).',
            xai: 'Sign in to the xAI Console (console.x.ai) and create an API key.',
            youtube:
                'In the Google Cloud console (console.cloud.google.com), pick a project, enable “YouTube Data API v3” under APIs & Services, then create an API key under Credentials.',
        },
        howToApply: 'How to get one',
        keepSaved: 'Leave empty to keep the saved value',
        llmMode: {
            catalog: 'Built-in providers',
            catalogHint: 'Pick OpenAI, Anthropic, Kimi or another built-in provider and paste its API key.',
            custom: 'Custom model API',
            customHint:
                'For relays, self-hosted gateways or any OpenAI- or Anthropic-format API: fill in the endpoint, protocol, model name and API key.',
            overrides: 'Custom settings',
            overridesHint: 'Leave empty to use the provider’s default model and official endpoint.',
            rolesLink: 'Assign models per role',
        },
        llmTopUpNote: 'New accounts need a top-up first, or the test reports no quota.',
        loadFailed: 'Failed to load the key status. Please refresh.',
        noSecretKey:
            'The server has no encryption key yet, so keys cannot be saved. Ask whoever deployed the system to set GROWTH_SECRET_KEY on the server and try again.',
        optional: 'Optional',
        protocol: { anthropic_messages: 'Anthropic Messages', openai_chat: 'OpenAI Chat Completions' },
        provider: {
            embedding: 'Embedding service',
            llm: 'Provider',
            web_search: 'Search service',
        },
        providerLabel: {
            anthropic: 'Anthropic (Claude)',
            custom: 'Custom (OpenAI-compatible)',
            deepseek: 'DeepSeek',
            firecrawl: 'Firecrawl',
            gemini: 'Google Gemini',
            glm: 'Zhipu GLM',
            google: 'Google Custom Search (existing accounts only; Google no longer accepts new customers)',
            googleai: 'Google AI',
            huggingface: 'Hugging Face',
            jina: 'Jina',
            kimi: 'Moonshot Kimi',
            minimax: 'MiniMax',
            mistral: 'Mistral',
            ollama: 'Ollama',
            openai: 'OpenAI',
            perplexity: 'Perplexity',
            qwen: 'Qwen',
            tavily: 'Tavily',
            traversaal: 'Traversaal',
            voyageai: 'Voyage AI',
            xai: 'xAI',
        },
        ready: 'Required items are ready.',
        readyLink: 'Find customers',
        reason: {
            format: 'Wrong format',
            permission: 'The key lacks the required service permission',
            quota: 'The provider reports no balance or quota left',
            rejected: 'The provider rejected this key; it may be incomplete or revoked',
            timeout: 'Test timed out',
            unreachable: 'Cannot reach the provider; check the server network or URL',
        },
        replace: 'Replace key',
        required: 'Required',
        saveAndTest: 'Save & test',
        saved: (card) => `${card} saved and tested`,
        savedButFailed: (card, reason) => `${card} saved, but the test failed: ${reason}`,
        status: {
            connected: 'Connected',
            invalid: 'Invalid',
            unset: 'Not set',
            untested: 'Not tested',
        },
        statusLine: {
            connected: (time) => `Connected, last tested ${time}`,
            invalid: (reason) => `Invalid: ${reason}. Please check and re-enter.`,
            unsetOptional: 'Not set. Works without it, better with it.',
            unsetRequired: 'Not set. The agent cannot work without it.',
            unsetSearch: 'Not set; using free DuckDuckGo and page fetching.',
            untested: 'Saved, not tested yet.',
        },
        telegramNextStep: 'Next: sign in to your Telegram account',
        telegramNote: 'These are app credentials, not a sign-in; sign in with your phone on the Accounts page.',
        test: 'Test connection',
        testEmail: 'Send test email',
        testEmailFailed: 'The test email was not sent; check the mail settings.',
        testEmailSent: (to) => `Test email sent to ${to}`,
        testing: 'Testing…',
        title: 'Keys & connections',
        tlsOption: { none: 'None', starttls: 'STARTTLS (usually port 587)', tls: 'SSL/TLS (usually port 465)' },
        unknownReason: 'Test failed',
    },
    delivery: {
        evidence: 'Each comes with the original message and its link',
        qualified: (n, watch) => `Qualified ${n} · watch list ${watch} (not delivered, still in Prospects)`,
        sources: (n) => `From ${n} groups`,
        title: (n) => `Delivered ${n} prospects`,
    },
    export: {
        allDelivered: 'All delivered',
        allQualified: 'All qualified prospects',
        button: 'Export CSV',
        currentRoundDelivered: 'Delivered this round',
        failed: 'Export failed, please retry',
        fileName: (business, stamp) => `Prospects_${business}_${stamp}.csv`,
        filtered: 'Current filter results',
        header: {
            bio: 'Bio',
            deliveredAt: 'Delivered at',
            evidenceText: (n) => `Evidence ${n} quote`,
            evidenceTime: (n) => `Evidence ${n} time`,
            evidenceUrl: (n) => `Evidence ${n} link`,
            foundIn: 'Found in project',
            foundRound: 'Found in round',
            lastActive: 'Last active',
            name: 'Player name',
            platform: 'Platform',
            score: 'Score',
            signals: 'Signals',
            source: 'Source',
            sourceUrl: 'Source link',
            status: 'Status',
            username: 'Username',
        },
        nothing: 'No prospects to export',
        unnamedBusiness: 'Unnamed business',
    },
    flowTabs: {
        agents: 'Agents',
        assistant: 'Assistant',
        automation: 'Automation',
        dashboard: 'Overview',
        files: 'Files',
        leads: 'Prospects',
        screenshots: 'Screenshots',
        sources: 'Sources',
        tasks: 'Rounds',
        terminal: 'Terminal',
        tools: 'Searches',
        vectorStores: 'Memory',
    },
    helpKeys: {
        loadFailed: 'Could not load the guide; reload to try again.',
        title: 'How to get the keys',
    },
    leads: {
        column: {
            evidence: 'Evidence',
            foundIn: 'Found in',
            lastActive: 'Last active',
            player: 'Player',
            score: 'Score',
            signals: 'Signals',
            source: 'Source',
            status: 'Status',
        },
        detail: {
            breakdown: 'Score breakdown',
            evidence: 'Evidence',
            openInTelegram: 'Open in Telegram',
            profile: 'Profile',
            profileActivity30d: (n) => `${n} messages in 30 days`,
            profileGroups: (n) => `In ${n} related groups`,
            profileLanguages: (languages) => `Languages: ${languages.join(', ')}`,
        },
        email: {
            failed: 'Sending failed, please retry.',
            notConfigured: 'Email notifications are not set up yet; set them up to send lists by email.',
            notConfiguredLink: 'Set up',
            send: 'Send by email',
            sent: (to) => `List sent to ${to}`,
        },
        empty: 'No prospects yet. They appear here live as the agent finds them.',
        emptyFiltered: 'No prospects match the filters.',
        evidenceCount: (n) => `${n}`,
        filter: {
            all: 'All',
            allSources: 'All sources',
            delivered: 'Delivered',
            flags: 'Flags',
            notDelivered: 'Not delivered',
            qualified: 'Qualified',
            watch: 'Watch list',
        },
        flag: {
            competitor: 'Mentions a competitor',
            paying: 'Pays',
            recent: 'Active in 7 days',
        },
        foundIn: (project, round) => `${project} · round ${round}`,
        loadFailed: 'Failed to load prospects. Please refresh.',
        searchPlaceholder: 'Search name, username, quote...',
        signal: {
            competitor: 'Mentions a competitor',
            genre_match: 'Genre match',
            paying: 'Pays',
            player: 'Player',
            recent: 'Recently active',
            switching: 'Wants to switch',
        },
        status: {
            delivered: (date) => `Delivered ${date}`,
            notDelivered: 'Not delivered',
        },
        tier: {
            qualified: 'Qualified',
            watch: 'Watch list',
        },
        total: (n) => `${n} prospects`,
    },
    nav: {
        dashboard: 'Overview',
        favoriteFlows: 'Favorite projects',
        flows: 'Projects',
        knowledges: 'Knowledge',
        newFlow: 'New project',
        recentFlows: 'Recent projects',
        resources: 'Resources',
        settings: 'Settings',
        templates: 'Templates',
    },
    newFlow: {
        assistantHint: 'Chat with the agent and search as you go.',
        automationHint: 'Say one sentence; the agent finds groups, reads messages and delivers the list.',
        chips: [
            'Find me 50 customers',
            'Find 30 players who want to quit or switch games',
            'Find 20 new players asking about similar games',
        ],
        missingAccount: 'No Telegram account is connected, so the agent cannot join groups or read messages yet.',
        missingAccountLink: 'Connect',
        missingProfile: 'No business profile yet, the agent does not know what you offer or who to find.',
        missingProfileLink: 'Fill in',
        pageTitle: 'New lead project',
        placeholder: 'e.g. Find me 50 players who want to switch games',
        subtitle: 'Say in one sentence who you want to find',
        title: 'Find your customers',
    },
    onboarding: {
        stepAccount: 'Connect a Telegram account',
        stepCredentials: 'Fill in keys & connections',
        stepFlow: 'Find customers',
        stepProfile: 'Fill in the business profile',
        title: 'Get started in four steps',
    },
    profile: {
        aiHint: 'All of this is passed to the AI automatically; no template needs editing.',
        defaultExcludes: ['Account sellers', 'Boosters', 'Official competitor accounts', 'Bots'],
        description:
            'Tell the agent what you offer and who to find. Fill it once, then just say “find me 50 customers”.',
        emptyHint: 'No business profile yet. Fill it in so the agent knows which players to look for.',
        field: {
            competitors: {
                help: 'Optional. Players who mention or leave a competitor are flagged',
                label: 'Competitors',
                placeholder: 'e.g. Legend X, World Y',
            },
            defaultTarget: {
                help: 'Used for “find some more”; a number in the sentence wins',
                label: 'Default target per round',
                placeholder: '50',
            },
            excludes: {
                help: 'Matching people are never saved',
                label: 'Exclusion rules',
                placeholder: 'e.g. boosters, account sellers, competitor staff',
            },
            games: {
                help: 'The agent searches player discussions by these names',
                label: 'Game / product names',
                placeholder: 'e.g. Star Era (one per line)',
            },
            genre: {
                help: 'An exact genre match scores higher; an unrelated one is dropped',
                label: 'Genre',
                placeholder: 'Choose a genre',
            },
            languages: { help: 'Which language groups to search', label: 'Languages', placeholder: 'Choose languages' },
            name: { help: 'Tells your businesses apart', label: 'Business name', placeholder: 'e.g. Star Era' },
            notes: {
                help: 'Optional. A paragraph is enough',
                label: 'Anything else',
                placeholder:
                    'Anything the AI should know: what makes the game special, where players hang out, what to avoid',
            },
            pitch: {
                help: 'A sentence or two; it decides which needs to look for',
                label: 'What you want players to know',
                placeholder: 'e.g. A new server opens with gifts for returning players',
            },
            regions: {
                help: 'Several are fine',
                label: 'Target regions',
                placeholder: 'e.g. Southeast Asia, Taiwan, Middle East',
            },
            signals: {
                help: 'Optional. The agent expands keywords on its own',
                label: 'Intent signals',
                placeholder: 'e.g. bored, looking for a new game, quitting',
            },
        },
        genres: ['MMORPG', 'SLG', 'Card', 'FPS', 'MOBA', 'Casual', 'Other'],
        languages: {
            ar: 'Arabic',
            en: 'English',
            id: 'Indonesian',
            ja: 'Japanese',
            ko: 'Korean',
            ms: 'Malay',
            th: 'Thai',
            vi: 'Vietnamese',
            zh: 'Chinese',
        },
        otherLanguage: 'Other',
        otherLanguagePlaceholder: 'Other languages, comma-separated, e.g. Tamil, Persian',
        required: {
            games: 'Enter a game or product name',
            genre: 'Choose a genre',
            languages: 'Choose at least one language',
            name: 'Enter a business name',
            pitch: 'Enter what you want players to know',
            regions: 'Enter a target region',
        },
        save: 'Save',
        saved: 'Business profile saved; it applies from the next round.',
        targetRange: 'Enter a whole number from 1 to 1000',
        title: 'Business profile',
    },
    progress: {
        delivered: 'Delivered',
        deliveredLine: (n, t) => ({ count: n, prefix: 'Delivered this round:', suffix: ` / ${t} prospects` }),
        emailFailed: (error) => `The list email was not sent: ${error}`,
        emailSent: 'List emailed',
        exportCsv: 'Export CSV',
        failed: (reason, n) => `Round stopped: ${reason}. The ${n} saved prospects are kept.`,
        foundLine: (n, t) => ({ count: n, prefix: 'Round progress:', suffix: ` / ${t} prospects` }),
        missed: 'Target missed, see the report for why',
        more: (n) => `Find ${n} more`,
        retry: 'Retry',
        retryText: 'Carry on with the previous round’s target',
        running: 'Running',
        sources: (s) =>
            `Sources: joined ${s.joined} · pending ${s.pending} · not joined ${s.not_joined} · unavailable ${s.unjoinable}`,
        viewLeads: 'View list',
        viewReport: 'View report',
        watch: (n) => `Watch list ${n}`,
    },
    quickActions: {
        changeDirection: 'Try another angle',
        changeDirectionText: 'Try another angle and find new groups and players',
        more: (n) => `Find ${n} more`,
        moreText: (n) => `Find ${n} more`,
        watchOnly: 'Watch list only',
        watchOnlyText: 'Re-check the watch list and promote the ones you can confirm',
    },
    settingsNav: {
        account: 'Account',
        accounts: 'Accounts',
        apiTokens: 'API Tokens',
        credentials: 'Keys & connections',
        growthProfile: 'Business profile',
        prompts: 'Prompts',
        providers: 'Providers',
        settings: 'Settings',
    },
    sources: {
        add: 'Add to pool',
        addedCount: (n) => `Added ${n}; they will be joined and read in the next round`,
        column: {
            joinStatus: 'Join status',
            lastRead: 'Last read',
            leads: 'Prospects',
            members: 'Members',
            origin: 'Origin',
            source: 'Source',
        },
        empty: 'The pool is empty. Groups the agent finds appear here, or paste your own links above.',
        emptyFiltered: 'No sources match.',
        existing: (link) => `${link} is already in the pool`,
        filter: {
            allOrigins: 'All origins',
            allStatuses: 'All join statuses',
        },
        invalid: (link) => `${link} is not a valid Telegram link`,
        joinStatus: {
            joined: 'Joined',
            not_joined: 'Not joined',
            pending_approval: 'Pending approval',
            unjoinable: 'Unavailable',
        },
        joinStatusHint: {
            pending_approval: 'A join request was sent; the system re-checks it periodically',
            unjoinable: 'The group is closed, banned or invite-only; the system will not retry',
        },
        loadFailed: 'Failed to load sources.',
        origin: {
            ai: 'Found by AI',
            manual: 'Added by hand',
        },
        placeholder:
            'Paste Telegram group or channel links, one per line, e.g. https://t.me/xxxx or https://t.me/+invite',
        searchPlaceholder: 'Search name or link...',
        tooMany: 'At most 100 links at a time; please add them in batches.',
        unresolved: (link) => `${link} could not be resolved right now, try again later`,
    },
    status: {
        created: 'Created',
        failed: 'Failed',
        finished: 'Finished',
        running: 'Running',
        waiting: 'Waiting',
    },
    telegram: {
        cliHint: 'Run the growth-tglogin command on the server to sign in; the account then appears in the list above.',
        credentialsMissing: 'Enter the Telegram api_id and api_hash under Keys & connections first',
        credentialsMissingLink: 'Fill in',
        description:
            'The system uses this account to join public groups and read public messages. Credentials stay on your own server.',
        fingerprint: {
            advanced: 'Fingerprint browser (advanced)',
            auto: (os, language, timezone) => `Generated fingerprint: ${os} · ${language} · ${timezone}`,
            externalHint:
                'When set, your own AdsPower/BitBrowser window is used and it manages the proxy and fingerprint.',
            externalPlaceholder: 'ws://127.0.0.1:xxxxx/devtools/browser/…',
            externalWs: 'Fingerprint browser address',
            usingExternal: 'Uses your own fingerprint browser',
        },
        floodWait: (n) => `Telegram asked us to wait ${n}s; we will resume automatically`,
        health: {
            accountName: (name, last4) => `${name} (…${last4})`,
            actionFailed: 'Action failed, please retry.',
            add: 'Add account',
            added: (a, n) => `${a} added; ${n} accounts available.`,
            allLimited: (time) =>
                `All accounts are rate limited; the first recovers at ${time} and the agent continues automatically.`,
            column: {
                account: 'Account',
                actions: 'Actions',
                lastSuccess: 'Last success',
                limit: 'Limit / recovery',
                platform: 'Platform',
                status: 'Status',
            },
            disable: 'Disable',
            disabled: (a) => `${a} disabled.`,
            empty: 'No account yet.',
            enable: 'Enable',
            enabled: (a) => `${a} enabled.`,
            hint: {
                active: 'Working normally',
                disabled: 'Disabled; the agent will not use this account',
                needs_login:
                    'The sign-in expired (password changed, signed out elsewhere or session expired); sign in again',
                rate_limited: (time) =>
                    `Telegram asked us to wait; recovers at ${time}, other accounts are used meanwhile`,
            },
            limitedNoBackup: (a, time) =>
                `${a} is rate limited and no other account is available. Resuming at ${time}.`,
            limitedSwitched: (a, b) => `${a} is rate limited; switched to ${b}. Work continues.`,
            limitedUntil: (time, minutes) => `Limited until ${time} (${minutes} min left)`,
            noneUsable: 'No usable Telegram account; the agent is paused. Sign in again or add an account.',
            note: 'Every extra account is extra insurance. When one is rate limited or signed out, the agent switches to another healthy account and keeps working.',
            onlyOne: 'Only one account connected. Add another so the agent can switch when one is limited.',
            platform: {
                facebook: 'Facebook',
                instagram: 'Instagram',
                telegram: 'Telegram',
                web: 'Web / forums',
                youtube: 'YouTube',
            },
            recovered: (a) => `${a} has recovered and is available again.`,
            recoveredAt: (relative) => `Recovered ${relative}`,
            relogin: 'Sign in again',
            reloginLink: 'Sign in again',
            remoteBrowserPending: 'Remote browser sign-in is not connected yet; this button will open it once it is.',
            signedOutNoBackup: (a) => `${a} signed out and no other account is available. Running projects are paused.`,
            signedOutSwitched: (a, b) => `${a} signed out; using ${b}. Please sign in again.`,
            status: {
                active: 'Healthy',
                disabled: 'Disabled',
                needs_login: 'Sign-in required',
                rate_limited: 'Rate limited',
            },
            title: 'Account health',
        },
        loadFailed: 'Failed to load the account status.',
        login: {
            back: 'Back',
            code: 'Code',
            codeHint: (phone) => `The code was sent to the Telegram app of ${phone} (not by SMS).`,
            codePlaceholder: '5 digits',
            done: (name) => `Connected: ${name}`,
            failed: 'Sign-in failed, please retry.',
            finish: 'Done',
            password: 'Two-step password',
            passwordHint: 'This account has two-step verification; enter the password you set in Telegram.',
            phone: 'Phone number',
            phonePlaceholder: 'With country code, e.g. +8613800000000',
            sendCode: 'Send code',
            signIn: 'Sign in',
            step: (n) => `Step ${n} of 3`,
            title: 'Add a Telegram account',
            titleRelogin: 'Sign in to Telegram again',
        },
        proxy: {
            column: 'Proxy',
            direct: 'Direct',
            edit: 'Proxy',
            field: 'Proxy (optional)',
            global: 'Default proxy',
            hint: 'Empty uses the default proxy from Keys & connections; without one it connects directly.',
            placeholder: 'http://user:pass@host:port or socks5://host:port',
            save: 'Save',
            saved: 'Proxy saved',
            test: 'Test proxy',
            testFailed: (error) => `The proxy cannot be reached: ${error}`,
            testOk: (latencyMs) => (latencyMs === null ? 'The proxy works' : `The proxy works (${latencyMs} ms)`),
            title: (name) => `Proxy for ${name}`,
        },
        remote: {
            done: 'I have signed in',
            failed: 'Sign-in not detected yet; finish signing in inside the window and click again.',
            hint: 'Below is a browser window opened on the server. Sign in as usual (including any code), then click “I have signed in”. Closing the window does not keep the sign-in.',
            loading: 'Opening the browser window…',
            placeholder: 'The browser service is not connected yet; the sign-in page will show here.',
            title: (platform) => `Sign in to ${platform}`,
            unavailable: 'The remote browser cannot be opened right now, please retry later.',
        },
        remoteLogin: 'Remote sign-in',
        sections: {
            facebook:
                'Signs in through a remote browser. The system uses the account to browse public pages and groups.',
            instagram:
                'Signs in through a remote browser. The system uses the account to browse public profiles and comments.',
            telegram:
                'Signs in with a phone number and code. Enter api_id and api_hash under Keys & connections first.',
            web: 'No account needed. The agent reads public web pages and forum posts directly.',
            youtube: 'No sign-in needed; the YouTube API key from Keys & connections reads comments on public videos.',
        },
        title: 'Accounts',
        youtubeKey: { connected: 'API key works', link: 'Open Keys & connections', missing: 'No YouTube API key yet' },
    },
    templates: {
        presets: [
            {
                text: `Find me {{TARGET_COUNT}} {{GENRE}} players who want to switch games

Steps:
1. Expand keywords from the game names, competitors and intent signals in the business profile
2. Search public Telegram groups and channels about them and join the ones you can
3. Read recent messages and find players who complain about their game, ask for recommendations or say they are quitting
4. Every prospect needs the original message and its link; skip account sellers, boosters, official competitor accounts and bots
5. Deliver the list once the target is reached`,
                title: 'Players who want to switch',
            },
            {
                text: `Find {{TARGET_COUNT}} players talking about {{COMPETITOR}}

Steps:
1. Find {{COMPETITOR}} player groups, trading groups and guide channels
2. Focus on messages unhappy with {{COMPETITOR}}: pay-to-win, a bad patch, wanting to leave
3. Prefer players who show they pay (top-ups, monthly cards, bundles)
4. Every prospect needs the original message and its link and is flagged as mentioning a competitor
5. Deliver the list once the target is reached`,
                title: 'Competitor players',
            },
            {
                text: `Find {{TARGET_COUNT}} new {{GENRE}} players in {{REGION}} who speak {{LANGUAGE}}

Steps:
1. Expand keywords in {{LANGUAGE}} and search local game groups and channels in {{REGION}}
2. Find players asking for a good {{GENRE}}, asking for recommendations or just starting
3. Keep only people who posted in the last 30 days
4. Every prospect needs the original message and its link
5. Deliver the list once the target is reached`,
                title: 'New players in a new region',
            },
            {
                text: `Add these groups to the source pool and find prospects in them:
{{GROUP_LINKS}}

Steps:
1. Join the groups above and note why any could not be joined
2. Read each group's recent messages and find players with purchase intent
3. Score them by the business profile; save the qualified, keep the watch list
4. Deliver the list once {{TARGET_COUNT}} are found`,
                title: 'Prospects from given groups',
            },
            {
                text: `Find me {{TARGET_COUNT}} veteran players who pay

Steps:
1. Search groups where {{GENRE}} veterans gather (guild, server and trading groups)
2. Find players who mention top-ups, monthly cards, bundles or gacha spending
3. Prefer people active in the last 7 days with an exact genre match
4. Every prospect needs the original message and its link; skip account sellers and boosters
5. Deliver the list once the target is reached`,
                title: 'Players likely to pay',
            },
        ],
        presetsTitle: 'Preset templates',
    },
    tool: {
        growth_add_sources: 'Add sources',
        growth_deliver_leads: 'Deliver list',
        growth_get_progress: 'Check progress',
        growth_join_source: 'Join source',
        growth_list_leads: 'List prospects',
        growth_search_sources: 'Search sources',
        growth_sync_source: 'Read messages',
        growth_upsert_leads: 'Save prospects',
    },
};

export const dictionaries = { en, zh } as const;

export const copy: Copy = zh;

// Accepts both the AgentType value (`primary_agent`) and the camelCase key of the provider and prompt configs
// (`primaryAgent`); a key with no entry, such as the `simple` config, falls back to `fallback`.
export const agentLabel = (type?: null | string, fallback = type ?? ''): string => {
    if (!type) {
        return fallback;
    }

    const key = type.replaceAll(/([A-Z])/g, '_$1').toLowerCase();

    return copy.agent[key] ?? fallback;
};

export const statusLabel = (status?: null | string): string => (status ? (copy.status[status] ?? status) : '');

export const toolLabel = (name?: null | string): string => (name ? (copy.tool[name] ?? name) : '');
