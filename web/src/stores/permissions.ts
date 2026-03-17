import { usePermissions } from '@workfort/ui-solid';
import type { SharkfinClient } from '@workfort/sharkfin-client';

export function createPermissionStore(client: SharkfinClient) {
  const { can, update, permissions } = usePermissions();
  client.capabilities().then((perms) => update(perms)).catch(() => {});
  return { can, permissions };
}
