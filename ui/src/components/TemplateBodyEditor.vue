<script setup lang="ts">
// Email template body: a rich-text (WYSIWYG) editor with an HTML source view.
// The source view is authoritative: a body the visual editor cannot keep exactly
// (see analyseBody) opens in source mode, and nothing is rewritten until the
// operator edits it in the visual editor. Template content is never executed
// here; rendering stays with the server's preview endpoint.
import { computed, nextTick, onBeforeUnmount, onMounted, ref, shallowRef, watch } from 'vue'
import { Editor } from '@tiptap/core'
import { UiAlert, UiButton, UiDropdownMenu, UiField, UiInput, useConfirm, type MenuItem } from '@go-tangra/ui'
import { analyseBody, bodyExtensions, serializeDoc } from '@/editor/bodyEditor'
import { insertAt } from '@/editor/goTemplate'

const props = withDefaults(
  defineProps<{
    id: string
    label: string
    modelValue?: unknown
    error?: string | undefined
    required?: boolean | undefined
    /** Declared (and, for system templates, required) variables offered by "Insert variable". */
    variables?: string[]
    rows?: number
    dataField?: string | undefined
  }>(),
  { modelValue: () => '', error: undefined, required: false, variables: () => [], rows: 12, dataField: undefined },
)
const emit = defineEmits<{ 'update:modelValue': [v: string]; blur: [] }>()
const confirm = useConfirm()

type Mode = 'visual' | 'source'
const mode = ref<Mode>('visual')
const notice = ref('')
const host = ref<HTMLElement | null>(null)
const source = ref<HTMLTextAreaElement | null>(null)
const editor = shallowRef<Editor | null>(null)
// Bumped on every editor transaction so the toolbar's active states re-evaluate.
const tick = ref(0)
// The body the editor currently shows (what it last loaded or emitted).
let shown = ''

const body = computed(() => String(props.modelValue ?? ''))
const UNFAITHFUL = 'This body uses HTML the visual editor cannot keep exactly (for example inline styles, other elements, or a template action between blocks), so it is shown as source.'
const UNBALANCED = 'This body has an unterminated template action ({{ without }}); fix it in the source view.'

function setBody(v: string): void {
  shown = v
  emit('update:modelValue', v)
}

function createEditor(): Editor {
  const e = new Editor({
    element: host.value!,
    extensions: bodyExtensions(),
    // TipTap's own <style> tag would be blocked by the CSP; main.css carries the rules.
    injectCSS: false,
    editorProps: {
      attributes: {
        role: 'textbox',
        'aria-multiline': 'true',
        'aria-label': props.label,
        'data-test': 'template-body-visual',
        ...(props.dataField ? { 'data-field': props.dataField } : {}),
        class: 'min-h-48 p-3 focus:outline-none',
      },
    },
    onUpdate: ({ editor: ed }) => setBody(serializeDoc(ed.state.doc)),
    onTransaction: () => tick.value++,
    onBlur: () => emit('blur'),
  })
  editor.value = e
  return e
}

/** Shows value in the visual editor; with force, also a body it cannot keep exactly (it is rewritten). */
function load(value: string, force = false): boolean {
  const a = analyseBody(value)
  if (!a.doc || (!a.faithful && !force)) {
    mode.value = 'source'
    notice.value = a.doc ? UNFAITHFUL : UNBALANCED
    return false
  }
  const e = editor.value ?? createEditor()
  e.commands.setContent(a.doc.toJSON(), { emitUpdate: false })
  e.commands.setTextSelection(0)
  shown = value
  notice.value = ''
  mode.value = 'visual'
  if (!a.faithful) setBody(serializeDoc(e.state.doc))
  return true
}

onMounted(() => load(body.value))
onBeforeUnmount(() => editor.value?.destroy())

// A new value from outside (drawer opened on another template, restore to
// built-in) replaces what the visual editor shows.
watch(body, (v) => {
  if (v !== shown && mode.value === 'visual') load(v)
})

