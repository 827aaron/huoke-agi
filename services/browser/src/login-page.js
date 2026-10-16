// The operator login page: a screenshot of the account's own browser that
// forwards clicks and keys, so a person can pass the login and any
// verification step on the server without a desktop session.
export function loginPage(accountId, platform) {
  const id = JSON.stringify(accountId);
  return `<!doctype html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>账号登录 · ${escapeHtml(accountId)}</title>
<style>
  body { font-family: system-ui, sans-serif; margin: 0; background: #111; color: #eee; }
  header { display: flex; flex-wrap: wrap; gap: 8px; align-items: center; padding: 10px 16px; background: #1d1d1d; }
  header b { margin-right: 12px; }
  button, input { font: inherit; padding: 6px 10px; border-radius: 6px; border: 1px solid #444; background: #2a2a2a; color: #eee; }
  button:hover { background: #3a3a3a; }
  #state { margin-left: auto; font-size: 14px; }
  #screen { display: block; max-width: 100%; cursor: crosshair; margin: 0 auto; }
  .ok { color: #6c6; } .bad { color: #e66; }
</style>
</head>
<body>
<header>
  <b>${escapeHtml(platform)} · ${escapeHtml(accountId)}</b>
  <button data-act="login">打开登录页</button>
  <input id="text" placeholder="输入文字后点发送" size="28">
  <button data-act="type">发送文字</button>
  <button data-act="Enter">回车</button>
  <button data-act="Tab">Tab</button>
  <button data-act="Backspace">退格</button>
  <button data-act="up">上滚</button>
  <button data-act="down">下滚</button>
  <button data-act="confirm">我已登录完成</button>
  <span id="state"></span>
</header>
<img id="screen" alt="浏览器画面">
<script>
const account = ${id};
const params = new URLSearchParams(location.search);
const auth = new URLSearchParams();
for (const k of ['ticket', 'token']) if (params.get(k)) auth.set(k, params.get(k));
const base = location.pathname.slice(0, location.pathname.lastIndexOf('/login/')) + '/sessions/' + encodeURIComponent(account);
const withAuth = (path) => base + path + (path.includes('?') ? '&' : '?') + auth.toString();
const headers = { 'Content-Type': 'application/json' };
let confirmed = false;
const img = document.getElementById('screen');
const stateEl = document.getElementById('state');
let busy = false;

async function call(path, body) {
  const res = await fetch(withAuth(path), { method: 'POST', headers, body: JSON.stringify(body || {}) });
  const data = await res.json().catch(() => ({}));
  if (!res.ok) throw new Error((data.error && data.error.message) || res.statusText);
  return data;
}

async function refresh() {
  const res = await fetch(withAuth('/screenshot.png?ts=' + Date.now()));
  if (!res.ok) return;
  const blob = await res.blob();
  const old = img.src;
  img.src = URL.createObjectURL(blob);
  if (old) URL.revokeObjectURL(old);
}

async function act(fn) {
  if (busy) return;
  busy = true;
  try { await fn(); } catch (e) { stateEl.textContent = e.message; stateEl.className = 'bad'; }
  busy = false;
  setTimeout(refresh, 400);
}

img.addEventListener('click', (e) => {
  const r = img.getBoundingClientRect();
  const x = (e.clientX - r.left) * (img.naturalWidth / r.width);
  const y = (e.clientY - r.top) * (img.naturalHeight / r.height);
  act(() => call('/input', { kind: 'click', x, y }));
});

document.querySelector('header').addEventListener('click', (e) => {
  const act_ = e.target.dataset && e.target.dataset.act;
  if (!act_) return;
  if (act_ === 'login') return act(() => call('/input', { kind: 'goto' }));
  if (act_ === 'type') {
    const input = document.getElementById('text');
    const text = input.value;
    input.value = '';
    return act(() => call('/input', { kind: 'type', text }));
  }
  if (act_ === 'up' || act_ === 'down') return act(() => call('/input', { kind: 'scroll', dy: act_ === 'up' ? -500 : 500 }));
  if (act_ === 'confirm') {
    return act(async () => {
      const s = await call('/login/confirm');
      confirmed = s.state === 'ok';
      stateEl.textContent = s.state === 'ok' ? '已登录，AI 可以使用这个账号了' : '还没有登录成功：' + (s.reason || s.state);
      stateEl.className = s.state === 'ok' ? 'ok' : 'bad';
    });
  }
  act(() => call('/input', { kind: 'press', key: act_ }));
});

// Closing the window counts as done: the service checks the login itself.
window.addEventListener('pagehide', () => {
  if (!confirmed) navigator.sendBeacon(withAuth('/login/confirm'), '{}');
});

refresh();
setInterval(() => { if (!busy && !document.hidden) refresh(); }, 1500);
</script>
</body>
</html>`;
}

function escapeHtml(s) {
  return String(s).replace(/[&<>"']/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' })[c]);
}
