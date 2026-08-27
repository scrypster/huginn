import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { nextTick } from 'vue'
import ReplyThread from '../ReplyThread.vue'
import SpaceReplyChip from '../SpaceReplyChip.vue'
import { classifyReplySpeech } from '../replySpeech'

const mockReplies = vi.fn()
const mockPost = vi.fn()
const mockMarkRead = vi.fn()

vi.mock('../../composables/useApi', () => ({
  api: {
    spaces: {
      replies: (...args: unknown[]) => mockReplies(...args),
      postMessage: (...args: unknown[]) => mockPost(...args),
      markThreadRead: (...args: unknown[]) => mockMarkRead(...args),
    },
  },
}))

const parent = {
  id: 'root-1',
  role: 'user' as const,
  content: 'What do you think?',
  agent: '',
}

beforeEach(() => {
  vi.clearAllMocks()
  mockReplies.mockResolvedValue({ messages: [] })
  mockMarkRead.mockResolvedValue({ ok: true, unseen: 0 })
  mockPost.mockResolvedValue({
    id: 'r-1',
    session_id: 's',
    seq: 2,
    ts: new Date().toISOString(),
    role: 'user',
    content: 'ok',
    agent: '',
    parent_id: 'root-1',
  })
})

describe('SpaceReplyChip', () => {
  it('hides when count is 0', () => {
    const w = mount(SpaceReplyChip, { props: { count: 0 } })
    expect(w.find('[data-testid="space-reply-chip"]').exists()).toBe(false)
  })

  it('shows "1 reply" and "N replies"', async () => {
    const w = mount(SpaceReplyChip, { props: { count: 1 } })
    expect(w.find('[data-testid="space-reply-chip"]').text()).toBe('1 reply')
    await w.setProps({ count: 3 })
    expect(w.find('[data-testid="space-reply-chip"]').text()).toBe('3 replies')
  })

  it('emits open on click', async () => {
    const w = mount(SpaceReplyChip, { props: { count: 2 } })
    await w.find('[data-testid="space-reply-chip"]').trigger('click')
    expect(w.emitted('open')).toBeTruthy()
  })
})

describe('ReplyThread drawer', () => {
  it('renders parent + composer when open', async () => {
    const w = mount(ReplyThread, {
      props: { visible: true, spaceId: 'sp-1', parent },
    })
    await flushPromises()
    expect(w.find('[data-testid="reply-thread-drawer"]').exists()).toBe(true)
    expect(w.find('[data-testid="reply-thread-parent"]').text()).toContain('What do you think?')
    expect(w.find('[data-testid="reply-thread-composer"]').exists()).toBe(true)
    expect(mockReplies).toHaveBeenCalledWith('sp-1', 'root-1')
  })

  it('does not render when closed', () => {
    const w = mount(ReplyThread, {
      props: { visible: false, spaceId: 'sp-1', parent },
    })
    expect(w.find('[data-testid="reply-thread-drawer"]').exists()).toBe(false)
  })

  it('posts a reply from the composer', async () => {
    const w = mount(ReplyThread, {
      props: { visible: true, spaceId: 'sp-1', parent },
    })
    await flushPromises()
    await w.find('[data-testid="reply-thread-input"]').setValue('sounds good')
    await w.find('[data-testid="reply-thread-composer"]').trigger('submit')
    await flushPromises()
    expect(mockPost).toHaveBeenCalledWith('sp-1', { content: 'sounds good', parent_id: 'root-1' })
    expect(w.emitted('posted')?.[0]).toEqual([1, 'root-1'])
  })

  it('hides TOOL_FAIL as teammate speech', async () => {
    mockReplies.mockResolvedValue({
      messages: [{
        id: 'r-fail',
        session_id: 's',
        seq: 2,
        ts: '',
        role: 'assistant',
        content: 'TOOL_FAIL: wait_for_threads exploded',
        agent: 'atlas',
        parent_id: 'root-1',
      }],
    })
    const w = mount(ReplyThread, {
      props: { visible: true, spaceId: 'sp-1', parent },
    })
    await flushPromises()
    const row = w.find('[data-testid="reply-thread-msg"]')
    expect(row.find('[data-testid="reply-speech"]').exists()).toBe(false)
    expect(row.find('[data-testid="system-fail-copy"]').text()).not.toContain('TOOL_FAIL')
    expect(row.find('[data-testid="system-fail-copy"]').text()).not.toContain('wait_for_threads')
    expect(row.find('[data-testid="system-fail-line"]').exists()).toBe(true)
  })

  it('hides wait_for_threads JSON as speech', async () => {
    mockReplies.mockResolvedValue({
      messages: [{
        id: 'r-json',
        session_id: 's',
        seq: 3,
        ts: '',
        role: 'assistant',
        content: '{"name":"wait_for_threads","arguments":{}}',
        agent: 'atlas',
        parent_id: 'root-1',
      }],
    })
    const w = mount(ReplyThread, {
      props: { visible: true, spaceId: 'sp-1', parent },
    })
    await flushPromises()
    const row = w.find('[data-testid="reply-thread-msg"]')
    expect(row.find('[data-testid="reply-speech"]').exists()).toBe(false)
    expect(row.find('[data-testid="reply-hidden"]').exists()).toBe(true)
    expect(row.text()).not.toContain('wait_for_threads')
  })
})

