import { defineStore } from 'pinia';

import { permissionService } from '../api/client';
import type {
  ListPermissionsResponse,
  GrantAccessResponse,
  CheckAccessResponse,
  GetEffectivePermissionsResponse,
  ResourceType,
  SubjectType,
  Relation,
  PermissionAction,
} from '../api/client';
import type { Paging } from '../types';

export const useNotificationPermissionStore = defineStore(
  'notification-permission',
  () => {
    async function grantAccess(request: {
      resourceType: ResourceType;
      resourceId: string;
      relation: Relation;
      subjectType: SubjectType;
      subjectId: string;
      expiresAt?: string;
    }): Promise<GrantAccessResponse> {
      return await permissionService.GrantAccess(request as any);
    }

    async function revokeAccess(request: {
      resourceType?: ResourceType;
      resourceId?: string;
      subjectType?: SubjectType;
      subjectId?: string;
      relation?: Relation;
    }): Promise<void> {
      await permissionService.RevokeAccess(request as any);
    }

    async function listPermissions(
      paging?: Paging,
      formValues?: {
        resourceType?: ResourceType;
        resourceId?: string;
        subjectType?: SubjectType;
        subjectId?: string;
      } | null,
    ): Promise<ListPermissionsResponse> {
      return await permissionService.ListPermissions({
        resourceType: formValues?.resourceType,
        resourceId: formValues?.resourceId,
        subjectType: formValues?.subjectType,
        subjectId: formValues?.subjectId,
        page: paging?.page,
        pageSize: paging?.pageSize,
      } as any);
    }

    async function checkAccess(
      subjectId: string,
      subjectType: SubjectType,
      resourceType: ResourceType,
      resourceId: string,
      permission: PermissionAction,
    ): Promise<CheckAccessResponse> {
      return await permissionService.CheckAccess({
        subjectId,
        subjectType,
        resourceType,
        resourceId,
        permission,
      } as any);
    }

    async function getEffectivePermissions(
      subjectId: string,
      subjectType: SubjectType,
      resourceType: ResourceType,
      resourceId: string,
    ): Promise<GetEffectivePermissionsResponse> {
      return await permissionService.GetEffectivePermissions({
        subjectId,
        subjectType,
        resourceType,
        resourceId,
      } as any);
    }

    function $reset() {}

    return {
      $reset,
      grantAccess,
      revokeAccess,
      listPermissions,
      checkAccess,
      getEffectivePermissions,
    };
  },
);
