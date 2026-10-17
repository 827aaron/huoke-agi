import { after, before, describe, test } from 'node:test';
import net from 'node:net';
import { chromium } from 'playwright-core';
import assert from 'node:assert/strict';
import http from 'node:http';
import fs from 'node:fs/promises';
import os from 'node:os';
import path from 'node:path';
import { loadConfig } from '../src/config.js';
import { createServer } from '../src/server.js';
import { SessionManager } from '../src/sessions.js';
import { PLATFORMS } from '../src/detect.js';
import { distance } from '../src/geo.js';

const TOKEN = 'test-token';

const FEED = `<!doctype html><html><body>
<div id="posts" style="min-height:2000px"></div>
<script>
let page = 0, loading = false;
async function more() {
  if (loading || page >= 3) return;
  loading = true;
  const res = await fetch('/api/graphql/', { method: 'POST', body: 'page=' + page });
  const data = await res.json();
  for (const p of data.posts) {
    const div = document.createElement('div');
    div.className = 'post';
    div.style.height = '700px';
    div.textContent = p;
    document.getElementById('posts').appendChild(div);
  }
  page += 1;
  loading = false;
}
more();
window.addEventListener('scroll', () => {
  if (window.scrollY + window.innerHeight > document.documentElement.scrollHeight - 300) more();
});
</script></body></html>`;

const WHOAMI = `<!doctype html><pre id="me"></pre><script>
document.getElementById('me').textContent = JSON.stringify({
  ua: navigator.userAgent, platform: navigator.platform, cores: navigator.hardwareConcurrency,
  language: navigator.language, tz: Intl.DateTimeFormat().resolvedOptions().timeZone,
  screen: screen.width + 'x' + screen.height, dpr: window.devicePixelRatio,
  gpu: (() => {
    const gl = document.createElement('canvas').getContext('webgl');
    const ext = gl && gl.getExtension('WEBGL_debug_renderer_info');
    return ext ? gl.getParameter(ext.UNMASKED_RENDERER_WEBGL) : '';
  })(),
});
</script>`;

const TIKTOK_VIDEO = `<!doctype html><html><head>
<style>@font-face { font-family: f; src: url(/media/f.woff2); } body { font-family: f; }</style></head><body>
<img src="/media/cover.jpg" width="100" height="100">
<video src="/media/clip.mp4" autoplay muted></video>
<div id="comments" style="height:300px;overflow:auto"><div style="height:3000px">comments</div></div>
<script>fetch('/api/comment/list/?aweme_id=123&cursor=0').then((r) => r.json());</script>
</body></html>`;

const WHERE = `<!doctype html><pre id="me">{}</pre><script>
navigator.geolocation.getCurrentPosition(
  (p) => { document.getElementById('me').textContent = JSON.stringify({ latitude: p.coords.latitude, longitude: p.coords.longitude }); },
  (e) => { document.getElementById('me').textContent = JSON.stringify({ error: e.code }); },
  { timeout: 2000 },
);
</script>`;

const PAGES = {
  '/where': WHERE,
  '/@creator/video/123': TIKTOK_VIDEO,
  '/@creator/video/124': '<!doctype html><div class="captcha">Drag the slider to fit the puzzle</div>',
  '/foryou': TIKTOK_VIDEO,
  '/whoami': WHOAMI,
  '/feed': FEED,
  '/login/': '<!doctype html><form><input name="email"><input type="password" name="pass"><button>Log in</button></form>',
  '/blocked': "<!doctype html><h1>You're Temporarily Blocked</h1><p>It looks like you were misusing this feature.</p>",
  '/group': `<!doctype html><h1>Indie Gamers</h1><p class="about">A place for players</p>
<button onclick="fetch('/api/graphql/', {method:'POST', body:'join'}).then(()=>{this.textContent='Cancel request'})">Join group</button>`,
};

// The feed tests count scrolls, which depend on the viewport height.
const FIXED_FP = {
  user_agent: 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/141.0.0.0 Safari/537.36',
  platform: 'Win32',
  locale: 'en-US',
  timezone: 'America/New_York',
  viewport: { width: 1366, height: 638 },
  screen: { width: 1366, height: 768 },
};

let fixture;
let fixtureUrl;
let inflight = 0;
let maxInflight = 0;
let graphqlCalls = 0;
let mediaCalls = 0;

let service;
let serviceUrl;
let manager;
let profilesDir;

