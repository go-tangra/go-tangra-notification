<script setup lang="ts">
import { computed, defineAsyncComponent, onMounted, ref, watch } from 'vue'
import { UiPage, UiAlert, UiCard, UiButton, UiDataTable, UiIcon, UiInput, UiDrawer, UiForm, UiSelect, UiTextarea, UiSwitch, UiBadge, UiSection, useConfirm, type Column, type SelectOption } from '@go-tangra/ui'
import { useZodForm } from '@go-tangra/ui/forms'
import { useTemplates } from '@/stores/templates'
import { useChannels } from '@/stores/channels'
import { ApiError, describe } from '@/api/client'
import { templateSchema, systemTemplateSchema } from '@/schemas'
import type { Template } from '@/api/types'

// The rich-text editor (TipTap/ProseMirror) is a separate chunk, fetched the
// first time an email template is opened.
const TemplateBodyEditor = defineAsyncComponent(() => import('@/components/TemplateBodyEditor.vue'))

const store = useTemplates()
const channels = useChannels()
const confirm = useConfirm()
const drawer = ref(false)
const selected = ref<Template | null>(null)
const error = ref('')
const query = ref('')
onMounted(async () => {
  await Promise.all([store.list(), channels.list()])
})
let timer: ReturnType<typeof setTimeout> | undefined
watch(query, (q) => {
  clearTimeout(timer)
  timer = setTimeout(() => store.list(undefined, q || undefined), 200)
})
const channelOptions = computed<SelectOption[]>(() => channels.items.map((c) => ({ title: c.name + ' (' + c.type + ')', value: c.id })))

const form = useZodForm(templateSchema, {
  onSubmit: async (v) => {
    if (selected.value) await store.update(selected.value.id, v)
    else await store.create(v)
  },
  onSuccess: () => {
    drawer.value = false
    void store.list()
  },
})
// System templates (sent by key by auth or warden): only subject and body change.
const isSystem = computed(() => !!selected.value?.system_key)
const sysForm = useZodForm(systemTemplateSchema, {
  onSubmit: async (v) => {
    const t = selected.value!
    try {
      await store.update(t.id, { name: t.name, channel_id: null, subject: v.subject, body: v.body, variables: [...(t.variables ?? [])], is_default: false })
    } catch (e) {
      error.value = e instanceof ApiError && e.reason === 'missing_required_variable' ? `The template must keep {{.${String(e.detail?.variable ?? '')}}}: the sending service always supplies it.` : describe(e)
      throw e
    }
  },
  onSuccess: () => {
    drawer.value = false
    void store.list()
  },
})
const activeForm = computed(() => (isSystem.value ? sysForm : form))
const owner = computed(() => selected.value?.system_key?.split('.')[0] ?? '')
async function restore(): Promise<void> {
  if (!selected.value || !(await confirm.ask({ title: `Restore the built-in wording of ${selected.value.name}?`, confirmLabel: 'Restore' }))) return
  try {
    const t = await store.restore(selected.value.id)
    selected.value = t
    sysForm.reset({ subject: t.subject, body: t.body })
  } catch (e) {
    error.value = describe(e)
  }
}

// Declared variables: a comma-separated field kept in sync with the schema's array.
const variablesText = ref('')
watch(variablesText, (t) => (form.values.variables = t.split(',').map((s) => s.trim()).filter(Boolean)))
const variables = computed(() => (isSystem.value ? (selected.value?.variables ?? []) : ((form.values.variables ?? []) as string[])))
const previewValues = ref<Record<string, string>>({})
const preview = ref<{ subject: string; body: string } | null>(null)
// Offered by "Insert variable": the declared ones, plus the required ones of a system template.
const insertable = computed(() => [...new Set([...variables.value, ...(selected.value?.required_variables ?? [])])])
// Remounts the body editor for every template opened (fresh mode and history).
const opened = ref(0)
const channelType = computed(() => (isSystem.value ? (selected.value?.channel_type ?? 'email') : (channels.items.find((c) => c.id === form.values.channel_id)?.type ?? 'email')))

