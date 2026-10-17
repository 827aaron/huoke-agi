import { randomBytes } from 'node:crypto';
import fs from 'node:fs/promises';
import path from 'node:path';
import { chromium } from 'playwright-core';
import { agentMayOpen, detectState, platformSpec, shouldCapture } from './detect.js';
import { ServiceError } from './errors.js';
import { parseLookup, placeNear, proxyKey } from './geo.js';
import {
  attachSuite,
  contextOptions,
  generateFingerprint,
  initScript,
  parseProxy,
  redactProxy,
  validateCdpUrl,
  validateFingerprint,
} from './profile.js';

const ACCOUNT_ID = /^[A-Za-z0-9_.-]{1,64}$/;

const sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms));

export function validateAccountId(id) {
  if (!ACCOUNT_ID.test(id || '') || id === '.' || id === '..') {
    throw new ServiceError('bad_request', `account id must match ${ACCOUNT_ID}`);
  }
}

// The fingerprint claims the engine's real major version, read once per process.
let cachedMajor = null;
async function chromeMajor() {
  if (cachedMajor) return cachedMajor;
  const browser = await chromium.launch({ headless: true });
  try {
    cachedMajor = browser.version().split('.')[0];
  } finally {
    await browser.close();
  }
  return cachedMajor;
}

class Session {
  constructor(manager, accountId, meta) {
    this.manager = manager;
    this.cfg = manager.cfg;
    this.accountId = accountId;
    this.meta = meta;
    this.dir = path.join(this.cfg.profilesDir, accountId);
    this.context = null;
    this.browser = null;
    this.page = null;
    this.captures = [];
    this.seq = 0;
    this.queue = Promise.resolve();
    this.idleTimer = null;
    this.operatorView = false;
  }

  get platform() {
    return this.meta.platform;
  }

  get spec() {
    return platformSpec(this.platform);
  }

  // One action at a time per account: two tabs driving one profile is the
  // pattern the platforms flag first, and a persistent profile cannot be shared.
  run(fn) {
    const next = this.queue.then(fn, fn);
    this.queue = next.catch(() => {});
    return next;
  }

  async saveMeta() {
    await fs.mkdir(this.dir, { recursive: true });
    const tmp = path.join(this.dir, 'meta.json.tmp');
    await fs.writeFile(tmp, JSON.stringify(this.meta, null, 2));
    await fs.rename(tmp, path.join(this.dir, 'meta.json'));
  }

  touch() {
    clearTimeout(this.idleTimer);
    if (this.cfg.idleCloseMs > 0) {
      this.idleTimer = setTimeout(() => {
        this.run(() => this.closeContext()).catch(() => {});
      }, this.cfg.idleCloseMs);
      this.idleTimer.unref();
    }
  }

  get proxy() {
    return 'proxy' in this.meta ? this.meta.proxy : this.cfg.proxy;
  }

  // Applies the account settings a request carries. A change takes effect on
  // the next launch, so a running context is closed rather than left on the
  // old exit IP or fingerprint.
  async configure(body) {
    let changed = false;
    if ('proxy' in body) {
      const proxy = body.proxy || '';
      parseProxy(proxy);
      if (proxy !== this.proxy) {
        this.meta.proxy = proxy;
        changed = true;
      }
    }
    if ('external_browser_ws' in body) {
      const ws = validateCdpUrl(body.external_browser_ws || '');
      if (ws !== (this.meta.external_browser_ws || '')) {
        if (ws) this.meta.external_browser_ws = ws;
        else delete this.meta.external_browser_ws;
        changed = true;
      }
    }
    if (body.region && !this.meta.fingerprint && body.region !== this.meta.region) {
      this.meta.region = String(body.region).toUpperCase();
      changed = true;
    }
    if (body.fingerprint && JSON.stringify(body.fingerprint) !== JSON.stringify(this.meta.fingerprint)) {
      this.meta.fingerprint = validateFingerprint(body.fingerprint);
      changed = true;
    }
    if (!changed) return;
    await this.saveMeta();
    await this.closeContext();
  }

