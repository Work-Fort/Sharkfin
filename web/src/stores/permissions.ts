import { createSignal } from 'solid-js';
import type { SharkfinClient } from '@workfort/sharkfin-client';

export function createPermissionStore(client: SharkfinClient) {
  const [permissions, setPermissions] = createSignal<Set<string>>(new Set());

  client.capabilities().then((perms) => setPermissions(new Set(perms))).catch(() => {});

  function can(permission: string): boolean {
    return permissions().has(permission);
  }

  return { can, permissions };
}
