/** Route → layout mapping for the Vue shell.

Chat / space / DM stay a three-column layout:
  icon rail + company/channel/DM sidebar + main pane.

Every other top-level surface (stats, logs, settings, people, plugins,
workflows, activity, analytics, …) is two-column: icon rail + full-width main.
The company/channel/DM rail is chat-only.
*/

export type AppSection =
  | 'chat'
  | 'agents'
  | 'memory'
  | 'models'
  | 'automation'
  | 'connections'
  | 'skills'
  | 'stats'
  | 'settings'
  | 'logs'
  | 'inbox'
  | 'cloud'
  | string

/** First path segment, with aliases folded onto the icon-rail section. */
export function sectionFromPath(path: string): AppSection {
  const raw = (path.split('?')[0] || '').trim()
  const seg = (raw.replace(/^#/, '').split('/').filter(Boolean)[0] || 'chat').toLowerCase()
  if (seg === 'routines' || seg === 'workflows') return 'automation'
  if (seg === 'space') return 'chat'
  return seg
}

/** Company / channel / DM sidebar — chat, space, and the / chat landing only. */
export function showChatSidebar(path: string): boolean {
  return sectionFromPath(path) === 'chat'
}

/** Context column (Column 2). Same as the chat sidebar: chat-only. */
export function showContextPanel(path: string): boolean {
  return showChatSidebar(path)
}

/** Top-level app surfaces that must not show the company/channel/DM rail. */
export const TOP_LEVEL_SECTIONS: readonly string[] = [
  'stats',        // analytics
  'logs',
  'settings',
  'agents',       // people
  'connections',  // plugins
  'skills',       // plugins / skill library
  'automation',   // workflows
  'inbox',        // activity
  'models',
  'memory',
  'cloud',
]

export type NavGroupId = 'chat' | 'admin' | 'logs'

export interface RailNavItem {
  section: string
  label: string
  path: string
  icon: string
}

export interface RailNavGroup {
  id: NavGroupId
  items: readonly RailNavItem[]
}

/** Slack-class icon-rail groups: chat, then admin, then logs. Settings is admin. */
export const NAV_GROUPS: readonly RailNavGroup[] = [
  {
    id: 'chat',
    items: [
      { section: 'chat',        label: 'Chat',         path: '/chat',           icon: 'chat' },
    ],
  },
  {
    id: 'admin',
    items: [
      { section: 'agents',      label: 'Agents',       path: '/agents',         icon: 'agents' },
      { section: 'memory',      label: 'Memory',       path: '/memory',         icon: 'memory' },
      { section: 'models',      label: 'Models',       path: '/models',         icon: 'models' },
      { section: 'automation',  label: 'Automation',   path: '/workflows',      icon: 'automation' },
      { section: 'connections', label: 'Connections',  path: '/connections',    icon: 'connections' },
      { section: 'skills',      label: 'Skills',       path: '/skills/browse',  icon: 'skills' },
      { section: 'settings',    label: 'Settings',     path: '/settings',       icon: 'settings' },
    ],
  },
  {
    id: 'logs',
    items: [
      { section: 'stats',       label: 'Stats',        path: '/stats',          icon: 'stats' },
      { section: 'logs',        label: 'Logs',         path: '/logs',           icon: 'logs' },
      { section: 'inbox',       label: 'Activity Log', path: '/inbox',          icon: 'inbox' },
    ],
  },
]

export const NAV_ITEMS = NAV_GROUPS.flatMap((g) => [...g.items])

