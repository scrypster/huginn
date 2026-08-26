export const MODEL_TOOL_WARNING =
  'This model is unlikely to use tools or delegate. Grants will not do what you expect.'

export interface ModelToolCapability {
  name?: string
  supportsTools?: boolean
}

// 7b / 3b / tiny as size tokens — 14b, 13b, and 70b stay quiet.
export function isLowTierToolClass(name: string): boolean {
  const n = name.toLowerCase()
  if (!n) return false
  if (hasSizeToken(n, '7b') || hasSizeToken(n, '3b')) return true
  return n.startsWith('tiny') || hasSizeToken(n, 'tiny')
}

export function modelUnreliableForTools(model: ModelToolCapability): boolean {
  if (!model.name) return false
  if (isLowTierToolClass(model.name)) return true
  return model.supportsTools === false
}

function hasSizeToken(name: string, token: string): boolean {
  let from = 0
  while (from <= name.length - token.length) {
    const i = name.indexOf(token, from)
    if (i < 0) return false
    const left = i === 0 || !isTokenChar(name.charAt(i - 1))
    const right = i + token.length === name.length || !isTokenChar(name.charAt(i + token.length))
    if (left && right) return true
    from = i + 1
  }
  return false
}

function isTokenChar(ch: string): boolean {
  return /[a-z0-9]/i.test(ch)
}
