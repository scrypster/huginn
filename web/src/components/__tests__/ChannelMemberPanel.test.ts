import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import ChannelMemberPanel from '../ChannelMemberPanel.vue'

interface SpaceMemberCard {
  name: string
  description: string
  vaultName: string
  isLead: boolean
  color: string
}

function makeMembers(): SpaceMemberCard[] {
  return [
    { name: 'Alice', description: 'Lead agent', vaultName: 'alice-vault', isLead: true, color: '#58a6ff' },
    { name: 'Bob',   description: 'Helper',      vaultName: '',            isLead: false, color: '#3fb950' },
  ]
}

describe('ChannelMemberPanel', () => {
  it('renders Lead badge on the lead agent', () => {
    const wrapper = mount(ChannelMemberPanel, {
      props: { members: makeMembers(), open: true },
    })
    const text = wrapper.text()
    expect(text).toContain('Lead')
    // Only one Lead badge
    const leads = wrapper.findAll('[data-testid="lead-badge"]')
    expect(leads).toHaveLength(1)
  })

  it('does not show Lead badge on non-lead members', () => {
    const wrapper = mount(ChannelMemberPanel, {
      props: { members: makeMembers(), open: true },
    })
    // Total of 1 lead badge (Alice), not 2
    const leads = wrapper.findAll('[data-testid="lead-badge"]')
    expect(leads).toHaveLength(1)
    // Verify the one lead badge belongs to Alice
    expect(leads[0].element.closest('div')?.textContent).toContain('Alice')
  })

  it('shows vault name when vaultName is set', () => {
    const wrapper = mount(ChannelMemberPanel, {
      props: { members: makeMembers(), open: true },
    })
    expect(wrapper.text()).toContain('alice-vault')
  })

  it('never renders the raw "No description" placeholder', () => {
    const wrapper = mount(ChannelMemberPanel, {
      props: {
        members: [
          { name: 'Steve', description: '', vaultName: '', isLead: true, color: '#58a6ff' },
        ],
        open: true,
      },
    })
    expect(wrapper.text()).not.toContain('No description')
    expect(wrapper.text()).toContain('Ready to chat')
  })

  it('emits toggle when chevron button is clicked', async () => {
    const wrapper = mount(ChannelMemberPanel, {
      props: { members: makeMembers(), open: true },
    })
    await wrapper.find('[data-testid="panel-toggle"]').trigger('click')
    expect(wrapper.emitted('toggle')).toHaveLength(1)
  })
})