describe('classifyReplySpeech', () => {
  it('keeps ordinary teammate speech', () => {
    expect(classifyReplySpeech('Ship it.')).toEqual({ kind: 'speech', text: 'Ship it.' })
  })

  it('classifies TOOL_FAIL as fail, not speech', () => {
    expect(classifyReplySpeech('TOOL_FAIL: nope').kind).toBe('fail')
  })

  it('hides wait_for_threads JSON', () => {
    const c = classifyReplySpeech('{"name":"wait_for_threads","arguments":{}}')
    expect(c.kind).toBe('hidden')
    expect(c.text).not.toContain('wait_for_threads')
  })
})

describe('SpaceReplyChip hallway copy', () => {
  it('shows last speech preview and never Delegated to @', async () => {
    const w = mount(SpaceReplyChip, { props: { count: 2, preview: 'Ship it.' } })
    expect(w.text()).toContain('2 replies')
    expect(w.find('[data-testid="space-reply-preview"]').text()).toContain('Ship it.')
    await w.setProps({ preview: 'Delegated to @Sam' })
    expect(w.text()).not.toContain('Delegated to @Sam')
  })

  it('spectator sees count only — no new-since', () => {
    const w = mount(SpaceReplyChip, { props: { count: 2, participant: false, newSince: 2 } })
    expect(w.text()).toContain('2 replies')
    expect(w.find('[data-testid="space-reply-new-since"]').exists()).toBe(false)
    expect(w.text()).not.toContain('new')
  })

  it('participant sees new since last here', () => {
    const w = mount(SpaceReplyChip, { props: { count: 3, participant: true, newSince: 2 } })
    expect(w.text()).toContain('3 replies')
    expect(w.find('[data-testid="space-reply-new-since"]').text()).toContain('2 new since you were last here')
  })

  it('shows teammate writing, not tool names', () => {
    const w = mount(SpaceReplyChip, { props: { count: 1, typingAgent: 'Steve' } })
    expect(w.find('[data-testid="space-reply-writing"]').text()).toBe('Steve is writing')
    expect(w.text()).not.toContain('tool')
    expect(w.text()).not.toContain('Inject')
  })
})

describe('ReplyThread live + always-on composer', () => {
  it('composer stays visible after work is done', async () => {
    const w = mount(ReplyThread, {
      props: { visible: true, spaceId: 'sp-1', parent },
    })
    await flushPromises()
    expect(w.find('[data-testid="reply-thread-composer"]').exists()).toBe(true)
  })

  it('appends a live incoming bubble', async () => {
    const w = mount(ReplyThread, {
      props: { visible: true, spaceId: 'sp-1', parent },
    })
    await flushPromises()
    await w.setProps({
      incoming: {
        id: 'live-1',
        session_id: 's',
        seq: 9,
        ts: '',
        role: 'assistant',
        content: 'on it',
        agent: 'Steve',
        parent_id: 'root-1',
      },
    })
    await flushPromises()
    expect(w.text()).toContain('on it')
    expect(w.text()).toContain('Steve')
  })

  it('shows Steve is writing / hit a snag', async () => {
    const w = mount(ReplyThread, {
      props: { visible: true, spaceId: 'sp-1', parent, typingAgent: 'Steve' },
    })
    await flushPromises()
    expect(w.find('[data-testid="reply-thread-writing"]').text()).toBe('Steve is writing')
    await w.setProps({ typingAgent: '', snagAgent: 'Steve', snagReason: 'not_in_company' })
    expect(w.find('[data-testid="reply-thread-snag"]').text()).toBe('Steve hit a snag')
    expect(w.find('[data-testid="reply-thread-snag"]').attributes('title')).toBe('not_in_company')
  })
})