async function toggleMode(): Promise<void> {
  if (mode.value === 'visual') {
    mode.value = 'source'
    return
  }
  const a = analyseBody(body.value)
  if (!a.doc) {
    notice.value = UNBALANCED
    return
  }
  if (!a.faithful && !(await confirm.ask({ title: 'Switch to the visual editor?', text: 'The visual editor cannot keep parts of this body exactly and will rewrite them. Template actions are kept as written.', confirmLabel: 'Switch' }))) return
  load(body.value, true)
}

const variableItems = computed<MenuItem[]>(() => [...new Set(props.variables)].map((v) => ({ key: v, label: '{{.' + v + '}}' })))

async function insertVariable(name: string): Promise<void> {
  const code = '{{.' + name + '}}'
  if (mode.value === 'visual' && editor.value) {
    editor.value.chain().focus().insertContent({ type: 'goAction', attrs: { code } }).run()
    return
  }
  const ta = source.value
  const v = body.value
  const r = insertAt(v, ta?.selectionStart ?? v.length, ta?.selectionEnd ?? v.length, code)
  setBody(r.value)
  await nextTick()
  ta?.focus()
  ta?.setSelectionRange(r.caret, r.caret)
}

defineExpose({ editor, mode })

function onSourceInput(ev: Event): void {
  setBody((ev.target as HTMLTextAreaElement).value)
}

// Toolbar ------------------------------------------------------------------
function active(name: string, attrs?: Record<string, unknown>): boolean {
  void tick.value
  return !!editor.value?.isActive(name, attrs)
}
const can = computed(() => {
  void tick.value
  return { undo: !!editor.value?.can().undo(), redo: !!editor.value?.can().redo() }
})
type Chain = ReturnType<Editor['chain']>
function run(fn: (c: Chain) => Chain): void {
  if (editor.value) fn(editor.value.chain().focus()).run()
}
const marks = [
  { name: 'bold', label: 'Bold', icon: 'icon-[mdi--format-bold]', cmd: (c: Chain) => c.toggleBold() },
  { name: 'italic', label: 'Italic', icon: 'icon-[mdi--format-italic]', cmd: (c: Chain) => c.toggleItalic() },
  { name: 'underline', label: 'Underline', icon: 'icon-[mdi--format-underline]', cmd: (c: Chain) => c.toggleUnderline() },
  { name: 'strike', label: 'Strikethrough', icon: 'icon-[mdi--format-strikethrough]', cmd: (c: Chain) => c.toggleStrike() },
]
const blocks = [
  { name: 'heading', attrs: { level: 1 }, label: 'Heading 1', icon: 'icon-[mdi--format-header-1]', cmd: (c: Chain) => c.toggleHeading({ level: 1 }) },
  { name: 'heading', attrs: { level: 2 }, label: 'Heading 2', icon: 'icon-[mdi--format-header-2]', cmd: (c: Chain) => c.toggleHeading({ level: 2 }) },
  { name: 'heading', attrs: { level: 3 }, label: 'Heading 3', icon: 'icon-[mdi--format-header-3]', cmd: (c: Chain) => c.toggleHeading({ level: 3 }) },
  { name: 'bulletList', label: 'Bulleted list', icon: 'icon-[mdi--format-list-bulleted]', cmd: (c: Chain) => c.toggleBulletList() },
  { name: 'orderedList', label: 'Numbered list', icon: 'icon-[mdi--format-list-numbered]', cmd: (c: Chain) => c.toggleOrderedList() },
  { name: 'blockquote', label: 'Quote', icon: 'icon-[mdi--format-quote-close]', cmd: (c: Chain) => c.toggleBlockquote() },
]

// Links: an inline form (no window.prompt). The href may be a template action.
const linkOpen = ref(false)
const linkHref = ref('')
const linkError = ref('')
function openLink(): void {
  linkHref.value = String(editor.value?.getAttributes('link').href ?? '')
  linkError.value = ''
  linkOpen.value = true
}
function applyLink(): void {
  const e = editor.value
  if (!e) return
  const href = linkHref.value.trim()
  if (!href) {
    e.chain().focus().extendMarkRange('link').unsetLink().run()
    linkOpen.value = false
    return
  }
  const { empty } = e.state.selection
  const ok = empty && !e.isActive('link') ? e.chain().focus().insertContent({ type: 'text', text: href, marks: [{ type: 'link', attrs: { href } }] }).run() : e.chain().focus().extendMarkRange('link').setLink({ href }).run()
  if (!ok) {
    linkError.value = 'Use an http(s), mailto or tel address, or a template action such as {{.link}}.'
    return
  }
  linkOpen.value = false
}
</script>

