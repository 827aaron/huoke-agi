# Browser service

A small HTTP service that keeps one persistent Chromium profile per platform account and lets the growth connectors (`backend/pkg/growth/connectors/browser.go`, `facebook.go`, `instagram.go`, `tiktok.go`) drive it. An operator logs each account in once through the login page; the agents reuse that session and never fill a login form or a verification code.

## Run

```bash
npm ci
BROWSER_TOKEN=change-me BROWSER_PROFILES_DIR=/data/profiles npm start
npm test   # needs a Chromium that playwright-core 1.56.1 can find
```

The Dockerfile builds on the Playwright image of the same version. Keep `/data/profiles` on a volume: it holds the logged-in profiles.

## Configuration

Every pacing value is a default an operator can change, not a quota.

| Variable | Default | Meaning |
|---|---|---|
| `BROWSER_TOKEN` | empty | Bearer token for every route except `/healthz`; empty accepts any request |
| `BROWSER_PUBLIC_URL` | empty | Base URL operators reach the service at, used in login links; empty gives relative links |
| `BROWSER_PROFILES_DIR` | `./data/profiles` | One directory per account: `profile/` and `meta.json` |
| `BROWSER_HEADLESS` | `true` | Set `false` under Xvfb to run a headed browser |
| `BROWSER_LOCALE`, `BROWSER_TIMEZONE` | `en-US`, `America/New_York` | Locale and timezone of a new fingerprint whose region is unknown |
| `BROWSER_PROXY` | none | Proxy for accounts that were never given one |
| `BROWSER_GEO_LOOKUP_URL` | `https://ipinfo.io/json` | Where an account's exit IP is placed, asked through its proxy; ipinfo.io, ip-api.com and ipapi.co answers are read |
| `BROWSER_ACTION_DELAY_MIN_MS` / `_MAX_MS` | 1500 / 4500 | Random pause before every agent action |
| `BROWSER_PACE_FACTOR_<PLATFORM>` | from the platform file | Multiplies the pause for one platform, e.g. `BROWSER_PACE_FACTOR_TIKTOK=1`; TikTok's file sets 2 |
| `BROWSER_WARMUP_DAYS`, `BROWSER_WARMUP_FACTOR` | 7, 2.5 | For this many days after a login the pauses are this many times longer |
| `BROWSER_RATE_LIMIT_BACKOFF_SEC` | 3600 | Cool-down after a page says the account is temporarily blocked |
| `BROWSER_LOGIN_TICKET_TTL_SEC` | 1800 | Lifetime of a login link |
| `BROWSER_IDLE_CLOSE_MS` | 600000 | Close an account's browser after this much idle time |
| `BROWSER_NAV_TIMEOUT_MS` | 45000 | Navigation timeout |

Actions on one account always run one at a time.

## API

All bodies are JSON. Every response to an account route carries `account`, the health signal: `{account_id, platform, status: active|needs_login|rate_limited, retry_after_sec, challenge, reason, warming_up}`. Errors are `{"error": {"code", "message", "retry_after_sec"?, "account_state"?}, "account"?}` with codes `bad_request` 400, `unauthorized` 401, `not_found` 404, `needs_login` 423, `rate_limited` 429 (also `Retry-After`), `unavailable` 502 (an external browser cannot be reached), `timeout` 504, `internal` 500.

| Route | Body | Returns |
|---|---|---|
| `POST /sessions/{id}/navigate` | `platform` (required the first time), `url`, `wait_ms` | `url, title, http_status, captures[], cursor` |
| `POST /sessions/{id}/scroll` | `times`, `wait_ms`, `selector` (a panel that scrolls on its own; the page when nothing matches) | `url, reached_end, captures[], cursor` |
| `POST /sessions/{id}/content` | `format: html\|text`, `selector` | `url, title, format, body` |
| `POST /sessions/{id}/click` | `selector` or `role`+`name` or `text`, `exact`, `wait_ms` | `url, captures[], cursor` |
| `POST /sessions/{id}/type` | target as for click, `text`, `submit` | `url, captures[], cursor` |
| `POST /sessions/{id}/captures` | `since` | `captures[], cursor` |
| `POST /sessions/{id}/health` | `platform`, `url` | the account record, after opening the platform home page if idle |
| `GET /sessions/{id}` | | the account record |
| `GET /sessions` | | every account record |
| `GET /sessions/{id}/screenshot.png` | | PNG |
| `POST /sessions/{id}/login/start` | `platform`, `url` | the account record plus `login_url` and `expires_at` |
| `POST /sessions/{id}/login/confirm` | | the account record; when the page is logged in, `state` becomes `ok`, the login link stops working and the browser closes so the profile is written |
| `POST /sessions/{id}/login/cancel` | | the account record, unchanged; the login link stops working and the browser closes |
| `POST /sessions/{id}/config` | `platform`, account settings | the account record |
| `POST /sessions/{id}/close` | | the account record |
| `DELETE /sessions/{id}` | | `204`; the browser closes and the profile directory goes, fingerprint and position with it; an account the service never held is `204` too. Service token only |