describe('ReplyThread live stream', () => {
  it('keeps writing until first speech token, then grows a bubble', async () => {
    const w = mount(ReplyThread, {
      props: { visible: true, spaceId: 'sp-1', parent, typingAgent: 'Steve' },
    })
    await flushPromises()
    expect(w.find('[data-testid="reply-thread-writing"]').text()).toBe('Steve is writing')
    expect(w.find('[data-testid="reply-thread-stream"]').exists()).toBe(false)
    await w.setProps({ streamAgent: 'Steve', streamText: 'on' })
    expect(w.find('[data-testid="reply-thread-writing"]').exists()).toBe(false)
    expect(w.find('[data-testid="reply-thread-stream"]').text()).toContain('on')
    await w.setProps({ streamAgent: 'Steve', streamText: 'on it' })
    expect(w.find('[data-testid="reply-thread-stream"]').text()).toContain('on it')
  })

  it('does not show TOOL_FAIL or wait_for_threads as stream speech', async () => {
    const w = mount(ReplyThread, {
      props: {
        visible: true,
        spaceId: 'sp-1',
        parent,
        typingAgent: 'Steve',
        streamAgent: 'Steve',
        streamText: 'TOOL_FAIL: wait_for_threads exploded',
      },
    })
    await flushPromises()
    expect(w.find('[data-testid="reply-thread-stream"]').exists()).toBe(false)
    expect(w.text()).not.toContain('TOOL_FAIL')
    expect(w.text()).not.toContain('wait_for_threads')
    await w.setProps({ streamText: '{"name":"wait_for_threads","arguments":{}}' })
    expect(w.find('[data-testid="reply-thread-stream"]').exists()).toBe(false)
    expect(w.text()).not.toContain('wait_for_threads')
  })

  it('replaces the stream bubble with the persisted incoming row', async () => {
    const w = mount(ReplyThread, {
      props: { visible: true, spaceId: 'sp-1', parent, streamAgent: 'Steve', streamText: 'on it' },
    })
    await flushPromises()
    expect(w.find('[data-testid="reply-thread-stream"]').exists()).toBe(true)
    await w.setProps({
      streamText: '',
      incoming: {
        id: 'live-steve',
        session_id: 's',
        seq: 9,
        ts: '',
        role: 'assistant',
        content: 'on it',
        agent: 'Steve',
        parent_id: 'root-1',
      },
    })
    await flushPromises()
    expect(w.find('[data-testid="reply-thread-stream"]').exists()).toBe(false)
    expect(w.text()).toContain('on it')
    expect(w.text()).toContain('Steve')
  })
})

describe('ReplyThread mention picker', () => {
  it('Lab space picker does not offer Reggie', async () => {
    const w = mount(ReplyThread, {
      props: { visible: true, spaceId: 'lab-1', parent, memberNames: ['Winston', 'Sam'] },
    })
    await flushPromises()
    await w.find('[data-testid="reply-thread-input"]').setValue('@')
    const picker = w.find('[data-testid="reply-mention-picker"]')
    expect(picker.exists()).toBe(true)
    expect(picker.text()).toContain('Winston')
    expect(picker.text()).toContain('Sam')
    expect(picker.text()).not.toContain('Reggie')
    expect(picker.text()).not.toContain('Steve')
  })

  it('Huginn space picker does not offer Lab-only Sam', async () => {
    const w = mount(ReplyThread, {
      props: { visible: true, spaceId: 'hug-1', parent, memberNames: ['Winston', 'Steve'] },
    })
    await flushPromises()
    await w.find('[data-testid="reply-thread-input"]').setValue('@S')
    const picker = w.find('[data-testid="reply-mention-picker"]')
    expect(picker.text()).toContain('Steve')
    expect(picker.text()).not.toContain('Sam')
  })

  it('drops @Reggie and hints instead of sending', async () => {
    const w = mount(ReplyThread, {
      props: { visible: true, spaceId: 'lab-1', parent, memberNames: ['Winston'] },
    })
    await flushPromises()
    await w.find('[data-testid="reply-thread-input"]').setValue('@Reggie ping')
    await w.find('[data-testid="reply-thread-composer"]').trigger('submit')
    await flushPromises()
    expect(mockPost).toHaveBeenCalledWith('lab-1', { content: 'ping', parent_id: 'root-1' })
    expect(w.find('[data-testid="reply-unknown-mention-hint"]').text()).toContain('Reggie')
  })
})

