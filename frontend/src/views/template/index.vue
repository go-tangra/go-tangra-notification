<script lang="ts" setup>
import type { VxeGridProps } from 'shell/adapter/vxe-table';

import { h, ref, onMounted } from 'vue';

import { Page, useVbenDrawer, type VbenFormProps } from 'shell/vben/common-ui';
import { LucideFilePenLine, LucideTrash2, LucideShield } from 'shell/vben/icons';

import { notification, Tag, Space, Button } from 'ant-design-vue';

import { useVbenVxeGrid } from 'shell/adapter/vxe-table';
import { type NotificationTemplate } from '../../api/client';
import { $t } from 'shell/locales';
import { useNotificationTemplateStore } from '../../stores/notification-template.state';
import { useNotificationChannelStore } from '../../stores/notification-channel.state';
import { channelTypeColor, enableBoolToColor, enableBoolToName } from '../../helpers';

import NotificationTemplateDrawer from './notification-template-drawer.vue';
import PermissionDrawer from '../permission/permission-drawer.vue';

const templateStore = useNotificationTemplateStore();
const channelStore = useNotificationChannelStore();

// Channel maps: channelId -> channel name and channelId -> channel type (for display)
const channelNameMap = ref<Record<string, string>>({});
const channelTypeMap = ref<Record<string, string>>({});

onMounted(async () => {
  const resp = await channelStore.listChannels();
  for (const ch of resp.channels ?? []) {
    channelNameMap.value[ch.id] = ch.name;
    channelTypeMap.value[ch.id] = ch.type;
  }
});

function channelNameById(channelId: string): string {
  return channelNameMap.value[channelId] || channelId;
}

function channelTypeById(channelId: string): string {
  return channelTypeMap.value[channelId] || '';
}

const formOptions: VbenFormProps = {
  collapsed: false,
  showCollapseButton: false,
  submitOnEnter: true,
  schema: [
    {
      component: 'Input',
      fieldName: 'name',
      label: $t('notification.page.template.name'),
      componentProps: {
        placeholder: $t('ui.placeholder.input'),
        allowClear: true,
      },
    },
  ],
};

const gridOptions: VxeGridProps<NotificationTemplate> = {
  toolbarConfig: {
    custom: true,
    export: true,
    refresh: true,
    zoom: true,
  },
  height: 'auto',
  exportConfig: {},
  pagerConfig: {
    enabled: false,
  },
  rowConfig: {
    isHover: true,
  },
  stripe: true,

  proxyConfig: {
    ajax: {
      query: async ({ page }) => {
        const resp = await templateStore.listTemplates({
          page: page.currentPage,
          pageSize: page.pageSize,
        });
        return {
          items: resp.templates ?? [],
          total: resp.total ?? 0,
        };
      },
    },
  },

  columns: [
    {
      title: $t('notification.page.template.name'),
      field: 'name',
    },
    {
      title: $t('notification.page.template.channel'),
      field: 'channelId',
      slots: { default: 'channel' },
      width: 150,
    },
    {
      title: $t('notification.page.template.subject'),
      field: 'subject',
    },
    {
      title: $t('notification.page.template.variables'),
      field: 'variables',
      width: 200,
    },
    {
      title: $t('notification.page.template.isDefault'),
      field: 'isDefault',
      slots: { default: 'isDefault' },
      width: 95,
    },
    {
      title: $t('ui.table.createdAt'),
      field: 'createTime',
      formatter: 'formatDateTime',
      width: 140,
    },
    {
      title: $t('ui.table.action'),
      field: 'action',
      fixed: 'right',
      slots: { default: 'action' },
      width: 130,
    },
  ],
};

const [Grid, gridApi] = useVbenVxeGrid({ gridOptions, formOptions });

const [Drawer, drawerApi] = useVbenDrawer({
  connectedComponent: NotificationTemplateDrawer,
  onOpenChange(isOpen: boolean) {
    if (!isOpen) {
      gridApi.query();
    }
  },
});

const [PermissionDrawerComponent, permissionDrawerApi] = useVbenDrawer({
  connectedComponent: PermissionDrawer,
});

function openDrawer(create: boolean, row?: any) {
  drawerApi.setData({ create, row });
  drawerApi.open();
}

function handleCreate() {
  openDrawer(true);
}

function handleEdit(row: any) {
  openDrawer(false, row);
}

function handlePermissions(row: any) {
  permissionDrawerApi.setData({
    resourceType: 'RESOURCE_TYPE_TEMPLATE',
    resourceId: row.id,
    resourceName: row.name,
  });
  permissionDrawerApi.open();
}

async function handleDelete(row: any) {
  try {
    await templateStore.deleteTemplate(row.id);
    notification.success({
      message: $t('ui.notification.delete_success'),
    });
    await gridApi.query();
  } catch {
    notification.error({
      message: $t('ui.notification.delete_failed'),
    });
  }
}
</script>

<template>
  <Page auto-content-height>
    <Grid :table-title="$t('notification.menu.template')">
      <template #toolbar-tools>
        <Button class="mr-2" type="primary" @click="handleCreate">
          {{ $t('notification.page.template.button.create') }}
        </Button>
      </template>
      <template #channel="{ row }">
        <Tag :color="channelTypeColor(channelTypeById(row.channelId))">
          {{ channelNameById(row.channelId) }}
        </Tag>
      </template>
      <template #isDefault="{ row }">
        <Tag :color="enableBoolToColor(row.isDefault)">
          {{ enableBoolToName(row.isDefault) }}
        </Tag>
      </template>
      <template #action="{ row }">
        <Space>
          <Button
            type="link"
            size="small"
            :icon="h(LucideFilePenLine)"
            :title="$t('ui.button.edit')"
            @click.stop="handleEdit(row)"
          />
          <Button
            type="link"
            size="small"
            :icon="h(LucideShield)"
            :title="$t('notification.page.permission.title')"
            @click.stop="handlePermissions(row)"
          />
          <a-popconfirm
            :cancel-text="$t('ui.button.cancel')"
            :ok-text="$t('ui.button.ok')"
            :title="$t('notification.page.template.confirmDelete')"
            @confirm="handleDelete(row)"
          >
            <Button danger type="link" size="small" :icon="h(LucideTrash2)" />
          </a-popconfirm>
        </Space>
      </template>
    </Grid>
    <Drawer />
    <PermissionDrawerComponent />
  </Page>
</template>
