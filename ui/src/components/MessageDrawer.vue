<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { useMessages } from '@/stores/messages'
import { useDirectory } from '@/stores/directory'
import { describe } from '@/api/client'
import type { Category, Message, UserHit } from '@/api/types'

const model = defineModel<boolean>({ required: true })
const props = defineProps<{ message: Message | null; categories: Category[] }>()
const emit = defineEmits<{ saved: [Message]; sent: [] }>()
const store = useMessages()
const dir = useDirectory()

const form = reactive({
  title: '',
  content: '',
  type: 'notification' as 'notification' | 'private' | 'group',
  category_id: '' as string | null,
  everyone: false,
  users: [] as string[],
  scheduled_at: '',
})
const editing = computed(() => !!props.message)
const editable = computed(() => !props.message || props.message.status === 'draft' || props.message.status === 'scheduled')
const err = ref('')
const busy = ref(false)
const userQuery = ref('')
const userHits = ref<UserHit[]>([])

const categoryItems = computed(() => [{ title: '—', value: '' }, ...props.categories.map((c) => ({ title: c.name, value: c.id }))])

watch(
  () => [model.value, props.message] as const,
  async ([open]) => {
    if (!open) return
    err.value = ''
    const m = props.message
    form.title = m?.title ?? ''
    form.content = m?.content ?? ''
    form.type = m?.type ?? 'notification'
    form.category_id = m?.category_id ?? ''
    form.everyone = m?.recipients.all ?? false
    form.users = [...(m?.recipients.users ?? [])]
    form.scheduled_at = m?.scheduled_at ?? ''
    await dir.resolveUsers(form.users)
  },
  { immediate: true },
)

watch(userQuery, async (q) => {
  userHits.value = await dir.searchUsers(q)
})

function input() {
  return {
    title: form.title,
    content: form.content,
    type: form.type,
    category_id: form.category_id || null,
    recipients: form.everyone ? { all: true } : { users: form.users },
    scheduled_at: form.scheduled_at || null,
  }
}

async function save(): Promise<void> {
  busy.value = true
  err.value = ''
  try {
    const saved = props.message ? await store.update(props.message.id, input()) : await store.create(input())
    emit('saved', saved)
    model.value = false
  } catch (e) {
    err.value = describe(e)
  } finally {
    busy.value = false
  }
}

async function saveAndSend(): Promise<void> {
  busy.value = true
  err.value = ''
  try {
    const saved = props.message ? await store.update(props.message.id, input()) : await store.create(input())
    await store.transition(saved.id, 'send')
    emit('sent')
    model.value = false
  } catch (e) {
    err.value = describe(e)
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <v-navigation-drawer v-model="model" location="right" temporary width="520" data-test="message-drawer">
    <v-card flat>
      <v-card-title>{{ editing ? 'Edit message' : 'New message' }}</v-card-title>
      <v-card-text>
        <v-text-field v-model="form.title" label="Title" density="compact" :readonly="!editable" data-test="message-title" />
        <v-textarea v-model="form.content" label="Content" rows="5" density="compact" :readonly="!editable" data-test="message-content" />
        <v-select v-model="form.type" :items="['notification', 'private', 'group']" label="Type" density="compact" :readonly="!editable" data-test="message-type" />
        <v-select v-model="form.category_id" :items="categoryItems" label="Category" density="compact" :readonly="!editable" data-test="message-category" />
        <v-switch v-model="form.everyone" label="Everyone in the tenant" density="compact" color="primary" :readonly="!editable" data-test="message-everyone" />
        <v-autocomplete
          v-if="!form.everyone"
          v-model="form.users"
          v-model:search="userQuery"
          :items="userHits.map((u) => ({ title: u.display_name, value: u.id }))"
          label="Recipients"
          multiple
          chips
          density="compact"
          :readonly="!editable"
          data-test="message-users"
        />
        <v-text-field v-model="form.scheduled_at" label="Schedule (optional, ISO 8601)" density="compact" :readonly="!editable" data-test="message-schedule" />
        <v-alert v-if="err" type="error" variant="tonal" density="compact" data-test="message-error">{{ err }}</v-alert>
      </v-card-text>
      <v-card-actions>
        <v-spacer />
        <v-btn @click="model = false">Cancel</v-btn>
        <v-btn v-if="editable" :loading="busy" data-test="message-save" @click="save">Save draft</v-btn>
        <v-btn v-if="editable" color="primary" :loading="busy" data-test="message-send" @click="saveAndSend">Send</v-btn>
      </v-card-actions>
    </v-card>
  </v-navigation-drawer>
</template>
