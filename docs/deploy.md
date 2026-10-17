# Deploying KeLai AGI

This guide is written for the person who deploys KeLai AGI and for an AI assistant helping them. Every command is meant to be copied as it stands and run from the repository root unless a step says otherwise. The same stack runs on a laptop and on a server; only the addresses in step 5 differ.

## 1. What gets installed

| Container | What it does | Port |
|---|---|---|
| `pentagi` | The KeLai AGI application: web interface, API and the agents | `8443` (HTTPS) |
| `pgvector` | PostgreSQL database with vector search: leads, sources, settings, memory, knowledge base | internal only |
| `scraper` | Built-in headless browser the agents use to read public web pages | internal only |
| `browser` | Keeps one logged-in browser per Facebook, Instagram and TikTok account; serves the login window | `7070` |
| `growth-worker` | Not a service: the image each task's sandbox container is started from. It exits right after `up`, which is expected | — |
| `pgexporter` | Optional database metrics, started only with `--profile monitoring` | — |

Language models run at the provider you configure (DeepSeek, OpenAI and so on), so no GPU is needed.

## 2. Hardware

| | Minimum | Recommended |
|---|---|---|
| CPU | 2 cores | 4 cores |
| Memory | 4 GB | 8 GB; 16 GB when many platform accounts work at the same time |
| Free disk | 40 GB | 80 GB on SSD |
| System | Linux x86_64 (Ubuntu 22.04 or 24.04) | the same; macOS and Windows work through Docker Desktop |

Where the numbers come from (measured on a test deployment unless marked):

- Images on disk, uncompressed:
  - `browser` 3.7 GB
  - `scraper` 4.7 GB
  - `pgvector` 0.6 GB
  - `pentagi` about 1 GB
  - `growth-worker` about 1.5 GB, estimated, because it carries Chromium and Python tools
  - Together about 11.5 GB.
- The first build leaves about 7.5 GB of build cache. `docker builder prune` removes it once the images are built.
- Memory at rest:
  - `pentagi` 150 MB
  - `pgvector` 60 MB
  - `scraper` 285 MB
  - `browser` 45 MB
  - About 0.6 GB together.
- Memory while running a task: the application stayed near 155 MB. Each task adds its own sandbox container, which lives until the task is deleted. The browser service grows to about 190 MB with one open page; a logged-in social platform page usually takes a few hundred MB per account.
- The first build compiles the Go server and the web interface, and is the heaviest step: keep about 4 GB of memory free while it runs (estimated).

## 3. Install Docker

Linux (Ubuntu):

```bash
curl -fsSL https://get.docker.com | sudo sh
sudo usermod -aG docker "$USER"
newgrp docker
docker compose version
```

`docker compose version` must print v2 or newer.

macOS or Windows:

1. Install Docker Desktop.
2. On Windows, enable WSL 2 when the installer asks.
3. In Docker Desktop, open Settings → Resources and give it at least 8 GB of memory and 60 GB of disk.

## 4. Get the code

```bash
git clone https://github.com/827aaron/huoke-agi.git kelai-agi
cd kelai-agi
cp .env.example .env
```

If the repository is private, the GitHub account doing the clone needs access to it.

## 5. Fill in `.env`

Open `.env` in a text editor. Three things are required. Everything else is optional or generated on first start.

### Required

| What | Variable | Notes |
|---|---|---|
| One language model key | for example `DEEPSEEK_API_KEY=` or `OPEN_AI_KEY=` or `ANTHROPIC_API_KEY=` | Can instead be entered after login under Settings → Keys & connections → Language model |
| Judgement model (Jev) key | `TYPESAFE_API_KEY=`, a TypeSafe key; or an OpenRouter key with `TYPESAFE_API_URL=https://openrouter.ai/api` | Screens every message read from a group; can also be entered on the settings page. Without it, reading groups stops with "no judgement model (Jev) is configured" |
| Embedding key for memory and the knowledge base | `EMBEDDING_KEY=` (an OpenAI key; `EMBEDDING_PROVIDER` defaults to `openai`) | Not needed when the language model is OpenAI. Without it, memory and knowledge base tools are switched off and only a log line says so. Usage is tiny |

### Change before the first start

Generate each value with `openssl rand -hex 32`.

| Variable | Why |
|---|---|
| `COOKIE_SIGNING_SALT=` | Signs login sessions |
| `PENTAGI_POSTGRES_PASSWORD=` | Database password. Set it before the first start, because the database keeps the first one |
| `LOCAL_SCRAPER_USERNAME=` / `LOCAL_SCRAPER_PASSWORD=` | Built-in browser login. Put the same pair into `SCRAPER_PRIVATE_URL=https://USER:PASS@scraper/` |

### Generated for you

`GROWTH_SECRET_KEY` (encrypts every key saved on the settings page) and `BROWSER_TOKEN` (between the application and the browser service) are generated on first start and kept in the data volumes. Leave them empty. Back up the volumes as described in step 10: losing `growth_secret_key` means entering every saved key again.

