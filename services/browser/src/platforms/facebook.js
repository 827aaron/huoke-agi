export default {
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
};