  // Generated once and kept: an account whose browser changes between visits
  // looks like a new device logging in every time.
  async fingerprint() {
    if (!this.meta.fingerprint) {
      this.meta.fingerprint = generateFingerprint({ chromeMajor: await chromeMajor(), region: this.meta.region, cfg: this.cfg });
      await this.saveMeta();
    }
    return this.meta.fingerprint;
  }

  async open() {
    this.touch();
    if (this.context) return;
    if (this.meta.external_browser_ws) {
      await this.attach();
    } else {
      await this.launch();
    }
    this.context.setDefaultNavigationTimeout(this.cfg.navigationTimeoutMs);
    this.context.setDefaultTimeout(Math.min(this.cfg.navigationTimeoutMs, 20000));
    this.context.on('close', () => {
      this.context = null;
      this.page = null;
    });
    await this.applyPlatform();
    this.page = this.context.pages()[0] || (await this.context.newPage());
    this.context.on('response', (response) => this.onResponse(response));
  }

  // Resource blocking stops for the operator login window: a verification
  // puzzle is an image.
  async applyPlatform() {
    const { block, initScript: script } = this.spec;
    if (script) await this.context.addInitScript(script);
    if (block.length === 0) return;
    await this.context.route('**/*', (route) => {
      if (!this.operatorView && block.includes(route.request().resourceType())) return route.abort('blockedbyclient');
      return route.continue();
    });
  }

  async launch() {
    await fs.mkdir(path.join(this.dir, 'profile'), { recursive: true });
    const fp = await this.fingerprint();
    const options = {
      ...contextOptions(fp),
      headless: this.cfg.headless,
      args: ['--disable-blink-features=AutomationControlled'],
    };
    const proxy = parseProxy(this.proxy);
    if (proxy) options.proxy = proxy;
    this.context = await chromium.launchPersistentContext(path.join(this.dir, 'profile'), options);
    if (fp.suite) await attachSuite(this.context, fp);
    else await this.context.addInitScript(initScript(fp));
    await this.applyGeolocation();
  }

  // A page that asks for the position gets one tens of metres from where the
  // exit IP is placed, kept for as long as the proxy is. The exit IP of a
  // rotating or residential proxy is not its host, so it is looked up through
  // the proxy itself. Without a proxy nothing is claimed, and a failed lookup
  // only leaves the position unset until the next launch.
  async applyGeolocation() {
    if (!this.proxy) return;
    const key = proxyKey(this.proxy);
    if (this.meta.geolocation?.proxy_key !== key) {
      const exit = await this.lookupExit();
      if (!exit) return;
      this.meta.geolocation = { ...placeNear(exit), proxy_key: key };
      await this.saveMeta();
    }
    const { latitude, longitude, accuracy } = this.meta.geolocation;
    await this.context.grantPermissions(['geolocation']);
    await this.context.setGeolocation({ latitude, longitude, accuracy });
  }

  async lookupExit() {
    try {
      const res = await this.context.request.get(this.cfg.geoLookupUrl, { timeout: 10000 });
      if (!res.ok()) throw new Error(`HTTP ${res.status()}`);
      const exit = parseLookup(await res.json());
      if (!exit) throw new Error('the answer carries no coordinates');
      return exit;
    } catch (err) {
      console.warn(`account ${this.accountId}: position left unset, exit IP lookup failed: ${err.message.split('\n')[0]}`);
      return null;
    }
  }

  // An external fingerprint browser (AdsPower, BitBrowser) owns its profile,
  // proxy and fingerprint; the service only drives it.
  async attach() {
    try {
      this.browser = await chromium.connectOverCDP(this.meta.external_browser_ws, { timeout: this.cfg.navigationTimeoutMs });
    } catch (err) {
      throw new ServiceError('unavailable', `cannot reach the external browser: ${err.message.split('\n')[0]}`);
    }
    this.browser.on('disconnected', () => {
      this.browser = null;
      this.context = null;
      this.page = null;
    });
    this.context = this.browser.contexts()[0] || (await this.browser.newContext());
  }