<template>
  <UiField :id="id" :label="label" :error="error" :required="required">
    <div class="tpl-editor rounded-box border bg-base-100" :class="error ? 'border-error' : 'border-base-content/20'">
      <div class="flex flex-wrap items-center gap-1 border-b border-base-content/20 p-1" role="toolbar" :aria-label="label + ' formatting'">
        <template v-if="mode === 'visual'">
          <button v-for="m in marks" :key="m.name" type="button" class="btn btn-sm btn-square btn-text" :class="{ 'btn-active': active(m.name) }" :aria-pressed="active(m.name)" :aria-label="m.label" :title="m.label" :data-test="'body-' + m.name" @click="run(m.cmd)"><span :class="m.icon" class="size-4" aria-hidden="true" /></button>
          <span class="mx-1 h-5 w-px bg-base-content/20" aria-hidden="true" />
          <button v-for="b in blocks" :key="b.label" type="button" class="btn btn-sm btn-square btn-text" :class="{ 'btn-active': active(b.name, b.attrs) }" :aria-pressed="active(b.name, b.attrs)" :aria-label="b.label" :title="b.label" @click="run(b.cmd)"><span :class="b.icon" class="size-4" aria-hidden="true" /></button>
          <span class="mx-1 h-5 w-px bg-base-content/20" aria-hidden="true" />
          <button type="button" class="btn btn-sm btn-square btn-text" :class="{ 'btn-active': active('link') }" :aria-pressed="active('link')" aria-label="Link" title="Link" data-test="body-link" @click="openLink"><span class="icon-[mdi--link-variant] size-4" aria-hidden="true" /></button>
          <button type="button" class="btn btn-sm btn-square btn-text" :disabled="!can.undo" aria-label="Undo" title="Undo" data-test="body-undo" @click="run((c) => c.undo())"><span class="icon-[mdi--undo] size-4" aria-hidden="true" /></button>
          <button type="button" class="btn btn-sm btn-square btn-text" :disabled="!can.redo" aria-label="Redo" title="Redo" data-test="body-redo" @click="run((c) => c.redo())"><span class="icon-[mdi--redo] size-4" aria-hidden="true" /></button>
        </template>
        <span class="grow" />
        <UiDropdownMenu v-if="variableItems.length" :items="variableItems" label="Insert variable" icon="mdi-plus" size="sm" align="end" data-test="body-insert-variable" @select="insertVariable" />
        <button type="button" class="btn btn-sm btn-text" :aria-pressed="mode === 'source'" data-test="body-mode" @click="toggleMode"><span class="icon-[mdi--xml] size-4" aria-hidden="true" />{{ mode === 'source' ? 'Visual editor' : 'HTML source' }}</button>
      </div>
      <div v-if="linkOpen && mode === 'visual'" class="flex flex-wrap items-end gap-2 border-b border-base-content/20 p-2" data-test="body-link-form">
        <UiInput :id="id + '-link'" v-model="linkHref" label="Link address" size="sm" :error="linkError || undefined" class="grow" placeholder="https://… or {{.link}}" @keydown.enter.prevent="applyLink" />
        <UiButton size="sm" data-test="body-link-apply" @click="applyLink">Apply</UiButton>
        <UiButton size="sm" variant="text" @click="linkOpen = false">Cancel</UiButton>
      </div>
      <div v-show="mode === 'visual'" ref="host" />
      <textarea v-if="mode === 'source'" :id="id" ref="source" :data-field="dataField" class="textarea w-full rounded-t-none border-0 font-mono text-sm" :rows="rows" :value="body" :aria-invalid="!!error || undefined" spellcheck="false" data-test="template-body-source" @input="onSourceInput" @blur="emit('blur')" />
    </div>
    <UiAlert v-if="notice && mode === 'source'" kind="warning" class="mt-2" data-test="template-body-notice">{{ notice }}</UiAlert>
  </UiField>
</template>
