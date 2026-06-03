<script lang="ts" setup>
import { computed, reactive, ref } from 'vue';

import { useVbenDrawer } from 'shell/vben/common-ui';
import { $t } from 'shell/locales';

import {
  Button,
  Form,
  FormItem,
  Input,
  InputNumber,
  InputPassword,
  notification,
  RadioGroup,
  Select,
} from 'ant-design-vue';

import type { ChannelType } from '../../api/client';
import { useNotificationChannelStore } from '../../stores/notification-channel.state';
import { channelTypeList, enableBoolList } from '../../helpers';

const channelStore = useNotificationChannelStore();

interface HeaderRow {
  name: string;
  value: string;
}

interface ChannelFormModel {
  name: string;
  type: ChannelType;
  enabled: boolean;
  isDefault: boolean;
  email: {
    host: string;
    port: number;
    username: string;
    password: string;
    from: string;
    tlsMode: string;
    headers: HeaderRow[];
  };
  sms: { provider: string; apiKey: string; fromNumber: string };
  slack: { webhookUrl: string; botToken: string; defaultChannel: string };
}

function emptyModel(): ChannelFormModel {
  return {
    name: '',
    type: 'CHANNEL_TYPE_EMAIL',
    enabled: true,
    isDefault: false,
    email: {
      host: '',
      port: 587,
      username: '',
      password: '',
      from: '',
      tlsMode: 'starttls',
      headers: [],
    },
    sms: { provider: '', apiKey: '', fromNumber: '' },
    slack: { webhookUrl: '', botToken: '', defaultChannel: '' },
  };
}

const form = reactive<ChannelFormModel>(emptyModel());

const data = ref<Record<string, any>>();
const isCreate = computed(() => Boolean(data.value?.create));

const getTitle = computed(() =>
  isCreate.value
    ? $t('notification.page.channel.create')
    : $t('notification.page.channel.edit'),
);

const tlsModeOptions = computed(() => [
  { value: 'starttls', label: $t('notification.page.channel.tlsStarttls') },
  { value: 'implicit', label: $t('notification.page.channel.tlsImplicit') },
  { value: 'none', label: $t('notification.page.channel.tlsNone') },
]);

function typeFilterOption(input: string, option: { label: string }): boolean {
  return option.label.toLowerCase().includes(input.toLowerCase());
}

function addHeader(): void {
  form.email.headers.push({ name: '', value: '' });
}

function removeHeader(index: number): void {
  form.email.headers.splice(index, 1);
}

// resetModel restores defaults then overlays a saved row when editing.
function resetModel(row?: Record<string, any>): void {
  Object.assign(form, emptyModel());
  if (!row) return;

  form.name = row.name ?? '';
  form.type = (row.type as ChannelType) ?? 'CHANNEL_TYPE_EMAIL';
  form.enabled = row.enabled ?? true;
  form.isDefault = row.isDefault ?? false;

  // config is a JSON string on the wire; decode into the typed sections.
  let cfg: Record<string, any> = {};
  if (typeof row.config === 'string' && row.config.trim()) {
    try {
      cfg = JSON.parse(row.config);
    } catch {
      cfg = {};
    }
  }

  if (form.type === 'CHANNEL_TYPE_EMAIL') {
    form.email.host = cfg.host ?? '';
    form.email.port = typeof cfg.port === 'number' ? cfg.port : 587;
    form.email.username = cfg.username ?? '';
    form.email.password = cfg.password ?? '';
    form.email.from = cfg.from ?? '';
    form.email.tlsMode = cfg.tls_mode ?? 'starttls';
    form.email.headers = cfg.custom_headers
      ? Object.entries(cfg.custom_headers).map(([name, value]) => ({
          name,
          value: String(value),
        }))
      : [];
  } else if (form.type === 'CHANNEL_TYPE_SMS') {
    form.sms.provider = cfg.provider ?? '';
    form.sms.apiKey = cfg.api_key ?? '';
    form.sms.fromNumber = cfg.from_number ?? '';
  } else if (form.type === 'CHANNEL_TYPE_SLACK') {
    form.slack.webhookUrl = cfg.webhook_url ?? '';
    form.slack.botToken = cfg.bot_token ?? '';
    form.slack.defaultChannel = cfg.default_channel ?? '';
  }
}