  async onResponse(response) {
    const url = response.url();
    if (!shouldCapture(this.platform, url)) return;
    let body = '';
    let truncated = false;
    try {
      body = await response.text();
    } catch {
      return;
    }
    if (Buffer.byteLength(body) > this.cfg.captureBodyMaxBytes) {
      body = body.slice(0, this.cfg.captureBodyMaxBytes);
      truncated = true;
    }
    this.seq += 1;
    this.captures.push({ seq: this.seq, url, status: response.status(), body, truncated });
    if (this.captures.length > this.cfg.captureLimit) this.captures.splice(0, this.captures.length - this.cfg.captureLimit);
  }

  capturesSince(seq) {
    return this.captures.filter((c) => c.seq > seq);
  }

  async closeContext() {
    clearTimeout(this.idleTimer);
    // Disconnecting leaves an external browser and its pages running.
    if (this.browser) await this.browser.close().catch(() => {});
    else if (this.context) await this.context.close().catch(() => {});
    this.browser = null;
    this.context = null;
    this.page = null;
    this.operatorView = false;
  }

  warmingUp() {
    if (!this.meta.login_confirmed_at || this.cfg.warmupDays === 0) return false;
    const age = Date.now() - Date.parse(this.meta.login_confirmed_at);
    return age < this.cfg.warmupDays * 86400 * 1000;
  }

  async pace() {
    const { actionDelayMinMs: min, actionDelayMaxMs: max } = this.cfg;
    let delay = min + Math.random() * (max - min);
    delay *= this.cfg.paceFactors[this.platform] ?? this.spec.paceFactor;
    if (this.warmingUp()) delay *= this.cfg.warmupFactor;
    if (delay > 0) await sleep(delay);
  }

  // Refuses work for an account the platform has logged out or is throttling.
  // Nothing here tries to get past a verification step; an operator does that.
  gate() {
    if (this.spec.needsLogin && this.meta.state !== 'ok') {
      throw new ServiceError('needs_login', this.meta.state_reason || 'account has not been logged in by an operator', {
        account_state: this.meta.state,
      });
    }
    const until = this.meta.cooldown_until ? Date.parse(this.meta.cooldown_until) : 0;
    if (until > Date.now()) {
      throw new ServiceError('rate_limited', this.meta.state_reason || 'platform asked to slow down', {
        retry_after_sec: Math.ceil((until - Date.now()) / 1000),
      });
    }
  }

  async signals() {
    const page = this.page;
    const [text, hasPasswordInput] = await Promise.all([
      page.evaluate(() => (document.body ? document.body.innerText.slice(0, 200000) : '')).catch(() => ''),
      page
        .$('input[type="password"]')
        .then((el) => el !== null)
        .catch(() => false),
    ]);
    return { url: page.url(), text, hasPasswordInput };
  }

  async check() {
    const result = detectState(this.platform, await this.signals());
    if (result.state === 'needs_login') {
      this.meta.state = 'needs_login';
      this.meta.state_reason = result.reason;
      this.meta.state_changed_at = new Date().toISOString();
      await this.saveMeta();
      throw new ServiceError('needs_login', result.reason, { account_state: 'needs_login' });
    }
    if (result.state === 'rate_limited') {
      const retry = this.cfg.rateLimitBackoffSec;
      this.meta.cooldown_until = new Date(Date.now() + retry * 1000).toISOString();
      this.meta.state_reason = result.reason;
      await this.saveMeta();
      throw new ServiceError('rate_limited', result.reason, { retry_after_sec: retry });
    }
    if (this.meta.cooldown_until) {
      delete this.meta.cooldown_until;
      await this.saveMeta();
    }
    return result;
  }

  async settle(waitMs) {
    await this.page.waitForLoadState('domcontentloaded').catch(() => {});
    await this.page.waitForLoadState('networkidle', { timeout: Math.max(waitMs, 1) }).catch(() => {});
  }

