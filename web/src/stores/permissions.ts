// SPDX-License-Identifier: Apache-2.0
import { usePermissions } from '@workfort/ui-solid';

export function createPermissionStore() {
  const { can, update, permissions } = usePermissions();
  return { can, permissions, update };
}
