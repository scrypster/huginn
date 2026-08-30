/** First-click: drop a YAML/JSON workflow onto the Workflows page. */

export const MAX_WORKFLOW_DROP_BYTES = 1 << 20

export function isWorkflowDropFilename(name: string): boolean {
  const base = name.split(/[/\\]/).pop() ?? ''
  if (!base || base.startsWith('.')) return false
  const ext = base.toLowerCase().slice(base.lastIndexOf('.'))
  return ext === '.yaml' || ext === '.yml' || ext === '.json'
}

export function workflowDropError(file: { name: string; size: number }): string | null {
  const base = file.name.split(/[/\\]/).pop() ?? file.name
  if (!isWorkflowDropFilename(base)) {
    return 'Drop a .yaml, .yml, or .json workflow file.'
  }
  if (file.size > MAX_WORKFLOW_DROP_BYTES) {
    return 'That file is too large (1 MB limit).'
  }
  if (file.size === 0) {
    return 'That file is empty.'
  }
  return null
}

export async function readWorkflowDropFile(file: File): Promise<{ filename: string; content: string }> {
  const filename = file.name.split(/[/\\]/).pop() ?? file.name
  const err = workflowDropError({ name: filename, size: file.size })
  if (err) throw new Error(err)
  const buf = await file.arrayBuffer()
  const bytes = new Uint8Array(buf)
  if (bytes.includes(0)) throw new Error('That file looks binary, not a workflow.')
  const content = new TextDecoder('utf-8', { fatal: false }).decode(bytes)
  if (!content.trim()) throw new Error('That file is empty.')
  return { filename, content }
}
