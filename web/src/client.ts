// SPDX-License-Identifier: Apache-2.0
import { SharkfinClient } from '@workfort/sharkfin-client';

/**
 * Derive the WebSocket URL from the current page URL.
 * The BFF proxies /forts/{fort}/api/sharkfin/ws → daemon WS endpoint.
 */
export function getWebSocketUrl(): string {
  const proto = location.protocol === 'https:' ? 'wss:' : 'ws:';
  const match = location.pathname.match(/^\/forts\/([^/]+)/);
  if (match) {
    return `${proto}//${location.host}/forts/${match[1]}/api/sharkfin/ws`;
  }
  // Direct daemon access (e2e tests, standalone mode).
  return `${proto}//${location.host}/ws`;
}

let _client: SharkfinClient | null = null;

/** Get or create the singleton SharkfinClient. */
export function getClient(): SharkfinClient {
  if (!_client) {
    _client = new SharkfinClient(getWebSocketUrl(), { reconnect: true });
  }
  return _client;
}

/** Reset the singleton (for tests). */
export function resetClient(): void {
  _client?.close();
  _client = null;
}
