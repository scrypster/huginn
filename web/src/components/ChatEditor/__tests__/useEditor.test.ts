import { describe, it, expect, vi, afterEach } from 'vitest'
import { defineComponent, nextTick, onMounted, ref } from 'vue'
import { mount, flushPromises } from '@vue/test-utils'
import { useEditor } from '../useEditor'

function pickerEl() {
  return document.body.querySelector('.tippy-box')
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

    const editor = wrapper.vm.api.editor.value
    expect(editor).toBeTruthy()

    editor!.commands.focus()
    editor!.commands.insertContent('@')
    await flushPromises()
    await nextTick()

    await vi.waitFor(() => {
      expect(pickerEl()).toBeTruthy()
      expect(pickerEl()!.textContent).toContain('Ada')
    })

    press(editor!.view, 'Escape')
    await flushPromises()
    await nextTick()

    await vi.waitFor(() => {
      expect(pickerEl()).toBeNull()
    })
    expect(onSend).not.toHaveBeenCalled()

    wrapper.unmount()
  })
})
