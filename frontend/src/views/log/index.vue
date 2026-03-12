<script lang="ts" setup>
import type { VxeGridProps } from 'shell/adapter/vxe-table';

import { Page, type VbenFormProps } from 'shell/vben/common-ui';

import { Tag } from 'ant-design-vue';

import { useVbenVxeGrid } from 'shell/adapter/vxe-table';
import { type NotificationLog } from '../../api/services';
import { $t } from 'shell/locales';
import { useNotificationLogStore } from '../../stores/notification-log.state';
import { channelTypeList, channelTypeLabel, channelTypeColor, deliveryStatusList, deliveryStatusLabel, deliveryStatusColor } from '../../helpers';

const logStore = useNotificationLogStore();

const formOptions: VbenFormProps = {
  collapsed: false,
  showCollapseButton: false,
  submitOnEnter: true,
  schema: [
    {
      component: 'Select',
      fieldName: 'channelType',
      label: $t('notification.page.log.channelType'),
      componentProps: {
        placeholder: $t('ui.placeholder.select'),
        options: channelTypeList(),
        filterOption: (input: string, option: any) =>
          option.label.toLowerCase().includes(input.toLowerCase()),
        showSearch: true,
        allowClear: true,
      },
    },
    {
      component: 'Select',
      fieldName: 'status',
      label: $t('notification.page.log.status'),
      componentProps: {
        placeholder: $t('ui.placeholder.select'),
        options: deliveryStatusList(),
        filterOption: (input: string, option: any) =>
          option.label.toLowerCase().includes(input.toLowerCase()),
        showSearch: true,
        allowClear: true,
      },
    },
  ],
};

const gridOptions: VxeGridProps<NotificationLog> = {
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
        const resp = await logStore.listNotifications(
          { page: page.currentPage, pageSize: page.pageSize },
          formValues,
        );
        return {
          items: resp.notifications ?? [],
          total: resp.total ?? 0,
        };
      },
    },
  },

  columns: [
    {
      title: $t('notification.page.log.recipient'),
      field: 'recipient',
    },
    {
      title: $t('notification.page.log.channelType'),
      field: 'channelType',
      slots: { default: 'channelType' },
      width: 120,
    },
    {
      title: $t('notification.page.log.renderedSubject'),
      field: 'renderedSubject',
    },
    {
      title: $t('notification.page.log.status'),
      field: 'status',
      slots: { default: 'status' },
      width: 110,
    },
    {
      title: $t('notification.page.log.errorMessage'),
      field: 'errorMessage',
      width: 200,
    },
    {
      title: $t('notification.page.log.sentAt'),
      field: 'sentAt',
      formatter: 'formatDateTime',
      width: 140,
    },
    {
      title: $t('ui.table.createdAt'),
      field: 'createTime',
      formatter: 'formatDateTime',
      width: 140,
    },
  ],
};

const [Grid] = useVbenVxeGrid({ gridOptions, formOptions });
</script>

<template>
  <Page auto-content-height>
    <Grid :table-title="$t('notification.menu.log')">
      <template #channelType="{ row }">
        <Tag :color="channelTypeColor(row.channelType)">
          {{ channelTypeLabel(row.channelType) }}
        </Tag>
      </template>
      <template #status="{ row }">
        <Tag :color="deliveryStatusColor(row.status)">
          {{ deliveryStatusLabel(row.status) }}
        </Tag>
      </template>
    </Grid>
  </Page>
</template>
