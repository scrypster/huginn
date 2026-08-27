import { describe, expect, it } from 'vitest'
import { isWorkflowDropFilename, readWorkflowDropFile, workflowDropError } from '../workflowDrop'

describe('workflow file-drop', () => {
  it('accepts yaml/yml/json basenames only', () => {
    expect(isWorkflowDropFilename('morning.yaml')).toBe(true)
    expect(isWorkflowDropFilename('morning.yml')).toBe(true)
    expect(isWorkflowDropFilename('pipe.json')).toBe(true)
    expect(isWorkflowDropFilename('notes.txt')).toBe(false)
    expect(isWorkflowDropFilename('.yaml')).toBe(false)
    expect(isWorkflowDropFilename('x.yaml.bak')).toBe(false)
  })

  it('rejects empty and huge files before read', () => {
    expect(workflowDropError({ name: 'ok.yaml', size: 0 })).toMatch(/empty/)
    expect(workflowDropError({ name: 'ok.yaml', size: (1 << 20) + 1 })).toMatch(/too large/)
    expect(workflowDropError({ name: 'ok.yaml', size: 12 })).toBeNull()
    expect(workflowDropError({ name: 'nope.txt', size: 12 })).toMatch(/yaml/i)
  })

  it('reads a json drop and strips path from the filename', async () => {
    const file = new File(
      ['{"id":"pipe","name":"Pipe","enabled":false,"schedule":"","steps":[{"name":"s","agent":"Steve","prompt":"go","position":0}]}'],
      '/tmp/pipe.json',
      { type: 'application/json' },
    )
    const got = await readWorkflowDropFile(file)
    expect(got.filename).toBe('pipe.json')
    expect(got.content).toContain('"id":"pipe"')
  })
})
