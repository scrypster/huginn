import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import DiffCard from '../DiffCard.vue'
import type { FileDiff } from '../../composables/useApi'

function makeDiff(overrides: Partial<FileDiff> = {}): FileDiff {
  return {
    path: 'mathutil.go',
    unified: '--- mathutil.go\n+++ mathutil.go\n@@ -1,3 +1,3 @@\n func Add(a, b int) int {\n-\treturn a - b\n+\treturn a + b\n }',
    added: 1,
    removed: 1,
    truncated: false,
    is_new: false,
    is_delete: false,
    ...overrides,
  }
}

describe('DiffCard', () => {
  it('renders nothing when no diffs are present', () => {
    const wrapper = mount(DiffCard, { props: { diffs: [undefined, undefined] } })
    expect(wrapper.find('button').exists()).toBe(false)
  })

  it('renders a collapsed summary chip when a diff is present', () => {
    const wrapper = mount(DiffCard, { props: { diffs: [makeDiff()] } })
    const button = wrapper.find('button')
    expect(button.exists()).toBe(true)
    expect(button.text()).toContain('1 file changed')
    expect(button.text()).toContain('+1')
    expect(button.text()).toContain('−1')
    expect(button.text()).toContain('view diff')
    // Collapsed by default: no diff body rendered yet.
    expect(wrapper.text()).not.toContain('return a + b')
  })

  it('expands to show a colored unified diff on click', async () => {
    const wrapper = mount(DiffCard, { props: { diffs: [makeDiff()] } })
    await wrapper.find('button').trigger('click')
    expect(wrapper.text()).toContain('return a - b')
    expect(wrapper.text()).toContain('return a + b')
    const added = wrapper.find('.diff-add')
    const removed = wrapper.find('.diff-remove')
    expect(added.exists()).toBe(true)
    expect(removed.exists()).toBe(true)
    expect(added.text()).toContain('+')
    expect(removed.text()).toContain('-')
    expect(wrapper.text()).toContain('hide diff')
  })

  it('shows a per-file path header and new/deleted badges', async () => {
    const wrapper = mount(DiffCard, {
      props: { diffs: [makeDiff({ path: 'new.go', is_new: true, added: 2, removed: 0 })] },
    })
    await wrapper.find('button').trigger('click')
    expect(wrapper.text()).toContain('new.go')
    expect(wrapper.text()).toContain('new')
  })

  it('shows a truncation note when the diff was capped', async () => {
    const wrapper = mount(DiffCard, {
      props: { diffs: [makeDiff({ truncated: true, unified: makeDiff().unified + '\n… diff truncated (200+ lines)' })] },
    })
    await wrapper.find('button').trigger('click')
    expect(wrapper.text()).toContain('diff truncated')
  })

  it('aggregates totals across multiple files', () => {
    const wrapper = mount(DiffCard, {
      props: {
        diffs: [
          makeDiff({ path: 'a.go', added: 2, removed: 1 }),
          makeDiff({ path: 'b.go', added: 5, removed: 0 }),
        ],
      },
    })
    const button = wrapper.find('button')
    expect(button.text()).toContain('2 files changed')
    expect(button.text()).toContain('+7')
    expect(button.text()).toContain('−1')
  })

  it('ignores undefined entries mixed with real diffs (tool calls that did not change a file)', () => {
    const wrapper = mount(DiffCard, {
      props: { diffs: [undefined, makeDiff(), undefined] },
    })
    expect(wrapper.find('button').text()).toContain('1 file changed')
  })
})
