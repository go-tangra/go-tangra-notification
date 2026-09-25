import { afterEach, describe, expect, it } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { defineComponent, h, ref } from 'vue'
import { useConfirm } from '@go-tangra/ui'
import type { Editor } from '@tiptap/core'
import TemplateBodyEditor from '@/components/TemplateBodyEditor.vue'

let wrapper: VueWrapper | undefined
afterEach(() => wrapper?.unmount())

/** Mounts the editor bound to a ref, like v-model in the templates view. */
function mountEditor(initial: string, variables: string[] = ['link', 'tenant']) {
  const body = ref(initial)
  const Host = defineComponent({
    setup: () => () => h(TemplateBodyEditor, { id: 'body', label: 'Body', modelValue: body.value, variables, 'onUpdate:modelValue': (v: string) => (body.value = v) }),
  })
  wrapper = mount(Host, { attachTo: document.body })
  const ed = () => (wrapper!.findComponent(TemplateBodyEditor).vm as unknown as { editor: Editor }).editor
  return { body, w: wrapper, ed }
}
const visual = () => document.querySelector<HTMLElement>('[data-test="template-body-visual"]')
const source = () => document.querySelector<HTMLTextAreaElement>('[data-test="template-body-source"]')
const click = async (sel: string) => {
  document.querySelector<HTMLElement>(sel)!.click()
  await flushPromises()
}
async function pickVariable(name: string): Promise<void> {
  await click('[data-test="body-insert-variable"] button')
  const item = [...document.querySelectorAll<HTMLElement>('[role="menuitem"]')].find((el) => el.textContent?.includes('{{.' + name + '}}'))
  item!.click()
  await flushPromises()
}

const INVITE = '<p>You are invited to {{if .tenant}}<strong>{{.tenant}}</strong> on {{end}}the platform.</p><p><a href="{{.link}}">Accept</a></p>'

describe('TemplateBodyEditor', () => {
  it('opens a representable body in the visual editor with actions shown as chips, and changes nothing until edited', async () => {
    const { body } = mountEditor(INVITE)
    await flushPromises()
    expect(visual()).toBeTruthy()
    expect(source()).toBeNull()
    const chips = [...visual()!.querySelectorAll('[data-go-action]')].map((c) => c.textContent)
    expect(chips).toEqual(['{{if .tenant}}', '{{.tenant}}', '{{end}}'])
    expect(visual()!.querySelector('a')?.getAttribute('href')).toBe('{{.link}}')
    expect(body.value).toBe(INVITE)
  })

  it('opens content it cannot keep exactly in source mode with a warning, untouched', async () => {
    const odd = '<div style="white-space: pre-wrap">{{.text}}</div>'
    const { body } = mountEditor(odd)
    await flushPromises()
    expect(source()?.value).toBe(odd)
    expect(document.querySelector('[data-test="template-body-notice"]')?.textContent).toContain('cannot keep')
    expect(body.value).toBe(odd)
  })

  it('switching visual → source → visual keeps the content exactly', async () => {
    const { body } = mountEditor(INVITE)
    await flushPromises()
    await click('[data-test="body-mode"]')
    expect(source()?.value).toBe(INVITE)
    await click('[data-test="body-mode"]')
    expect(visual()).toBeTruthy()
    expect(visual()!.style.display).not.toBe('none')
    expect(body.value).toBe(INVITE)
  })

  it('source edits reach the model and show up in the visual editor', async () => {
    const { body } = mountEditor('<p>a</p>')
    await flushPromises()
    await click('[data-test="body-mode"]')
    const ta = source()!
    ta.value = '<p>Hi {{ .tenant }}, <a href="{{.link}}">go</a></p>'
    ta.dispatchEvent(new Event('input'))
    await flushPromises()
    expect(body.value).toBe('<p>Hi {{ .tenant }}, <a href="{{.link}}">go</a></p>')
    await click('[data-test="body-mode"]')
    expect([...visual()!.querySelectorAll('[data-go-action]')].map((c) => c.textContent)).toEqual(['{{ .tenant }}'])
    expect(body.value).toBe('<p>Hi {{ .tenant }}, <a href="{{.link}}">go</a></p>')
  })

  it('switching unrepresentable source to visual asks first; declining keeps source', async () => {
    const odd = '{{if .message}}<p>m</p>{{end}}'
    const { body } = mountEditor(odd)
    await flushPromises()
    await click('[data-test="body-mode"]')
    expect(useConfirm().state.pending).toBeTruthy()
    useConfirm().answer(false)
    await flushPromises()
    expect(source()?.value).toBe(odd)
    expect(body.value).toBe(odd)
  })

  it('insert variable in the visual editor adds a {{.name}} chip to the stored body', async () => {
    const { body, ed } = mountEditor('<p>Hello, !</p>')
    await flushPromises()
    ed().commands.setTextSelection(8) // after "Hello, "
    await pickVariable('tenant')
    expect(body.value).toBe('<p>Hello, {{.tenant}}!</p>')
    expect(visual()!.querySelector('[data-go-action]')?.textContent).toBe('{{.tenant}}')
  })

  it('insert variable in source mode inserts {{.name}} at the caret', async () => {
    const { body } = mountEditor('<p>Open </p>')
    await flushPromises()
    await click('[data-test="body-mode"]')
    const ta = source()!
    ta.setSelectionRange(8, 8)
    await pickVariable('link')
    expect(body.value).toBe('<p>Open {{.link}}</p>')
  })

  it('formatting in the visual editor keeps actions intact', async () => {
    const { body, ed } = mountEditor('<p>Hi {{.tenant}}</p>')
    await flushPromises()
    ed().commands.selectAll()
    await click('[data-test="body-bold"]')
    expect(body.value).toBe('<p><strong>Hi {{.tenant}}</strong></p>')
    await click('[data-test="body-undo"]')
    expect(body.value).toBe('<p>Hi {{.tenant}}</p>')
  })
})
