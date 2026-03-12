<script lang="ts" setup>
import type { VxeGridProps } from 'shell/adapter/vxe-table';

import { h } from 'vue';

import { Page, useVbenDrawer, type VbenFormProps } from 'shell/vben/common-ui';
import { LucideFilePenLine, LucideTrash2, LucideShield } from 'shell/vben/icons';

import { notification, Tag, Space, Button } from 'ant-design-vue';

import { useVbenVxeGrid } from 'shell/adapter/vxe-table';
import { type NotificationChannel } from '../../api/services';
import { $t } from 'shell/locales';
import { useNotificationChannelStore } from '../../stores/notification-channel.state';
import { channelTypeList, channelTypeLabel, channelTypeColor, enableBoolToColor, enableBoolToName } from '../../helpers';

import NotificationChannelDrawer from './notification-channel-drawer.vue';
import PermissionDrawer from '../permission/permission-drawer.vue';

const channelStore = useNotificationChannelStore();

const formOptions: VbenFormProps = {
  collapsed: false,
  showCollapseButton: false,
  submitOnEnter: true,
  schema: [
    {
      component: 'Input',
      fieldName: 'name',
      label: $t('notification.page.channel.name'),
      componentProps: {
        placeholder: $t('ui.placeholder.input'),
        allowClear: true,
      },
    },
    {
      component: 'Select',
      fieldName: 'channelType',
      label: $t('notification.page.channel.channelType'),
      componentProps: {
        placeholder: $t('ui.placeholder.select'),
        options: channelTypeList(),
        filterOption: (input: string, option: any) =>
          option.label.toLowerCase().includes(input.toLowerCase()),
        showSearch: true,
        allowClear: true,
      },
    },
  ],
};

const gridOptions: VxeGridProps<NotificationChannel> = {
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
        const resp = await channelStore.listChannels({
          page: page.currentPage,
          pageSize: page.pageSize,
        });
        return {
          items: resp.channels ?? [],
          total: resp.total ?? 0,
        };
      },
    },
  },

  columns: [
    {
      title: $t('notification.page.channel.name'),
      field: 'name',
    },
    {
      title: $t('notification.page.channel.channelType'),
      field: 'type',
      slots: { default: 'channelType' },
      width: 120,
    },
    {
      title: $t('ui.table.status'),
      field: 'enabled',
      slots: { default: 'enabled' },
      width: 95,
    },
    {
      title: $t('notification.page.channel.isDefault'),
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
  connectedComponent: NotificationChannelDrawer,
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
    resourceType: 'RESOURCE_TYPE_CHANNEL',
    resourceId: row.id,
    resourceName: row.name,
  });
  permissionDrawerApi.open();
}

async function handleDelete(row: any) {
  try {
    await channelStore.deleteChannel(row.id);
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
    <Grid :table-title="$t('notification.menu.channel')">
      <template #toolbar-tools>
        <Button class="mr-2" type="primary" @click="handleCreate">
          {{ $t('notification.page.channel.button.create') }}
        </Button>
      </template>
      <template #channelType="{ row }">
        <Tag :color="channelTypeColor(row.type)">
          {{ channelTypeLabel(row.type) }}
        </Tag>
      </template>
      <template #enabled="{ row }">
        <Tag :color="enableBoolToColor(row.enabled)">
          {{ enableBoolToName(row.enabled) }}
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
            :title="$t('notification.page.channel.confirmDelete')"
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