function startFixture() {
  return new Promise((resolve) => {
    fixture = http.createServer(async (req, res) => {
      const url = new URL(req.url, 'http://x');
      if (url.pathname === '/api/graphql/') {
        let body = '';
        for await (const chunk of req) body += chunk;
        graphqlCalls += 1;
        const n = Number((body.match(/page=(\d+)/) || [])[1] || 0);
        res.writeHead(200, { 'Content-Type': 'application/json' });
        return res.end(JSON.stringify({ posts: body === 'join' ? [] : [`post ${n}a`, `post ${n}b`], request: body }));
      }
      if (url.pathname.startsWith('/media/')) {
        mediaCalls += 1;
        res.writeHead(200, { 'Content-Type': 'application/octet-stream' });
        return res.end(Buffer.alloc(1024));
      }
      if (url.pathname === '/api/comment/list/') {
        res.writeHead(200, { 'Content-Type': 'application/json' });
        return res.end(JSON.stringify({ comments: [{ cid: '9001', text: 'which server is this?' }], has_more: 0 }));
      }
      if (url.pathname === '/slow') {
        inflight += 1;
        maxInflight = Math.max(maxInflight, inflight);
        await new Promise((r) => setTimeout(r, Number(url.searchParams.get('ms') || 200)));
        inflight -= 1;
        res.writeHead(200, { 'Content-Type': 'text/html' });
        return res.end('<p>slow</p>');
      }
      const page = PAGES[url.pathname];
      if (!page) {
        res.writeHead(404, { 'Content-Type': 'text/html' });
        return res.end('<p>missing</p>');
      }
      res.writeHead(200, { 'Content-Type': 'text/html; charset=utf-8' });
      res.end(page);
    });
    fixture.listen(0, '127.0.0.1', () => {
      fixtureUrl = `http://127.0.0.1:${fixture.address().port}`;
      resolve();
    });
  });
}

async function startService() {
  const cfg = loadConfig({
    BROWSER_TOKEN: TOKEN,
    BROWSER_PROFILES_DIR: profilesDir,
    BROWSER_ACTION_DELAY_MIN_MS: '0',
    BROWSER_ACTION_DELAY_MAX_MS: '0',
    BROWSER_RATE_LIMIT_BACKOFF_SEC: '600',
    BROWSER_NAV_TIMEOUT_MS: '15000',
    BROWSER_GEO_LOOKUP_URL: 'http://geo.invalid/json',
  });
  manager = new SessionManager(cfg);
  service = createServer(cfg, manager);
  await new Promise((resolve) => service.listen(0, '127.0.0.1', resolve));
  serviceUrl = `http://127.0.0.1:${service.address().port}`;
}

async function stopService() {
  await new Promise((resolve) => service.close(resolve));
  await manager.closeAll();
}

async function call(method, route, body, token = TOKEN) {
  const res = await fetch(serviceUrl + route, {
    method,
    headers: { 'Content-Type': 'application/json', ...(token ? { Authorization: `Bearer ${token}` } : {}) },
    body: body ? JSON.stringify(body) : undefined,
  });
  const type = res.headers.get('content-type') || '';
  const data = type.includes('json') ? await res.json() : Buffer.from(await res.arrayBuffer());
  return { status: res.status, data, headers: res.headers };
}

before(async () => {
  profilesDir = await fs.mkdtemp(path.join(os.tmpdir(), 'browser-service-'));
  await startFixture();
  await startService();
});

after(async () => {
  await stopService();
  await new Promise((resolve) => fixture.close(resolve));
  await fs.rm(profilesDir, { recursive: true, force: true });
});

describe('auth and routing', () => {
  test('healthz needs no token', async () => {
    const r = await call('GET', '/healthz', null, '');
    assert.equal(r.status, 200);
    assert.deepEqual(r.data, { ok: true });
  });

  test('every other route needs the token', async () => {
    const r = await call('GET', '/sessions', null, 'wrong');
    assert.equal(r.status, 401);
    assert.equal(r.data.error.code, 'unauthorized');
  });

  test('rejects an account id that could escape the profiles directory', async () => {
    const r = await call('POST', '/sessions/..%2Fetc/navigate', { platform: 'web', url: fixtureUrl + '/feed' });
    assert.equal(r.status, 400);
    assert.equal(r.data.error.code, 'bad_request');
  });

  test('an unknown account without a platform is not found', async () => {
    const r = await call('GET', '/sessions/nobody');
    assert.equal(r.status, 404);
  });

  test('an account keeps the platform it was created with', async () => {
    assert.equal((await call('GET', '/sessions/fixed?platform=instagram')).status, 200);
    const r = await call('POST', '/sessions/fixed/navigate', { platform: 'facebook', url: fixtureUrl + '/feed' });
    assert.equal(r.status, 400);
    assert.match(r.data.error.message, /instagram account/);
  });
});

