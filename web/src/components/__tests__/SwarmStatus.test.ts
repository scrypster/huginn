import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { ref } from 'vue'
import SwarmStatus from '../SwarmStatus.vue'
import type { SwarmState, SwarmAgent } from '../../composables/useSwarmStatus'

// Mock useSwarmStatus so we can control swarmState in tests
const mockSwarmState = ref<SwarmState | null>(null)

vi.mock('../../composables/useSwarmStatus', () => ({
  useSwarmStatus: () => ({
    swarmState: mockSwarmState,
    isSwarmActive: ref(false),
    clearSwarm: vi.fn(),
  }),
}))

function makeAgent(overrides: Partial<SwarmAgent> = {}): SwarmAgent {
  return {
    id: 'agent-1',
    name: 'Tom',
    status: 'running',
    output: '',
    ...overrides,
  }
}

function makeSwarm(overrides: Partial<SwarmState> = {}): SwarmState {
  return {
    sessionId: 'sess-1',
    agents: [makeAgent()],
    complete: false,
    cancelled: false,
    droppedEvents: 0,
    ...overrides,
  }
}

describe('SwarmStatus', () => {
  beforeEach(() => {
    mockSwarmState.value = null
  })

  it('renders nothing when swarmState is null', () => {
    mockSwarmState.value = null
    const wrapper = mount(SwarmStatus)
    expect(wrapper.find('.swarm-status').exists()).toBe(false)
  })

  // ── In-progress rendering ──────────────────────────────────────────

  describe('in-progress swarm', () => {
    it('renders "Swarm running" for in-progress swarms', () => {
      mockSwarmState.value = makeSwarm()
      const wrapper = mount(SwarmStatus)
      expect(wrapper.text()).toContain('Swarm running')
    })

    it('renders all agent names', () => {
      mockSwarmState.value = makeSwarm({
        agents: [
          makeAgent({ id: '1', name: 'Tom', status: 'running' }),
          makeAgent({ id: '2', name: 'Sarah', status: 'done' }),
          makeAgent({ id: '3', name: 'DevOps', status: 'waiting' }),
        ],
      })
      const wrapper = mount(SwarmStatus)
      expect(wrapper.text()).toContain('Tom')
      expect(wrapper.text()).toContain('Sarah')
      expect(wrapper.text()).toContain('DevOps')
    })

    it('shows running indicator ◉ for running agents', () => {
      mockSwarmState.value = makeSwarm({
        agents: [makeAgent({ id: '1', name: 'Tom', status: 'running' })],
      })
      const wrapper = mount(SwarmStatus)
      expect(wrapper.html()).toContain('◉')
    })

    it('shows checkmark ✓ for done agents with success', () => {
      mockSwarmState.value = makeSwarm({
        agents: [makeAgent({ id: '1', name: 'Tom', status: 'done', success: true })],
      })
      const wrapper = mount(SwarmStatus)
      expect(wrapper.html()).toContain('✓')
    })

    it('shows error icon ✗ for failed agents', () => {
      mockSwarmState.value = makeSwarm({
        agents: [makeAgent({ id: '1', name: 'Tom', status: 'error' })],
      })
      const wrapper = mount(SwarmStatus)
      expect(wrapper.html()).toContain('✗')
    })

    it('shows agent output text', () => {
      mockSwarmState.value = makeSwarm({
        agents: [makeAgent({ id: '1', name: 'Tom', status: 'running', output: 'Building code...' })],
      })
      const wrapper = mount(SwarmStatus)
      expect(wrapper.text()).toContain('Building code...')
    })

    it('shows agent error text', () => {
      mockSwarmState.value = makeSwarm({
        agents: [makeAgent({ id: '1', name: 'Tom', status: 'error', error: 'timeout exceeded' })],
      })
      const wrapper = mount(SwarmStatus)
      expect(wrapper.text()).toContain('timeout exceeded')
    })

    it('shows agent count in header', () => {
      mockSwarmState.value = makeSwarm({
        agents: [
          makeAgent({ id: '1', name: 'Tom' }),
          makeAgent({ id: '2', name: 'Sarah', status: 'done' }),
          makeAgent({ id: '3', name: 'DevOps', status: 'waiting' }),
        ],
      })
      const wrapper = mount(SwarmStatus)
      expect(wrapper.text()).toContain('3 agents')
    })

    it('renders with empty agents array', () => {
      mockSwarmState.value = makeSwarm({ agents: [] })
      const wrapper = mount(SwarmStatus)
      expect(wrapper.find('.swarm-status').exists()).toBe(true)
    })
  })

  // ── Completed rendering ────────────────────────────────────────────

  describe('completed swarm', () => {
    it('renders "Swarm complete" for completed swarms', () => {
      mockSwarmState.value = makeSwarm({ complete: true })
      const wrapper = mount(SwarmStatus)
      expect(wrapper.text()).toContain('Swarm complete')
    })

    it('does not render "Swarm running" when complete', () => {
      mockSwarmState.value = makeSwarm({ complete: true })
      const wrapper = mount(SwarmStatus)
      expect(wrapper.text()).not.toContain('Swarm running')
    })

    it('shows (cancelled) when swarm is cancelled', () => {
      mockSwarmState.value = makeSwarm({ complete: true, cancelled: true })
      const wrapper = mount(SwarmStatus)
      expect(wrapper.text()).toContain('cancelled')
    })
  })

  // ── Dropped events warning ─────────────────────────────────────────

  describe('dropped events warning', () => {
    it('shows warning when droppedEvents > 0', () => {
      mockSwarmState.value = makeSwarm({ droppedEvents: 5 })
      const wrapper = mount(SwarmStatus)
      expect(wrapper.text()).toContain('5 event(s) dropped')
    })

    it('does not show warning when droppedEvents = 0', () => {
      mockSwarmState.value = makeSwarm({ droppedEvents: 0 })
      const wrapper = mount(SwarmStatus)
      expect(wrapper.html()).not.toContain('dropped')
    })
  })

  // ── Agent list class ───────────────────────────────────────────────

  describe('agent list rendering', () => {
    it('renders each agent with .swarm-agent class', () => {
      mockSwarmState.value = makeSwarm({
        agents: [
          makeAgent({ id: '1', name: 'Agent1' }),
          makeAgent({ id: '2', name: 'Agent2', status: 'done' }),
        ],
      })
      const wrapper = mount(SwarmStatus)
      expect(wrapper.findAll('.swarm-agent')).toHaveLength(2)
    })
  })

  // ── Reactive updates ───────────────────────────────────────────────

  describe('reactive updates', () => {
    it('re-renders when swarmState changes from running to complete', async () => {
      mockSwarmState.value = makeSwarm()
      const wrapper = mount(SwarmStatus)
      expect(wrapper.text()).toContain('Swarm running')

      mockSwarmState.value = makeSwarm({ complete: true })
      await wrapper.vm.$nextTick()
      expect(wrapper.text()).toContain('Swarm complete')
    })

    it('hides when swarmState is set to null', async () => {
      mockSwarmState.value = makeSwarm()
      const wrapper = mount(SwarmStatus)
      expect(wrapper.find('.swarm-status').exists()).toBe(true)

      mockSwarmState.value = null
      await wrapper.vm.$nextTick()
      expect(wrapper.find('.swarm-status').exists()).toBe(false)
    })
  })
})
