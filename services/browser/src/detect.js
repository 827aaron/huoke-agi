// Session-state detection from what a page shows. The platforms change their
// wording and paths often; keeping the patterns here keeps the fix in one file.

export const PLATFORMS = {
  facebook: {
    home: 'https://www.facebook.com/',
    login: 'https://www.facebook.com/login/',
    capture: [/\/api\/graphql\/?(\?|$)/],
    loginPaths: [/^\/login(\/|\.php|$)/, /^\/checkpoint(\/|$)/, /^\/recover(\/|$)/, /^\/two_step_verification(\/|$)/],
    rateLimitText: [
      /you['’]re temporarily blocked/i,
      /you can['’]t use this feature right now/i,
      /we limit how often you can post, comment or do other things/i,
      /你暂时被禁止/,
      /你暂时无法使用此功能/,
    ],
  },
  instagram: {
    home: 'https://www.instagram.com/',
    login: 'https://www.instagram.com/accounts/login/',
    capture: [/\/api\/v1\//, /\/graphql\/query\/?/, /\/api\/graphql\/?(\?|$)/],
    loginPaths: [/^\/accounts\/login(\/|$)/, /^\/challenge(\/|$)/, /^\/accounts\/suspended(\/|$)/, /^\/accounts\/disabled(\/|$)/, /^\/auth_platform(\/|$)/],
    rateLimitText: [
      /please wait a few minutes before you try again/i,
      /we restrict certain activity to protect our community/i,
      /请稍等几分钟再试/,
      /请稍后重试/,
    ],
  },
  web: {
    home: 'about:blank',
    login: 'about:blank',
    capture: [],
    loginPaths: [],
    rateLimitText: [],
  },
};

export function platformSpec(platform) {
  const spec = PLATFORMS[platform];
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
  if (platform !== 'web' && hasPasswordInput) return { state: 'needs_login', reason: 'login form on page' };
  for (const re of spec.rateLimitText) {
    if (re.test(text)) return { state: 'rate_limited', reason: `page says "${text.match(re)[0]}"` };
  }
  return { state: 'ok', reason: '' };
}

export function shouldCapture(platform, url) {
  return platformSpec(platform).capture.some((re) => re.test(url));
}
