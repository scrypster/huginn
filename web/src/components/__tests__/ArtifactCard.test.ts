import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import ArtifactCard from '../ArtifactCard.vue'

// Mock marked to avoid ESM issues in tests
vi.mock('marked', () => ({
  marked: {
    parse: (content: string) => `<p>${content}</p>`,
  },
}))

const baseArtifact = {
  id: 'art-1',
  kind: 'code_patch' as const,
  title: 'Fix auth bug',
  content: '+const fix = true\n-const old = false\n@@ -1,3 +1,3 @@',
  agent_name: 'Tom',
  status: 'draft' as const,
}

function makeArtifact(overrides: Partial<typeof baseArtifact> = {}) {
  return { ...baseArtifact, ...overrides }
}

describe('ArtifactCard', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  // ── code_patch ──────────────────────────────────────────────────────

  describe('code_patch rendering', () => {
    it('renders added lines in green', () => {
      const wrapper = mount(ArtifactCard, {
        props: { artifact: makeArtifact({ kind: 'code_patch', content: '+added line' }) },
      })
      const greenSpan = wrapper.find('span.text-huginn-green')
      expect(greenSpan.exists()).toBe(true)
      expect(greenSpan.text()).toContain('+added line')
    })

    it('renders removed lines in red', () => {
      const wrapper = mount(ArtifactCard, {
        props: { artifact: makeArtifact({ kind: 'code_patch', content: '-removed line' }) },
      })
      const redSpan = wrapper.find('span.text-huginn-red')
      expect(redSpan.exists()).toBe(true)
      expect(redSpan.text()).toContain('-removed line')
    })

    it('renders @@ hunk headers in blue', () => {
      const wrapper = mount(ArtifactCard, {
        props: { artifact: makeArtifact({ kind: 'code_patch', content: '@@ -1,3 +1,3 @@' }) },
      })
      const blueSpan = wrapper.find('span.text-huginn-blue')
      expect(blueSpan.exists()).toBe(true)
    })

    it('renders unchanged lines in muted', () => {
      const wrapper = mount(ArtifactCard, {
        props: { artifact: makeArtifact({ kind: 'code_patch', content: ' unchanged line' }) },
      })
      const mutedSpan = wrapper.find('span.text-huginn-muted')
      expect(mutedSpan.exists()).toBe(true)
    })

    it('uses monospace pre element for diff', () => {
      const wrapper = mount(ArtifactCard, {
        props: { artifact: makeArtifact({ kind: 'code_patch', content: '+line' }) },
      })
      expect(wrapper.find('pre').exists()).toBe(true)
    })
  })

  // ── document ─────────────────────────────────────────────────────────

  describe('document rendering', () => {
    it('renders markdown content as HTML', () => {
      const wrapper = mount(ArtifactCard, {
        props: { artifact: makeArtifact({ kind: 'document', content: '# Hello' }) },
      })
      const mdContent = wrapper.find('.md-content')
      expect(mdContent.exists()).toBe(true)
      // marked.parse is mocked to wrap in <p>
      expect(wrapper.html()).toContain('Hello')
    })

    it('shows document kind label', () => {
      const wrapper = mount(ArtifactCard, {
        props: { artifact: makeArtifact({ kind: 'document', content: 'text' }) },
      })
      expect(wrapper.html()).toContain('Document')
    })
  })

  // ── structured_data ───────────────────────────────────────────────────

  describe('structured_data rendering', () => {
    it('renders pretty JSON in a pre element', () => {
      const obj = { key: 'value', count: 42 }
      const wrapper = mount(ArtifactCard, {
        props: { artifact: makeArtifact({ kind: 'structured_data', content: JSON.stringify(obj) }) },
      })
      const pre = wrapper.find('pre')
      expect(pre.exists()).toBe(true)
      expect(pre.text()).toContain('"key"')
      expect(pre.text()).toContain('"value"')
    })

    it('handles invalid JSON gracefully', () => {
      const wrapper = mount(ArtifactCard, {
        props: { artifact: makeArtifact({ kind: 'structured_data', content: 'not json' }) },
      })
      expect(wrapper.find('pre').text()).toBe('not json')
    })
  })

  // ── timeline ─────────────────────────────────────────────────────────

  describe('timeline rendering', () => {
    it('renders a table with timestamp/event/agent columns', () => {
      const rows = [
        { timestamp: '2024-01-01', event: 'Started', agent: 'Tom' },
        { timestamp: '2024-01-02', event: 'Finished', agent: 'Sarah' },
      ]
      const wrapper = mount(ArtifactCard, {
        props: { artifact: makeArtifact({ kind: 'timeline', content: JSON.stringify(rows) }) },
      })
      expect(wrapper.find('table').exists()).toBe(true)
      const ths = wrapper.findAll('th')
      expect(ths.some(th => th.text() === 'Timestamp')).toBe(true)
      expect(ths.some(th => th.text() === 'Event')).toBe(true)
      expect(ths.some(th => th.text() === 'Agent')).toBe(true)
      expect(wrapper.html()).toContain('Started')
    })
  })

  // ── file_bundle ───────────────────────────────────────────────────────

  describe('file_bundle rendering', () => {
    it('renders a list of files with paths', () => {
      const files = [
        { path: 'src/main.ts', content: 'console.log("hi")' },
        { path: 'src/util.ts', content: 'export const x = 1' },
      ]
      const wrapper = mount(ArtifactCard, {
        props: { artifact: makeArtifact({ kind: 'file_bundle', content: JSON.stringify(files) }) },
      })
      expect(wrapper.html()).toContain('src/main.ts')
      expect(wrapper.html()).toContain('src/util.ts')
    })

    it('shows copy buttons per file', () => {
      const files = [
        { path: 'index.ts', content: 'code' },
      ]
      const wrapper = mount(ArtifactCard, {
        props: { artifact: makeArtifact({ kind: 'file_bundle', content: JSON.stringify(files) }) },
      })
      const copyBtns = wrapper.findAll('button').filter(b => b.text().includes('copy'))
      expect(copyBtns.length).toBeGreaterThan(0)
    })
  })

  // ── Accept / Reject buttons ───────────────────────────────────────────

  describe('accept/reject buttons', () => {
    it('shows accept and reject buttons when status is draft', () => {
      const wrapper = mount(ArtifactCard, {
        props: { artifact: makeArtifact({ status: 'draft' }) },
      })
      const buttons = wrapper.findAll('button')
      const acceptBtn = buttons.find(b => b.text().includes('Accept'))
      const rejectBtn = buttons.find(b => b.text().includes('Reject'))
      expect(acceptBtn?.exists()).toBe(true)
      expect(rejectBtn?.exists()).toBe(true)
    })

    it('does NOT show accept/reject buttons when status is accepted', () => {
      const wrapper = mount(ArtifactCard, {
        props: { artifact: makeArtifact({ status: 'accepted' }) },
      })
      const html = wrapper.html()
      expect(html).not.toContain('>Accept<')
      expect(html).not.toContain('>Reject<')
    })

    it('does NOT show accept/reject buttons when status is rejected', () => {
      const wrapper = mount(ArtifactCard, {
        props: { artifact: makeArtifact({ status: 'rejected' }) },
      })
      const html = wrapper.html()
      expect(html).not.toContain('>Accept<')
      expect(html).not.toContain('>Reject<')
    })

    it('does NOT show accept/reject buttons when status is superseded', () => {
      const wrapper = mount(ArtifactCard, {
        props: { artifact: makeArtifact({ status: 'superseded' }) },
      })
      const html = wrapper.html()
      expect(html).not.toContain('>Accept<')
      expect(html).not.toContain('>Reject<')
    })
  })

  // ── Emit events ───────────────────────────────────────────────────────

  describe('emit events', () => {
    it('emits accept event with artifact id when Accept is clicked', async () => {
      const wrapper = mount(ArtifactCard, {
        props: { artifact: makeArtifact({ id: 'art-42', status: 'draft' }) },
      })
      const acceptBtn = wrapper.findAll('button').find(b => b.text().includes('Accept'))
      await acceptBtn!.trigger('click')
      expect(wrapper.emitted('accept')).toBeTruthy()
      expect(wrapper.emitted('accept')![0]).toEqual(['art-42'])
    })

    it('emits reject event with artifact id when Reject is clicked', async () => {
      const wrapper = mount(ArtifactCard, {
        props: { artifact: makeArtifact({ id: 'art-99', status: 'draft' }) },
      })
      const rejectBtn = wrapper.findAll('button').find(b => b.text().includes('Reject'))
      await rejectBtn!.trigger('click')
      expect(wrapper.emitted('reject')).toBeTruthy()
      expect(wrapper.emitted('reject')![0]).toEqual(['art-99'])
    })
  })

  // ── Status badge ─────────────────────────────────────────────────────

  describe('status badge', () => {
    it('shows status text in badge', () => {
      const wrapper = mount(ArtifactCard, {
        props: { artifact: makeArtifact({ status: 'accepted' }) },
      })
      expect(wrapper.html()).toContain('accepted')
    })

    it('shows rejection reason when status is rejected', () => {
      const wrapper = mount(ArtifactCard, {
        props: {
          artifact: makeArtifact({
            status: 'rejected',
            rejection_reason: 'Too many changes',
          }),
        },
      })
      expect(wrapper.html()).toContain('Too many changes')
    })
  })

  // ── Title and metadata ────────────────────────────────────────────────

  describe('header metadata', () => {
    it('displays artifact title', () => {
      const wrapper = mount(ArtifactCard, {
        props: { artifact: makeArtifact({ title: 'My Patch Title' }) },
      })
      expect(wrapper.html()).toContain('My Patch Title')
    })

    it('displays agent name', () => {
      const wrapper = mount(ArtifactCard, {
        props: { artifact: makeArtifact({ agent_name: 'SkyNet' }) },
      })
      expect(wrapper.html()).toContain('SkyNet')
    })
  })
})