describe('facebook account lifecycle', () => {
  test('a new account refuses work until an operator logs in', async () => {
    const r = await call('POST', '/sessions/fb1/navigate', { platform: 'facebook', fingerprint: FIXED_FP, url: fixtureUrl + '/feed' });
    assert.equal(r.status, 423);
    assert.equal(r.data.error.code, 'needs_login');
    assert.equal(r.data.error.account_state, 'new');
  });

  test('confirming while the login form is still shown keeps the account blocked', async () => {
    await call('POST', '/sessions/fb1/login/start', { url: fixtureUrl + '/login/' });
    const r = await call('POST', '/sessions/fb1/login/confirm');
    assert.equal(r.status, 200);
    assert.equal(r.data.state, 'new');
    assert.match(r.data.reason, /login/);
  });

  test('confirming on a logged-in page unlocks the account and starts warm-up', async () => {
    await call('POST', '/sessions/fb1/input', { kind: 'goto', text: fixtureUrl + '/group' });
    const r = await call('POST', '/sessions/fb1/login/confirm');
    assert.equal(r.data.state, 'ok');
    assert.ok(r.data.login_confirmed_at);
    assert.equal(r.data.warming_up, true);
  });

  test('navigate returns the graphql responses the page fetched', async () => {
    const r = await call('POST', '/sessions/fb1/navigate', { url: fixtureUrl + '/feed', wait_ms: 1500 });
    assert.equal(r.status, 200, JSON.stringify(r.data));
    assert.deepEqual(r.data.account, {
      account_id: 'fb1',
      platform: 'facebook',
      status: 'active',
      retry_after_sec: 0,
      challenge: false,
      reason: '',
      warming_up: true,
    });
    assert.equal(r.data.http_status, 200);
    assert.equal(r.data.captures.length, 1);
    assert.match(r.data.captures[0].url, /\/api\/graphql\/$/);
    assert.deepEqual(JSON.parse(r.data.captures[0].body).posts, ['post 0a', 'post 0b']);
    assert.ok(r.data.cursor >= 1);
  });

  test('scroll loads the next pages and reports the end of the feed', async () => {
    const r = await call('POST', '/sessions/fb1/scroll', { times: 10, wait_ms: 800 });
    assert.equal(r.status, 200, JSON.stringify(r.data));
    const posts = r.data.captures.flatMap((c) => JSON.parse(c.body).posts);
    assert.deepEqual(posts, ['post 1a', 'post 1b', 'post 2a', 'post 2b']);
    assert.equal(r.data.reached_end, true);
  });

  test('captures since a cursor returns only later responses', async () => {
    const all = await call('POST', '/sessions/fb1/captures', { since: 0 });
    assert.equal(all.data.captures.length, 3);
    const later = await call('POST', '/sessions/fb1/captures', { since: all.data.captures[0].seq });
    assert.equal(later.data.captures.length, 2);
  });

  test('click by role and name, with the response it triggers', async () => {
    await call('POST', '/sessions/fb1/navigate', { url: fixtureUrl + '/group', wait_ms: 300 });
    const r = await call('POST', '/sessions/fb1/click', { role: 'button', name: 'Join group', wait_ms: 800 });
    assert.equal(r.status, 200, JSON.stringify(r.data));
    assert.equal(JSON.parse(r.data.captures[0].body).request, 'join');
    const text = await call('POST', '/sessions/fb1/content', { format: 'text', selector: 'button' });
    assert.equal(text.data.body, 'Cancel request');
  });

  test('a click target that is not on the page is not_found', async () => {
    const r = await call('POST', '/sessions/fb1/click', { role: 'button', name: 'Nothing like this' });
    assert.equal(r.status, 404);
  });

  test('content as html and as text of one element', async () => {
    const html = await call('POST', '/sessions/fb1/content', { format: 'html' });
    assert.match(html.data.body, /<h1>Indie Gamers<\/h1>/);
    const text = await call('POST', '/sessions/fb1/content', { format: 'text', selector: '.about' });
    assert.equal(text.data.body, 'A place for players');
  });

  test('a page that returns 404 is not_found', async () => {
    const r = await call('POST', '/sessions/fb1/navigate', { url: fixtureUrl + '/nope', wait_ms: 100 });
    assert.equal(r.status, 404);
    assert.equal(r.data.error.code, 'not_found');
  });

  test('a rate-limit page backs off and later calls wait it out', async () => {
    const r = await call('POST', '/sessions/fb1/navigate', { url: fixtureUrl + '/blocked', wait_ms: 100 });
    assert.equal(r.status, 429);
    assert.equal(r.data.error.code, 'rate_limited');
    assert.equal(r.data.error.retry_after_sec, 600);
    assert.equal(r.headers.get('retry-after'), '600');
    assert.equal(r.data.account.status, 'rate_limited');
    assert.equal(r.data.account.retry_after_sec, 600);
    const again = await call('POST', '/sessions/fb1/navigate', { url: fixtureUrl + '/group' });
    assert.equal(again.status, 429);
    assert.ok(again.data.error.retry_after_sec > 590 && again.data.error.retry_after_sec <= 600);
  });

  test('a logged-out page stops the account and survives a restart', async () => {
    const health = await call('POST', '/sessions/fb1/health', { url: fixtureUrl + '/group' });
    assert.equal(health.data.state, 'ok');
    assert.equal(health.data.cooldown_until, null);

    const r = await call('POST', '/sessions/fb1/navigate', { url: fixtureUrl + '/login/', wait_ms: 100 });
    assert.equal(r.status, 423);
    assert.equal(r.data.error.account_state, 'needs_login');
    assert.equal(r.data.account.status, 'needs_login');
    assert.equal(r.data.account.challenge, true);

    await stopService();
    await startService();
    const after = await call('POST', '/sessions/fb1/navigate', { url: fixtureUrl + '/group' });
    assert.equal(after.status, 423);
    const listed = await call('GET', '/sessions');
    const fb1 = listed.data.sessions.find((s) => s.account_id === 'fb1');
    assert.equal(fb1.state, 'needs_login');
    assert.equal(fb1.platform, 'facebook');
  });
});

