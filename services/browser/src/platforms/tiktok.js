// TikTok pages are video players: a tab left to itself streams megabytes a
// minute. Agents only need the JSON behind search, a creator's video list and
// a video's comments, so media is never fetched and players never start.
export default {
  home: 'https://www.tiktok.com/',
  login: 'https://www.tiktok.com/login',
  capture: [
    /\/api\/search\/(general|item|user)\/full\/?/,
    /\/api\/comment\/list\/(reply\/)?/,
    /\/api\/post\/item_list\/?/,
    /\/api\/user\/detail\/?/,
  ],
  loginPaths: [/^\/login(\/|$)/],
  // The slider and shape puzzles are verification; an operator solves them in
  // the login window.
  challengeText: [
    /drag the slider to fit the puzzle/i,
    /select 2 objects that are the same shape/i,
    /verify to continue/i,
    /拖动滑块完成拼图/,
    /请完成安全验证/,
  ],
  rateLimitText: [
    /too many attempts\. try again later/i,
    /you are visiting too frequently/i,
    /maximum number of attempts reached/i,
    /操作过于频繁/,
  ],
  // Search, creator pages and single videos (whose page holds the comments).
  agentPaths: [/^\/search(\/(video|user))?\/?$/, /^\/@[^/]+\/?$/, /^\/@[^/]+\/(video|photo)\/\d+\/?$/],
  block: ['media', 'image', 'font'],
  paceFactor: 2,
  initScript: `HTMLMediaElement.prototype.play = function () { this.pause(); return Promise.resolve(); };
HTMLMediaElement.prototype.load = function () {};`,
};
