import { usePermissions } from '@workfort/ui-solid';
import type { SharkfinClient } from '@workfort/sharkfin-client';

export function createPermissionStore(client: SharkfinClient) {
  const { can, update, permissions } = usePermissions();
  client.capabilities().then((perms) => {
    console.log('[sharkfin] capabilities:', perms);
    console.log('[sharkfin] can BEFORE update:', can('channel_list'));
    update(perms);
    console.log('[sharkfin] can AFTER update:', can('channel_list'));
    console.log('[sharkfin] permissions AFTER update:', permissions());
  }).catch((err) => {
    console.error('[sharkfin] capabilities error:', err);
  });
  return { can, permissions, update };
}
