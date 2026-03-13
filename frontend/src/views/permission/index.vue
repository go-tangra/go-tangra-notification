<script lang="ts" setup>
import type { VxeGridProps } from 'shell/adapter/vxe-table';

import { h, onMounted, ref } from 'vue';

import { Page, useVbenDrawer, type VbenFormProps } from 'shell/vben/common-ui';
import { LucideTrash, LucidePencil, LucideUsers } from 'shell/vben/icons';

import { notification, Space, Button, Tag } from 'ant-design-vue';

import { useVbenVxeGrid } from 'shell/adapter/vxe-table';
import { type NotificationPermission, userService } from '../../api/client';
import { $t } from 'shell/locales';
import { useNotificationPermissionStore } from '../../stores/notification-permission.state';

import PermissionDrawer from './permission-drawer.vue';

const permissionStore = useNotificationPermissionStore();

const users = ref<any[]>([]);
const roles = ref<any[]>([]);

async function loadSubjects() {
  try {
    const [usersResp, rolesResp] = await Promise.all([
      userService.ListUsers({ noPaging: true }),
      userService.ListRoles({ noPaging: true }),
    ]);
    users.value = usersResp.items ?? [];
    roles.value = rolesResp.items ?? [];
  } catch (e) {
    console.error('Failed to load subjects:', e);
  }
}

function resolveSubjectName(subjectType: string | undefined, subjectId: string | undefined): string {
  if (!subjectId) return '';

  if (subjectType === 'SUBJECT_TYPE_USER') {
    const user = users.value.find((u) => String(u.id) === subjectId);
    if (user) {
      return `${user.realname || user.username} (${user.username})`;
    }
  } else if (subjectType === 'SUBJECT_TYPE_ROLE') {
    const role = roles.value.find((r) => r.code === subjectId);
    if (role) {
      return role.name ?? subjectId;
    }
  } else if (subjectType === 'SUBJECT_TYPE_CLIENT') {
    return subjectId === '*' ? $t('notification.page.permission.allServices') : subjectId;
  }

  return subjectId;
}

onMounted(() => loadSubjects());

const formOptions: VbenFormProps = {
  collapsed: false,
  showCollapseButton: false,
  submitOnEnter: true,
  schema: [
    {
      component: 'Select',
      fieldName: 'resourceType',
      label: $t('notification.page.permission.resourceType'),
      componentProps: {
        placeholder: $t('ui.placeholder.select'),
        allowClear: true,
        options: [
          { label: $t('notification.page.permission.resourceTypeTemplate'), value: 'RESOURCE_TYPE_TEMPLATE' },
          { label: $t('notification.page.permission.resourceTypeChannel'), value: 'RESOURCE_TYPE_CHANNEL' },
        ],
      },
    },
  ],
};

const gridOptions: VxeGridProps<NotificationPermission> = {
  height: 'auto',
  stripe: false,
  toolbarConfig: {
    custom: true,
    export: true,
    import: false,
    refresh: true,
    zoom: true,
  },
  exportConfig: {},
  rowConfig: {
    isHover: true,
  },
  pagerConfig: {
    enabled: true,
    pageSize: 20,
    pageSizes: [10, 20, 50, 100],
  },

  proxyConfig: {
    ajax: {
      query: async ({ page }, formValues) => {
        const resp = await permissionStore.listPermissions(
          { page: page.currentPage, pageSize: page.pageSize },
          { resourceType: formValues?.resourceType },
        );
        return {
          items: resp.permissions ?? [],
          total: resp.total ?? 0,
        };
      },
    },
  },

  columns: [
    { title: $t('ui.table.seq'), type: 'seq', width: 50 },
    {
      title: $t('notification.page.permission.resourceType'),
      field: 'resourceType',
      width: 120,
      slots: { default: 'resourceType' },
    },
    {
      title: $t('notification.page.permission.resourceId'),
      field: 'resourceId',
      width: 200,
    },
    {
      title: $t('notification.page.permission.subjectType'),
      field: 'subjectType',
      width: 100,
      slots: { default: 'subjectType' },
    },
    {
      title: $t('notification.page.permission.subjectId'),
      field: 'subjectId',
      width: 200,
      slots: { default: 'subjectId' },
    },
    {
      title: $t('notification.page.permission.relation'),
      field: 'relation',
      width: 120,
      slots: { default: 'relation' },
    },
    {
      title: $t('ui.table.createdAt'),
      field: 'createTime',
      formatter: 'formatDateTime',
      width: 160,
    },
    {
      title: $t('ui.table.action'),
      field: 'action',
      fixed: 'right',
      slots: { default: 'action' },
      width: 120,
    },
  ],
};

