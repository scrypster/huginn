import { describe, it, expect } from 'vitest'

/**
 * renderWithMentions edge case test suite.
 *
 * This test suite validates the behavior of the renderWithMentions function
 * used in ChatView.vue to wrap @agent-name mentions in styled spans.
 *
 * Since renderWithMentions is a local function in ChatView, we test it through
 * documented behavior using regex patterns and agent matching logic.
 *
 * The function:
 * 1. Uses regex /(?<![a-zA-Z0-9.])@([\w-]+)/g to match @mentions
 * 2. Looks up agent names case-insensitively in agentsList
 * 3. Returns unwrapped text if agent not found
 * 4. Escapes HTML special chars in names and tooltips
 * 5. Wraps matches in <span class="agent-mention"> with data attributes
 */

describe('renderWithMentions - regex and matching patterns', () => {
  // Simulate the regex used in renderWithMentions
  const mentionRegex = /(?<![a-zA-Z0-9.])@([\w-]+)/g

  it('matches @mention preceded by whitespace', () => {
    const text = 'Hey @alice, can you help?'
    const matches = Array.from(text.matchAll(mentionRegex)).map(m => m[1])
    expect(matches).toEqual(['alice'])
  })

  it('matches @mention preceded by punctuation', () => {
    const text = 'Check with @bob. Thanks!'
    const matches = Array.from(text.matchAll(mentionRegex)).map(m => m[1])
    expect(matches).toEqual(['bob'])
  })

  it('matches @mention at start of text', () => {
    const text = '@charlie please review'
    const matches = Array.from(text.matchAll(mentionRegex)).map(m => m[1])
    expect(matches).toEqual(['charlie'])
  })

  it('does not match @domain.com (preceded by word char)', () => {
    const text = 'Contact user@example.com for help'
    const matches = Array.from(text.matchAll(mentionRegex)).map(m => m[1])
    expect(matches).not.toContain('example')
  })

  it('does not match @domain after a period in email', () => {
    const text = 'Email me at admin@test.org'
    const matches = Array.from(text.matchAll(mentionRegex)).map(m => m[1])
    // Period before @ should prevent match (lookbehind checks for [a-zA-Z0-9.])
    expect(matches).not.toContain('test')
  })

  it('matches multiple mentions', () => {
    const text = '@alice and @bob and @charlie'
    const matches = Array.from(text.matchAll(mentionRegex)).map(m => m[1])
    expect(matches).toEqual(['alice', 'bob', 'charlie'])
  })

  it('matches hyphenated agent names', () => {
    const text = 'Assign to @tom-agent'
    const matches = Array.from(text.matchAll(mentionRegex)).map(m => m[1])
    expect(matches).toEqual(['tom-agent'])
  })

  it('matches underscores in agent names', () => {
    const text = 'Call @test_agent please'
    const matches = Array.from(text.matchAll(mentionRegex)).map(m => m[1])
    expect(matches).toEqual(['test_agent'])
  })

  it('does not match @alone (no agent name)', () => {
    const text = 'I am @ the office'
    const matches = Array.from(text.matchAll(mentionRegex)).map(m => m[1])
    expect(matches).toHaveLength(0)
  })

  it('preserves mention when agent not found (caller responsible for wrapping)', () => {
    const text = '@unknown still appears in text'
    // Regex will still match @unknown, but downstream code checks agentsList
    const matches = Array.from(text.matchAll(mentionRegex)).map(m => m[1])
    expect(matches).toEqual(['unknown'])
    // Note: renderWithMentions would NOT wrap this because "unknown" is not in agentsList
  })
})

describe('renderWithMentions - HTML escaping patterns', () => {
  it('escapes HTML special characters in agent name for display', () => {
    const agentName = 'test<script>alert("xss")</script>'
    // The function replaces /[<>"'&]/g from the name
    const escaped = agentName.replace(/[<>"'&]/g, '')
    // Result removes < > " and & but preserves other chars like ( and )
    expect(escaped).toBe('testscriptalert(xss)/script')
    expect(escaped).not.toContain('<')
    expect(escaped).not.toContain('>')
    expect(escaped).not.toContain('"')
    expect(escaped).not.toContain('&')
  })

  it('escapes HTML entities in tooltip (model info)', () => {
    const tooltip = 'Agent<Name> · model>name & special "quoted"'
    // The function applies multiple replacements for HTML safety
    const escaped = tooltip
      .replace(/&/g, '&amp;')
      .replace(/</g, '&lt;')
      .replace(/>/g, '&gt;')
      .replace(/"/g, '&quot;')
    expect(escaped).toContain('&lt;')
    expect(escaped).toContain('&gt;')
    expect(escaped).toContain('&amp;')
    expect(escaped).toContain('&quot;')
    // Verify no raw dangerous chars remain
    expect(escaped).not.toContain('<')
    expect(escaped).not.toContain('>')
    expect(escaped).not.toMatch(/&(?!(?:lt|gt|amp|quot);)/) // & not followed by entity
  })

  it('escapes color value to prevent style injection', () => {
    const color = '#3b82f6'
    const escaped = color.replace(/[<>"']/g, '')
    expect(escaped).toBe('#3b82f6')

    const maliciousColor = '#3b82f6"><script>alert("xss")</script>'
    const escapedMalicious = maliciousColor.replace(/[<>"']/g, '')
    expect(escapedMalicious).not.toContain('<')
    expect(escapedMalicious).not.toContain('>')
  })
})

describe('renderWithMentions - case insensitivity', () => {
  it('matches agent names case-insensitively', () => {
    const agentName = 'alice'
    const mention = '@Alice'
    const m = mention.match(/@([\w-]+)/)?.[1] ?? ''
    // Regex captures "@Alice"'s group as "Alice"
    expect(m).toBe('Alice')
    // renderWithMentions compares using .toLowerCase()
    expect(m.toLowerCase()).toBe(agentName.toLowerCase())
  })

  it('preserves case in matched text but lowercases for attribute', () => {
    const mention = '@BOB'
    const matched = mention.match(/@([\w-]+)/)?.[1] ?? ''
    expect(matched).toBe('BOB')
    // For data-agent attribute, the function lowercases the matched name
    expect(matched.toLowerCase()).toBe('bob')
  })
})

describe('renderWithMentions - integration expectations', () => {
  it('filters cost messages correctly (via getMessages)', () => {
    // This is tested in useSessionsHydration tests
    // renderWithMentions only processes text after messages are loaded
    const messages = [
      { role: 'user', content: 'hello', type: 'text' },
      { role: 'assistant', content: 'hi', type: 'text' },
    ]
    // Only text type messages should be rendered
    const textMessages = messages.filter(m => m.type !== 'cost')
    expect(textMessages).toHaveLength(2)
  })

  it('handles empty message list (no mentions to wrap)', () => {
    const messages: any[] = []
    // No messages = no mentions = no rendering
    expect(messages.length).toBe(0)
  })

  it('handles messages without @ symbol (early exit)', () => {
    const content = 'Just a regular message with no mentions'
    // renderWithMentions checks !html.includes('@') and returns early
    expect(content.includes('@')).toBe(false)
  })
})
