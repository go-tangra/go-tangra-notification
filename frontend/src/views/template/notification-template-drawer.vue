<script lang="ts" setup>
import { computed, ref } from 'vue';

import { useVbenDrawer, useVbenForm } from 'shell/vben/common-ui';
import { $t } from 'shell/locales';

import { notification } from 'ant-design-vue';

import { useNotificationTemplateStore } from '../../stores/notification-template.state';
import { useNotificationChannelStore } from '../../stores/notification-channel.state';
import { enableBoolList } from '../../helpers';

const templateStore = useNotificationTemplateStore();
const channelStore = useNotificationChannelStore();

const data = ref();
const channelOptions = ref<{ value: string; label: string; type: string }[]>([]);

async function loadChannels() {
  const resp = await channelStore.listChannels();
  channelOptions.value = (resp.channels ?? []).map((ch: any) => ({
    value: ch.id,
    label: ch.name,
    type: ch.type,
  }));
}

const getTitle = computed(() =>
  data.value?.create
    ? $t('notification.page.template.create')
    : $t('notification.page.template.edit'),
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
      label: $t('notification.page.template.name'),
      componentProps: {
        placeholder: $t('ui.placeholder.input'),
        allowClear: true,
      },
      rules: 'required',
    },
    {
      component: 'Select',
      fieldName: 'channelId',
      label: $t('notification.page.template.channel'),
      componentProps: {
        placeholder: $t('ui.placeholder.select'),
        options: channelOptions,
        filterOption: (input: string, option: any) =>
          option.label.toLowerCase().includes(input.toLowerCase()),
        showSearch: true,
      },
      rules: 'selectRequired',
    },
    {
      component: 'Input',
      fieldName: 'subject',
      label: $t('notification.page.template.subject'),
      componentProps: {
        placeholder: 'e.g. Hello {{.Name}}',
        allowClear: true,
      },
      rules: 'required',
    },
    {
      component: 'Textarea',
      fieldName: 'body',
      label: $t('notification.page.template.body'),
      componentProps: {
        placeholder: 'Go template syntax: {{.Variable}}',
        allowClear: true,
        rows: 8,
      },
      rules: 'required',
    },
    {
      component: 'Input',
      fieldName: 'variables',
      label: $t('notification.page.template.variables'),
      componentProps: {
        placeholder: 'e.g. Name,Email,Link',
        allowClear: true,
      },
    },
    {
      component: 'RadioGroup',
      fieldName: 'isDefault',
      label: $t('notification.page.template.isDefault'),
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

    try {
      await (data.value?.create
        ? templateStore.createTemplate(values)
        : templateStore.updateTemplate(data.value.row.id, values));

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

  async onOpenChange(isOpen: boolean) {
    if (isOpen) {
      await loadChannels();
      data.value = drawerApi.getData<Record<string, any>>();
      if (data.value?.row) {
        baseFormApi.setValues(data.value.row);
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
  <Drawer :title="getTitle" class="w-full max-w-[600px]">
    <BaseForm />
  </Drawer>
</template>