describe('web accounts and operator tools', () => {
  test('a web account works without a login', async () => {
    const r = await call('POST', '/sessions/web1/navigate', { platform: 'web', url: fixtureUrl + '/group', wait_ms: 100 });
    assert.equal(r.status, 200, JSON.stringify(r.data));
    assert.equal(r.data.title, '');
    assert.equal(r.data.captures.length, 0);
  });

  test('actions on one account run one at a time', async () => {
    maxInflight = 0;
    const calls = [1, 2, 3].map(() =>
      call('POST', '/sessions/web1/navigate', { url: `${fixtureUrl}/slow?ms=250`, wait_ms: 50 }),
    );
    const results = await Promise.all(calls);
    for (const r of results) assert.equal(r.status, 200);
    assert.equal(maxInflight, 1);
  });

  test('screenshot is a png', async () => {
    const r = await call('GET', '/sessions/web1/screenshot.png');
    assert.equal(r.status, 200);
    assert.deepEqual([...r.data.subarray(0, 4)], [0x89, 0x50, 0x4e, 0x47]);
  });

  test('the login page is served with the token in the query', async () => {
    const res = await fetch(`${serviceUrl}/login/ig1?platform=instagram&token=${TOKEN}`);
    assert.equal(res.status, 200);
    const html = await res.text();
    assert.match(html, /instagram · ig1/);
    assert.match(html, /我已登录完成/);
    const script = html.split('<script>')[1].split('</script>')[0];
    assert.doesNotThrow(() => new Function(script));
  });
});

