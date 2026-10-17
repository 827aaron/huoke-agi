import path from 'node:path';

function int(env, name, def) {
  const raw = env[name];
  if (raw === undefined || raw === '') return def;
  const n = Number.parseInt(raw, 10);
  if (!Number.isFinite(n) || n < 0) throw new Error(`${name} must be a non-negative integer, got ${raw}`);
  return n;
}

function float(env, name, def) {
  const raw = env[name];
  if (raw === undefined || raw === '') return def;
  const n = Number.parseFloat(raw);
  if (!Number.isFinite(n) || n < 0) throw new Error(`${name} must be a non-negative number, got ${raw}`);
  return n;
}

function bool(env, name, def) {
  const raw = env[name];
  if (raw === undefined || raw === '') return def;
  return ['1', 'true', 'yes', 'on'].includes(raw.toLowerCase());
}

// Every pacing value is a default the operator can change; none of them is a quota.
export function loadConfig(env = process.env) {
  const cfg = {
    host: env.BROWSER_HOST || '0.0.0.0',
    port: int(env, 'BROWSER_PORT', 7070),
    token: env.BROWSER_TOKEN || '',
    // How an operator's browser reaches this service, for login links. Empty
    // gives links relative to wherever the caller proxies the service.
    publicUrl: (env.BROWSER_PUBLIC_URL || '').replace(/\/+$/, ''),
    loginTicketTtlSec: int(env, 'BROWSER_LOGIN_TICKET_TTL_SEC', 1800),
    profilesDir: path.resolve(env.BROWSER_PROFILES_DIR || './data/profiles'),
    headless: bool(env, 'BROWSER_HEADLESS', true),
    locale: env.BROWSER_LOCALE || 'en-US',
    timezoneId: env.BROWSER_TIMEZONE || '',
    proxy: env.BROWSER_PROXY || '',
    navigationTimeoutMs: int(env, 'BROWSER_NAV_TIMEOUT_MS', 45000),
    actionDelayMinMs: int(env, 'BROWSER_ACTION_DELAY_MIN_MS', 1500),
    actionDelayMaxMs: int(env, 'BROWSER_ACTION_DELAY_MAX_MS', 4500),
    warmupDays: int(env, 'BROWSER_WARMUP_DAYS', 7),
    warmupFactor: float(env, 'BROWSER_WARMUP_FACTOR', 2.5),
    rateLimitBackoffSec: int(env, 'BROWSER_RATE_LIMIT_BACKOFF_SEC', 3600),
    idleCloseMs: int(env, 'BROWSER_IDLE_CLOSE_MS', 10 * 60 * 1000),
    captureLimit: int(env, 'BROWSER_CAPTURE_LIMIT', 300),
    captureBodyMaxBytes: int(env, 'BROWSER_CAPTURE_BODY_MAX_BYTES', 4 * 1024 * 1024),
  };
  // BROWSER_PACE_FACTOR_TIKTOK=1 overrides the factor a platform file sets.
  cfg.paceFactors = {};
  for (const name of Object.keys(env)) {
    const m = name.match(/^BROWSER_PACE_FACTOR_([A-Z0-9_]+)$/);
    if (m) cfg.paceFactors[m[1].toLowerCase()] = float(env, name, 1);
  }
  if (cfg.actionDelayMaxMs < cfg.actionDelayMinMs) cfg.actionDelayMaxMs = cfg.actionDelayMinMs;
  return cfg;
}
