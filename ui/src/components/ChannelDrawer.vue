<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { useChannels } from '@/stores/channels'
import { describe } from '@/api/client'
import { SECRET_FIELDS, SET_MARKER, type Channel, type ChannelType, type Settings } from '@/api/types'

const model = defineModel<boolean>({ required: true })
const props = defineProps<{ channel: Channel | null }>()
const emit = defineEmits<{ saved: [Channel]; removed: [string] }>()
const store = useChannels()

const types: ChannelType[] = ['email', 'sms', 'slack', 'sse']
const form = reactive({
  name: '',
  type: 'email' as ChannelType,
  enabled: false,
  is_default: false,
  host: '',
  port: 587,
  tls: 'starttls',
  username: '',
  password: '',
  from: '',
  reply_to: '',
  api_key: '',
  account: '',
})
const editing = computed(() => !!props.channel)
const err = ref('')
const busy = ref(false)
const testRecipient = ref('')
const testResult = ref('')

watch(
  () => [model.value, props.channel] as const,
  ([open]) => {
    if (!open) return
    err.value = ''
    testResult.value = ''
    const c = props.channel
    form.name = c?.name ?? ''
    form.type = c?.type ?? 'email'
    form.enabled = c?.enabled ?? false
    form.is_default = c?.is_default ?? false
    const s = (c?.settings ?? {}) as Record<string, unknown>
    form.host = String(s.host ?? '')
    form.port = Number(s.port ?? 587)
    form.tls = String(s.tls ?? 'starttls')
    form.username = String(s.username ?? '')
    form.from = String(s.from ?? '')
    form.reply_to = String(s.reply_to ?? '')
    form.account = String(s.account ?? '')
    form.password = s.password === SET_MARKER ? SET_MARKER : ''
    form.api_key = s.api_key === SET_MARKER ? SET_MARKER : ''
  },
  { immediate: true },
)

function settings(): Settings {
  if (form.type === 'email') {
    const s: Settings = { host: form.host, port: form.port, tls: form.tls, from: form.from }
    if (form.username) s.username = form.username
    if (form.reply_to) s.reply_to = form.reply_to
    if (form.password) s.password = form.password
    return s
  }
  const s: Settings = { account: form.account }
  if (form.api_key) s.api_key = form.api_key
  return s
}

async function save(): Promise<void> {
  busy.value = true
  err.value = ''
  try {
    const input = { name: form.name, type: form.type, settings: settings(), enabled: form.enabled, is_default: form.is_default }
    const saved = props.channel ? await store.update(props.channel.id, input) : await store.create(input)
    emit('saved', saved)
    model.value = false
  } catch (e) {
    err.value = describe(e)
  } finally {
    busy.value = false
  }
}

async function remove(): Promise<void> {
  if (!props.channel) return
  busy.value = true
  err.value = ''
  try {
    await store.remove(props.channel.id)
    emit('removed', props.channel.id)
    model.value = false
  } catch (e) {
    err.value = describe(e)
  } finally {
    busy.value = false
  }
}

async function sendTest(): Promise<void> {
  if (!props.channel || !testRecipient.value) return
  busy.value = true
  testResult.value = ''
  try {
    const entry = await store.test(props.channel.id, testRecipient.value)
    testResult.value = entry.status === 'sent' ? 'Test message sent.' : 'Failed: ' + (entry.error ?? 'unknown')
  } catch (e) {
    testResult.value = describe(e)
  } finally {
    busy.value = false
  }
}

const secretHint = computed(() => (SECRET_FIELDS[form.type].length ? 'Leave blank to keep the stored value.' : ''))
</script>

<template>
  <v-navigation-drawer v-model="model" location="right" temporary width="440" data-test="channel-drawer">
    <v-card flat>
      <v-card-title>{{ editing ? 'Edit channel' : 'New channel' }}</v-card-title>
      <v-card-text>
        <v-text-field v-model="form.name" label="Name" density="compact" data-test="channel-name" />
        <v-select v-model="form.type" :items="types" label="Type" density="compact" :disabled="editing" data-test="channel-type" />
        <template v-if="form.type === 'email'">
          <v-text-field v-model="form.host" label="SMTP host" density="compact" data-test="channel-host" />
          <v-text-field v-model.number="form.port" label="Port" type="number" density="compact" data-test="channel-port" />
          <v-select v-model="form.tls" :items="['implicit', 'starttls', 'none']" label="TLS" density="compact" data-test="channel-tls" />
          <v-text-field v-model="form.from" label="From" density="compact" data-test="channel-from" />
          <v-text-field v-model="form.reply_to" label="Reply-To (optional)" density="compact" />
          <v-text-field v-model="form.username" label="Username (optional)" density="compact" />
          <v-text-field v-model="form.password" label="Password" type="password" density="compact" :hint="secretHint" persistent-hint data-test="channel-password" />
        </template>
        <template v-else>
          <v-text-field v-model="form.account" label="Account" density="compact" data-test="channel-account" />
          <v-text-field v-model="form.api_key" label="API key" type="password" density="compact" :hint="secretHint" persistent-hint data-test="channel-apikey" />
          <v-alert type="info" variant="tonal" density="compact" class="mb-2">No provider yet: this type stores settings but cannot deliver.</v-alert>
        </template>
        <v-switch v-model="form.enabled" label="Enabled" density="compact" color="primary" data-test="channel-enabled" />
        <v-switch v-model="form.is_default" label="Default for this type" density="compact" color="primary" data-test="channel-default" />
        <v-alert v-if="err" type="error" variant="tonal" density="compact" data-test="channel-error">{{ err }}</v-alert>
        <template v-if="editing">
          <v-divider class="my-3" />
          <div class="text-subtitle-2 mb-2">Send a test message</div>
          <v-text-field v-model="testRecipient" label="Recipient" density="compact" data-test="channel-test-recipient" />
          <v-btn size="small" variant="tonal" :loading="busy" prepend-icon="mdi-send-check-outline" data-test="channel-test-send" @click="sendTest">Send test</v-btn>
          <div v-if="testResult" class="text-caption mt-2" data-test="channel-test-result">{{ testResult }}</div>
        </template>
      </v-card-text>
      <v-card-actions>
        <v-btn v-if="editing && props.channel?.permissions?.delete" color="error" variant="text" data-test="channel-delete" @click="remove">Delete</v-btn>
        <v-spacer />
        <v-btn @click="model = false">Cancel</v-btn>
        <v-btn color="primary" :loading="busy" data-test="channel-save" @click="save">Save</v-btn>
      </v-card-actions>
    </v-card>
  </v-navigation-drawer>
</template>
