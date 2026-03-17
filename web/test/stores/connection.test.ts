import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { createMockClient, flushPromises } from '../helpers';

// We need to mock the client module so initApp uses our mock.
let mockClient: ReturnType<typeof createMockClient>;

vi.mock('../../src/client', () => ({
  getClient: () => mockClient,
}));

// Import after mock setup.
import { initApp, getStores, connectionState, loading, resetStores } from '../../src/stores';

describe('connection lifecycle', () => {
  beforeEach(() => {
    resetStores();
    mockClient = createMockClient();
    mockClient.channels.mockResolvedValue([{ name: 'general', public: true, member: true }]);
    mockClient.users.mockResolvedValue([{ username: 'alice', online: true }]);
    mockClient.unreadCounts.mockResolvedValue([]);
  });

  afterEach(() => {
    resetStores();
  });

  it('starts in connecting/loading state', () => {
    expect(connectionState()).toBe('connecting');
    expect(loading()).toBe(true);
  });

  it('calls client.connect() during initApp', async () => {
    await initApp();
    expect(mockClient.connect).toHaveBeenCalledOnce();
  });

  it('sets connected and loading=false after initApp', async () => {
    await initApp();
    expect(connectionState()).toBe('connected');
    expect(loading()).toBe(false);
  });

  it('creates stores accessible via getStores()', async () => {
    await initApp();
    const stores = getStores();
    expect(stores.channels).toBeDefined();
    expect(stores.messages).toBeDefined();
    expect(stores.users).toBeDefined();
    expect(stores.unread).toBeDefined();
    expect(stores.permissions).toBeDefined();
  });

  it('throws if getStores() called before initApp()', () => {
    expect(() => getStores()).toThrow('initApp() must be called before getStores()');
  });

  it('sets disconnected state on disconnect event', async () => {
    await initApp();
    mockClient._emit('disconnect');
    expect(connectionState()).toBe('disconnected');
  });

  it('sets connected state on reconnect event', async () => {
    await initApp();
    mockClient._emit('disconnect');
    expect(connectionState()).toBe('disconnected');
    mockClient._emit('reconnect');
    expect(connectionState()).toBe('connected');
  });

  it('refetches data on reconnect', async () => {
    await initApp();
    // Reset call counts after initial store creation fetches.
    mockClient.channels.mockClear();
    mockClient.users.mockClear();
    mockClient.unreadCounts.mockClear();

    mockClient._emit('reconnect');
    await flushPromises();

    expect(mockClient.channels).toHaveBeenCalledOnce();
    expect(mockClient.users).toHaveBeenCalledOnce();
    expect(mockClient.unreadCounts).toHaveBeenCalledOnce();
  });

  it('refetches capabilities on reconnect', async () => {
    await initApp();
    // Reset call count after initial store creation fetch.
    mockClient.capabilities.mockClear();

    mockClient._emit('reconnect');
    await flushPromises();

    expect(mockClient.capabilities).toHaveBeenCalledOnce();
  });

  it('resetStores clears state back to initial', async () => {
    await initApp();
    expect(loading()).toBe(false);
    expect(connectionState()).toBe('connected');

    resetStores();
    expect(loading()).toBe(true);
    expect(connectionState()).toBe('connecting');
    expect(() => getStores()).toThrow();
  });
});
