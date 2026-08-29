// Behavioral personality presets. Values MUST mirror
// internal/agents/personality.go's PersonalityPresets exactly — do not
// invent another set on either side.
export type Personality =
  | 'default'
  | 'strict-reviewer'
  | 'fast-builder'
  | 'skeptical-architect'
  | 'terse-operator'

export type PersonalityOption = {
  value: Personality
  label: string
  description: string
}

export const PERSONALITY_PRESETS: PersonalityOption[] = [
  {
    value: 'default',
    label: 'Default',
    description: 'No behavioral overlay — standard Huginn tone.',
  },
  {
    value: 'strict-reviewer',
    label: 'Strict Reviewer',
    description: 'Verification-first; vets its own work before calling anything done. Turns on the vet loop by default.',
  },
  {
    value: 'fast-builder',
    label: 'Fast Builder',
    description: 'Biases to action, ships in small steps.',
  },
  {
    value: 'skeptical-architect',
    label: 'Skeptical Architect',
    description: 'Challenges your framing with evidence before agreeing.',
  },
  {
    value: 'terse-operator',
    label: 'Terse Operator',
    description: 'Short, no-filler replies — facts and next steps only.',
  },
]

export const DEFAULT_PERSONALITY: Personality = 'default'

export function normalizePersonality(value?: string): Personality {
  const known = PERSONALITY_PRESETS.map(p => p.value)
  return (known as string[]).includes(value ?? '') ? (value as Personality) : DEFAULT_PERSONALITY
}

export function personalityLabel(value?: string): string {
  const p = PERSONALITY_PRESETS.find(p => p.value === value)
  return p ? p.label : 'Default'
}