// buildConfig serializes the typed sections back into the JSON string the
// backend stores. Returns undefined for SSE (no config needed).
function buildConfig(): string | undefined {
  switch (form.type) {
    case 'CHANNEL_TYPE_EMAIL': {
      const cfg: Record<string, unknown> = {
        host: form.email.host.trim(),
        port: form.email.port,
        username: form.email.username,
        password: form.email.password,
        from: form.email.from.trim(),
        tls_mode: form.email.tlsMode,
      };
      const headers: Record<string, string> = {};
      for (const h of form.email.headers) {
        const name = h.name.trim();
        if (name) headers[name] = h.value;
      }
      if (Object.keys(headers).length > 0) cfg.custom_headers = headers;
      return JSON.stringify(cfg);
    }
    case 'CHANNEL_TYPE_SMS':
      return JSON.stringify({
        provider: form.sms.provider.trim(),
        api_key: form.sms.apiKey,
        from_number: form.sms.fromNumber.trim(),
      });
    case 'CHANNEL_TYPE_SLACK':
      return JSON.stringify({
        webhook_url: form.slack.webhookUrl.trim(),
        bot_token: form.slack.botToken,
        default_channel: form.slack.defaultChannel.trim(),
      });
    default:
      return '{}';
  }
}

// validate performs the client-side checks; the backend remains the
// authority (e.g. RFC-5322 header names, reserved headers, address syntax).
function validate(): string | null {
  if (!form.name.trim()) return $t('notification.page.channel.name');
  if (form.type === 'CHANNEL_TYPE_EMAIL') {
    if (!form.email.host.trim()) return $t('notification.page.channel.host');
    if (!form.email.from.trim()) return $t('notification.page.channel.from');
    // A header value without a name is meaningless — flag it early.
    const orphan = form.email.headers.find(
      (h) => h.value.trim() && !h.name.trim(),
    );
    if (orphan) return $t('notification.page.channel.headerName');
  } else if (form.type === 'CHANNEL_TYPE_SLACK') {
    if (!form.slack.webhookUrl.trim())
      return $t('notification.page.channel.slackWebhookUrl');
  } else if (form.type === 'CHANNEL_TYPE_SMS') {
    if (!form.sms.provider.trim())
      return $t('notification.page.channel.smsProvider');
  }
  return null;
}