describe('operator login session', () => {
  async function fetchWith(method, route, body) {
    const res = await fetch(serviceUrl + route, { method, body: body ? JSON.stringify(body) : undefined });
    const type = res.headers.get('content-type') || '';
    return { status: res.status, data: type.includes('json') ? await res.json() : await res.text() };
  }

  test('login/start opens the login page and returns a ticketed link', async () => {
    const r = await call('POST', '/sessions/ig2/login/start', { platform: 'instagram', url: fixtureUrl + '/login/' });
    assert.equal(r.status, 200, JSON.stringify(r.data));
    assert.match(r.data.login_url, /^\/login\/ig2\?ticket=[A-Za-z0-9_-]{32}$/);
    assert.ok(Date.parse(r.data.expires_at) > Date.now());
    assert.equal(r.data.state, 'new');
    assert.equal(r.data.account.status, 'needs_login');
  });

  test('a ticket reaches its own login page and nothing an agent uses', async () => {
    const { data } = await call('POST', '/sessions/ig2/login/start', { url: fixtureUrl + '/login/' });
    const ticket = new URL(data.login_url, 'http://x').searchParams.get('ticket');

    assert.equal((await fetchWith('GET', `/login/ig2?ticket=${ticket}`)).status, 200);
    assert.equal((await fetchWith('GET', `/sessions/ig2/screenshot.png?ticket=${ticket}`)).status, 200);
    assert.equal((await fetchWith('GET', `/sessions/ig2?ticket=${ticket}`)).status, 200);

    assert.equal((await fetchWith('POST', `/sessions/ig2/navigate?ticket=${ticket}`, { url: fixtureUrl + '/group' })).status, 401);
    assert.equal((await fetchWith('POST', `/sessions/ig2/login/start?ticket=${ticket}`, {})).status, 401);
    assert.equal((await fetchWith('GET', `/sessions/fb1/screenshot.png?ticket=${ticket}`)).status, 401);
    assert.equal((await fetchWith('GET', '/sessions?ticket=' + ticket)).status, 401);
  });

  test('done on the login page turns the account active and lets work resume', async () => {
    const { data } = await call('POST', '/sessions/ig2/login/start', { url: fixtureUrl + '/login/' });
    const ticket = new URL(data.login_url, 'http://x').searchParams.get('ticket');

    const early = await fetchWith('POST', `/sessions/ig2/login/confirm?ticket=${ticket}`, {});
    assert.equal(early.data.state, 'new');
    assert.equal((await call('POST', '/sessions/ig2/navigate', { url: fixtureUrl + '/group' })).status, 423);

    await fetchWith('POST', `/sessions/ig2/input?ticket=${ticket}`, { kind: 'goto', text: fixtureUrl + '/group' });
    const done = await fetchWith('POST', `/sessions/ig2/login/confirm?ticket=${ticket}`, {});
    assert.equal(done.data.state, 'ok');
    assert.equal(done.data.account.status, 'active');

    assert.equal(done.data.open, false);
    assert.equal((await fetchWith('GET', `/login/ig2?ticket=${ticket}`)).status, 401);
    assert.equal((await fetchWith('POST', `/sessions/ig2/login/confirm?ticket=${ticket}`, {})).status, 401);

    const work = await call('POST', '/sessions/ig2/navigate', { url: fixtureUrl + '/group', wait_ms: 100 });
    assert.equal(work.status, 200, JSON.stringify(work.data));
  });

  test('confirming an account that is already logged in leaves its browser closed', async () => {
    await call('POST', '/sessions/ig2/close');
    const r = await call('POST', '/sessions/ig2/login/confirm');
    assert.equal(r.data.state, 'ok');
    assert.equal(r.data.open, false);
  });

  test('cancel ends the operator session and leaves the account as it was', async () => {
    const { data } = await call('POST', '/sessions/ig3/login/start', { platform: 'instagram', url: fixtureUrl + '/login/' });
    const ticket = new URL(data.login_url, 'http://x').searchParams.get('ticket');

    const r = await call('POST', '/sessions/ig3/login/cancel');
    assert.equal(r.status, 200);
    assert.equal(r.data.state, 'new');
    assert.equal(r.data.open, false);
    assert.equal((await fetchWith('GET', `/login/ig3?ticket=${ticket}`)).status, 401);
    assert.equal((await fetchWith('POST', `/sessions/ig3/login/cancel?ticket=${ticket}`, {})).status, 401);
  });
});

async function whoami(account) {
  const r = await call('POST', `/sessions/${account}/content`, { format: 'text', selector: '#me' });
  assert.equal(r.status, 200, JSON.stringify(r.data));
  return JSON.parse(r.data.body);
}

