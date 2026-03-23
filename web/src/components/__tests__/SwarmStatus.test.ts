import { describe, it, expect, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import SwarmStatus from '../SwarmStatus.vue'
import type { SwarmState } from '../SwarmStatus.vue'

function makeSwarm(overrides: Partial<SwarmState> = {}): SwarmState {
  return {
    id: 'swarm-1',
    name: 'auth system rebuild',
    agents: [
      { name: 'Tom', progress: 60, status: 'running', activeTools: 'bash, read_file' },
      { name: 'Sarah', progress: 100, status: 'done' },
      { name: 'DevOps', progress: 0, status: 'waiting', waitingOn: 'Tom' },
    ],
    completed: false,
    ...overrides,
  }
}

describe('SwarmStatus', () => {

  // ── In-progress rendering ─────────────────────────────────────────────

  describe('in-progress swarm', () => {
    it('renders the swarm name', () => {
      const wrapper = mount(SwarmStatus, { props: { swarm: makeSwarm() } })
      expect(wrapper.html()).toContain('auth system rebuild')
    })

    it('renders all agent names', () => {
      const wrapper = mount(SwarmStatus, { props: { swarm: makeSwarm() } })
      expect(wrapper.html()).toContain('Tom')
      expect(wrapper.html()).toContain('Sarah')
      expect(wrapper.html()).toContain('DevOps')
    })

    it('renders progress bars using block chars', () => {
      const wrapper = mount(SwarmStatus, { props: { swarm: makeSwarm() } })
      const html = wrapper.html()
      // Tom at 60% => 3 filled, 2 empty out of 5 chars
      expect(html).toContain('█')
      expect(html).toContain('░')
    })

    it('shows done checkmark for completed agents', () => {
      const wrapper = mount(SwarmStatus, { props: { swarm: makeSwarm() } })
      expect(wrapper.html()).toContain('done ✓')
    })

    it('shows waiting-on for waiting agents', () => {
      const wrapper = mount(SwarmStatus, { props: { swarm: makeSwarm() } })
      expect(wrapper.html()).toContain('waiting on Tom')
    })

    it('shows active tools for running agents', () => {
      const wrapper = mount(SwarmStatus, {
        props: {
          swarm: makeSwarm({
            agents: [
              { name: 'Tom', progress: 50, status: 'running', activeTools: 'bash, read_file' },
            ],
          }),
        },
      })
      expect(wrapper.html()).toContain('bash, read_file')
    })

    it('shows "Swarm: name" prefix for in-progress', () => {
      const wrapper = mount(SwarmStatus, { props: { swarm: makeSwarm() } })
      expect(wrapper.html()).toContain('Swarm:')
      expect(wrapper.html()).not.toContain('Swarm complete:')
    })
  })

  // ── Completed rendering ───────────────────────────────────────────────

  describe('completed swarm', () => {
    it('renders "Swarm complete:" for completed swarms', () => {
      const wrapper = mount(SwarmStatus, {
        props: {
          swarm: makeSwarm({
            completed: true,
            agentCount: 3,
            durationMs: 252000,
            artifactCount: 3,
          }),
        },
      })
      expect(wrapper.html()).toContain('Swarm complete:')
    })

    it('shows agent count in completion line', () => {
      const wrapper = mount(SwarmStatus, {
        props: {
          swarm: makeSwarm({
            completed: true,
            agentCount: 3,
            durationMs: 60000,
            artifactCount: 2,
          }),
        },
      })
      expect(wrapper.html()).toContain('3 agents')
    })

    it('shows duration in completion line', () => {
      const wrapper = mount(SwarmStatus, {
        props: {
          swarm: makeSwarm({
            completed: true,
            agentCount: 2,
            durationMs: 252000, // 4m 12s
            artifactCount: 0,
          }),
        },
      })
      expect(wrapper.html()).toContain('4m 12s')
    })

    it('shows artifact count in completion line', () => {
      const wrapper = mount(SwarmStatus, {
        props: {
          swarm: makeSwarm({
            completed: true,
            agentCount: 1,
            durationMs: 10000,
            artifactCount: 5,
          }),
        },
      })
      expect(wrapper.html()).toContain('5 artifacts')
    })

    it('does not render agent progress bars when completed', () => {
      const wrapper = mount(SwarmStatus, {
        props: {
          swarm: makeSwarm({
            completed: true,
            agentCount: 2,
            durationMs: 5000,
            artifactCount: 1,
          }),
        },
      })
      // The in-progress agents div should not be rendered
      expect(wrapper.find('.swarm-agent').exists()).toBe(false)
    })
  })

  // ── Progress bar computation ──────────────────────────────────────────

  describe('progress bar rendering', () => {
    it('full progress (100%) shows all filled chars', () => {
      const wrapper = mount(SwarmStatus, {
        props: {
          swarm: makeSwarm({
            agents: [{ name: 'Agent', progress: 100, status: 'done' }],
          }),
        },
      })
      // 5 filled blocks
      expect(wrapper.html()).toContain('█████')
    })

    it('zero progress (0%) shows all empty chars', () => {
      const wrapper = mount(SwarmStatus, {
        props: {
          swarm: makeSwarm({
            agents: [{ name: 'Agent', progress: 0, status: 'waiting' }],
          }),
        },
      })
      expect(wrapper.html()).toContain('░░░░░')
    })
  })

  // ── Updates when swarm_status event arrives ───────────────────────────

  describe('reactive updates', () => {
    it('re-renders when swarm prop updates', async () => {
      const swarm = makeSwarm()
      const wrapper = mount(SwarmStatus, { props: { swarm } })
      expect(wrapper.html()).toContain('Swarm:')

      await wrapper.setProps({
        swarm: {
          ...swarm,
          completed: true,
          agentCount: 3,
          durationMs: 30000,
          artifactCount: 1,
        },
      })

      expect(wrapper.html()).toContain('Swarm complete:')
    })
  })

  // ── Edge cases ────────────────────────────────────────────────────────

  describe('edge cases', () => {
    it('renders with empty agents array', () => {
      const wrapper = mount(SwarmStatus, {
        props: { swarm: makeSwarm({ agents: [] }) },
      })
      expect(wrapper.html()).toContain('auth system rebuild')
    })

    it('renders with undefined durationMs', () => {
      const wrapper = mount(SwarmStatus, {
        props: {
          swarm: makeSwarm({ completed: true, agentCount: 1, durationMs: undefined, artifactCount: 0 }),
        },
      })
      expect(wrapper.html()).toContain('0s')
    })
  })
})