describe('ReplyThread composer autofocus', () => {
  afterEach(() => {
    document.querySelectorAll('[data-testid="memory-vault-modal"], [role="dialog"]').forEach(el => el.remove())
  })

  it('focuses the thread composer when the drawer opens', async () => {
    const hallway = document.createElement('textarea')
    hallway.setAttribute('data-testid', 'hallway-composer')
    document.body.appendChild(hallway)
    hallway.focus()
    expect(document.activeElement).toBe(hallway)

    const w = mount(ReplyThread, {
      attachTo: document.body,
      props: { visible: true, spaceId: 'sp-1', parent },
    })
    await flushPromises()
    await nextTick()
    const input = w.find('[data-testid="reply-thread-input"]').element as HTMLInputElement
    expect(document.activeElement).toBe(input)
    w.unmount()
    hallway.remove()
  })

  it('re-focuses when switching parent threads', async () => {
    const w = mount(ReplyThread, {
      attachTo: document.body,
      props: { visible: true, spaceId: 'sp-1', parent },
    })
    await flushPromises()
    await nextTick()
    const input = w.find('[data-testid="reply-thread-input"]').element as HTMLInputElement
    input.blur()
    expect(document.activeElement).not.toBe(input)

    await w.setProps({ parent: { ...parent, id: 'root-2', content: 'Other thread' } })
    await flushPromises()
    await nextTick()
    expect(document.activeElement).toBe(input)
    w.unmount()
  })

  it('does not steal focus from a memory-chip modal', async () => {
    const modal = document.createElement('div')
    modal.setAttribute('data-testid', 'memory-vault-modal')
    const modalInput = document.createElement('input')
    modal.appendChild(modalInput)
    document.body.appendChild(modal)
    modalInput.focus()
    expect(document.activeElement).toBe(modalInput)

    const w = mount(ReplyThread, {
      attachTo: document.body,
      props: { visible: true, spaceId: 'sp-1', parent },
    })
    await flushPromises()
    await nextTick()
    expect(document.activeElement).toBe(modalInput)
    w.unmount()
    modal.remove()
  })

  it('does not steal focus from another overlay dialog', async () => {
    const overlay = document.createElement('div')
    overlay.setAttribute('role', 'dialog')
    overlay.setAttribute('aria-modal', 'true')
    const overlayInput = document.createElement('input')
    overlay.appendChild(overlayInput)
    document.body.appendChild(overlay)
    overlayInput.focus()

    const w = mount(ReplyThread, {
      attachTo: document.body,
      props: { visible: true, spaceId: 'sp-1', parent },
    })
    await flushPromises()
    await nextTick()
    expect(document.activeElement).toBe(overlayInput)
    w.unmount()
    overlay.remove()
  })

  it('exposes created_at on replies and reveals a relative label', async () => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date('2026-08-27T15:00:00-04:00'))
    mockReplies.mockResolvedValue({
      messages: [{
        id: 'r-age',
        session_id: 's',
        seq: 2,
        ts: '2026-08-27T14:00:00-04:00',
        created_at: '2026-08-27T14:00:00-04:00',
        role: 'assistant',
        content: 'an hour ago',
        agent: 'atlas',
        parent_id: 'root-1',
      }],
    })
    const w = mount(ReplyThread, {
      props: {
        visible: true,
        spaceId: 'sp-1',
        parent: { ...parent, created_at: '2026-08-26T12:00:00-04:00' },
      },
    })
    await flushPromises()
    const stamps = w.findAll('[data-testid="msg-rel-time"]')
    expect(stamps.length).toBeGreaterThanOrEqual(2)
    expect(stamps.some(s => s.text() === '1h')).toBe(true)
    expect(stamps.some(s => s.text() === 'yesterday')).toBe(true)
    expect(w.find('[data-testid="reply-day-sep"]').exists()).toBe(true)
    await w.findAll('[data-testid="msg-time-row"]')[1]!.trigger('mouseenter')
    expect(w.findAll('[data-testid="msg-time-row"]')[1]!.classes()).toContain('is-revealed')
    vi.useRealTimers()
  })
})
