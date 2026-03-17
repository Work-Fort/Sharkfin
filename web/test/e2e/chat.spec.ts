import { test, expect } from '@playwright/test';
import { readFileSync } from 'fs';
import { tmpdir } from 'os';
import { join } from 'path';

function getConfig() {
  const path = process.env.SHARKFIN_E2E_CONFIG || join(tmpdir(), 'sharkfin-e2e-config.json');
  return JSON.parse(readFileSync(path, 'utf-8'));
}

test.describe('Sharkfin Web UI E2E', () => {
  let config: { daemonAddr: string; adminToken: string; aliceToken: string };

  test.beforeAll(() => {
    config = getConfig();
  });

  test('UI health endpoint responds', async ({ request }) => {
    const res = await request.get(`http://${config.daemonAddr}/ui/health`);
    expect(res.ok()).toBe(true);
  });

  test('UI serves index.html with correct title', async ({ page }) => {
    await page.goto(`http://${config.daemonAddr}/ui/`);
    await expect(page).toHaveTitle('Sharkfin');
    // The #root mount point should be present.
    await expect(page.locator('#root')).toBeAttached();
  });

  test('static assets are served correctly', async ({ request }) => {
    // The CSS bundle should be accessible.
    const indexResp = await request.get(`http://${config.daemonAddr}/ui/`);
    const html = await indexResp.text();
    // Extract a CSS asset path from the HTML.
    const cssMatch = html.match(/href="(\/assets\/[^"]+\.css)"/);
    expect(cssMatch).toBeTruthy();
    const cssResp = await request.get(`http://${config.daemonAddr}/ui${cssMatch![1]}`);
    expect(cssResp.ok()).toBe(true);
    expect(cssResp.headers()['content-type']).toContain('text/css');
  });

  // TODO: Full component rendering and message delivery e2e requires either:
  // 1. A standalone mount entry point (not Module Federation remote), or
  // 2. Injecting a JWT into the browser context for WS auth.
  // The Module Federation remote exports components but does not mount them
  // when loaded standalone. A future task should add a standalone dev entry
  // that renders SharkfinChat directly for e2e testing.
});
