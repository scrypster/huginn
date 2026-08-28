/**
 * Reproduces the production "@ picker doesn't open" bug: ChatEditor.vue
 * fetches the agent catalog asynchronously in onMounted(), and the editor
 * autofocuses immediately. A user (or an automation typing at native speed)
 * who hits "@" before that fetch resolves gets a suggestion popup that opens
 * with an empty roster — and, because tiptap-suggestion only re-runs
 * items() on the *next* keystroke, it then never updates even once the
 * roster arrives, unless the user happens to keep typing.
 *
 * This mounts the real ChatEditor.vue (not a useEditor stub, unlike
 * ChatEditor.test.ts) wired the way ChatView.vue wires it in SPACE mode —
 * `:member-names` from the active space's roster — with a controllable,
 * delayed api.agents.list() so the race is deterministic instead of
 * timing-dependent.
 */
import { describe, it, expect, vi, beforeAll, afterEach } from 'vitest'
import { mount, flushPromises, type VueWrapper } from '@vue/test-utils'
import type { Editor } from '@tiptap/vue-3'
import ChatEditor from '../ChatEditor.vue'

// vue-test-utils' `vm` proxy auto-unwraps refs exposed via defineExpose, but
// guard against either shape so this test doesn't depend on that internal.
function getExposedEditor(wrapper: VueWrapper): Editor {
  const exposed = (wrapper.vm as unknown as { editor: Editor | { value: Editor } }).editor
  return (exposed && 'value' in exposed ? exposed.value : exposed) as Editor
}

function fakeRect(): DOMRect {
  return {
    x: 0, y: 0, top: 0, left: 0, right: 8, bottom: 16, width: 8, height: 16,
    toJSON() { return this },
  }
}

function fakeRects(): DOMRectList {
  const rect = fakeRect()
  const list = [rect] as unknown as DOMRectList
  Object.assign(list, { item: (i: number) => (i === 0 ? rect : null), length: 1 })
  return list
}

beforeAll(() => {
  Range.prototype.getBoundingClientRect = fakeRect
  Range.prototype.getClientRects = fakeRects
  Element.prototype.getBoundingClientRect = fakeRect
  Element.prototype.getClientRects = fakeRects
})

function pickerText(): string {
  return document.body.querySelector('.tippy-box')?.textContent ?? ''
}

// One entry per api.agents.list() call, in call order — ChatEditor.vue's
// onMounted retry loop calls it again after a failure, so a single shared
// resolve/reject pair can't distinguish "the first attempt" from "the retry".
type PendingCall = {
  resolve: (agents: Array<Record<string, unknown>>) => void
  reject: (err: unknown) => void
}
let pendingCalls: PendingCall[] = []

vi.mock('../../../composables/useApi', () => ({
  api: {
    agents: {
      list: vi.fn(() => new Promise((resolve, reject) => {
        pendingCalls.push({ resolve, reject })
      })),
    },
  },
}))

describe('ChatEditor mention picker — agents-fetch race (production reproduction)', () => {
  afterEach(() => {
    pendingCalls = []
    document.body.querySelectorAll('[data-tippy-root], .tippy-box').forEach(n => n.remove())
    vi.clearAllMocks()
  })

  it('opens with the real roster once agents load, even though @ was typed while the fetch was still pending', async () => {
    // Wired the way ChatView.vue wires the hallway/space composer: a
    // member-names roster is already known synchronously (it comes from
    // the space object), independent of the separate agents-catalog fetch.
    const wrapper = mount(ChatEditor, {
      props: { memberNames: ['Reggie', 'Steve', 'Winston'] },
      attachTo: document.body,
    })
    await flushPromises()

    // Simulate autofocus + immediate typing: "@" lands before api.agents.list()
    // has resolved (the call is still pending at this point).
    expect(pendingCalls).toHaveLength(1)
    const editor = getExposedEditor(wrapper)
    expect(editor).toBeTruthy()
    editor.view.dispatch(editor.state.tr.insertText('@'))
    await flushPromises()

    // The fetch resolves late — this is what a transient slow network /
    // 503-then-retry looks like in production.
    pendingCalls[0]!.resolve([
      { name: 'Reggie', color: '#f0883e' },
      { name: 'Steve', color: '#3fb950' },
      { name: 'Winston', color: '#d2a8ff' },
      { name: 'Nova', color: '#a5a5ff' },
    ])

    await vi.waitFor(() => {
      expect(pickerText()).toContain('Reggie')
    }, { timeout: 2000 })
    // Nova is not on this space's roster (member-names), so she must not
    // be suggested even though she is in the global agent catalog.
    expect(pickerText()).not.toContain('Nova')
    expect(pickerText()).toContain('Steve')
    expect(pickerText()).toContain('Winston')

    wrapper.unmount()
  })

  it('recovers the roster after a transient fetch failure instead of leaving the picker permanently empty', async () => {
    const wrapper = mount(ChatEditor, {
      props: { memberNames: ['Reggie', 'Steve'] },
      attachTo: document.body,
    })
    await flushPromises()

    expect(pendingCalls).toHaveLength(1)
    pendingCalls[0]!.reject(new Error('503 Service Unavailable'))

    // The retry (ChatEditor.vue's onMounted loop) issues a second
    // api.agents.list() call after a short backoff — resolve that one.
    await vi.waitFor(() => {
      expect(pendingCalls).toHaveLength(2)
    }, { timeout: 2000 })
    pendingCalls[1]!.resolve([{ name: 'Reggie', color: '#f0883e' }, { name: 'Steve', color: '#3fb950' }])

    const editor = getExposedEditor(wrapper)
    editor.view.dispatch(editor.state.tr.insertText('@'))
    if (editor.state.selection.from !== 2) editor.commands.setTextSelection(2)

    await vi.waitFor(() => {
      expect(pickerText()).toContain('Reggie')
    }, { timeout: 2000 })

    wrapper.unmount()
  })
})