describe('per-account fingerprint', () => {
  let first;

  test('a new account gets a fingerprint for its region and the page sees it', async () => {
    const r = await call('POST', '/sessions/fp1/navigate', { platform: 'web', region: 'jp', url: fixtureUrl + '/whoami', wait_ms: 0 });
    assert.equal(r.status, 200, JSON.stringify(r.data));
    first = (await call('GET', '/sessions/fp1')).data.fingerprint;
    assert.equal(first.version, 2);
    assert.equal(first.region, 'JP');
    assert.equal(first.locale, 'ja-JP');
    assert.equal(first.timezone, 'Asia/Tokyo');
    const seen = await whoami('fp1');
    assert.equal(seen.ua, first.user_agent);
    assert.doesNotMatch(seen.ua, /Headless|Linux/);
    assert.equal(seen.platform, first.platform);
    assert.equal(seen.cores, first.hardware_concurrency);
    assert.equal(seen.language, 'ja-JP');
    assert.equal(seen.tz, 'Asia/Tokyo');
    assert.equal(seen.screen, `${first.screen.width}x${first.screen.height}`);
    assert.equal(seen.dpr, first.device_scale_factor);
    assert.equal(seen.gpu, first.suite.fingerprint.videoCard.renderer);
  });

  test('the fingerprint survives a reopen and a service restart', async () => {
    await call('POST', '/sessions/fp1/close');
    await stopService();
    await startService();
    const r = await call('POST', '/sessions/fp1/navigate', { platform: 'web', region: 'US', url: fixtureUrl + '/whoami', wait_ms: 0 });
    assert.equal(r.status, 200);
    assert.deepEqual(r.data.account && (await call('GET', '/sessions/fp1')).data.fingerprint, first);
    assert.equal((await whoami('fp1')).ua, first.user_agent);
  });

  test('a fingerprint the backend stored replaces the generated one', async () => {
    const stored = { ...first, platform: 'MacIntel', hardware_concurrency: 6, locale: 'de-DE', timezone: 'Europe/Berlin' };
    const r = await call('POST', '/sessions/fp1/navigate', { fingerprint: stored, url: fixtureUrl + '/whoami', wait_ms: 0 });
    assert.equal(r.status, 200, JSON.stringify(r.data));
    const seen = await whoami('fp1');
    assert.equal(seen.platform, 'MacIntel');
    assert.equal(seen.cores, 6);
    assert.equal(seen.tz, 'Europe/Berlin');
  });

  test('a fingerprint without its required fields is refused', async () => {
    const r = await call('POST', '/sessions/fp1/config', { fingerprint: { user_agent: 'x' } });
    assert.equal(r.status, 400);
    assert.match(r.data.error.message, /fingerprint\.platform/);
  });
});

