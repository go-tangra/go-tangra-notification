<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { UiPage, UiAlert, UiCard, UiButton, UiDataTable, UiBadge, UiIcon, UiStatusChip, UiDrawer, UiForm, UiInput, UiSelect, UiNumberInput, UiSwitch, UiSecretField, UiSection, useConfirm, useToast, type Column, type SelectOption } from '@go-tangra/ui'
import { useZodForm } from '@go-tangra/ui/forms'
import { useChannels } from '@/stores/channels'
import { describe } from '@/api/client'
import { channelSchema, testMessageSchema, CHANNEL_TYPES, TLS_MODES } from '@/schemas'
import { SET_MARKER, type Channel, type Settings } from '@/api/types'

const store = useChannels()
const confirm = useConfirm()
const toast = useToast()
const drawer = ref(false)
const selected = ref<Channel | null>(null)
const error = ref('')
onMounted(() => store.list())
const typeOptions: SelectOption[] = CHANNEL_TYPES.map((t) => ({ title: t, value: t }))
const tlsOptions: SelectOption[] = TLS_MODES.map((t) => ({ title: t, value: t }))

const form = useZodForm(channelSchema, {
  onSubmit: async (v) => {
    // Secrets travel only when typed (or as the unchanged marker); blank keeps the stored one.
    const settings: Settings = v.type === 'email' ? { host: v.host, port: v.port, tls: v.tls, from: v.from, ...(v.username ? { username: v.username } : {}), ...(v.reply_to ? { reply_to: v.reply_to } : {}), ...(v.password ? { password: v.password } : {}) } : { account: v.account, ...(v.api_key ? { api_key: v.api_key } : {}) }
    const input = { name: v.name, type: v.type, settings, enabled: v.enabled, is_default: v.is_default }
    if (selected.value) await store.update(selected.value.id, input)
    else await store.create(input)
  },
  onSuccess: () => {
    drawer.value = false
    void store.list()
  },
})
const isEmail = computed(() => form.values.type === 'email')
/** The platform channel comes from the platform_email configuration: read-only here, testable. */
const readOnly = computed(() => !!selected.value?.managed)
function open(c: Channel | null): void {
  selected.value = c
  error.value = ''
  const s = (c?.settings ?? {}) as Record<string, unknown>
  form.reset({ name: c?.name ?? '', type: c?.type ?? 'email', enabled: c?.enabled ?? false, is_default: c?.is_default ?? false, host: String(s.host ?? ''), port: Number(s.port ?? 587), tls: (TLS_MODES.includes(s.tls as (typeof TLS_MODES)[number]) ? s.tls : 'starttls') as (typeof TLS_MODES)[number], username: String(s.username ?? ''), from: String(s.from ?? ''), reply_to: String(s.reply_to ?? ''), account: String(s.account ?? ''), password: s.password === SET_MARKER ? SET_MARKER : '', api_key: s.api_key === SET_MARKER ? SET_MARKER : '' })
  testForm.reset({ recipient: '' })
  drawer.value = true
}
async function remove(): Promise<void> {
  if (!selected.value || !(await confirm.ask({ title: `Delete ${selected.value.name}?`, danger: true, confirmLabel: 'Delete' }))) return
  try {
    await store.remove(selected.value.id)
    drawer.value = false
    void store.list()
  } catch (e) {
    error.value = describe(e)
  }
}
const testResult = ref('')
const testForm = useZodForm(testMessageSchema, {
  initial: { recipient: '' },
  onSubmit: async (v) => {
    const entry = await store.test(selected.value!.id, v.recipient)
    testResult.value = entry.status === 'sent' ? 'Test message sent.' : 'Failed: ' + (entry.error ?? 'unknown')
    if (entry.status === 'sent') toast.success('Test message sent')
  },
})
const columns: Column<Channel>[] = [
  { key: 'name', label: 'Name', sortable: true },
  { key: 'type', label: 'Type', width: 'sm' },
  { key: 'enabled', label: 'Status', width: 'sm', format: (c) => (c.enabled ? 'enabled' : 'disabled') },
  { key: 'is_default', label: 'Default', width: 'sm', format: (c) => (c.is_default ? 'yes' : '') },
  { key: 'template_count', label: 'Templates', align: 'end', format: (c) => String(c.template_count ?? 0) },
]
</script>

