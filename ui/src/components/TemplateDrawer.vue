<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { useTemplates } from '@/stores/templates'
import { describe } from '@/api/client'
import type { Channel, Template } from '@/api/types'

const model = defineModel<boolean>({ required: true })
const props = defineProps<{ template: Template | null; channels: Channel[] }>()
const emit = defineEmits<{ saved: [Template]; removed: [string] }>()
const store = useTemplates()

const form = reactive({ name: '', channel_id: '', subject: '', body: '', variables: [] as string[], is_default: false })
const editing = computed(() => !!props.template)
const err = ref('')
const busy = ref(false)
const previewValues = reactive<Record<string, string>>({})
const preview = ref<{ subject: string; body: string } | null>(null)

const channelItems = computed(() => props.channels.map((c) => ({ title: c.name + ' (' + c.type + ')', value: c.id })))
const channelType = computed(() => props.channels.find((c) => c.id === form.channel_id)?.type ?? 'email')

watch(
  () => [model.value, props.template] as const,
  ([open]) => {
    if (!open) return
    err.value = ''
    preview.value = null
    const t = props.template
    form.name = t?.name ?? ''
    form.channel_id = t?.channel_id ?? props.channels[0]?.id ?? ''
    form.subject = t?.subject ?? ''
    form.body = t?.body ?? ''
    form.variables = [...(t?.variables ?? [])]
    form.is_default = t?.is_default ?? false
  },
  { immediate: true },
)

async function save(): Promise<void> {
  busy.value = true
  err.value = ''
  try {
    const input = { name: form.name, channel_id: form.channel_id, subject: form.subject, body: form.body, variables: form.variables, is_default: form.is_default }
    const saved = props.template ? await store.update(props.template.id, input) : await store.create(input)
    emit('saved', saved)
    model.value = false
  } catch (e) {
    err.value = describe(e)
  } finally {
    busy.value = false
  }
}

async function remove(): Promise<void> {
  if (!props.template) return
  busy.value = true
  try {
    await store.remove(props.template.id)
    emit('removed', props.template.id)
    model.value = false
  } catch (e) {
    err.value = describe(e)
  } finally {
    busy.value = false
  }
}

async function doPreview(): Promise<void> {
  err.value = ''
  try {
    const out = await store.preview({
      template_id: props.template?.id,
      channel_type: channelType.value,
      subject: form.subject,
      body: form.body,
      variables: form.variables,
      values: { ...previewValues },
    })
    preview.value = { subject: out.rendered_subject, body: out.rendered_body }
  } catch (e) {
    err.value = describe(e)
    preview.value = null
  }
}
</script>

<template>
  <v-navigation-drawer v-model="model" location="right" temporary width="560" data-test="template-drawer">
    <v-card flat>
      <v-card-title>{{ editing ? 'Edit template' : 'New template' }}</v-card-title>
      <v-card-text>
        <v-text-field v-model="form.name" label="Name" density="compact" data-test="template-name" />
        <v-select v-model="form.channel_id" :items="channelItems" label="Channel" density="compact" data-test="template-channel" />
        <v-text-field v-model="form.subject" label="Subject" density="compact" data-test="template-subject" />
        <v-textarea v-model="form.body" label="Body (Go template)" rows="6" density="compact" data-test="template-body" />
        <v-combobox v-model="form.variables" label="Declared variables" multiple chips density="compact" data-test="template-variables" />
        <v-switch v-model="form.is_default" label="Default for this channel" density="compact" color="primary" data-test="template-default" />
        <v-divider class="my-3" />
        <div class="text-subtitle-2 mb-2">Preview</div>
        <v-text-field
          v-for="v in form.variables"
          :key="v"
          v-model="previewValues[v]"
          :label="v"
          density="compact"
          :data-test="'preview-var-' + v"
        />
        <v-btn size="small" variant="tonal" prepend-icon="mdi-eye-outline" data-test="template-preview" @click="doPreview">Preview</v-btn>
        <div v-if="preview" class="mt-3" data-test="template-preview-result">
          <div class="text-caption text-medium-emphasis">Subject</div>
          <div class="mb-2" data-test="preview-subject">{{ preview.subject }}</div>
          <div class="text-caption text-medium-emphasis">Body</div>
          <pre class="preview-body" data-test="preview-body">{{ preview.body }}</pre>
        </div>
        <v-alert v-if="err" type="error" variant="tonal" density="compact" class="mt-2" data-test="template-error">{{ err }}</v-alert>
      </v-card-text>
      <v-card-actions>
        <v-btn v-if="editing && props.template?.permissions?.delete" color="error" variant="text" data-test="template-delete" @click="remove">Delete</v-btn>
        <v-spacer />
        <v-btn @click="model = false">Cancel</v-btn>
        <v-btn color="primary" :loading="busy" data-test="template-save" @click="save">Save</v-btn>
      </v-card-actions>
    </v-card>
  </v-navigation-drawer>
</template>

<style scoped>
.preview-body {
  white-space: pre-wrap;
  word-break: break-word;
  font-family: inherit;
  margin: 0;
}
</style>