function open(t: Template | null): void {
  selected.value = t
  error.value = ''
  preview.value = null
  previewValues.value = {}
  form.reset({ name: t?.name ?? '', channel_id: t?.channel_id ?? channels.items[0]?.id ?? '', subject: t?.subject ?? '', body: t?.body ?? '', variables: [...(t?.variables ?? [])], is_default: t?.is_default ?? false })
  sysForm.reset({ subject: t?.subject ?? '', body: t?.body ?? '' })
  variablesText.value = (t?.variables ?? []).join(', ')
  opened.value++
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
async function doPreview(): Promise<void> {
  error.value = ''
  try {
    const f = activeForm.value
    const out = await store.preview({ template_id: selected.value?.id, channel_type: channelType.value, subject: String(f.values.subject ?? ''), body: String(f.values.body ?? ''), variables: variables.value, values: { ...previewValues.value } })
    preview.value = { subject: out.rendered_subject, body: out.rendered_body }
  } catch (e) {
    error.value = describe(e)
    preview.value = null
  }
}
const columns: Column<Template>[] = [
  { key: 'name', label: 'Name', sortable: true },
  { key: 'channel_name', label: 'Channel', format: (t) => (t.channel_name ?? '') + (t.channel_type ? ' (' + t.channel_type + ')' : '') },
  { key: 'subject', label: 'Subject', hideOnStack: true },
  { key: 'is_default', label: 'Default', width: 'sm', format: (t) => (t.is_default ? 'yes' : '') },
]
</script>

<template>
  <UiPage title="Templates">
    <template #actions>
      <UiButton icon="mdi-plus" :disabled="!channels.items.length" data-test="template-new" @click="open(null)">New template</UiButton>
    </template>
    <template #filters><UiInput id="template-search" v-model="query" label="Search" sr-only-label placeholder="Search templates" type="search" class="w-full md:max-w-sm" data-test="template-search" /></template>
    <UiAlert v-if="store.error" kind="error" class="mb-3">{{ store.error }}</UiAlert>
    <UiCard :padded="false">
      <UiDataTable :items="store.items" :columns="columns" :loading="store.loading" caption="Templates" empty-title="No templates" clickable :row-attrs="(t) => ({ 'data-test': 'template-row-' + t.id })" data-test="templates-table" @row-click="open">
        <template #cell-name="{ row }">
          <span>{{ row.name }}</span>
          <UiBadge v-if="row.system_key" class="ms-2" color="info" :data-test="'template-system-' + row.id">System</UiBadge>
          <UiBadge v-if="row.system_key && row.edited" class="ms-1" color="warning" soft>Edited</UiBadge>
        </template>
        <template #cell-is_default="{ row }"><UiIcon v-if="row.is_default" name="mdi-star" size="sm" class="text-warning" label="Default template" /></template>
      </UiDataTable>
    </UiCard>
    <UiDrawer v-model="drawer" :title="selected ? 'Edit template' : 'New template'" size="xl" data-test="template-drawer">
      <UiAlert v-if="error" kind="error" class="mb-3" data-test="template-error">{{ error }}</UiAlert>
      <template v-if="isSystem">
        <UiAlert kind="info" class="mb-3" data-test="template-system-note">
          System template <strong>{{ selected?.system_key }}</strong>, sent by the {{ owner }} service. You can change the subject and body; name, channel and variables are fixed. Upgrades keep your wording; "Restore built-in" brings the original back.
        </UiAlert>
        <UiForm :form="sysForm">
          <div class="flex flex-col gap-3">
            <UiInput v-bind="sysForm.field('subject')" label="Subject" required data-test="template-subject" />
            <TemplateBodyEditor v-if="channelType === 'email'" :key="opened" v-bind="sysForm.field('body')" label="Body (Go template, HTML)" required :variables="insertable" data-test="template-body" />
            <UiTextarea v-else v-bind="sysForm.field('body')" label="Body (Go template)" :rows="10" required data-test="template-body" />
            <div class="flex flex-wrap items-center gap-1" data-test="template-required">
              <span class="text-xs text-base-content/70">Variables (required ones must stay):</span>
              <UiBadge v-for="v in selected?.variables ?? []" :key="v" :color="selected?.required_variables?.includes(v) ? 'primary' : 'neutral'" soft>{{ v }}{{ selected?.required_variables?.includes(v) ? ' (required)' : '' }}</UiBadge>
            </div>
            <p v-if="selected?.secret_variables?.length" class="text-xs text-base-content/70" data-test="template-secret-note">
              Secret: {{ selected.secret_variables.join(', ') }}. The delivery log stores [redacted] in place of these values.
            </p>
          </div>
        </UiForm>
      </template>
      <UiForm v-else :form="form">
        <div class="flex flex-col gap-3">
          <UiInput v-bind="form.field('name')" label="Name" required data-test="template-name" />
          <UiSelect v-bind="form.field('channel_id')" label="Channel" :options="channelOptions" :clearable="false" required data-test="template-channel" />
          <UiInput v-bind="form.field('subject')" label="Subject" required data-test="template-subject" />
          <TemplateBodyEditor v-if="channelType === 'email'" :key="opened" v-bind="form.field('body')" label="Body (Go template, HTML)" required :variables="insertable" data-test="template-body" />
          <UiTextarea v-else v-bind="form.field('body')" label="Body (Go template)" :rows="6" required data-test="template-body" />
          <UiInput id="template-variables" v-model="variablesText" label="Declared variables (comma-separated)" :error="form.errors.value.variables ?? form.errors.value['variables.0']" data-test="template-variables" />
          <div v-if="variables.length" class="flex flex-wrap gap-1"><UiBadge v-for="v in variables" :key="v" color="primary">{{ v }}</UiBadge></div>
          <UiSwitch v-bind="form.field('is_default')" label="Default for this channel" data-test="template-default" />
        </div>
      </UiForm>
      <UiSection title="Preview" class="mt-4">
        <div class="grid grid-cols-1 gap-2 md:grid-cols-2">
          <UiInput v-for="v in variables" :id="'preview-var-' + v" :key="v" v-model="previewValues[v]" :label="v" size="sm" :data-test="'preview-var-' + v" />
        </div>
        <UiButton size="sm" variant="soft" icon="mdi-eye-outline" class="mt-2" data-test="template-preview" @click="doPreview">Preview</UiButton>
        <div v-if="preview" class="mt-3 rounded-box border border-base-300 p-3" data-test="template-preview-result">
          <div class="text-xs text-base-content/70">Subject</div>
          <div class="mb-2" data-test="preview-subject">{{ preview.subject }}</div>
          <div class="text-xs text-base-content/70">Body</div>
          <pre class="whitespace-pre-wrap break-words font-sans" data-test="preview-body">{{ preview.body }}</pre>
        </div>
      </UiSection>
      <template #actions>
        <UiButton v-if="selected && selected.permissions?.delete && !isSystem" variant="text" color="error" data-test="template-delete" @click="remove">Delete</UiButton>
        <UiButton v-if="isSystem && selected?.permissions?.write" variant="text" icon="mdi-restore" data-test="template-restore" @click="restore">Restore built-in</UiButton>
        <UiButton variant="text" @click="drawer = false">Cancel</UiButton>
        <UiButton :loading="activeForm.submitting.value" data-test="template-save" @click="activeForm.submit()">Save</UiButton>
      </template>
    </UiDrawer>
  </UiPage>
</template>