<template>
  <UiPage title="Channels">
    <template #actions><UiButton icon="mdi-plus" data-test="channel-new" @click="open(null)">New channel</UiButton></template>
    <UiAlert v-if="store.error" kind="error" class="mb-3">{{ store.error }}</UiAlert>
    <UiCard :padded="false">
      <UiDataTable :items="store.items" :columns="columns" :loading="store.loading" caption="Channels" empty-title="No channels yet" clickable :row-attrs="(c) => ({ 'data-test': 'channel-row-' + c.id })" data-test="channels-table" @row-click="open">
        <template #cell-name="{ row }">
          <span>{{ row.name }}</span>
          <UiBadge v-if="row.managed" class="ms-2" color="info" :data-test="'channel-managed-' + row.id">Managed</UiBadge>
        </template>
        <template #cell-type="{ row }"><UiBadge>{{ row.type }}</UiBadge></template>
        <template #cell-enabled="{ row }"><UiStatusChip :status="row.enabled ? 'enabled' : 'disabled'" /></template>
        <template #cell-is_default="{ row }"><UiIcon v-if="row.is_default" name="mdi-star" size="sm" class="text-warning" label="Default channel" /></template>
      </UiDataTable>
    </UiCard>
    <UiDrawer v-model="drawer" :title="readOnly ? 'Platform channel' : selected ? 'Edit channel' : 'New channel'" size="lg" data-test="channel-drawer">
      <UiAlert v-if="error" kind="error" class="mb-3" data-test="channel-error">{{ error }}</UiAlert>
      <UiAlert v-if="readOnly" kind="info" class="mb-3" data-test="channel-managed-note">
        Managed by configuration: this channel is created from the notification module's platform_email setting and carries all platform email. Change the relay there and restart the notification module.
      </UiAlert>
      <UiForm :form="form">
        <div class="flex flex-col gap-3">
          <UiInput v-bind="form.field('name')" :disabled="readOnly" label="Name" required data-test="channel-name" />
          <UiSelect v-bind="form.field('type')" :disabled="readOnly || !!selected" label="Type" :options="typeOptions" :clearable="false" required data-test="channel-type" />
          <template v-if="isEmail">
            <UiInput v-bind="form.field('host')" :disabled="readOnly" label="SMTP host" required data-test="channel-host" />
            <UiNumberInput v-bind="form.field('port')" :disabled="readOnly" label="Port" :min="1" :max="65535" required data-test="channel-port" />
            <UiSelect v-bind="form.field('tls')" :disabled="readOnly" label="TLS" :options="tlsOptions" :clearable="false" data-test="channel-tls" />
            <UiInput v-bind="form.field('from')" :disabled="readOnly" label="From" type="email" required data-test="channel-from" />
            <UiInput v-bind="form.field('reply_to')" :disabled="readOnly" label="Reply-To (optional)" type="email" />
            <UiInput v-bind="form.field('username')" :disabled="readOnly" label="Username (optional)" autocomplete="off" />
            <UiSecretField v-bind="form.field('password')" :disabled="readOnly" label="Password" hint="Leave blank to keep the stored value." data-test="channel-password" />
          </template>
          <template v-else>
            <UiInput v-bind="form.field('account')" :disabled="readOnly" label="Account" required data-test="channel-account" />
            <UiSecretField v-bind="form.field('api_key')" :disabled="readOnly" label="API key" hint="Leave blank to keep the stored value." data-test="channel-apikey" />
            <UiAlert kind="info">No provider yet: this type stores settings but cannot deliver.</UiAlert>
          </template>
          <UiSwitch v-bind="form.field('enabled')" :disabled="readOnly" label="Enabled" data-test="channel-enabled" />
          <UiSwitch v-bind="form.field('is_default')" :disabled="readOnly" label="Default for this type" data-test="channel-default" />
        </div>
      </UiForm>
      <UiSection v-if="selected" title="Send a test message" class="mt-4">
        <UiForm :form="testForm">
          <div class="flex flex-wrap items-end gap-2">
            <UiInput v-bind="testForm.field('recipient')" label="Recipient" class="grow" data-test="channel-test-recipient" />
            <UiButton type="submit" size="sm" variant="soft" icon="mdi-send-check-outline" :loading="testForm.submitting.value" data-test="channel-test-send">Send test</UiButton>
          </div>
        </UiForm>
        <p v-if="testResult" class="mt-2 text-xs" data-test="channel-test-result">{{ testResult }}</p>
      </UiSection>
      <template #actions>
        <UiButton v-if="selected && selected.permissions?.delete && !readOnly" variant="text" color="error" data-test="channel-delete" @click="remove">Delete</UiButton>
        <UiButton variant="text" @click="drawer = false">{{ readOnly ? 'Close' : 'Cancel' }}</UiButton>
        <UiButton v-if="!readOnly" :loading="form.submitting.value" data-test="channel-save" @click="form.submit()">Save</UiButton>
      </template>
    </UiDrawer>
  </UiPage>
</template>
