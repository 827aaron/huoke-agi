import { loadConfig } from './config.js';
import { createServer } from './server.js';
import { SessionManager } from './sessions.js';

const cfg = loadConfig();
const manager = new SessionManager(cfg);
const server = createServer(cfg, manager);

if (!cfg.token) console.warn('BROWSER_TOKEN is empty: every request is accepted without a token');

server.listen(cfg.port, cfg.host, () => {
  console.log(`browser service on ${cfg.host}:${cfg.port}, profiles in ${cfg.profilesDir}`);
});

async function shutdown() {
  server.close();
  await manager.closeAll();
  process.exit(0);
}
process.on('SIGINT', shutdown);
process.on('SIGTERM', shutdown);