const [Drawer, drawerApi] = useVbenDrawer({
  onCancel() {
    drawerApi.close();
  },

  async onConfirm() {
    const missing = validate();
    if (missing) {
      notification.error({
        message: $t('ui.placeholder.input') + ': ' + missing,
      });
      return;
    }

    setLoading(true);
    const config = buildConfig();
    try {
      if (isCreate.value) {
        await channelStore.createChannel({
          name: form.name.trim(),
          type: form.type,
          config,
          enabled: form.enabled,
          isDefault: form.isDefault,
        });
      } else {
        await channelStore.updateChannel(data.value!.row.id, {
          id: data.value!.row.id,
          name: form.name.trim(),
          config,
          enabled: form.enabled,
          isDefault: form.isDefault,
        });
      }
      notification.success({
        message: isCreate.value
          ? $t('ui.notification.create_success')
          : $t('ui.notification.update_success'),
      });
      drawerApi.close();
    } catch {
      notification.error({
        message: isCreate.value
          ? $t('ui.notification.create_failed')
          : $t('ui.notification.update_failed'),
      });
    } finally {
      setLoading(false);
    }
  },

  onOpenChange(isOpen: boolean) {
    if (isOpen) {
      data.value = drawerApi.getData<Record<string, any>>();
      resetModel(data.value?.row);
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
    <Form layout="vertical">
      <FormItem :label="$t('notification.page.channel.name')" required>
        <Input
          v-model:value="form.name"
          :placeholder="$t('ui.placeholder.input')"
          allow-clear
        />
      </FormItem>

      <FormItem :label="$t('notification.page.channel.channelType')" required>
        <Select
          v-model:value="form.type"
          :placeholder="$t('ui.placeholder.select')"
          :options="channelTypeList()"
          :disabled="!isCreate"
          show-search
          :filter-option="typeFilterOption"
        />
      </FormItem>

      <!-- EMAIL configuration -->
      <template v-if="form.type === 'CHANNEL_TYPE_EMAIL'">
        <div class="text-muted-foreground mb-2 mt-1 text-sm font-medium">
          {{ $t('notification.page.channel.emailSettings') }}
        </div>
        <FormItem :label="$t('notification.page.channel.host')" required>
          <Input v-model:value="form.email.host" placeholder="smtp.example.com" />
        </FormItem>
        <FormItem :label="$t('notification.page.channel.port')">
          <InputNumber
            v-model:value="form.email.port"
            :min="1"
            :max="65535"
            class="w-full"
          />
        </FormItem>
        <FormItem :label="$t('notification.page.channel.from')" required>
          <Input
            v-model:value="form.email.from"
            placeholder="noreply@example.com"
          />
        </FormItem>
        <FormItem :label="$t('notification.page.channel.tlsMode')">
          <Select v-model:value="form.email.tlsMode" :options="tlsModeOptions" />
        </FormItem>
        <FormItem :label="$t('notification.page.channel.username')">
          <Input v-model:value="form.email.username" allow-clear />
        </FormItem>
        <FormItem :label="$t('notification.page.channel.password')">
          <InputPassword
            v-model:value="form.email.password"
            autocomplete="new-password"
            allow-clear
          />
        </FormItem>

        <FormItem :label="$t('notification.page.channel.customHeaders')">
          <div class="text-muted-foreground mb-2 text-xs">
            {{ $t('notification.page.channel.customHeadersHint') }}
          </div>
          <div
            v-for="(header, index) in form.email.headers"
            :key="index"
            class="mb-2 flex items-center gap-2"
          >
            <Input
              v-model:value="header.name"
              :placeholder="$t('notification.page.channel.headerName')"
              style="flex: 2"
            />
            <Input
              v-model:value="header.value"
              :placeholder="$t('notification.page.channel.headerValue')"
              style="flex: 3"
            />
            <Button danger type="text" @click="removeHeader(index)">✕</Button>
          </div>
          <Button type="dashed" block @click="addHeader">
            + {{ $t('notification.page.channel.addHeader') }}
          </Button>
        </FormItem>
      </template>

      <!-- SMS configuration -->
      <template v-else-if="form.type === 'CHANNEL_TYPE_SMS'">
        <div class="text-muted-foreground mb-2 mt-1 text-sm font-medium">
          {{ $t('notification.page.channel.smsSettings') }}
        </div>
        <FormItem :label="$t('notification.page.channel.smsProvider')" required>
          <Input v-model:value="form.sms.provider" allow-clear />
        </FormItem>
        <FormItem :label="$t('notification.page.channel.smsApiKey')">
          <InputPassword
            v-model:value="form.sms.apiKey"
            autocomplete="new-password"
            allow-clear
          />
        </FormItem>
        <FormItem :label="$t('notification.page.channel.smsFromNumber')">
          <Input v-model:value="form.sms.fromNumber" allow-clear />
        </FormItem>
      </template>

      <!-- Slack configuration -->
      <template v-else-if="form.type === 'CHANNEL_TYPE_SLACK'">
        <div class="text-muted-foreground mb-2 mt-1 text-sm font-medium">
          {{ $t('notification.page.channel.slackSettings') }}
        </div>
        <FormItem
          :label="$t('notification.page.channel.slackWebhookUrl')"
          required
        >
          <Input
            v-model:value="form.slack.webhookUrl"
            placeholder="https://hooks.slack.com/services/..."
            allow-clear
          />
        </FormItem>
        <FormItem :label="$t('notification.page.channel.slackBotToken')">
          <InputPassword
            v-model:value="form.slack.botToken"
            autocomplete="new-password"
            allow-clear
          />
        </FormItem>
        <FormItem :label="$t('notification.page.channel.slackDefaultChannel')">
          <Input v-model:value="form.slack.defaultChannel" placeholder="#general" allow-clear />
        </FormItem>
      </template>

      <!-- SSE: no config -->
      <template v-else-if="form.type === 'CHANNEL_TYPE_SSE'">
        <div class="text-muted-foreground mb-2 text-sm">
          {{ $t('notification.page.channel.sseNoConfig') }}
        </div>
      </template>

      <FormItem :label="$t('ui.table.status')">
        <RadioGroup
          v-model:value="form.enabled"
          option-type="button"
          button-style="solid"
          :options="enableBoolList()"
        />
      </FormItem>

      <FormItem :label="$t('notification.page.channel.isDefault')">
        <RadioGroup
          v-model:value="form.isDefault"
          option-type="button"
          button-style="solid"
          :options="enableBoolList()"
        />
      </FormItem>
    </Form>
  </Drawer>
</template>
