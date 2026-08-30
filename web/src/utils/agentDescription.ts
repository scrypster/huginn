/** Fallback shown when an agent has neither a description nor a system prompt. */
export const DEFAULT_AGENT_DESCRIPTION = 'Ready to chat'

/**
 * One-line copy for agent list rows, cards, and member panels.
 *
 * Preference order:
 *  1. explicit description
 *  2. first sentence of the system prompt (strips a short "You are <Name>, " prefix)
 *  3. DEFAULT_AGENT_DESCRIPTION
 *
 * Never returns the literal placeholder "No description".
 */
export function agentDisplayDescription(agent: {
  description?: unknown
  system_prompt?: unknown
  name?: unknown
} | null | undefined): string {
  const explicit = typeof agent?.description === 'string' ? agent.description.trim() : ''
  if (explicit) return explicit

  let prompt = typeof agent?.system_prompt === 'string' ? agent.system_prompt.trim() : ''
  if (prompt) {
    const comma = prompt.indexOf(', ')
    if (comma > 0 && comma < 20) {
      prompt = prompt.slice(comma + 2).trim()
    }
    const end = prompt.search(/[.!?]/)
    if (end > 0) prompt = prompt.slice(0, end)
    if (prompt.length > 200) prompt = prompt.slice(0, 197) + '...'
    if (prompt) return prompt
  }

  return DEFAULT_AGENT_DESCRIPTION
}
