# KeLai AGI (客来 AGI)

<p align="center"><img src="frontend/public/favicon/web-app-manifest-192x192.png" alt="KeLai AGI" width="96" height="96"></p>

## 中文简介

客来 AGI 是一个帮游戏厂商找玩家的获客智能体。你在网页上说一句“帮我找 50 个对这类游戏感兴趣、愿意付费的玩家”，它会用你授权的账号去找公开的群组、论坛和频道，读里面的公开消息，判断谁有购买意向，最后把带原文证据和联系方式的线索名单交给你（页面下载 CSV，也可以发到邮箱）。

- 来源平台：Telegram、网页和论坛、Facebook、Instagram、YouTube、TikTok。
- 每条消息先由判断模型 Jev 初筛打分，过线的才交给大模型写理由，费用低。
- 可以设定时任务，每天或每周自动跑，到结束时间自动交付。
- 用 Docker 部署，所有密钥都可以在网页设置页里填。部署步骤见 [docs/deploy.md](docs/deploy.md)。

## What it does

KeLai AGI is a lead-finding agent for game studios. You describe who you are looking for in plain words, for example "find 50 players who like gacha RPGs and pay for them", and the agents:

1. Search for public groups, forums, channels and pages where those players talk.
2. Join or open them with the accounts you connected, and read the public messages.
3. Screen every message for buying intent with a small, cheap judgement model (Jev), and score each author against your business profile.
4. Write the evidence and the reason for every qualified lead, with the contacts found.
5. Deliver the list: a table in the web interface, a CSV download, and an email with the CSV when a mail server is configured.

A task can run once or on a schedule (daily, on weekdays, weekly or on one date) with an end time by which whatever was found is delivered.

## Source platforms

| Platform | How it is read |
|---|---|
| Telegram | Your own Telegram accounts, signed in with a phone code |
| Web pages and forums | The built-in browser and web search |
| Facebook, Instagram | A logged-in browser per account, kept by the browser service |
| YouTube | The official YouTube Data API with your API keys |
| TikTok | A logged-in browser per account, at a slower pace |

Every account can have its own proxy. Pacing between requests and joins is built in and can be tuned on the settings page; risk reminders warn, they do not block.

## How it works

A task is planned by a lead planner, which hands sub-tasks to specialists: a source scout that finds and lists sources, a prospect researcher that joins sources, reads messages and saves leads, and a reporter that assembles the delivery. The assistant chat can do all of this directly or hand work to the same specialists. Each task runs in its own sandbox container. Leads, sources, memory and the knowledge base live in PostgreSQL with vector search.

## Requirements

- Docker with Compose.
- 2 CPU cores, 4 GB memory and 40 GB free disk at minimum; 4 cores, 8 GB and 80 GB on SSD recommended.
- No GPU: language models run at the provider you choose.

## Keys

| Key | Required | Notes |
|---|---|---|
| Language model | yes | DeepSeek, OpenAI, Anthropic, Gemini, Qwen and others, or an OpenAI-compatible gateway such as OpenRouter |
| Judgement model (Jev) | yes | A TypeSafe key, or an OpenRouter key |
| Embedding model | yes | Used by memory and the knowledge base |
| Telegram `api_id` / `api_hash` | for Telegram | From my.telegram.org |
| YouTube Data API key | for YouTube | From Google Cloud, free quota |
| Web search | no | DuckDuckGo works without a key; Tavily and others are optional |
| Mail server | no | For emailing lead lists |
| Proxy | no | Global, or per account |

All of them can be entered after login under Settings → Keys & connections, or in `.env` before the first start.

## Quick start

```bash
git clone https://github.com/827aaron/huoke-agi.git
cd huoke-agi
cp .env.example .env    # set the values listed in docs/deploy.md, step 5
docker compose build
docker compose up -d
```

Open `https://localhost:8443`, log in as `admin@pentagi.com` with password `admin`, and set a new password when asked. Then fill in the keys, the business profile and the accounts, and start a task from one of the presets under Templates.

The full guide, with server deployment, accounts for colleagues and customers, backup, upgrade and troubleshooting, is [docs/deploy.md](docs/deploy.md). Per-provider model settings are in [docs/providers.md](docs/providers.md).

## Development

```bash
# backend/
go generate ./cmd/installer/files/
go build -trimpath -o pentagi ./cmd/pentagi
go test ./...

# frontend/
corepack enable
pnpm install
pnpm run dev            # http://localhost:8000
pnpm run test
```

Conventions, the test tiers and the code generators are described in [CLAUDE.md](CLAUDE.md).

## Responsible use

KeLai AGI reads public conversations through accounts you own. Make sure the way you use it follows each platform's terms and the law where you and your prospects are.

## License

MIT, see [LICENSE](LICENSE). KeLai AGI is derived from the open-source project PentAGI by the PentAGI Development Team; its copyright notice is kept in [LICENSE](LICENSE) and [NOTICE](NOTICE). Third-party licenses are listed in [licenses/](licenses/).
