import { describe, it, expect, beforeEach } from 'vitest';
import { getWebSocketUrl } from '../src/client';

describe('getWebSocketUrl', () => {
  beforeEach(() => {
    Object.defineProperty(window, 'location', {
      value: {
        protocol: 'https:',
        host: 'app.example.com',
        pathname: '/forts/myfort/chat',
      },
      writable: true,
    });
  });

  it('derives wss URL from HTTPS page', () => {
    expect(getWebSocketUrl()).toBe('wss://app.example.com/forts/myfort/api/sharkfin/ws');
  });

  it('derives ws URL from HTTP page', () => {
    (window.location as any).protocol = 'http:';
    expect(getWebSocketUrl()).toBe('ws://app.example.com/forts/myfort/api/sharkfin/ws');
  });

  it('handles nested chat routes', () => {
    (window.location as any).pathname = '/forts/myfort/chat/dm/alice';
    expect(getWebSocketUrl()).toBe('wss://app.example.com/forts/myfort/api/sharkfin/ws');
  });
});
