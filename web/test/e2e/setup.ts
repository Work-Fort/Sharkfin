import { execSync, spawn, type ChildProcess } from 'child_process';
import { mkdtempSync, writeFileSync } from 'fs';
import { tmpdir } from 'os';
import { join, dirname } from 'path';
import { fileURLToPath } from 'url';
import * as jose from 'jose';
import { createServer, type Server } from 'http';
import net from 'net';
import WebSocket from 'ws';

let daemon: ChildProcess;
let jwksServer: Server;
const __filename = fileURLToPath(import.meta.url);
const __dirname = dirname(__filename);
const PROJECT_ROOT = join(__dirname, '..', '..', '..');

async function findFreePort(): Promise<number> {
  return new Promise((resolve, reject) => {
    const srv = net.createServer();
    srv.listen(0, '127.0.0.1', () => {
      const port = (srv.address() as net.AddressInfo).port;
      srv.close(() => resolve(port));
    });
    srv.on('error', reject);
  });
}

export default async function globalSetup() {
  // 1. Build web UI.
  execSync('pnpm build', { cwd: join(PROJECT_ROOT, 'web'), stdio: 'inherit' });

  // 2. Build Go binary.
  execSync('go build -o /tmp/sharkfin-e2e .', { cwd: PROJECT_ROOT, stdio: 'inherit' });

  // 3. Start JWKS stub.
  const { privateKey, publicKey } = await jose.generateKeyPair('RS256');
  const publicJWK = await jose.exportJWK(publicKey);
  publicJWK.kid = 'test-key-1';
  publicJWK.alg = 'RS256';
  const jwks = { keys: [publicJWK] };

  jwksServer = createServer((req, res) => {
    if (req.url === '/v1/jwks') {
      res.writeHead(200, { 'Content-Type': 'application/json' });
      res.end(JSON.stringify(jwks));
    } else if (req.url === '/v1/verify-api-key' && req.method === 'POST') {
      res.writeHead(200, { 'Content-Type': 'application/json' });
      res.end(JSON.stringify({
        valid: true,
        key: { userId: 'bridge-id', metadata: { username: 'bridge', name: 'Bridge', display_name: 'Bridge', type: 'service' } },
      }));
    } else {
      res.writeHead(404);
      res.end();
    }
  });
  const jwksPort = await findFreePort();
  await new Promise<void>((resolve) => jwksServer.listen(jwksPort, '127.0.0.1', resolve));

  // 4. Sign JWTs.
  async function signJWT(sub: string, username: string, name: string, type: string): Promise<string> {
    return new jose.SignJWT({ username, name, display_name: name, type })
      .setProtectedHeader({ alg: 'RS256', kid: 'test-key-1' })
      .setSubject(sub)
      .setIssuer('passport-stub')
      .setAudience('sharkfin')
      .setIssuedAt()
      .setExpirationTime('1h')
      .sign(privateKey);
  }

  const adminToken = await signJWT('admin-id', 'admin', 'Admin', 'user');
  const aliceToken = await signJWT('alice-id', 'alice', 'Alice', 'user');

  // 5. Start daemon.
  const xdgDir = mkdtempSync(join(tmpdir(), 'sharkfin-e2e-'));
  const daemonPort = await findFreePort();
  const daemonAddr = `127.0.0.1:${daemonPort}`;

  daemon = spawn('/tmp/sharkfin-e2e', [
    'daemon',
    '--daemon', daemonAddr,
    '--passport-url', `http://127.0.0.1:${jwksPort}`,
    '--log-level', 'disabled',
    '--ui-dir', join(PROJECT_ROOT, 'web', 'dist'),
  ], {
    env: {
      ...process.env,
      XDG_CONFIG_HOME: join(xdgDir, 'config'),
      XDG_STATE_HOME: join(xdgDir, 'state'),
    },
    stdio: ['ignore', 'inherit', 'inherit'],
  });

  // Wait for daemon to be ready.
  for (let i = 0; i < 50; i++) {
    try {
      const res = await fetch(`http://${daemonAddr}/ui/health`);
      if (res.ok) break;
    } catch { /* not ready */ }
    await new Promise((r) => setTimeout(r, 100));
  }

  // 6. Register admin user by connecting via WS (user record created on first connect).
  await new Promise<void>((resolve, reject) => {
    const ws = new WebSocket(`ws://${daemonAddr}/ws`, {
      headers: { Authorization: `Bearer ${adminToken}` },
    });
    ws.on('open', () => {
      ws.close();
    });
    ws.on('close', () => resolve());
    ws.on('error', reject);
  });

  // 7. Grant admin role.
  execSync(`/tmp/sharkfin-e2e admin set-role admin admin`, {
    env: {
      ...process.env,
      XDG_CONFIG_HOME: join(xdgDir, 'config'),
      XDG_STATE_HOME: join(xdgDir, 'state'),
    },
    stdio: 'inherit',
  });

  // 8. Write config for tests.
  const configPath = join(tmpdir(), 'sharkfin-e2e-config.json');
  writeFileSync(configPath, JSON.stringify({ daemonAddr, adminToken, aliceToken, xdgDir }));
  process.env.SHARKFIN_E2E_CONFIG = configPath;

  // Return teardown.
  return async () => {
    daemon?.kill();
    jwksServer?.close();
  };
}
