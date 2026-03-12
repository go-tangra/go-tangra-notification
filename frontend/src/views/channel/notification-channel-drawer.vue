<script lang="ts" setup>
import { computed, ref } from 'vue';

import { useVbenDrawer, useVbenForm } from 'shell/vben/common-ui';
import { $t } from 'shell/locales';

import { notification } from 'ant-design-vue';

import { useNotificationChannelStore } from '../../stores/notification-channel.state';
import { channelTypeList, enableBoolList } from '../../helpers';

const channelStore = useNotificationChannelStore();

const data = ref();

const getTitle = computed(() =>
  data.value?.create
    ? $t('notification.page.channel.create')
    : $t('notification.page.channel.edit'),
);

const [BaseForm, baseFormApi] = useVbenForm({
  showDefaultActions: false,
  commonConfig: {
    componentProps: {
      class: 'w-full',
    },
  },
  schema: [
    {
      component: 'Input',
      fieldName: 'name',
      label: $t('notification.page.channel.name'),
      componentProps: {
        placeholder: $t('ui.placeholder.input'),
        allowClear: true,
      },
      rules: 'required',
    },
    {
      component: 'Select',
      fieldName: 'type',
      label: $t('notification.page.channel.channelType'),
      componentProps: {
        placeholder: $t('ui.placeholder.select'),
        options: channelTypeList(),
        filterOption: (input: string, option: any) =>
          option.label.toLowerCase().includes(input.toLowerCase()),
        showSearch: true,
      },
      rules: 'selectRequired',
    },
    {
      component: 'Textarea',
      fieldName: 'config',
      label: $t('notification.page.channel.config'),
      componentProps: {
        placeholder:
          '{"host":"smtp.example.com","port":587,"username":"...","password":"...","from":"noreply@example.com","tls_mode":"starttls"}',
        allowClear: true,
        rows: 6,
      },
      rules: 'required',
    },
    {
      component: 'RadioGroup',
      fieldName: 'enabled',
      label: $t('ui.table.status'),
      defaultValue: true,
      rules: 'selectRequired',
      componentProps: {
        optionType: 'button',
        buttonStyle: 'solid',
        options: enableBoolList(),
      },
    },
    {
      component: 'RadioGroup',
      fieldName: 'isDefault',
      label: $t('notification.page.channel.isDefault'),
      defaultValue: false,
      rules: 'selectRequired',
      componentProps: {
        optionType: 'button',
        buttonStyle: 'solid',
        options: enableBoolList(),
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
    const { type: _type, ...updateValues } = values;

    try {
      await (data.value?.create
        ? channelStore.createChannel(values)
        : channelStore.updateChannel(data.value.row.id, updateValues));

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
      if (data.value?.row) {
        baseFormApi.setValues({
          ...data.value.row,
        });
      }
      setLoading(false);
    }
  },
});

function setLoading(loading: boolean) {
  drawerApi.setState({ confirmLoading: loading });
}
</script>

<template>
  <Drawer :title="getTitle">
    <BaseForm />
  </Drawer>
</template>
