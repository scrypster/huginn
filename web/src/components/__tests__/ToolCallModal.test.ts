import { describe, it, expect, afterEach } from 'vitest'
import { mount } from '@vue/test-utils'
import ToolCallModal from '../ToolCallModal.vue'
import type { ToolCallRecord } from '../../composables/useSessions'

// ToolCallModal renders via <Teleport to="body">, so its content lives
// directly under document.body — outside the mounted wrapper's own element
// tree — and @vue/test-utils' wrapper.find() does not reach it. Query
// document.body directly instead.
function mountModal(tc: ToolCallRecord | null) {
  const el = document.createElement('div')
  document.body.appendChild(el)
  mount(ToolCallModal, { props: { open: true, tc }, attachTo: el })
}

afterEach(() => {
  document.body.innerHTML = ''
})

describe('ToolCallModal image rendering', () => {
  it('renders an <img> when the tool call result carries an image data URI', () => {
    const tc: ToolCallRecord = {
      id: '1',
      name: 'browser_take_screenshot',
      args: {},
      result: 'image captured: image/png, ~1024 bytes',
      done: true,
      image: 'data:image/png;base64,ZmFrZQ==',
    }
    mountModal(tc)
    const img = document.body.querySelector('img')
    expect(img).not.toBeNull()
    expect(img?.getAttribute('src')).toBe('data:image/png;base64,ZmFrZQ==')
  })

  it('does not render an image block when the tool call has no image', () => {
    const tc: ToolCallRecord = {
      id: '2',
      name: 'browser_navigate',
      args: {},
      result: 'Navigated to https://example.com',
      done: true,
    }
    mountModal(tc)
    expect(document.body.querySelector('img')).toBeNull()
  })

  it('expands the image to full size on click', async () => {
    const tc: ToolCallRecord = {
      id: '3',
      name: 'browser_take_screenshot',
      args: {},
      result: 'image captured',
      done: true,
      image: 'data:image/png;base64,ZmFrZQ==',
    }
    mountModal(tc)
    const img = document.body.querySelector('img')
    expect(img?.className).toContain('max-h-80')
    const button = document.body.querySelector<HTMLButtonElement>('button.cursor-zoom-in')
    button?.click()
    await new Promise(resolve => setTimeout(resolve, 0))
    expect(document.body.querySelector('img')?.className).not.toContain('max-h-80')
  })
})
