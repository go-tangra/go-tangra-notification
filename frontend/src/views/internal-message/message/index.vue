<script lang="ts" setup>
import type { VxeGridProps } from 'shell/adapter/vxe-table';

import { h } from 'vue';

import { Page, useVbenDrawer, type VbenFormProps } from 'shell/vben/common-ui';
import { LucideFilePenLine, LucideTrash2 } from 'shell/vben/icons';

import { notification, Tag, Button } from 'ant-design-vue';

import { useVbenVxeGrid } from 'shell/adapter/vxe-table';
import { type InternalMessage } from '../../../api/client';
import { $t } from 'shell/locales';
import { useInternalMessageStore } from '../../../stores/internal-message.state';
import { useInternalMessageCategoryStore } from '../../../stores/internal-message-category.state';
import {
  internalMessageStatusList,
  internalMessageStatusLabel,
  internalMessageStatusColor,
  internalMessageTypeList,
  internalMessageTypeLabel,
  internalMessageTypeColor,
} from '../../../helpers';

import InternalMessageDrawer from './internal-message-drawer.vue';

const internalMessageStore = useInternalMessageStore();
const internalMessageCategoryStore = useInternalMessageCategoryStore();

const formOptions: VbenFormProps = {
  collapsed: false,
  showCollapseButton: false,
  submitOnEnter: true,
  schema: [
    {
      component: 'Input',
      fieldName: 'title',
      label: $t('notification.page.internalMessage.title'),
      componentProps: {
        placeholder: $t('ui.placeholder.input'),
        allowClear: true,
      },
    },
    {
      component: 'Select',
      fieldName: 'status',
      label: $t('notification.page.internalMessage.status'),
      componentProps: {
        placeholder: $t('ui.placeholder.select'),
        options: internalMessageStatusList(),
        filterOption: (input: string, option: any) =>
          option.label.toLowerCase().includes(input.toLowerCase()),
        showSearch: true,
        allowClear: true,
      },
    },
    {
      component: 'Select',
      fieldName: 'type',
      label: $t('notification.page.internalMessage.type'),
      componentProps: {
        placeholder: $t('ui.placeholder.select'),
        options: internalMessageTypeList(),
        filterOption: (input: string, option: any) =>
          option.label.toLowerCase().includes(input.toLowerCase()),
        showSearch: true,
        allowClear: true,
      },
    },
    {
      component: 'ApiTreeSelect',
      fieldName: 'category_id',
      label: $t('notification.page.internalMessage.categoryId'),
      componentProps: {
        placeholder: $t('ui.placeholder.select'),
        numberToString: true,
        showSearch: true,
        treeDefaultExpandAll: true,
        childrenField: 'children',
        labelField: 'name',
        valueField: 'id',
        treeNodeFilterProp: 'label',
        api: async () => {
          const result =
            await internalMessageCategoryStore.listInternalMessageCategory(
              undefined,
              { is_enabled: 'true' },
            );
          return result.items;
        },
      },
    },
  ],
};

const gridOptions: VxeGridProps<InternalMessage> = {
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
      query: async ({ page }, formValues) => {
        return await internalMessageStore.listMessage(
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
      title: $t('notification.page.internalMessage.title'),
      field: 'title',
    },
    {
      title: $t('notification.page.internalMessage.categoryName'),
      field: 'categoryName',
    },
    {
      title: $t('notification.page.internalMessage.status'),
      field: 'status',
      slots: { default: 'status' },
    },
    {
      title: $t('notification.page.internalMessage.type'),
      field: 'type',
      slots: { default: 'type' },
    },
    {
      title: $t('notification.page.internalMessage.senderName'),
      field: 'senderName',
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
  connectedComponent: InternalMessageDrawer,
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
    await internalMessageStore.deleteMessage(row.id);
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
    <Grid :table-title="$t('notification.menu.internalMessage')">
      <template #toolbar-tools>
        <Button class="mr-2" type="primary" @click="handleCreate">
          {{ $t('notification.page.internalMessage.buttonCreate') }}
        </Button>
      </template>
      <template #status="{ row }">
        <Tag :color="internalMessageStatusColor(row.status)">
          {{ internalMessageStatusLabel(row.status) }}
        </Tag>
      </template>
      <template #type="{ row }">
        <Tag :color="internalMessageTypeColor(row.type)">
          {{ internalMessageTypeLabel(row.type) }}
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
          :title="$t('notification.page.internalMessage.confirmDelete')"
          @confirm="handleDelete(row)"
        >
          <Button danger type="link" size="small" :icon="h(LucideTrash2)" />
        </a-popconfirm>
      </template>
    </Grid>
    <Drawer />
  </Page>
</template>