describe('per-account proxy', () => {
  let proxy;
  let proxyUrl;
  let geo;
  let lookups = 0;
  const seen = [];

  before(async () => {
    proxy = http.createServer((req, res) => {
      seen.push({ url: req.url, auth: req.headers['proxy-authorization'] || '' });
      if (req.headers['proxy-authorization'] !== 'Basic ' + Buffer.from('alice:s3cr:t').toString('base64')) {
        res.writeHead(407, { 'Proxy-Authenticate': 'Basic realm="p"' });
        return res.end();
      }
      if (req.url.endsWith('/where')) {
        res.writeHead(200, { 'Content-Type': 'text/html' });
        return res.end(WHERE);
      }
      res.writeHead(200, { 'Content-Type': 'text/html' });
      res.end(`<!doctype html><p id="me">{"via":"proxy","url":${JSON.stringify(req.url)}}</p>`);
    });
    // Playwright's request client tunnels even plain http through CONNECT.
    geo = http.createServer((_, res) => {
      lookups += 1;
      res.writeHead(200, { 'Content-Type': 'application/json' });
      res.end(JSON.stringify({ ip: '203.0.113.7', loc: '13.7563,100.5018' }));
    });
    await new Promise((resolve) => geo.listen(0, '127.0.0.1', resolve));
    proxy.on('connect', (req, socket) => {
      if (req.url !== 'geo.invalid:80' || req.headers['proxy-authorization'] !== 'Basic ' + Buffer.from('alice:s3cr:t').toString('base64')) {
        return socket.end('HTTP/1.1 407 Proxy Authentication Required\r\nProxy-Authenticate: Basic realm="p"\r\n\r\n');
      }
      const upstream = net.connect(geo.address().port, '127.0.0.1', () => {
        socket.write('HTTP/1.1 200 Connection Established\r\n\r\n');
        upstream.pipe(socket);
        socket.pipe(upstream);
      });
    });
    await new Promise((resolve) => proxy.listen(0, '127.0.0.1', resolve));
    proxyUrl = `http://alice:${encodeURIComponent('s3cr:t')}@127.0.0.1:${proxy.address().port}`;
  });

  after(async () => {
    await new Promise((resolve) => proxy.close(resolve));
    await new Promise((resolve) => geo.close(resolve));
  });

  test('pages load through the account proxy with its credentials', async () => {
    const r = await call('POST', '/sessions/px1/navigate', { platform: 'web', proxy: proxyUrl, url: 'http://site.invalid/home', wait_ms: 0 });
    assert.equal(r.status, 200, JSON.stringify(r.data));
    assert.deepEqual(await whoami('px1'), { via: 'proxy', url: 'http://site.invalid/home' });
    assert.ok(seen.some((s) => s.auth.startsWith('Basic ')));
  });

  test('a page that asks for the position gets one tens of metres from the exit IP', async () => {
    const r = await call('POST', '/sessions/px1/navigate', { url: fixtureUrl + '/where', wait_ms: 0 });
    assert.equal(r.status, 200, JSON.stringify(r.data));
    let where = {};
    for (let i = 0; i < 20 && !('latitude' in where); i++) {
      where = await whoami('px1');
      if (!('latitude' in where)) await new Promise((resolve) => setTimeout(resolve, 100));
    }
    const metres = distance({ latitude: 13.7563, longitude: 100.5018 }, where);
    assert.ok(metres >= 19 && metres <= 81, `${metres} m from the exit IP`);
    assert.deepEqual(
      { latitude: where.latitude, longitude: where.longitude },
      (({ latitude, longitude }) => ({ latitude, longitude }))((await call('GET', '/sessions/px1')).data.geolocation),
    );
  });

  test('the position is kept across a reopen without asking again', async () => {
    const before = (await call('GET', '/sessions/px1')).data.geolocation;
    const asked = lookups;
    await manager.closeAll();
    const r = await call('POST', '/sessions/px1/navigate', { url: fixtureUrl + '/where', wait_ms: 0 });
    assert.equal(r.status, 200, JSON.stringify(r.data));
    assert.deepEqual((await call('GET', '/sessions/px1')).data.geolocation, before);
    assert.equal(lookups, asked);
  });

  test('the account shows its proxy without the password', async () => {
    const d = (await call('GET', '/sessions/px1')).data;
    assert.equal(d.proxy, `http://***:***@127.0.0.1:${proxy.address().port}`);
  });

  test('the operator login window uses the same proxy', async () => {
    const r = await call('POST', '/sessions/px1/login/start', { url: 'http://site.invalid/login' });
    assert.equal(r.status, 200, JSON.stringify(r.data));
    assert.equal(r.data.url, 'http://site.invalid/login');
    assert.equal(seen.at(-1).url, 'http://site.invalid/login');
  });

  test('a login ticket cannot move the account to another proxy', async () => {
    const start = await call('POST', '/sessions/px1/login/start', { url: 'http://site.invalid/login' });
    const ticket = new URL(start.data.login_url, 'http://x').searchParams.get('ticket');
    const res = await fetch(`${serviceUrl}/sessions/px1/input?ticket=${ticket}`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ kind: 'press', key: 'Tab', proxy: '' }),
    });
    assert.equal(res.status, 200);
    assert.match((await call('GET', '/sessions/px1')).data.proxy, /127\.0\.0\.1/);
  });

  test('an empty proxy switches the account to a direct connection', async () => {
    const r = await call('POST', '/sessions/px1/navigate', { proxy: '', url: fixtureUrl + '/whoami', wait_ms: 0 });
    assert.equal(r.status, 200, JSON.stringify(r.data));
    const before = seen.length;
    assert.equal(typeof (await whoami('px1')).ua, 'string');
    assert.equal(seen.length, before);
    assert.equal((await call('GET', '/sessions/px1')).data.proxy, '');
    assert.equal((await call('GET', '/sessions/px1')).data.geolocation, null, 'no position without a proxy');
  });

  test('a proxy the browser cannot use is refused', async () => {
    for (const [proxyValue, message] of [
      ['ftp://h:21', /scheme ftp:/],
      ['not a url', /must be a URL/],
      ['socks5://u:p@h:1080', /SOCKS5/],
      ['http://h', /host and a port/],
    ]) {
      const r = await call('POST', '/sessions/px1/config', { proxy: proxyValue });
      assert.equal(r.status, 400, proxyValue);
      assert.match(r.data.error.message, message);
    }
  });
});

function freePort() {
  return new Promise((resolve) => {
    const srv = net.createServer().listen(0, '127.0.0.1', () => {
      const { port } = srv.address();
      srv.close(() => resolve(port));
    });
  });
}

