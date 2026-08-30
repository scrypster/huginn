export type MemoryMode = 'passive' | 'conversational' | 'immersive'

export type MemoryModeOption = {
  value: MemoryMode
  label: string
  description: string
  behaviors: string[]
}

/** Agent-settings / vault-attach taxonomy. Do not invent another set. */
export const MEMORY_MODES: MemoryModeOption[] = [
  {
    value: 'passive',
    label: 'Passive',
    description: 'Uses memory only when you explicitly ask. Minimal footprint — good for focused single-task agents.',
    behaviors: [
      'Recalls only when you say "recall" or "what do you remember"',
      'Stores only when you say "remember this"',
      'Extracts entities from what you ask it to store',
      'No automatic memory activity between requests',
    ],
  },
  {
    value: 'conversational',
    label: 'Conversational',
    description: 'Proactively recalls at session start, writes new learnings, links related memories, and signals helpful/unhelpful recalls. The balanced default.',
    behaviors: [
      'Recalls context at the start of every conversation',
      'Re-recalls when the topic shifts significantly',
      'Stores facts, decisions, preferences, and project context',
      'Uses batch writes when multiple topics are covered',
      'Extracts entities and builds knowledge graph relationships',
      'Links related memories with typed relationships (supports, depends_on, contradicts…)',
      'Records decisions with rationale and alternatives via muninn_decide',
      'Evolves stale memories instead of creating duplicates',
      'Signals helpful/unhelpful recalls to improve recall quality over time',
    ],
  },
  {
    value: 'immersive',
    label: 'Immersive',
    description: 'Full knowledge-graph stewardship. Orients at every session start, recalls before every action, maintains lifecycle, and continuously improves recall quality.',
    behaviors: [
      'Calls "where did we leave off?" at every session start',
      'Recalls before every significant decision or action',
      'Uses deep, causal, and adversarial recall modes for complex topics',
      'Stores every fact, decision, observation, and preference atomically',
      'Always extracts entities and entity relationships at write time',
      'Links memories proactively; surfaces contradictions to you',
      'Evolves changed facts with a reason — no duplicates',
      'Consolidates fragmented memories on the same topic',
      'Records decisions with rationale, alternatives, and supporting memory IDs',
      'Tracks goal and task lifecycle (active → completed → blocked…)',
      'Stores hierarchical knowledge as memory trees (plans, specs, breakdowns)',
      'Continuous feedback loop on every recalled memory improves scoring over time',
    ],
  },
]

export const DEFAULT_MEMORY_MODE: MemoryMode = 'conversational'

export function normalizeMemoryMode(value?: string): MemoryMode {
  if (value === 'passive' || value === 'immersive' || value === 'conversational') return value
  return DEFAULT_MEMORY_MODE
}
