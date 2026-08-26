import { describe, it, expect, vi, afterEach, beforeAll } from 'vitest'
import { defineComponent, nextTick, onMounted, ref } from 'vue'
import { mount, flushPromises } from '@vue/test-utils'
import { useEditor } from '../useEditor'

const steve = { name: 'Steve', color: '#58a6ff' }
const tess = { name: 'Tess', color: '#3fb950' }
const chris = { name: 'Chris', color: '#d2a8ff' }

function fakeRect(): DOMRect {
  return {
    x: 0, y: 0, top: 0, left: 0, right: 8, bottom: 16, width: 8, height: 16,
    toJSON() { return this },
  }
}

function fakeRects(): DOMRectList {
  const rect = fakeRect()
  const list = [rect] as unknown as DOMRectList
  Object.assign(list, {
    item: (i: number) => (i === 0 ? rect : null),
    length: 1,
  })
  return list
}

beforeAll(() => {
  Range.prototype.getBoundingClientRect = fakeRect
  Range.prototype.getClientRects = fakeRects
  Element.prototype.getBoundingClientRect = fakeRect
  Element.prototype.getClientRects = fakeRects
})

function pickerEl() {
  return document.body.querySelector('.tippy-box')
}

function pickerText(): string {
  return pickerEl()?.textContent ?? ''
}

function mountEditor(memberNames: string[] | undefined) {
  const Host = defineComponent({
    setup() {
      const el = ref<HTMLElement>()
      const agents = ref([steve, tess, chris])
      const members = ref(memberNames)
      const api = useEditor({
        agents,
        onSend: vi.fn(),
        memberNames: members,
      })
      onMounted(() => {
        if (el.value) api.init(el.value)
      })
      return { el, api }
    },
    template: '<div ref="el"></div>',
  })
  return mount(Host, { attachTo: document.body })
}

async function typeAt(wrapper: ReturnType<typeof mount>) {
  await flushPromises()
  await nextTick()
  await new Promise<void>(r => requestAnimationFrame(() => r()))
  const editor = wrapper.vm.api.editor.value
  expect(editor).toBeTruthy()
  editor!.view.dispatch(editor!.state.tr.insertText('@'))
  await vi.waitFor(() => {
    if (editor!.state.selection.from !== 2) {
      editor!.commands.setTextSelection(2)
    }
    expect(pickerEl()).toBeTruthy()
  })
  return editor
}

describe('useEditor mention picker roster', () => {
  afterEach(() => {
    document.body.querySelectorAll('[data-tippy-root], .tippy-box').forEach(n => n.remove())
  })

  it('channel picker lists only members', async () => {
    const wrapper = mountEditor(['Steve', 'Chris'])
    await typeAt(wrapper)
    const names = pickerText()
    expect(names).toContain('Steve')
    expect(names).toContain('Chris')
    expect(names).not.toContain('Tess')
    wrapper.unmount()
  })

  it('DM picker lists only that agent', async () => {
    const wrapper = mountEditor(['Steve'])
    await typeAt(wrapper)
    const names = pickerText()
    expect(names).toContain('Steve')
    expect(names).not.toContain('Tess')
    expect(names).not.toContain('Chris')
    wrapper.unmount()
  })

  it('a non-member is not suggested', async () => {
    const wrapper = mountEditor(['Steve', 'Chris'])
    await typeAt(wrapper)
    expect(pickerText()).not.toContain('Tess')
    wrapper.unmount()
  })
})