describe('external browser', () => {
  let external;
  let endpoint;

  before(async () => {
    const port = await freePort();
    external = await chromium.launch({ headless: true, args: [`--remote-debugging-port=${port}`] });
    endpoint = `http://127.0.0.1:${port}`;
  });

  after(() => external.close());

  test('an account with a CDP endpoint drives that browser instead of its own', async () => {
    const r = await call('POST', '/sessions/ext1/navigate', { platform: 'web', external_browser_ws: endpoint, url: fixtureUrl + '/whoami', wait_ms: 0 });
    assert.equal(r.status, 200, JSON.stringify(r.data));
    const d = (await call('GET', '/sessions/ext1')).data;
    assert.equal(d.external_browser, true);
    assert.equal(d.fingerprint, null);
    assert.match((await whoami('ext1')).ua, /HeadlessChrome/);
    const targets = await (await fetch(endpoint + '/json/list')).json();
    assert.ok(targets.some((t) => t.url === fixtureUrl + '/whoami'), JSON.stringify(targets));
  });

  test('closing the account leaves the external browser running', async () => {
    assert.equal((await call('POST', '/sessions/ext1/close')).status, 200);
    assert.equal(external.isConnected(), true);
    const r = await call('POST', '/sessions/ext1/navigate', { url: fixtureUrl + '/whoami', wait_ms: 0 });
    assert.equal(r.status, 200);
  });

  test('an external browser that cannot be reached is unavailable', async () => {
    const port = await freePort();
    const r = await call('POST', '/sessions/ext2/navigate', { platform: 'web', external_browser_ws: `http://127.0.0.1:${port}`, url: fixtureUrl + '/whoami' });
    assert.equal(r.status, 502);
    assert.equal(r.data.error.code, 'unavailable');
  });
});

describe('tiktok', () => {
  test('each file in src/platforms is a platform', () => {
    assert.deepEqual(Object.keys(PLATFORMS).sort(), ['facebook', 'instagram', 'tiktok', 'web']);
    assert.equal(PLATFORMS.web.needsLogin, false);
    assert.equal(PLATFORMS.facebook.paceFactor, 1);
  });

  test('the operator login window loads images, so a verification puzzle shows', async () => {
    mediaCalls = 0;
    await call('POST', '/sessions/tt1/login/start', { platform: 'tiktok', url: fixtureUrl + '/@creator/video/123' });
    await call('POST', '/sessions/tt1/input', { kind: 'goto', text: fixtureUrl + '/@creator/video/123' });
    assert.ok(mediaCalls > 0);
    const r = await call('POST', '/sessions/tt1/login/confirm');
    assert.equal(r.data.state, 'ok');
  });

  test('a video page gives its comments and fetches no media', async () => {
    mediaCalls = 0;
    const r = await call('POST', '/sessions/tt1/navigate', { url: fixtureUrl + '/@creator/video/123', wait_ms: 800 });
    assert.equal(r.status, 200, JSON.stringify(r.data));
    assert.equal(r.data.captures.length, 1);
    assert.match(r.data.captures[0].url, /\/api\/comment\/list\/\?aweme_id=123/);
    const scrolled = await call('POST', '/sessions/tt1/scroll', { times: 1, wait_ms: 200, selector: '#comments' });
    assert.equal(scrolled.status, 200, JSON.stringify(scrolled.data));
    assert.equal(mediaCalls, 0);
  });

  test('agents do not open pages other than search, creators and videos', async () => {
    mediaCalls = 0;
    const r = await call('POST', '/sessions/tt1/navigate', { url: fixtureUrl + '/foryou' });
    assert.equal(r.status, 400);
    assert.match(r.data.error.message, /tiktok agents only open/);
    assert.equal(mediaCalls, 0);
  });

  test('a verification puzzle stops the account for an operator', async () => {
    const r = await call('POST', '/sessions/tt1/navigate', { url: fixtureUrl + '/@creator/video/124', wait_ms: 100 });
    assert.equal(r.status, 423);
    assert.match(r.data.error.message, /verification on page/);
    assert.equal(r.data.account.challenge, true);
  });
});

describe('pace', () => {
  test('a platform factor stretches the pause and the environment overrides it', async () => {
    const cfg = loadConfig({ BROWSER_PROFILES_DIR: profilesDir, BROWSER_ACTION_DELAY_MIN_MS: '100', BROWSER_ACTION_DELAY_MAX_MS: '100', BROWSER_WARMUP_DAYS: '0' });
    const timed = async (c, platform) => {
      const s = await new SessionManager(c).get(`pace-${platform}-${Object.keys(c.paceFactors).length}`, platform);
      const start = Date.now();
      await s.pace();
      return Date.now() - start;
    };
    assert.ok((await timed(cfg, 'tiktok')) >= 190);
    const fast = loadConfig({ ...{ BROWSER_PROFILES_DIR: profilesDir, BROWSER_ACTION_DELAY_MIN_MS: '100', BROWSER_ACTION_DELAY_MAX_MS: '100', BROWSER_WARMUP_DAYS: '0' }, BROWSER_PACE_FACTOR_TIKTOK: '0' });
    assert.ok((await timed(fast, 'tiktok')) < 90);
  });
});
