import { describe, test } from 'node:test';
import assert from 'node:assert/strict';
import { generateFingerprint, parseProxy, redactProxy } from '../src/profile.js';

const cfg = { locale: 'en-US', timezoneId: '' };
const zeros = () => 0;

describe('generateFingerprint', () => {
  test('takes locale and timezone from a known region', () => {
    const fp = generateFingerprint({ chromeMajor: '141', region: 'br', cfg, rand: zeros });
    assert.deepEqual(fp, {
      version: 1,
      os: 'windows',
      region: 'BR',
      chrome_version: '141',
      user_agent: 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/141.0.0.0 Safari/537.36',
      platform: 'Win32',
      locale: 'pt-BR',
      timezone: 'America/Sao_Paulo',
      viewport: { width: 1366, height: 638 },
      screen: { width: 1366, height: 768 },
      device_scale_factor: 1,
      hardware_concurrency: 4,
    });
  });

  test('falls back to the configured locale for an unknown region', () => {
    const fp = generateFingerprint({ chromeMajor: '141', region: 'ZZ', cfg: { locale: 'en-GB', timezoneId: 'Europe/Dublin' }, rand: zeros });
    assert.equal(fp.region, '');
    assert.equal(fp.locale, 'en-GB');
    assert.equal(fp.timezone, 'Europe/Dublin');
  });

  test('a mac fingerprint keeps a mac platform and a retina scale', () => {
    const fp = generateFingerprint({ chromeMajor: '141', region: 'US', cfg, rand: () => 0.99 });
    assert.equal(fp.os, 'mac');
    assert.equal(fp.platform, 'MacIntel');
    assert.match(fp.user_agent, /Macintosh; Intel Mac OS X/);
    assert.equal(fp.device_scale_factor, 2);
  });
});

describe('parseProxy', () => {
  test('splits credentials out of the server address', () => {
    assert.deepEqual(parseProxy('http://bob:p%40ss@10.0.0.1:3128'), { server: 'http://10.0.0.1:3128', username: 'bob', password: 'p@ss' });
  });

  test('empty is a direct connection', () => {
    assert.equal(parseProxy(''), null);
  });

  test('socks5 without credentials is accepted', () => {
    assert.deepEqual(parseProxy('socks5://10.0.0.1:1080'), { server: 'socks5://10.0.0.1:1080' });
  });
});

describe('redactProxy', () => {
  test('hides the user and password', () => {
    assert.equal(redactProxy('https://bob:pw@proxy.example:8443'), 'https://***:***@proxy.example:8443');
  });
});
