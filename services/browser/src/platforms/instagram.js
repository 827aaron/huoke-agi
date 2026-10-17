export default {
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
};
