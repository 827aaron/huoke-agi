import { FingerprintGenerator } from 'fingerprint-generator';
import { FingerprintInjector } from 'fingerprint-injector';
import { ServiceError } from './errors.js';

// Locale and timezone per exit country, so a page sees a browser that matches
// the IP it arrives from. Countries outside the table fall back to the default.
export const REGIONS = {
  US: { locale: 'en-US', timezones: ['America/New_York', 'America/Chicago', 'America/Los_Angeles'] },
  CA: { locale: 'en-CA', timezones: ['America/Toronto', 'America/Vancouver'] },
  GB: { locale: 'en-GB', timezones: ['Europe/London'] },
  AU: { locale: 'en-AU', timezones: ['Australia/Sydney', 'Australia/Melbourne'] },
  DE: { locale: 'de-DE', timezones: ['Europe/Berlin'] },
  FR: { locale: 'fr-FR', timezones: ['Europe/Paris'] },
  ES: { locale: 'es-ES', timezones: ['Europe/Madrid'] },
  IT: { locale: 'it-IT', timezones: ['Europe/Rome'] },
  NL: { locale: 'nl-NL', timezones: ['Europe/Amsterdam'] },
  RU: { locale: 'ru-RU', timezones: ['Europe/Moscow'] },
  TR: { locale: 'tr-TR', timezones: ['Europe/Istanbul'] },
  BR: { locale: 'pt-BR', timezones: ['America/Sao_Paulo'] },
  MX: { locale: 'es-MX', timezones: ['America/Mexico_City'] },
  JP: { locale: 'ja-JP', timezones: ['Asia/Tokyo'] },
  KR: { locale: 'ko-KR', timezones: ['Asia/Seoul'] },
  CN: { locale: 'zh-CN', timezones: ['Asia/Shanghai'] },
  HK: { locale: 'zh-HK', timezones: ['Asia/Hong_Kong'] },
  TW: { locale: 'zh-TW', timezones: ['Asia/Taipei'] },
  SG: { locale: 'en-SG', timezones: ['Asia/Singapore'] },
  MY: { locale: 'ms-MY', timezones: ['Asia/Kuala_Lumpur'] },
  TH: { locale: 'th-TH', timezones: ['Asia/Bangkok'] },
  VN: { locale: 'vi-VN', timezones: ['Asia/Ho_Chi_Minh'] },
  ID: { locale: 'id-ID', timezones: ['Asia/Jakarta'] },
  PH: { locale: 'en-PH', timezones: ['Asia/Manila'] },
  IN: { locale: 'en-IN', timezones: ['Asia/Kolkata'] },
  AE: { locale: 'ar-AE', timezones: ['Asia/Dubai'] },
  SA: { locale: 'ar-SA', timezones: ['Asia/Riyadh'] },
};

const SYSTEMS = [
  {
    os: 'windows',
    suiteOs: 'windows',
    weight: 7,
    platform: 'Win32',
    ua: 'Windows NT 10.0; Win64; x64',
    viewports: [
      [1366, 768],
      [1536, 864],
      [1440, 900],
      [1600, 900],
      [1920, 1080],
    ],
    scales: [1, 1, 1.25],
  },
  {
    os: 'mac',
    suiteOs: 'macos',
    weight: 3,
    platform: 'MacIntel',
    ua: 'Macintosh; Intel Mac OS X 10_15_7',
    viewports: [
      [1440, 900],
      [1512, 982],
      [1680, 1050],
    ],
    scales: [2],
  },
];

const CORES = [4, 8, 8, 12, 16];

const pick = (list, rand) => list[Math.floor(rand() * list.length) % list.length];

let defaultGenerator;
function suiteGenerator() {
  defaultGenerator ??= new FingerprintGenerator();
  return defaultGenerator;
}

// The browser's own major version goes into the UA: claiming another version
// than the engine has is easier to spot than any default. fingerprint-suite
// draws the rest (screen, cores, memory, WebGL, codecs, fonts) as a set seen
// together on real devices; when its data has no device for this engine
// version, the small tables above are used instead.
export function generateFingerprint({ chromeMajor, region, cfg, rand = Math.random, generator = suiteGenerator() }) {
  const country = (region || '').toUpperCase();
  const known = REGIONS[country];
  const total = SYSTEMS.reduce((n, s) => n + s.weight, 0);
  let roll = rand() * total;
  const system = SYSTEMS.find((s) => (roll -= s.weight) < 0) || SYSTEMS[0];
  const locale = known ? known.locale : cfg.locale;
  const base = {
    os: system.os,
    region: known ? country : '',
    chrome_version: String(chromeMajor),
    locale,
    timezone: known ? pick(known.timezones, rand) : cfg.timezoneId || 'America/New_York',
  };
  const suite = generator && drawSuite(generator, Number(chromeMajor), system.suiteOs, locale);
  if (suite) {
    const { navigator: nav, screen } = suite.fingerprint;
    return {
      version: 2,
      ...base,
      user_agent: nav.userAgent,
      platform: nav.platform,
      viewport: { width: screen.width, height: Math.max(screen.availHeight - 80, 480) },
      screen: { width: screen.width, height: screen.height },
      device_scale_factor: screen.devicePixelRatio || 1,
      hardware_concurrency: nav.hardwareConcurrency || 8,
      suite,
    };
  }
  const [width, height] = pick(system.viewports, rand);
  return {
    version: 1,
    ...base,
    user_agent: `Mozilla/5.0 (${system.ua}) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/${chromeMajor}.0.0.0 Safari/537.36`,
    platform: system.platform,
    viewport: { width, height: height - 130 },
    screen: { width, height },
    device_scale_factor: pick(system.scales, rand),
    hardware_concurrency: pick(CORES, rand),
  };
}

