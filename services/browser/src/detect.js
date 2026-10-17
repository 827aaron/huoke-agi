// Session-state detection from what a page shows. Each platform is one file in
// ./platforms, named after the platform; adding a platform is adding a file.
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const DIR = path.join(path.dirname(fileURLToPath(import.meta.url)), 'platforms');

async function loadPlatforms() {
  const out = {};
  for (const file of fs.readdirSync(DIR).sort()) {
    if (!file.endsWith('.js')) continue;
    const spec = (await import(path.join(DIR, file))).default;
    out[file.slice(0, -3)] = {
      needsLogin: true,
      challengeText: [],
      agentPaths: null,
      block: [],
      paceFactor: 1,
      initScript: '',
      ...spec,
    };
  }
  return out;
}

export const PLATFORMS = await loadPlatforms();

export function platformSpec(platform) {
  const spec = Object.hasOwn(PLATFORMS, platform) ? PLATFORMS[platform] : null;
  if (!spec) throw Object.assign(new Error(`unknown platform ${platform}`), { code: 'bad_request' });
  return spec;
}

// signals: { url, text, hasPasswordInput }. text is the visible body text.
export function detectState(platform, { url = '', text = '', hasPasswordInput = false }) {
  const spec = platformSpec(platform);
  let pathname = '';
  try {
    pathname = new URL(url).pathname;
  } catch {
    pathname = '';
  }
  for (const re of spec.loginPaths) {
    if (re.test(pathname)) return { state: 'needs_login', reason: `redirected to ${pathname}` };
  }
  if (spec.needsLogin && hasPasswordInput) return { state: 'needs_login', reason: 'login form on page' };
  for (const re of spec.challengeText) {
    if (re.test(text)) return { state: 'needs_login', reason: `verification on page: "${text.match(re)[0]}"` };
  }
  for (const re of spec.rateLimitText) {
    if (re.test(text)) return { state: 'rate_limited', reason: `page says "${text.match(re)[0]}"` };
  }
  return { state: 'ok', reason: '' };
}

export function shouldCapture(platform, url) {
  return platformSpec(platform).capture.some((re) => re.test(url));
}

// Agent navigation stays on the pages a platform's connector reads, when the
// platform lists them. The operator login window is not limited.
export function agentMayOpen(platform, url) {
  const { agentPaths } = platformSpec(platform);
  if (!agentPaths) return true;
  let pathname;
  try {
    pathname = new URL(url).pathname;
  } catch {
    return false;
  }
  return agentPaths.some((re) => re.test(pathname));
}
