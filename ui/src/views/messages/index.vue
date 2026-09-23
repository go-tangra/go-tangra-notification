<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { UiPage, UiAlert, UiCard, UiButton, UiDataTable, UiStatusChip, UiDrawer, UiForm, UiInput, UiSelect, UiTextarea, UiSwitch, UiCombobox, UiBadge, useConfirm, type Column, type SelectOption } from '@freya/ui'
import { useZodForm } from '@freya/ui/forms'
import { useMessages } from '@/stores/messages'
import { useCategories } from '@/stores/categories'
import { useDirectory } from '@/stores/directory'
import { describe } from '@/api/client'
import { messageSchema, messageFilterSchema, MESSAGE_TYPES, MESSAGE_STATUSES } from '@/schemas'
import type { Message } from '@/api/types'

const store = useMessages()
const categories = useCategories()
const dir = useDirectory()
const confirm = useConfirm()
const drawer = ref(false)
const selected = ref<Message | null>(null)
const error = ref('')
const filter = useZodForm(messageFilterSchema, { onSubmit: (f) => store.list({ status: f.status }) })
const reload = () => void filter.submit()
onMounted(async () => {
  await Promise.all([reload(), categories.list()])
})
const statusOptions: SelectOption[] = MESSAGE_STATUSES.map((s) => ({ title: s, value: s }))
const typeOptions: SelectOption[] = MESSAGE_TYPES.map((t) => ({ title: t, value: t }))
const categoryOptions = computed<SelectOption[]>(() => categories.items.map((c) => ({ title: c.name, value: c.id })))
const statusColors = { draft: 'neutral', scheduled: 'info', published: 'success', revoked: 'warning', archived: 'neutral' } as const

const editable = computed(() => !selected.value || selected.value.status === 'draft' || selected.value.status === 'scheduled')
const sendAfterSave = ref(false)
const form = useZodForm(messageSchema, {
  onSubmit: async (v) => {
    const input = { title: v.title, content: v.content, type: v.type, category_id: v.category_id || null, recipients: v.everyone ? { all: true } : { users: v.users }, scheduled_at: v.scheduled_at ?? null }
    const saved = selected.value ? await store.update(selected.value.id, input) : await store.create(input)
    if (sendAfterSave.value) await store.transition(saved.id, 'send')
  },
  onSuccess: () => {
    drawer.value = false
    reload()
  },
})
const everyone = computed(() => !!form.values.everyone)
const users = computed(() => (form.values.users ?? []) as string[])
const userHits = ref<SelectOption[]>([])
const pick = ref('')
async function search(q: string): Promise<void> {
  userHits.value = (await dir.searchUsers(q)).map((u) => ({ title: u.display_name, value: u.id }))
}
function addUser(id: unknown): void {
  if (typeof id === 'string' && id && !users.value.includes(id)) form.values.users = [...users.value, id]
  pick.value = ''
}
function removeUser(id: string): void {
  form.values.users = users.value.filter((u) => u !== id)
}
async function open(m: Message | null): Promise<void> {
  selected.value = m
  error.value = ''
  sendAfterSave.value = false
  form.reset({ title: m?.title ?? '', content: m?.content ?? '', type: m?.type ?? 'notification', category_id: m?.category_id ?? '', everyone: m?.recipients.all ?? false, users: [...(m?.recipients.users ?? [])], scheduled_at: m?.scheduled_at ? m.scheduled_at.slice(0, 16) : '' })
  drawer.value = true
  await dir.resolveUsers(users.value)
}
const save = (send: boolean) => {
  sendAfterSave.value = send
  return form.submit()
}
async function act(m: Message, action: 'cancel' | 'revoke' | 'archive' | 'remove'): Promise<void> {
  if (action === 'remove' && !(await confirm.ask({ title: `Delete “${m.title}”?`, danger: true, confirmLabel: 'Delete' }))) return
  error.value = ''
  try {
    if (action === 'remove') await store.remove(m.id)
    else await store.transition(m.id, action)
    reload()
  } catch (e) {
    error.value = describe(e)
  }
}
const columns: Column<Message>[] = [
  { key: 'title', label: 'Title', sortable: true },
  { key: 'status', label: 'Status', width: 'sm' },
  { key: 'category_name', label: 'Category', hideOnStack: true },
  { key: 'recipients', label: 'Recipients', format: (m) => (m.recipients.all ? 'everyone' : String(m.recipient_count ?? m.recipients.users?.length ?? 0)) },
  { key: 'read_count', label: 'Read', align: 'end', format: (m) => String(m.read_count ?? 0), hideOnStack: true },
]
</script>