// The generator relaxes constraints it cannot meet, down to another browser,
// so what it returns is checked rather than trusted.
function drawSuite(generator, major, os, locale) {
  let suite;
  try {
    suite = generator.getFingerprint({
      browsers: [{ name: 'chrome', minVersion: major, maxVersion: major }],
      operatingSystems: [os],
      devices: ['desktop'],
      locales: [locale],
    });
  } catch {
    return null;
  }
  const ua = suite.fingerprint.navigator.userAgent || '';
  if (!ua.includes(`Chrome/${major}.`) || /Edg\/|OPR\//.test(ua)) return null;
  return suite;
}

// The top-level fields win over the suite's copies of them, so a fingerprint
// the backend edited is what the page sees.
export async function attachSuite(context, fp) {
  const suite = structuredClone(fp.suite);
  const nav = suite.fingerprint.navigator;
  nav.userAgent = fp.user_agent;
  nav.platform = fp.platform;
  nav.hardwareConcurrency = fp.hardware_concurrency || nav.hardwareConcurrency;
  nav.language = fp.locale;
  nav.languages = acceptLanguage(fp.locale).split(',').map((l) => l.split(';')[0]);
  Object.assign(suite.fingerprint.screen, {
    width: fp.screen.width,
    height: fp.screen.height,
    devicePixelRatio: fp.device_scale_factor || 1,
  });
  suite.headers['user-agent'] = fp.user_agent;
  await new FingerprintInjector().attachFingerprintToPlaywright(context, suite);
}

export function validateFingerprint(fp) {
  if (!fp || typeof fp !== 'object' || Array.isArray(fp)) throw new ServiceError('bad_request', 'fingerprint must be an object');
  const need = ['user_agent', 'platform', 'locale', 'timezone'];
  for (const key of need) {
    if (typeof fp[key] !== 'string' || !fp[key]) throw new ServiceError('bad_request', `fingerprint.${key} is required`);
  }
  for (const key of ['viewport', 'screen']) {
    const v = fp[key];
    if (!v || !(v.width > 0) || !(v.height > 0)) throw new ServiceError('bad_request', `fingerprint.${key} needs width and height`);
  }
  return fp;
}

export function contextOptions(fp) {
  return {
    userAgent: fp.user_agent,
    locale: fp.locale,
    timezoneId: fp.timezone,
    viewport: { width: fp.viewport.width, height: fp.viewport.height },
    screen: { width: fp.screen.width, height: fp.screen.height },
    deviceScaleFactor: fp.device_scale_factor || 1,
    extraHTTPHeaders: { 'Accept-Language': acceptLanguage(fp.locale) },
  };
}

function acceptLanguage(locale) {
  const lang = locale.split('-')[0];
  return lang === locale ? locale : `${locale},${lang};q=0.9`;
}

// Context options cannot set navigator.platform or hardwareConcurrency, and a
// Linux platform under a Windows UA is the first mismatch a page checks.
export function initScript(fp) {
  const values = JSON.stringify({ platform: fp.platform, hardwareConcurrency: fp.hardware_concurrency || 8 });
  return `(() => {
  const v = ${values};
  for (const [key, value] of Object.entries(v)) {
    Object.defineProperty(Navigator.prototype, key, { get: () => value, configurable: true });
  }
})();`;
}

// Accepts http://, https:// and socks5://, with optional user:pass@, and
// returns Playwright's proxy option. Empty means a direct connection.
export function parseProxy(value) {
  if (!value) return null;
  let url;
  try {
    url = new URL(value);
  } catch {
    throw new ServiceError('bad_request', 'proxy must be a URL such as http://user:pass@host:port');
  }
  if (!['http:', 'https:', 'socks5:'].includes(url.protocol)) {
    throw new ServiceError('bad_request', `proxy scheme ${url.protocol} is not supported; use http, https or socks5`);
  }
  if (!url.hostname || !url.port) throw new ServiceError('bad_request', 'proxy needs a host and a port');
  const username = decodeURIComponent(url.username);
  const password = decodeURIComponent(url.password);
  if (url.protocol === 'socks5:' && username) {
    throw new ServiceError('bad_request', 'Chromium cannot authenticate to a SOCKS5 proxy; use an http proxy or an IP-allowlisted socks5 one');
  }
  const option = { server: `${url.protocol}//${url.hostname}:${url.port}` };
  if (username) {
    option.username = username;
    option.password = password;
  }
  return option;
}

export function redactProxy(value) {
  if (!value) return '';
  try {
    const url = new URL(value);
    if (url.username || url.password) {
      url.username = url.username ? '***' : '';
      url.password = url.password ? '***' : '';
    }
    return url.toString().replace(/\/$/, '');
  } catch {
    return '***';
  }
}

export function validateCdpUrl(value) {
  if (!value) return '';
  let url;
  try {
    url = new URL(value);
  } catch {
    throw new ServiceError('bad_request', 'external_browser_ws must be a ws:// or http:// CDP endpoint');
  }
  if (!['ws:', 'wss:', 'http:', 'https:'].includes(url.protocol)) {
    throw new ServiceError('bad_request', 'external_browser_ws must be a ws:// or http:// CDP endpoint');
  }
  return value;
}
