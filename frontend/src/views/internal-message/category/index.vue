<script lang="ts" setup>
import type { VxeGridProps } from 'shell/adapter/vxe-table';

import { h } from 'vue';

import { Page, useVbenDrawer, type VbenFormProps } from 'shell/vben/common-ui';
import { LucideFilePenLine, LucideTrash2 } from 'shell/vben/icons';

import { notification, Tag, Button } from 'ant-design-vue';

import { useVbenVxeGrid } from 'shell/adapter/vxe-table';
import { type InternalMessageCategory } from '../../../api/client';
import { $t } from 'shell/locales';
import { useInternalMessageCategoryStore } from '../../../stores/internal-message-category.state';
import { enableBoolToColor, enableBoolToName } from '../../../helpers';

import InternalMessageCategoryDrawer from './internal-message-category-drawer.vue';

const internalMessageCategoryStore = useInternalMessageCategoryStore();

const formOptions: VbenFormProps = {
  collapsed: false,
  showCollapseButton: false,
  submitOnEnter: true,
  schema: [
    {
      component: 'Input',
      fieldName: 'name',
      label: $t('notification.page.internalMessageCategory.name'),
      componentProps: {
        placeholder: $t('ui.placeholder.input'),
        allowClear: true,
      },
    },
    {
      component: 'Input',
      fieldName: 'code',
      label: $t('notification.page.internalMessageCategory.code'),
      componentProps: {
        placeholder: $t('ui.placeholder.input'),
        allowClear: true,
      },
    },
  ],
};

const gridOptions: VxeGridProps<InternalMessageCategory> = {
  toolbarConfig: {
    custom: true,
    export: true,
    refresh: true,
    zoom: true,
  },
  exportConfig: {},
  pagerConfig: {
    enabled: false,
  },
  rowConfig: {
    isHover: true,
  },
  height: 'auto',
  stripe: true,

  proxyConfig: {
    ajax: {
      query: async ({ page }, formValues) => {
        return await internalMessageCategoryStore.listInternalMessageCategory(
          {
            page: page.currentPage,
            pageSize: page.pageSize,
          },
          formValues,
        );
      },
    },
  },

  columns: [
    {
      title: $t('notification.page.internalMessageCategory.name'),
      field: 'name',
    },
    {
      title: $t('notification.page.internalMessageCategory.code'),
      field: 'code',
    },
    { title: $t('ui.table.sortOrder'), field: 'sortOrder', width: 70 },
    {
      title: $t('ui.table.status'),
      field: 'isEnabled',
      slots: { default: 'isEnabled' },
      width: 95,
    },
    {
      title: $t('ui.table.createdAt'),
      field: 'createdAt',
      formatter: 'formatDateTime',
      width: 140,
    },
    {
      title: $t('ui.table.action'),
      field: 'action',
      fixed: 'right',
      slots: { default: 'action' },
      width: 90,
    },
  ],
};

const [Grid, gridApi] = useVbenVxeGrid({ gridOptions, formOptions });

const [Drawer, drawerApi] = useVbenDrawer({
  connectedComponent: InternalMessageCategoryDrawer,
  onOpenChange(isOpen: boolean) {
    if (!isOpen) {
      gridApi.query();
    }
  },
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

async function handleDelete(row: any) {
  try {
    await internalMessageCategoryStore.deleteInternalMessageCategory(row.id);
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
    <Grid :table-title="$t('notification.menu.internalMessageCategory')">
      <template #toolbar-tools>
        <Button class="mr-2" type="primary" @click="handleCreate">
          {{ $t('notification.page.internalMessageCategory.buttonCreate') }}
        </Button>
      </template>
      <template #isEnabled="{ row }">
        <Tag :color="enableBoolToColor(row.isEnabled)">
          {{ enableBoolToName(row.isEnabled) }}
        </Tag>
      </template>
      <template #action="{ row }">
        <Button
          type="link"
          size="small"
          :icon="h(LucideFilePenLine)"
          @click.stop="handleEdit(row)"
        />
        <a-popconfirm
          :cancel-text="$t('ui.button.cancel')"
          :ok-text="$t('ui.button.ok')"
          :title="$t('notification.page.internalMessageCategory.confirmDelete')"
          @confirm="handleDelete(row)"
        >
          <Button danger type="link" size="small" :icon="h(LucideTrash2)" />
        </a-popconfirm>
      </template>
    </Grid>
    <Drawer />
  </Page>
</template>