  async navigate({ url, wait_ms: waitMs = 2000 }) {
    if (!url) throw new ServiceError('bad_request', 'url is required');
    if (!agentMayOpen(this.platform, url)) {
      throw new ServiceError('bad_request', `${this.platform} agents only open the pages its connector reads, not ${url}`);
    }
    this.gate();
    await this.open();
    await this.pace();
    const cursor = this.seq;
    const response = await this.page.goto(url, { waitUntil: 'domcontentloaded' });
    await this.settle(waitMs);
    await this.check();
    if (response && response.status() === 404) throw new ServiceError('not_found', `${url} returned 404`);
    return {
      url: this.page.url(),
      title: await this.page.title(),
      http_status: response ? response.status() : 0,
      captures: this.capturesSince(cursor),
      cursor: this.seq,
    };
  }

  // selector names a scrollable panel (a comment list beside a video); the
  // wheel is turned over it, or over the page when nothing matches.
  async scroll({ times = 3, wait_ms: waitMs = 1500, selector = '' }) {
    this.gate();
    await this.open();
    const cursor = this.seq;
    if (selector) {
      const panel = this.page.locator(selector).first();
      if ((await panel.count()) > 0) await panel.hover().catch(() => {});
    }
    let reachedEnd = false;
    for (let i = 0; i < Math.max(1, times); i += 1) {
      await this.pace();
      const before = await this.page.evaluate(() => document.documentElement.scrollHeight);
      await this.page.mouse.wheel(0, Math.round(900 * (0.8 + Math.random() * 0.3)));
      // The feed fetches from its scroll handler, after the wheel returns; an
      // idle check made at once sees no request and reports the end too early.
      await sleep(Math.min(waitMs, 500));
      await this.settle(waitMs);
      const state = await this.page.evaluate(() => ({
        height: document.documentElement.scrollHeight,
        bottom: window.scrollY + window.innerHeight,
      }));
      if (state.bottom >= state.height - 2 && state.height === before) {
        reachedEnd = true;
        break;
      }
    }
    await this.check();
    return { url: this.page.url(), reached_end: reachedEnd, captures: this.capturesSince(cursor), cursor: this.seq };
  }

  async content({ format = 'html', selector = '' }) {
    this.gate();
    await this.open();
    await this.check();
    const target = selector ? this.page.locator(selector).first() : null;
    let body;
    if (format === 'text') {
      body = target ? await target.innerText() : await this.page.evaluate(() => document.body.innerText);
    } else if (format === 'html') {
      body = target ? await target.evaluate((el) => el.outerHTML) : await this.page.content();
    } else {
      throw new ServiceError('bad_request', `format must be html or text, got ${format}`);
    }
    return { url: this.page.url(), title: await this.page.title(), format, body };
  }

  locator({ selector, role, name, text, exact = false }) {
    if (selector) return this.page.locator(selector);
    if (role) return this.page.getByRole(role, name ? { name, exact } : {});
    if (text) return this.page.getByText(text, { exact });
    throw new ServiceError('bad_request', 'one of selector, role or text is required');
  }

  async click({ wait_ms: waitMs = 2000, ...target }) {
    this.gate();
    await this.open();
    await this.pace();
    const cursor = this.seq;
    const loc = this.locator(target).first();
    if ((await loc.count()) === 0) throw new ServiceError('not_found', 'nothing on the page matches the click target');
    await loc.click();
    await this.settle(waitMs);
    await this.check();
    return { url: this.page.url(), captures: this.capturesSince(cursor), cursor: this.seq };
  }

  async type({ text = '', submit = false, wait_ms: waitMs = 2000, ...target }) {
    this.gate();
    await this.open();
    await this.pace();
    const cursor = this.seq;
    const loc = this.locator(target).first();
    if ((await loc.count()) === 0) throw new ServiceError('not_found', 'nothing on the page matches the input target');
    await loc.click();
    await loc.pressSequentially(text, { delay: 40 + Math.random() * 80 });
    if (submit) await loc.press('Enter');
    await this.settle(waitMs);
    await this.check();
    return { url: this.page.url(), captures: this.capturesSince(cursor), cursor: this.seq };
  }

