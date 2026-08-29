import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import PRCard from '../PRCard.vue'
import type { ToolCallRecord } from '../../composables/useSessions'

function makeTC(overrides: Partial<ToolCallRecord> = {}): ToolCallRecord {
  return { id: 't1', name: 'gh_pr_create', args: {}, result: '', done: true, ...overrides }
}

describe('PRCard', () => {
  it('renders nothing when no PR was opened', () => {
    const wrapper = mount(PRCard, { props: { toolCalls: [makeTC({ name: 'read_file', result: 'x' })] } })
    expect(wrapper.find('a').exists()).toBe(false)
  })

  it('renders nothing when toolCalls is empty/absent', () => {
    expect(mount(PRCard, { props: {} }).find('a').exists()).toBe(false)
    expect(mount(PRCard, { props: { toolCalls: [] } }).find('a').exists()).toBe(false)
  })

  it('renders a clickable PR link with number and title', () => {
    const wrapper = mount(PRCard, {
      props: {
        toolCalls: [
          makeTC({ args: { title: 'Add PR card' }, result: 'https://github.com/scrypster/huginn/pull/123' }),
        ],
      },
    })
    const links = wrapper.findAll('a')
    expect(links.length).toBeGreaterThan(0)
    for (const link of links) {
      expect(link.attributes('href')).toBe('https://github.com/scrypster/huginn/pull/123')
      expect(link.attributes('target')).toBe('_blank')
      expect(link.attributes('rel')).toContain('noopener')
    }
    expect(wrapper.text()).toContain('#123')
    expect(wrapper.text()).toContain('Add PR card')
  })

  it('shows the branch when derivable from a preceding git_push', () => {
    const wrapper = mount(PRCard, {
      props: {
        toolCalls: [
          makeTC({ name: 'git_push', result: 'pushed feature-x to origin' }),
          makeTC({ args: { title: 'x' }, result: 'https://github.com/scrypster/huginn/pull/1' }),
        ],
      },
    })
    expect(wrapper.text()).toContain('feature-x')
  })

  it('shows a pending checks pill when gh_pr_checks reported pending', () => {
    const wrapper = mount(PRCard, {
      props: {
        toolCalls: [
          makeTC({ args: { title: 'x' }, result: 'https://github.com/scrypster/huginn/pull/1' }),
          makeTC({ name: 'gh_pr_checks', result: 'pending: checks still running', checksStatus: 'pending' }),
        ],
      },
    })
    expect(wrapper.text().toLowerCase()).toContain('pending')
  })

  it('shows a passing checks pill when gh_pr_checks reported success', () => {
    const wrapper = mount(PRCard, {
      props: {
        toolCalls: [
          makeTC({ args: { title: 'x' }, result: 'https://github.com/scrypster/huginn/pull/1' }),
          makeTC({ name: 'gh_pr_checks', result: 'lint  pass  2m', checksStatus: 'passed' }),
        ],
      },
    })
    expect(wrapper.text().toLowerCase()).toContain('passing')
  })

  it('shows a failing checks pill when gh_pr_checks reported a failure', () => {
    const wrapper = mount(PRCard, {
      props: {
        toolCalls: [
          makeTC({ args: { title: 'x' }, result: 'https://github.com/scrypster/huginn/pull/1' }),
          makeTC({ name: 'gh_pr_checks', result: 'gh pr checks: exit status 1', checksStatus: 'failed' }),
        ],
      },
    })
    expect(wrapper.text().toLowerCase()).toContain('failing')
  })

  it('shows no checks pill when no checks call ran', () => {
    const wrapper = mount(PRCard, {
      props: {
        toolCalls: [makeTC({ args: { title: 'x' }, result: 'https://github.com/scrypster/huginn/pull/1' })],
      },
    })
    expect(wrapper.text().toLowerCase()).not.toContain('checks')
  })

  it('supports a glab_mr_create result', () => {
    const wrapper = mount(PRCard, {
      props: {
        toolCalls: [
          makeTC({
            name: 'glab_mr_create',
            args: { title: 'Add MR card' },
            result: 'https://gitlab.com/scrypster/huginn/-/merge_requests/456',
          }),
        ],
      },
    })
    expect(wrapper.text()).toContain('#456')
    expect(wrapper.text()).toContain('Add MR card')
  })
})
