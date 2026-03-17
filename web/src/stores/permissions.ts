import { usePermissions } from '@workfort/ui-solid';

export function createPermissionStore() {
  const { can, update, permissions } = usePermissions();
  return { can, permissions, update };
}
