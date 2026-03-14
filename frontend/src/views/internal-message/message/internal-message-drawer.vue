<script lang="ts" setup>
import { computed, ref } from 'vue';

import { useVbenDrawer, useVbenForm } from 'shell/vben/common-ui';
import { $t } from 'shell/locales';

import { notification } from 'ant-design-vue';

import type { SendMessageRequest } from '../../../api/client';
import { useInternalMessageStore } from '../../../stores/internal-message.state';
import { useInternalMessageCategoryStore } from '../../../stores/internal-message-category.state';
import { internalMessageStatusList, internalMessageTypeList } from '../../../helpers';

const internalMessageStore = useInternalMessageStore();
const internalMessageCategoryStore = useInternalMessageCategoryStore();

const data = ref();

const getTitle = computed(() =>
  data.value?.create
    ? $t('notification.page.internalMessage.drawerCreate')
    : $t('notification.page.internalMessage.drawerUpdate'),
);

const [BaseForm, baseFormApi] = useVbenForm({
  showDefaultActions: false,
  commonConfig: {
    formItemClass: 'col-span-2 md:col-span-1',
  },
  wrapperClass: 'grid-cols-2 gap-x-4',

  schema: [
    {
      component: 'Select',
      fieldName: 'status',
      label: $t('notification.page.internalMessage.status'),
      defaultValue: 'DRAFT',
      componentProps: {
        class: 'w-full',
        placeholder: $t('ui.placeholder.select'),
        options: internalMessageStatusList(),
        showSearch: true,
      },
      rules: 'selectRequired',
    },
    {
      component: 'Select',
      fieldName: 'type',
      label: $t('notification.page.internalMessage.type'),
      defaultValue: 'NOTIFICATION',
      componentProps: {
        class: 'w-full',
        placeholder: $t('ui.placeholder.select'),
        options: internalMessageTypeList(),
        showSearch: true,
      },
      rules: 'selectRequired',
    },
    {
      component: 'ApiTreeSelect',
      fieldName: 'categoryId',
      label: $t('notification.page.internalMessage.categoryId'),
      rules: 'selectRequired',
      formItemClass: 'col-span-2 md:col-span-2',
      componentProps: {
        class: 'w-full',
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
    {
      component: 'Input',
      fieldName: 'title',
      label: $t('notification.page.internalMessage.title'),
      rules: 'required',
      formItemClass: 'col-span-2 md:col-span-2',
      componentProps: {
        placeholder: $t('ui.placeholder.input'),
        allowClear: true,
      },
    },
    {
      component: 'Textarea',
      fieldName: 'content',
      label: $t('notification.page.internalMessage.content'),
      formItemClass: 'col-span-2 md:col-span-2',
      componentProps: {
        placeholder: $t('ui.placeholder.input'),
        rows: 6,
      },
    },
  ],
});

const [Drawer, drawerApi] = useVbenDrawer({
  onCancel() {
    drawerApi.close();
  },

  async onConfirm() {
    const validate = await baseFormApi.validate();
    if (!validate.valid) {
      return;
    }

    setLoading(true);

    const values = await baseFormApi.getValues();

    try {
      await (data.value?.create
        ? internalMessageStore.sendMessage({
            ...values,
            targetAll: true,
          } as SendMessageRequest)
        : internalMessageStore.updateMessage(data.value.row.id, values));

      notification.success({
        message: data.value?.create
          ? $t('ui.notification.create_success')
          : $t('ui.notification.update_success'),
      });
    } catch {
      notification.error({
        message: data.value?.create
          ? $t('ui.notification.create_failed')
          : $t('ui.notification.update_failed'),
      });
    } finally {
      drawerApi.close();
      setLoading(false);
    }
  },

  onOpenChange(isOpen: boolean) {
    if (isOpen) {
      data.value = drawerApi.getData<Record<string, any>>();
      baseFormApi.setValues(data.value?.row);
      setLoading(false);
    }
  },
});

function setLoading(loading: boolean) {
  drawerApi.setState({ confirmLoading: loading });
}
</script>

<template>
  <Drawer :title="getTitle" class="w-full max-w-[800px]">
    <BaseForm class="mx-4" />
  </Drawer>
</template>
