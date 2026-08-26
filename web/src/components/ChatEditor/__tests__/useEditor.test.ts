import { describe, it, expect, vi, afterEach, beforeAll } from 'vitest'
import { defineComponent, nextTick, onMounted, ref } from 'vue'
import { mount, flushPromises } from '@vue/test-utils'
import { useEditor } from '../useEditor'

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
  // jsdom Range/Element layout APIs are incomplete; ProseMirror + tippy need them.
  Range.prototype.getBoundingClientRect = fakeRect
  Range.prototype.getClientRects = fakeRects
  Element.prototype.getBoundingClientRect = fakeRect
  Element.prototype.getClientRects = fakeRects
})

function pickerEl() {
  return document.body.querySelector('.tippy-box')
}

function suggestionState(editor: { state: { plugins: readonly { getState: (s: unknown) => unknown }[] } }) {
  return editor.state.plugins
    .map(p => p.getState(editor.state))
    .find((s): s is { active: boolean } => !!s && typeof s === 'object' && 'active' in (s as object))
}

function press(view: { someProp: (p: string, f: (fn: Function) => unknown) => unknown }, key: string) {
  const event = new KeyboardEvent('keydown', { key, bubbles: true, cancelable: true })
  view.someProp('handleKeyDown', (fn: Function) => fn(view, event))
  return event
}

describe('useEditor mention picker', () => {
  afterEach(() => {
    document.body.querySelectorAll('[data-tippy-root], .tippy-box').forEach(n => n.remove())
  })

  it('dismisses the @ picker on Escape without calling onSend', async () => {
    const onSend = vi.fn()
    const Host = defineComponent({
      setup() {
        const el = ref<HTMLElement>()
        const agents = ref([{ name: 'Ada', color: '#58a6ff' }])
        const api = useEditor({ agents, onSend })
        onMounted(() => {
          if (el.value) api.init(el.value)
        })
        return { el, api }
      },
      template: '<div ref="el"></div>',
    })

    const wrapper = mount(Host, { attachTo: document.body })
    await flushPromises()
    await nextTick()
    // Let TipTap autofocus's requestAnimationFrame finish before typing.
    await new Promise<void>(r => requestAnimationFrame(() => r()))

    const editor = wrapper.vm.api.editor.value
    expect(editor).toBeTruthy()

    editor!.view.dispatch(editor!.state.tr.insertText('@'))
    expect(suggestionState(editor!)?.active).toBe(true)

    await vi.waitFor(() => {
      // jsdom reports a collapsed DOM selection at the start of the
      // contenteditable, which ProseMirror syncs back over the typed @.
      if (editor!.state.selection.from !== 2) {
        editor!.commands.setTextSelection(2)
      }
      expect(pickerEl()).toBeTruthy()
      expect(pickerEl()!.textContent).toContain('Ada')
    })

    press(editor!.view, 'Escape')
    await flushPromises()
    await nextTick()

    await vi.waitFor(() => {
      expect(pickerEl()).toBeNull()
    })
    expect(suggestionState(editor!)?.active).toBe(false)
    expect(onSend).not.toHaveBeenCalled()

    // Same @ must not rematch until the cursor leaves it.
    editor!.commands.setTextSelection(2)
    editor!.view.dispatch(editor!.state.tr.insertText('d'))
    await flushPromises()
    expect(suggestionState(editor!)?.active).toBe(false)
    expect(pickerEl()).toBeNull()
    expect(onSend).not.toHaveBeenCalled()

    wrapper.unmount()
  })
})