### Deploying on a server

Set these when people will open the interface from other computers:

```ini
PENTAGI_LISTEN_IP=0.0.0.0
PUBLIC_URL=https://SERVER_ADDRESS:8443
BROWSER_LISTEN_IP=0.0.0.0
BROWSER_PUBLIC_URL=http://SERVER_ADDRESS:7070
```

Open ports 8443 and 7070 in the firewall. Port 7070 serves the window an operator uses to log in to Facebook, Instagram or TikTok. Each login link carries a one-time ticket.

### Optional

All of these can also be entered on the settings page:

- Telegram: `TG_API_ID`, `TG_API_HASH`, from my.telegram.org.
- YouTube: `YOUTUBE_API_KEY`.
- Search: `TAVILY_API_KEY`, `GOOGLE_API_KEY` and others. DuckDuckGo works without a key.
- Proxy: `GROWTH_PROXY_URL`, the default proxy for platform accounts.
- Email: see below.

### Email for lead lists

Set the mail server once in `.env`, and every customer on this deployment can receive lead lists without running a mail server of their own:

```ini
SMTP_HOST=smtp.example.com
SMTP_PORT=587
SMTP_USERNAME=leads@example.com
SMTP_PASSWORD=
SMTP_FROM=KeLai AGI <leads@example.com>
SMTP_TLS=starttls
```

`SMTP_TLS` is `starttls` for port 587, `tls` for port 465, or `none`. With this set, a customer enters only the addresses that should receive the lists on the Email card; left empty, lists go to their login email. A customer can still enter their own server on that card, and theirs is then used for their mail. With `SMTP_HOST` empty, a customer who enters no server of their own gets no email; their lead lists stay on the page. Check the server with the card's test button, which sends a test message.


The defaults already in `.env.example` keep these:

- `ASSISTANT_USE_AGENTS=true`
- `HTTP_CLIENT_TIMEOUT=300`
- `UPDATE_CHECK_INTERVAL` empty: the server sends nothing out on its own.

## 6. Build and start

```bash
docker compose build
docker compose up -d
docker compose ps
docker compose logs -f pentagi
```

The first build takes 10–30 minutes depending on the machine and the network. Stop following the log with Ctrl+C once it prints `API server listening on 0.0.0.0:8443`. In `docker compose ps`, `pentagi`, `pgvector`, `scraper` and `browser` are `running`, and `growth-worker` has exited with code 0.

## 7. First login

1. Open `https://localhost:8443`, or the `PUBLIC_URL` you set.
2. The certificate is self-signed, so the browser warns once. Continue to the site.
3. Log in as `admin@pentagi.com` with password `admin`.
4. You are asked to set a new password at once. A valid password is either 16 or more characters of any kind, or 8–15 characters containing a lowercase letter, an uppercase letter, a digit and one of `!@#$&*`.

## 8. What to fill in after login

Every card in Settings → Keys & connections has a test button and says whether it can be left empty.

1. **Keys & connections:**
   - Language model, with the embedding model under its advanced section.
   - Judgement model (Jev).
   - Telegram, YouTube.
   - Search.
   - Email.
   - Proxy.
2. **Business profile:** the product and its audience, target languages and regions, signal words, exclusions and notes. Every task reads it, and a starting profile for a game publisher or a licensed online casino can be copied in. An exclusion applies only when a message says so explicitly; a person is never excluded on a guess from their writing style, apparent age or the platform they post on.
3. **Accounts:**
   - Telegram signs in with a phone code.
   - Facebook, Instagram and TikTok sign in through the login window served by the browser service.
   - Each account can have its own proxy.
4. **Model services** (optional): a different model per agent role.

To start work, open New task, pick a preset from Templates and send it. When using the assistant, keep the switch that lets specialists collaborate turned on. Only the specialists can join groups, read messages and save leads.

## 9. Accounts for colleagues and customers

Give each person their own login instead of sharing the administrator's. Only an administrator can create accounts.

The interface has no user management page yet; one is planned. Until it ships, create accounts on the API page that comes with the server:

1. Log in to KeLai AGI as the administrator in your browser.
2. In the same browser, open `https://localhost:8443/api/v1/swagger/index.html`, or the same path under your `PUBLIC_URL`. It uses the login you just made, so no token is needed.
3. Open `POST /users/`, press Try it out, replace the example body with the one below, and press Execute.
4. The response shows `"status": "success"`. Send the person the address, the email and the starting password.

```json
{
  "mail": "colleague@example.com",
  "name": "Colleague",
  "password": "Welcome2Kelai!",
  "role_id": 2,
  "status": "active",
  "type": "local",
  "password_change_required": true
}
```

- `role_id` `2` is an ordinary user. `1` is an administrator, who can also create and block accounts.
- `status` must be `active`, otherwise the person cannot log in.
- `password` must meet the policy in step 7.
- `password_change_required: true` makes the person choose their own password at first login.

Without a browser, for example from an AI assistant on the server, the same request works with `curl`:

```bash
curl -sk -c jar -H 'Content-Type: application/json' \
  -d '{"mail":"admin@pentagi.com","password":"YOUR_ADMIN_PASSWORD"}' \
  https://localhost:8443/api/v1/auth/login
curl -sk -b jar -H 'Content-Type: application/json' \
  -d '{"mail":"colleague@example.com","name":"Colleague","password":"Welcome2Kelai!","role_id":2,"status":"active","type":"local","password_change_required":true}' \
  https://localhost:8443/api/v1/users/
rm jar
```

To stop someone from logging in:

1. Find their `hash` with `GET /users/`, setting `page` to `1` and `type` to `init`.
2. Send `PUT /users/{hash}` with their record from that list and `status` changed to `blocked`. This also ends the sessions they already have open.
3. Set `status` back to `active` to let them in again, or use `DELETE /users/{hash}` to remove the account.

**Google and GitHub sign-in.** If `OAUTH_GOOGLE_CLIENT_ID` or `OAUTH_GITHUB_CLIENT_ID` is set, anyone who can open the login page and has a verified Google or GitHub email gets an ordinary account automatically, the first time they sign in. There is no list of allowed addresses. Leave those variables empty unless only your own team can reach the server, and block unwanted accounts as described above.

## 10. Backup and restore

The volumes carry the folder name as a prefix. List them with `docker volume ls`. The ones to keep:

| Volume | Holds |
|---|---|
| `<prefix>_pentagi-postgres-data` | the database |
| `<prefix>_pentagi-data` | `growth_secret_key`, Telegram sessions, task files |
| `<prefix>_browser-profiles` | logged-in platform browsers |
| `<prefix>_browser-token` | the browser service token |

Back up (replace `postgres` and `pentagidb` if you changed `PENTAGI_POSTGRES_USER` or `PENTAGI_POSTGRES_DB`):

```bash
mkdir -p backup
docker compose exec -T pgvector pg_dump -U postgres pentagidb > backup/db.sql
docker compose stop
for v in pentagi-data browser-profiles browser-token; do
  docker run --rm -v "$(basename "$PWD")_$v":/v -v "$PWD/backup":/b alpine tar czf "/b/$v.tgz" -C /v .
done
docker compose start
```

Restore on a new machine:

1. Follow steps 3–5, using the same `.env`.
2. Run `docker compose up -d pgvector`.
3. Run `docker compose exec -T pgvector psql -U postgres pentagidb < backup/db.sql`.
4. Run the loop above with `tar xzf "/b/$v.tgz" -C /v`.
5. Run `docker compose up -d`.

## 11. Upgrade

```bash
git pull
docker compose build
docker compose up -d
```

Back up first. Database changes apply by themselves when the application starts, and a failed change stops the start with a message in `docker compose logs pentagi`.

## 12. Troubleshooting

| Symptom | Cause and fix |
|---|---|
| The assistant does the work itself and no leads appear | The switch that lets specialists collaborate is off for that assistant. Turn it on in the assistant input, or set `ASSISTANT_USE_AGENTS=true` in `.env` and restart |
| A task reports "no judgement model (Jev) is configured" | Enter the Jev key on the settings page or set `TYPESAFE_API_KEY` |
| Telegram login says it cannot reach Telegram, or times out | The server cannot reach Telegram directly. Give the account a proxy on the Accounts page, or set `GROWTH_PROXY_URL`, and use the proxy test button |
| Facebook, Instagram or TikTok login link does not open | Run `docker compose ps browser`. On a server, `BROWSER_PUBLIC_URL` must be the server address, not `localhost`, and port 7070 must be open |
| `browser` stays in a waiting state | It waits until `pentagi` has written the token on its first start. Check that `pentagi` is running |
| Saving a key on the settings page fails | The data volume is not writable, so the encryption key could not be stored. See `docker compose logs pentagi` |
| Memory and knowledge base never show up | No embedding model. Fill in the embedding model under the language model card on the settings page, set `EMBEDDING_KEY`, or choose OpenAI or OpenRouter as the language model |
| A forum such as Pantip shows "not allowed to access this page" and the researcher skips it | The site blocks data-centre IP addresses. The proxy on the Accounts page covers platform accounts, not the researcher's own browsing, so run the server on a residential connection or route the host's outbound traffic through a residential proxy or VPN |
| A task cannot start its sandbox | The worker image is missing: run `docker compose build growth-worker`. `pentagi` needs `/var/run/docker.sock`, mounted by the compose file |
| A specialist hangs for minutes | A model call waits at most `HTTP_CLIENT_TIMEOUT` seconds, 300 by default. Check the model provider's status and key |
| Port 8443 is taken | Set `PENTAGI_LISTEN_PORT` to another port and update `PUBLIC_URL` |
| Disk fills up | Run `docker builder prune` after building, and delete finished tasks you no longer need: each keeps a sandbox container |
| Image downloads fail or crawl | Configure a Docker registry mirror close to the server, then run the build again |