<template>
  <UiPage title="Messages">
    <template #actions><UiButton icon="mdi-plus" data-test="message-new" @click="open(null)">New message</UiButton></template>
    <template #filters>
      <UiForm :form="filter" class="w-full md:max-w-xs"><UiSelect v-bind="filter.field('status')" label="Status" :options="statusOptions" size="sm" data-test="message-status-filter" @update:model-value="reload" /></UiForm>
    </template>
    <UiAlert v-if="error || store.error" kind="error" class="mb-3" data-test="message-list-error">{{ error || store.error }}</UiAlert>
    <UiCard :padded="false">
      <UiDataTable :items="store.items" :columns="columns" :loading="store.loading" caption="Messages" empty-title="No messages" clickable :row-attrs="(m) => ({ 'data-test': 'message-row-' + m.id })" data-test="messages-table" @row-click="open">
        <template #cell-status="{ row }"><UiStatusChip :status="row.status" :colors="statusColors" /></template>
        <template #actions="{ row }">
          <UiButton v-if="row.status === 'scheduled'" size="xs" variant="text" :data-test="'message-cancel-' + row.id" @click="act(row, 'cancel')">Cancel</UiButton>
          <UiButton v-if="row.status === 'published'" size="xs" variant="text" :data-test="'message-revoke-' + row.id" @click="act(row, 'revoke')">Revoke</UiButton>
          <UiButton v-if="row.status === 'published' || row.status === 'revoked'" size="xs" variant="text" :data-test="'message-archive-' + row.id" @click="act(row, 'archive')">Archive</UiButton>
          <UiButton v-if="row.status === 'draft' || row.status === 'archived' || row.status === 'revoked'" size="xs" variant="text" color="error" icon="mdi-delete-outline" icon-only label="Delete" :data-test="'message-remove-' + row.id" @click="act(row, 'remove')" />
        </template>
      </UiDataTable>
    </UiCard>
    <UiDrawer v-model="drawer" :title="selected ? 'Edit message' : 'New message'" size="lg" data-test="message-drawer">
      <UiForm :form="form">
        <div class="flex flex-col gap-3">
          <UiInput v-bind="form.field('title')" label="Title" :readonly="!editable" required data-test="message-title" />
          <UiTextarea v-bind="form.field('content')" label="Content" :rows="5" :disabled="!editable" required data-test="message-content" />
          <UiSelect v-bind="form.field('type')" label="Type" :options="typeOptions" :clearable="false" :disabled="!editable" required data-test="message-type" />
          <UiSelect v-bind="form.field('category_id')" label="Category" :options="categoryOptions" :disabled="!editable" data-test="message-category" />
          <UiSwitch v-bind="form.field('everyone')" label="Everyone in the tenant" :disabled="!editable" data-test="message-everyone" />
          <template v-if="!everyone">
            <UiCombobox id="message-users" v-model="pick" label="Add recipient" :options="userHits" :error="form.errors.value.users" placeholder="Type a name" :disabled="!editable" data-test="message-users" @search="search" @update:model-value="addUser" />
            <div v-if="users.length" class="flex flex-wrap gap-1">
              <span v-for="u in users" :key="u" class="badge badge-soft badge-primary gap-1">{{ dir.userName(u) || u }}<button v-if="editable" type="button" class="icon-[mdi--close] size-3" :aria-label="'Remove ' + (dir.userName(u) || u)" @click="removeUser(u)" /></span>
            </div>
          </template>
          <UiInput v-bind="form.field('scheduled_at')" label="Schedule (optional)" type="datetime-local" :readonly="!editable" data-test="message-schedule" />
        </div>
      </UiForm>
      <template #actions>
        <UiButton variant="text" @click="drawer = false">Cancel</UiButton>
        <UiButton v-if="editable" variant="soft" :loading="form.submitting.value" data-test="message-save" @click="save(false)">Save draft</UiButton>
        <UiButton v-if="editable" :loading="form.submitting.value" data-test="message-send" @click="save(true)">Send</UiButton>
        <UiBadge v-if="!editable">{{ selected?.status }}</UiBadge>
      </template>
    </UiDrawer>
  </UiPage>
</template>
