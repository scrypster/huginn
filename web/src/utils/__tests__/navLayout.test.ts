import { describe, it, expect } from 'vitest'
import {
  sectionFromPath,
  showChatSidebar,
  showContextPanel,
  TOP_LEVEL_SECTIONS,
  NAV_GROUPS,
  NAV_ITEMS,
} from '../navLayout'

describe('sectionFromPath', () => {
  it('maps chat and space onto the chat section', () => {
    expect(sectionFromPath('/')).toBe('chat')
    expect(sectionFromPath('/chat')).toBe('chat')
    expect(sectionFromPath('/chat/sess-1')).toBe('chat')
    expect(sectionFromPath('/space/sp-1')).toBe('chat')
    expect(sectionFromPath('/space/sp-1/extra')).toBe('chat')
  })

  it('folds workflow aliases onto automation', () => {
    expect(sectionFromPath('/workflows')).toBe('automation')
    expect(sectionFromPath('/workflows/wf-1')).toBe('automation')
    expect(sectionFromPath('/workflows/wf-1/runs/run-9')).toBe('automation')
    expect(sectionFromPath('/routines')).toBe('automation')
  })

  it('keeps other first segments as their own section', () => {
    expect(sectionFromPath('/stats')).toBe('stats')
    expect(sectionFromPath('/logs')).toBe('logs')
    expect(sectionFromPath('/settings')).toBe('settings')
    expect(sectionFromPath('/agents')).toBe('agents')
    expect(sectionFromPath('/agents/atlas')).toBe('agents')
    expect(sectionFromPath('/connections')).toBe('connections')
    expect(sectionFromPath('/skills/browse')).toBe('skills')
    expect(sectionFromPath('/inbox')).toBe('inbox')
    expect(sectionFromPath('/models')).toBe('models')
    expect(sectionFromPath('/memory')).toBe('memory')
    expect(sectionFromPath('/cloud')).toBe('cloud')
  })
})

describe('showChatSidebar / showContextPanel', () => {
  const chatRoutes = ['/', '/chat', '/chat/sess-1', '/space/abc', '/space/abc/x']
  const topLevelRoutes = [
    '/stats',
    '/logs',
    '/settings',
    '/agents',
    '/agents/atlas',
    '/connections',
    '/skills',
    '/skills/browse',
    '/workflows',
    '/workflows/wf-1',
    '/routines',
    '/inbox',
    '/models',
    '/memory',
    '/cloud',
  ]

  it('shows the company/channel/DM rail on chat and space routes only', () => {
    for (const path of chatRoutes) {
      expect(showChatSidebar(path), path).toBe(true)
      expect(showContextPanel(path), path).toBe(true)
    }
  })

  it('hides the rail on every top-level app surface', () => {
    for (const path of topLevelRoutes) {
      expect(showChatSidebar(path), path).toBe(false)
      expect(showContextPanel(path), path).toBe(false)
    }
  })

  it('covers the documented top-level section names', () => {
    expect(TOP_LEVEL_SECTIONS).toEqual(expect.arrayContaining([
      'stats', 'logs', 'settings', 'agents', 'connections', 'skills',
      'automation', 'inbox', 'models', 'memory', 'cloud',
    ]))
    for (const section of TOP_LEVEL_SECTIONS) {
      expect(showChatSidebar('/' + section)).toBe(false)
    }
  })
})

describe('NAV_GROUPS Slack-class order', () => {
  it('is chat, then admin, then logs — no mixed sections', () => {
    expect(NAV_GROUPS.map((g) => g.id)).toEqual(['chat', 'admin', 'logs'])
  })

  it('keeps chat in its own group', () => {
    expect(NAV_GROUPS[0].items.map((i) => i.section)).toEqual(['chat'])
  })

  it('puts settings with admin, not between stats and logs', () => {
    const admin = NAV_GROUPS.find((g) => g.id === 'admin')!.items.map((i) => i.section)
    const logs = NAV_GROUPS.find((g) => g.id === 'logs')!.items.map((i) => i.section)
    expect(admin).toContain('settings')
    expect(admin).not.toEqual(expect.arrayContaining(['stats', 'logs', 'inbox']))
    expect(logs).toEqual(['stats', 'logs', 'inbox'])
    expect(logs).not.toContain('settings')
  })

  it('flat NAV_ITEMS follows group order', () => {
    expect(NAV_ITEMS.map((i) => i.section)).toEqual([
      'chat',
      'agents', 'memory', 'models', 'automation', 'connections', 'skills', 'settings',
      'stats', 'logs', 'inbox',
    ])
  })
})

