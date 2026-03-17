import { usePermissions } from '@workfort/ui-solid';
import type { SharkfinClient } from '@workfort/sharkfin-client';

export function createPermissionStore(client: SharkfinClient) {
  const { can, update, permissions } = usePermissions();
  client.capabilities().then((perms) => {
    console.log('[sharkfin] capabilities:', perms);
    update(perms);
  }).catch((err) => {
    console.error('[sharkfin] capabilities error:', err);
  });
  return { can, permissions, update };
}