`captures` are the JSON responses the page fetched from the platform's own API (Facebook `/api/graphql/`, Instagram `/api/v1/` and `/graphql/query`, TikTok `/api/search/`, `/api/comment/list/`, `/api/post/item_list/`), which the connectors parse instead of the DOM.

An account record is `{account_id, platform, state: new|ok|needs_login, state_reason, login_confirmed_at, cooldown_until, warming_up, open, url, proxy, external_browser, fingerprint}`, with the proxy password masked. Agent routes refuse an account whose state is not `ok` with `needs_login`, and one in a cool-down with `rate_limited`.

## Operator login

`login/start` opens the platform's login page in the account's browser and returns `login_url`, a page that shows that browser and forwards the operator's clicks and keys. The link carries a ticket that reaches only that account's login page, its screenshot, its input and `login/confirm`. When the operator presses "我已登录完成", or closes the window, the service checks the page; a logged-in page sets the account to `ok` and agent work on it resumes.

## Proxy, fingerprint and external browsers

Every POST made with the service token may carry account settings; they are stored in `meta.json`, and a change closes the account's running browser so the next action relaunches it. A login ticket cannot change them. The operator login window is the same browser, so login and later automation share one exit IP and one fingerprint.

- `proxy`: `http://`, `https://` or `socks5://` with optional `user:pass@`; empty means a direct connection, and an account never given one uses `BROWSER_PROXY`. Chromium cannot authenticate to SOCKS5, so a socks5 proxy with credentials is refused.
- `region`: the proxy's ISO country. It only shapes the first fingerprint.
- `fingerprint`: generated once, when the account first launches, and kept. [fingerprint-suite](https://github.com/apify/fingerprint-suite) (Apache-2.0) draws a desktop Chrome of the engine's own version for the account's OS and locale, with the screen, cores, memory, WebGL card, codecs and fonts seen together on real devices, and injects it into every page (`version: 2`, the drawn set under `suite`). When its data has no device for the engine version, a small built-in table is used instead (`version: 1`). The top-level fields (`user_agent`, `platform`, `locale`, `timezone`, `viewport`, `screen`, `device_scale_factor`, `hardware_concurrency`) win over the suite's copies, so a fingerprint the backend edited is what the page sees. Locale and timezone follow `region` when it is in the table in `src/profile.js`. The backend stores the generated one from the account record and may send it back to restore it.
- `geolocation`: with a proxy, the exit IP is looked up through that proxy on the first launch and the account is placed 20–80 m from where the lookup puts it, at a random bearing, and kept in `meta.json` until the proxy changes. A page that asks for the position gets it without a prompt. Without a proxy no position is claimed, and a failed lookup leaves it unset until the next launch. Timezone and locale still follow `region`.
- `external_browser_ws`: a CDP endpoint of a fingerprint browser (AdsPower, BitBrowser local API). The service connects to it instead of launching Chromium and applies none of its own proxy or fingerprint settings, since that browser owns them. Closing the account disconnects without closing that browser. Empty switches back to the built-in browser.

## Platforms

Each platform is one file in `src/platforms/`, named after it and loaded at start; adding a platform is adding a file. A file exports:

| Field | Meaning |
|---|---|
| `home`, `login` | The page a health check opens and the page the operator login window opens |
| `capture` | URL patterns of the platform's own API responses that are captured |
| `loginPaths` | Paths that mean the account was logged out or sent to a checkpoint |
| `challengeText` | Page text of a verification (a slider puzzle); stops the account as `needs_login` for an operator |
| `rateLimitText` | Page text that starts the cool-down |
| `needsLogin` | `false` for public pages that need no operator login (default `true`) |
| `agentPaths` | When set, the only paths an agent may `navigate` to; the operator login window is not limited |
| `block` | Playwright resource types never fetched for agent work, e.g. `media`, `image`, `font`; the operator login window loads them so a puzzle shows |
| `paceFactor` | Multiplies the pause before every agent action (default 1) |
| `initScript` | Script run in every page before the platform's own |

TikTok blocks media, images and fonts, keeps its players from starting, opens only search, creator pages and single videos (whose page holds the comments), and pauses twice as long as the other platforms. Its verification puzzles stop the account until an operator solves one in the login window.