  async capturesAfter({ since = 0 }) {
    return { captures: this.capturesSince(Number(since) || 0), cursor: this.seq };
  }

  async screenshot({ full_page: fullPage = false } = {}) {
    await this.open();
    return this.page.screenshot({ type: 'png', fullPage });
  }

  // Raw input from the operator login page. Not paced and not gated: a person
  // is at the keyboard.
  async input({ kind, x, y, text, key, dy }) {
    this.operatorView = true;
    await this.open();
    const page = this.page;
    switch (kind) {
      case 'click':
        await page.mouse.click(Number(x), Number(y));
        break;
      case 'type':
        await page.keyboard.type(String(text || ''), { delay: 30 });
        break;
      case 'press':
        await page.keyboard.press(String(key || 'Enter'));
        break;
      case 'scroll':
        await page.mouse.wheel(0, Number(dy) || 400);
        break;
      case 'goto':
        await page.goto(String(text || platformSpec(this.platform).login), { waitUntil: 'domcontentloaded' });
        break;
      default:
        throw new ServiceError('bad_request', `unknown input kind ${kind}`);
    }
    await page.waitForLoadState('domcontentloaded').catch(() => {});
    return { url: page.url() };
  }

  async loginStart({ url } = {}) {
    this.operatorView = true;
    await this.open();
    await this.page.goto(url || platformSpec(this.platform).login, { waitUntil: 'domcontentloaded' });
    return { url: this.page.url() };
  }

  // A confirmed login ends the operator session: its link stops working and
  // the browser closes, which writes the new cookies to the profile now rather
  // than on Chromium's own schedule. The next agent action relaunches it.
  async confirmLogin() {
    if (!this.context && this.meta.state === 'ok') return { ...this.describe(), reason: '' };
    await this.open();
    const result = detectState(this.platform, await this.signals());
    if (result.state === 'needs_login') {
      return { ...this.describe(), state: this.meta.state, reason: result.reason };
    }
    this.markLoggedIn();
    await this.saveMeta();
    this.manager.revokeTickets(this.accountId);
    await this.closeContext();
    return { ...this.describe(), reason: result.reason };
  }

  async cancelLogin() {
    this.manager.revokeTickets(this.accountId);
    await this.closeContext();
    return this.describe();
  }

  markLoggedIn() {
    if (this.meta.state !== 'ok') {
      this.meta.state = 'ok';
      this.meta.state_changed_at = new Date().toISOString();
      this.meta.login_confirmed_at = this.meta.state_changed_at;
    }
    delete this.meta.state_reason;
    delete this.meta.cooldown_until;
  }

  async health({ url } = {}) {
    await this.open();
    const current = this.page.url();
    if (url || current === 'about:blank') {
      await this.page.goto(url || platformSpec(this.platform).home, { waitUntil: 'domcontentloaded' });
      await this.settle(1500);
    }
    const result = detectState(this.platform, await this.signals());
    if (result.state === 'needs_login') {
      if (this.meta.state !== 'needs_login') {
        this.meta.state = 'needs_login';
        this.meta.state_changed_at = new Date().toISOString();
      }
      this.meta.state_reason = result.reason;
    } else if (result.state === 'ok') {
      // A page that loads logged in means an operator finished the login.
      this.markLoggedIn();
    }
    if (result.state === 'rate_limited') {
      this.meta.cooldown_until = new Date(Date.now() + this.cfg.rateLimitBackoffSec * 1000).toISOString();
      this.meta.state_reason = result.reason;
    }
    await this.saveMeta();
    return { ...this.describe(), reason: result.reason };
  }