const [Grid, gridApi] = useVbenVxeGrid({ gridOptions, formOptions });

const [PermissionDrawerComponent, permissionDrawerApi] = useVbenDrawer({
  connectedComponent: PermissionDrawer,
  onOpenChange(isOpen: boolean) {
    if (!isOpen) {
      gridApi.query();
    }
  },
});

function handleCreatePermission() {
  permissionDrawerApi.setData({ mode: 'create' });
  permissionDrawerApi.open();
}

function handleEditPermission(row: any) {
  permissionDrawerApi.setData({
    mode: 'edit',
    resourceType: row.resourceType,
    resourceId: row.resourceId,
    permission: row,
  });
  permissionDrawerApi.open();
}

async function handleDeletePermission(row: any) {
  if (!row.id) return;
  try {
    await permissionStore.revokeAccess({
      resourceType: row.resourceType,
      resourceId: row.resourceId,
      subjectType: row.subjectType,
      subjectId: row.subjectId,
      relation: row.relation,
    });
    notification.success({ message: $t('ui.notification.delete_success') });
    await gridApi.query();
  } catch {
    notification.error({ message: $t('ui.notification.delete_failed') });
  }
}

function getResourceTypeLabel(type: string) {
  switch (type) {
    case 'RESOURCE_TYPE_TEMPLATE':
      return $t('notification.page.permission.resourceTypeTemplate');
    case 'RESOURCE_TYPE_CHANNEL':
      return $t('notification.page.permission.resourceTypeChannel');
    default:
      return type;
  }
}

function getSubjectTypeLabel(type: string) {
  switch (type) {
    case 'SUBJECT_TYPE_USER':
      return $t('notification.page.permission.user');
    case 'SUBJECT_TYPE_ROLE':
      return $t('notification.page.permission.role');
    case 'SUBJECT_TYPE_CLIENT':
      return $t('notification.page.permission.client');
    default:
      return type;
  }
}

function getRelationLabel(relation: string) {
  switch (relation) {
    case 'RELATION_OWNER':
      return $t('notification.page.permission.owner');
    case 'RELATION_EDITOR':
      return $t('notification.page.permission.editor');
    case 'RELATION_VIEWER':
      return $t('notification.page.permission.viewer');
    case 'RELATION_SHARER':
      return $t('notification.page.permission.sharer');
    default:
      return relation;
  }
}

function getRelationColor(relation: string) {
  switch (relation) {
    case 'RELATION_OWNER':
      return 'red';
    case 'RELATION_EDITOR':
      return 'orange';
    case 'RELATION_VIEWER':
      return 'blue';
    case 'RELATION_SHARER':
      return 'purple';
    default:
      return 'default';
  }
}
</script>

<template>
  <Page auto-content-height>
    <Grid :table-title="$t('notification.page.permission.title')">
      <template #toolbar-tools>
        <Button class="mr-2" type="primary" @click="handleCreatePermission">
          {{ $t('notification.page.permission.grant') }}
        </Button>
      </template>
      <template #resourceType="{ row }">
        <Tag>{{ getResourceTypeLabel(row.resourceType) }}</Tag>
      </template>
      <template #subjectType="{ row }">
        <div class="flex items-center gap-1">
          <component :is="LucideUsers" class="size-4" />
          <span>{{ getSubjectTypeLabel(row.subjectType) }}</span>
        </div>
      </template>
      <template #subjectId="{ row }">
        {{ resolveSubjectName(row.subjectType, row.subjectId) }}
      </template>
      <template #relation="{ row }">
        <Tag :color="getRelationColor(row.relation)">
          {{ getRelationLabel(row.relation) }}
        </Tag>
      </template>
      <template #action="{ row }">
        <Space>
          <Button
            type="link"
            size="small"
            :icon="h(LucidePencil)"
            :title="$t('ui.button.edit')"
            @click.stop="handleEditPermission(row)"
          />
          <a-popconfirm
            :cancel-text="$t('ui.button.cancel')"
            :ok-text="$t('ui.button.ok')"
            :title="$t('notification.page.permission.confirmRevoke')"
            @confirm="handleDeletePermission(row)"
          >
            <Button
              danger
              type="link"
              size="small"
              :icon="h(LucideTrash)"
              :title="$t('notification.page.permission.revoke')"
            />
          </a-popconfirm>
        </Space>
      </template>
    </Grid>

    <PermissionDrawerComponent />
  </Page>
</template>
