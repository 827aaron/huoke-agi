import http from 'node:http';
import { timingSafeEqual } from 'node:crypto';
import { ServiceError, STATUS, toServiceError } from './errors.js';
import { loginPage } from './login-page.js';

const MAX_BODY = 1024 * 1024;

const ACTIONS = {
  navigate: (s, b) => s.navigate(b),
  scroll: (s, b) => s.scroll(b),
  content: (s, b) => s.content(b),
  click: (s, b) => s.click(b),
  type: (s, b) => s.type(b),
  captures: (s, b) => s.capturesAfter(b),
  input: (s, b) => s.input(b),
  'login/start': async (s, b, m, cfg) => {
    await s.loginStart(b);
    const { ticket, expiresAt } = m.issueTicket(s.accountId);
    const path = `/login/${encodeURIComponent(s.accountId)}?ticket=${ticket}`;
    return { ...s.describe(), login_url: cfg.publicUrl + path, expires_at: expiresAt };
  },
  'login/confirm': (s) => s.confirmLogin(),
  'login/cancel': (s) => s.cancelLogin(),
  health: (s, b) => s.health(b),
  config: (s) => s.describe(),
  close: async (s) => {
    await s.closeContext();
    return s.describe();
  },
};

function readBody(req) {
  return new Promise((resolve, reject) => {
    const chunks = [];
    let size = 0;
    req.on('data', (chunk) => {
      size += chunk.length;
      if (size > MAX_BODY) {
        reject(new ServiceError('bad_request', 'request body too large'));
        req.destroy();
        return;
      }
      chunks.push(chunk);
    });
    req.on('end', () => {
      if (size === 0) return resolve({});
      try {
        const parsed = JSON.parse(Buffer.concat(chunks).toString('utf8'));
        if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) throw new Error('not an object');
        resolve(parsed);
      } catch {
        reject(new ServiceError('bad_request', 'body must be a JSON object'));
      }
    });
    req.on('error', reject);
  });
}

function sendJSON(res, status, payload) {
  const body = JSON.stringify(payload);
  res.writeHead(status, { 'Content-Type': 'application/json; charset=utf-8', 'Content-Length': Buffer.byteLength(body) });
  res.end(body);
}

function sendError(res, err, session) {
  const e = toServiceError(err);
  const error = { code: e.code, message: e.message };
  if (e.retry_after_sec !== undefined) error.retry_after_sec = e.retry_after_sec;
  if (e.account_state !== undefined) error.account_state = e.account_state;
  if (e.retry_after_sec !== undefined) res.setHeader('Retry-After', String(e.retry_after_sec));
  const payload = { error };
  if (session) payload.account = session.signal();
  sendJSON(res, STATUS[e.code] || 500, payload);
}

function tokenMatches(cfg, req, url) {
  if (!cfg.token) return true;
  const header = req.headers.authorization || '';
  const given = header.startsWith('Bearer ') ? header.slice(7) : url.searchParams.get('token') || '';
  const a = Buffer.from(given);
  const b = Buffer.from(cfg.token);
  return a.length === b.length && timingSafeEqual(a, b);
}

// What a login ticket may reach: its own account's login page and the calls
// that page makes, nothing an agent uses.
const TICKET_ROUTES = new Set(['GET login', 'GET sessions/', 'GET sessions/screenshot.png', 'POST sessions/input', 'POST sessions/login/confirm']);

function ticketRoute(method, parts) {
  if (parts[0] === 'login' && parts.length === 2) return { key: `${method} login`, accountId: parts[1] };
  if (parts[0] === 'sessions' && parts.length >= 2) return { key: `${method} sessions/${parts.slice(2).join('/')}`, accountId: parts[1] };
  return null;
}

export function createServer(cfg, manager) {
  return http.createServer(async (req, res) => {
    const url = new URL(req.url, 'http://localhost');
    let session = null;
    try {
      if (req.method === 'GET' && url.pathname === '/healthz') return sendJSON(res, 200, { ok: true });
      const parts = url.pathname.split('/').filter(Boolean).map(decodeURIComponent);
      const trusted = tokenMatches(cfg, req, url);
      if (!trusted) {
        const route = ticketRoute(req.method, parts);
        const ok = route && TICKET_ROUTES.has(route.key) && manager.ticketAllows(url.searchParams.get('ticket'), route.accountId);
        if (!ok) throw new ServiceError('unauthorized', 'missing or wrong token');
      }

      if (req.method === 'GET' && parts.length === 1 && parts[0] === 'sessions') {
        return sendJSON(res, 200, { sessions: await manager.list() });
      }

      if (req.method === 'GET' && parts.length === 2 && parts[0] === 'login') {
        const session = await manager.get(parts[1], url.searchParams.get('platform') || undefined);
        const html = loginPage(session.accountId, session.platform);
        res.writeHead(200, { 'Content-Type': 'text/html; charset=utf-8', 'Cache-Control': 'no-store' });
        return res.end(html);
      }

      if (parts[0] !== 'sessions' || parts.length < 2) throw new ServiceError('not_found', `no route ${req.method} ${url.pathname}`);
      const accountId = parts[1];
      const action = parts.slice(2).join('/');

      if (req.method === 'DELETE' && action === '') {
        await manager.remove(accountId);
        res.writeHead(204);
        return res.end();
      }

      if (req.method === 'GET') {
        session = await manager.get(accountId, url.searchParams.get('platform') || undefined);
        if (action === '') return sendJSON(res, 200, session.describe());
        if (action === 'health') return sendJSON(res, 200, await session.run(() => session.health({})));
        if (action === 'screenshot.png') {
          const png = await session.run(() => session.screenshot({ full_page: url.searchParams.get('full_page') === 'true' }));
          res.writeHead(200, { 'Content-Type': 'image/png', 'Content-Length': png.length, 'Cache-Control': 'no-store' });
          return res.end(png);
        }
        throw new ServiceError('not_found', `no route GET ${url.pathname}`);
      }

      if (req.method !== 'POST' || !ACTIONS[action]) throw new ServiceError('not_found', `no route ${req.method} ${url.pathname}`);
      const body = await readBody(req);
      session = await manager.get(accountId, body.platform);
      // A login ticket drives the window; only the service token may move an
      // account to another proxy, fingerprint or browser.
      const result = await session.run(async () => {
        if (trusted) await session.configure(body);
        return ACTIONS[action](session, body, manager, cfg);
      });
      return sendJSON(res, 200, { ...result, account: session.signal() });
    } catch (err) {
      return sendError(res, err, session);
    }
  });
}