  // The health signal attached to every response, so the caller can keep its
  // own account table current without a separate check.
  signal() {
    const until = this.meta.cooldown_until ? Date.parse(this.meta.cooldown_until) : 0;
    let status = 'active';
    if (this.spec.needsLogin && this.meta.state !== 'ok') status = 'needs_login';
    else if (until > Date.now()) status = 'rate_limited';
    return {
      account_id: this.accountId,
      platform: this.platform,
      status,
      retry_after_sec: status === 'rate_limited' ? Math.ceil((until - Date.now()) / 1000) : 0,
      challenge: this.meta.state === 'needs_login',
      reason: this.meta.state_reason || '',
      warming_up: this.warmingUp(),
    };
  }

  describe() {
    return {
      account_id: this.accountId,
      platform: this.platform,
      state: this.meta.state,
      state_reason: this.meta.state_reason || '',
      login_confirmed_at: this.meta.login_confirmed_at || null,
      cooldown_until: this.meta.cooldown_until || null,
      warming_up: this.warmingUp(),
      open: Boolean(this.context),
      url: this.page ? this.page.url() : null,
      proxy: redactProxy(this.proxy),
      external_browser: Boolean(this.meta.external_browser_ws),
      fingerprint: this.meta.fingerprint || null,
      geolocation: this.proxy && this.meta.geolocation?.proxy_key === proxyKey(this.proxy) ? geolocationOf(this.meta.geolocation) : null,
    };
  }
}

const geolocationOf = ({ latitude, longitude, accuracy }) => ({ latitude, longitude, accuracy });

export class SessionManager {
  constructor(cfg) {
    this.cfg = cfg;
    this.sessions = new Map();
    this.tickets = new Map();
  }

  async readMeta(accountId) {
    try {
      return JSON.parse(await fs.readFile(path.join(this.cfg.profilesDir, accountId, 'meta.json'), 'utf8'));
    } catch (err) {
      if (err.code === 'ENOENT') return null;
      throw err;
    }
  }

  // platform is required the first time an account is seen and must match after.
  async get(accountId, platform) {
    validateAccountId(accountId);
    let session = this.sessions.get(accountId);
    if (!session) {
      let meta = await this.readMeta(accountId);
      if (!meta) {
        if (!platform) throw new ServiceError('not_found', `account ${accountId} does not exist; pass platform to create it`);
        meta = { platform, state: platformSpec(platform).needsLogin ? 'new' : 'ok', created_at: new Date().toISOString() };
      }
      session = new Session(this, accountId, meta);
      await session.saveMeta();
      this.sessions.set(accountId, session);
    }
    if (platform && platform !== session.platform) {
      throw new ServiceError('bad_request', `account ${accountId} is a ${session.platform} account, not ${platform}`);
    }
    return session;
  }

  // A login ticket lets an operator's browser drive one account's login page
  // without holding the service token.
  issueTicket(accountId) {
    const ticket = randomBytes(24).toString('base64url');
    const expiresAt = Date.now() + this.cfg.loginTicketTtlSec * 1000;
    this.tickets.set(ticket, { accountId, expiresAt });
    for (const [t, v] of this.tickets) if (v.expiresAt < Date.now()) this.tickets.delete(t);
    return { ticket, expiresAt: new Date(expiresAt).toISOString() };
  }

  revokeTickets(accountId) {
    for (const [t, v] of this.tickets) if (v.accountId === accountId) this.tickets.delete(t);
  }

  ticketAllows(ticket, accountId) {
    const t = ticket && this.tickets.get(ticket);
    return Boolean(t && t.accountId === accountId && t.expiresAt > Date.now());
  }

  async list() {
    let entries = [];
    try {
      entries = await fs.readdir(this.cfg.profilesDir, { withFileTypes: true });
    } catch (err) {
      if (err.code !== 'ENOENT') throw err;
    }
    const out = [];
    for (const entry of entries) {
      if (!entry.isDirectory()) continue;
      const live = this.sessions.get(entry.name);
      if (live) {
        out.push(live.describe());
        continue;
      }
      const meta = await this.readMeta(entry.name);
      if (meta) out.push(new Session(this, entry.name, meta).describe());
    }
    return out;
  }

  async closeAll() {
    await Promise.all([...this.sessions.values()].map((s) => s.closeContext()));
    this.sessions.clear();
  }
}
