import { defineStore } from 'pinia';

import {
  PermissionService,
  type ListPermissionsResponse,
  type GrantAccessResponse,
  type CheckAccessResponse,
  type GetEffectivePermissionsResponse,
  type ResourceType,
  type SubjectType,
  type RelationType,
  type PermissionAction,
} from '../api/services';
import type { Paging } from '../types';

export const useNotificationPermissionStore = defineStore(
  'notification-permission',
  () => {
    async function grantAccess(request: {
      resourceType: ResourceType;
      resourceId: string;
      relation: RelationType;
      subjectType: SubjectType;
      subjectId: string;
      expiresAt?: string;
    }): Promise<GrantAccessResponse> {
      return await PermissionService.grant(request);
    }

    async function revokeAccess(request: {
      resourceType?: ResourceType;
      resourceId?: string;
      subjectType?: SubjectType;
      subjectId?: string;
      relation?: RelationType;
    }): Promise<void> {
      return await PermissionService.revoke(request);
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
      return await PermissionService.list({
        resourceType: formValues?.resourceType,
        resourceId: formValues?.resourceId,
        subjectType: formValues?.subjectType,
        subjectId: formValues?.subjectId,
        page: paging?.page,
        pageSize: paging?.pageSize,
      });
    }

    async function checkAccess(
      subjectId: string,
      subjectType: SubjectType,
      resourceType: ResourceType,
      resourceId: string,
      permission: PermissionAction,
    ): Promise<CheckAccessResponse> {
      return await PermissionService.check({
        subjectId,
        subjectType,
        resourceType,
        resourceId,
        permission,
      });
    }

    async function getEffectivePermissions(
      subjectId: string,
      subjectType: SubjectType,
      resourceType: ResourceType,
      resourceId: string,
    ): Promise<GetEffectivePermissionsResponse> {
      return await PermissionService.getEffective({
        subjectId,
        subjectType,
        resourceType,
        resourceId,
      });
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
