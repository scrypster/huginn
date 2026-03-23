<template>
  <div class="artifact-card rounded-xl border border-huginn-border overflow-hidden mt-4"
    style="background:rgba(22,27,34,0.8)">

    <!-- Header -->
    <div class="flex items-center gap-2.5 px-4 py-3 border-b border-huginn-border"
      style="background:rgba(30,37,48,0.6)">
      <!-- Kind icon -->
      <span class="text-huginn-blue flex-shrink-0 text-sm">
        <template v-if="artifact.kind === 'code_patch'">
          <svg class="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
            <polyline points="16 18 22 12 16 6" /><polyline points="8 6 2 12 8 18" />
          </svg>
        </template>
        <template v-else-if="artifact.kind === 'document'">
          <svg class="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
            <path d="M14 2H6a2 2 0 00-2 2v16a2 2 0 002 2h12a2 2 0 002-2V8z" />
            <polyline points="14 2 14 8 20 8" />
          </svg>
        </template>
        <template v-else-if="artifact.kind === 'timeline'">
          <svg class="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
            <line x1="12" y1="2" x2="12" y2="22" /><path d="M17 5H9.5a3.5 3.5 0 000 7h5a3.5 3.5 0 010 7H6" />
          </svg>
        </template>
        <template v-else>
          <svg class="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
            <rect x="3" y="3" width="18" height="18" rx="2" ry="2" /><line x1="3" y1="9" x2="21" y2="9" />
          </svg>
        </template>
      </span>

      <div class="flex-1 min-w-0">
        <p class="text-xs font-semibold text-huginn-text truncate">{{ artifact.title }}</p>
        <p class="text-[10px] text-huginn-muted">by {{ artifact.agent_name }} · {{ kindLabel }}</p>
      </div>

      <!-- Status badge -->
      <span class="text-[10px] font-semibold px-2 py-0.5 rounded-full flex-shrink-0"
        :class="statusClass">
        {{ artifact.status }}
      </span>
    </div>

    <!-- Content -->
    <div class="px-4 py-3 overflow-x-auto max-h-80">

      <!-- code_patch: diff view -->
      <template v-if="artifact.kind === 'code_patch'">
        <pre class="text-xs font-mono leading-relaxed whitespace-pre-wrap break-words"><template v-for="(line, i) in diffLines" :key="i"><span :class="lineClass(line)">{{ line }}</span>
</template></pre>
      </template>

      <!-- document: rendered markdown -->
      <template v-else-if="artifact.kind === 'document'">
        <div class="md-content text-sm text-huginn-text leading-relaxed"
          v-html="renderedMarkdown" />
      </template>

      <!-- timeline: table -->
      <template v-else-if="artifact.kind === 'timeline'">
        <table class="w-full text-xs text-huginn-text">
          <thead>
            <tr class="border-b border-huginn-border text-huginn-muted text-left">
              <th class="pb-1.5 pr-4 font-medium">Timestamp</th>
              <th class="pb-1.5 pr-4 font-medium">Event</th>
              <th class="pb-1.5 font-medium">Agent</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="(row, i) in timelineRows" :key="i"
              class="border-b border-huginn-border/40 hover:bg-huginn-surface/30 transition-colors">
              <td class="py-1.5 pr-4 text-huginn-muted whitespace-nowrap">{{ row.timestamp }}</td>
              <td class="py-1.5 pr-4 break-words">{{ row.event }}</td>
              <td class="py-1.5 text-huginn-muted whitespace-nowrap">{{ row.agent }}</td>
            </tr>
          </tbody>
        </table>
      </template>

      <!-- structured_data: pretty JSON -->
      <template v-else-if="artifact.kind === 'structured_data'">
        <pre class="text-xs font-mono text-huginn-muted leading-relaxed whitespace-pre overflow-x-auto">{{ prettyJson }}</pre>
      </template>

      <!-- file_bundle: file tree with copy buttons -->
      <template v-else-if="artifact.kind === 'file_bundle'">
        <div class="space-y-2">
          <div v-for="(file, i) in bundleFiles" :key="i"
            class="flex items-start gap-2 rounded-lg border border-huginn-border p-2.5"
            style="background:rgba(30,37,48,0.5)">
            <svg class="w-3.5 h-3.5 text-huginn-muted flex-shrink-0 mt-0.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
              <path d="M14 2H6a2 2 0 00-2 2v16a2 2 0 002 2h12a2 2 0 002-2V8z" />
              <polyline points="14 2 14 8 20 8" />
            </svg>
            <div class="flex-1 min-w-0">
              <p class="text-xs font-medium text-huginn-blue truncate">{{ file.path }}</p>
              <pre v-if="file.content" class="text-[10px] text-huginn-muted mt-1 max-h-24 overflow-y-auto whitespace-pre-wrap break-words leading-relaxed">{{ file.content }}</pre>
            </div>
            <button @click="copyFile(file.content)"
              class="flex-shrink-0 text-[10px] text-huginn-muted hover:text-huginn-text px-1.5 py-0.5 rounded border border-huginn-border hover:border-huginn-blue/30 transition-all">
              {{ copiedIndex === i ? 'copied' : 'copy' }}
            </button>
          </div>
        </div>
      </template>

    </div>

    <!-- Superseded banner -->
    <div v-if="artifact.status === 'superseded'"
      class="flex items-center gap-2 px-4 py-2 border-t border-huginn-border text-xs text-huginn-muted/70"
      style="background:rgba(139,148,158,0.05)">
      <svg class="w-3 h-3 flex-shrink-0 text-huginn-muted/50" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
        <polyline points="17 1 21 5 17 9"/><path d="M3 11V9a4 4 0 014-4h14"/>
        <polyline points="7 23 3 19 7 15"/><path d="M21 13v2a4 4 0 01-4 4H3"/>
      </svg>
      Replaced by a newer version
    </div>

    <!-- Rejection reason -->
    <div v-if="artifact.status === 'rejected' && artifact.rejection_reason"
      class="px-4 py-2 border-t border-huginn-border text-xs text-huginn-red"
      style="background:rgba(255,123,114,0.05)">
      Rejected: {{ artifact.rejection_reason }}
    </div>

    <!-- Accept / Reject actions (draft only) -->
    <div v-if="artifact.status === 'draft'"
      class="flex items-center gap-2 px-4 py-2.5 border-t border-huginn-border"
      style="background:rgba(22,27,34,0.4)">
      <button @click="$emit('accept', artifact.id)"
        class="flex items-center gap-1.5 px-3 py-1.5 rounded-lg text-xs font-medium transition-all duration-150
               text-huginn-green border border-huginn-green/30 hover:bg-huginn-green/15 active:scale-95">
        <svg class="w-3 h-3" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round">
          <polyline points="20 6 9 17 4 12" />
        </svg>
        Accept
      </button>
      <button @click="$emit('reject', artifact.id)"
        class="flex items-center gap-1.5 px-3 py-1.5 rounded-lg text-xs font-medium transition-all duration-150
               text-huginn-red border border-huginn-red/30 hover:bg-huginn-red/15 active:scale-95">
        <svg class="w-3 h-3" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round">
          <line x1="18" y1="6" x2="6" y2="18" /><line x1="6" y1="6" x2="18" y2="18" />
        </svg>
        Reject
      </button>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { marked } from 'marked'

interface Artifact {
  id: string
  kind: 'code_patch' | 'document' | 'timeline' | 'structured_data' | 'file_bundle'
  title: string
  content: string
  metadata_json?: string
  agent_name: string
  status: 'draft' | 'accepted' | 'rejected' | 'superseded' | 'failed'
  rejection_reason?: string
}

const props = defineProps<{ artifact: Artifact }>()
const emit = defineEmits<{
  (e: 'accept', id: string): void
  (e: 'reject', id: string): void
}>()

const copiedIndex = ref<number | null>(null)

const kindLabel = computed(() => {
  const labels: Record<string, string> = {
    code_patch: 'Code Patch',
    document: 'Document',
    timeline: 'Timeline',
    structured_data: 'Structured Data',
    file_bundle: 'File Bundle',
  }
  return labels[props.artifact.kind] ?? props.artifact.kind
})

const statusClass = computed(() => {
  const map: Record<string, string> = {
    draft: 'bg-huginn-blue/20 text-huginn-blue',
    accepted: 'bg-huginn-green/20 text-huginn-green',
    rejected: 'bg-huginn-red/20 text-huginn-red',
    superseded: 'bg-huginn-muted/20 text-huginn-muted',
    failed: 'bg-huginn-red/20 text-huginn-red',
  }
  return map[props.artifact.status] ?? 'bg-huginn-muted/20 text-huginn-muted'
})

// code_patch
const diffLines = computed(() => props.artifact.content.split('\n'))

function lineClass(line: string): string {
  if (line.startsWith('+') && !line.startsWith('+++')) return 'text-huginn-green block'
  if (line.startsWith('-') && !line.startsWith('---')) return 'text-huginn-red block'
  if (line.startsWith('@@')) return 'text-huginn-blue block'
  return 'text-huginn-muted block'
}

// document
const renderedMarkdown = computed(() => {
  if (!props.artifact.content) return ''
  return marked.parse(props.artifact.content) as string
})

// timeline
interface TimelineRow { timestamp: string; event: string; agent: string }
const timelineRows = computed((): TimelineRow[] => {
  try {
    const parsed = JSON.parse(props.artifact.content)
    if (Array.isArray(parsed)) return parsed as TimelineRow[]
  } catch { /* fall through */ }
  // Try line-by-line parsing
  return props.artifact.content.split('\n').filter(Boolean).map(line => {
    const parts = line.split('|').map(s => s.trim())
    return { timestamp: parts[0] ?? '', event: parts[1] ?? line, agent: parts[2] ?? '' }
  })
})

// structured_data
const prettyJson = computed(() => {
  try {
    return JSON.stringify(JSON.parse(props.artifact.content), null, 2)
  } catch {
    return props.artifact.content
  }
})

// file_bundle
interface BundleFile { path: string; content: string }
const bundleFiles = computed((): BundleFile[] => {
  try {
    const parsed = JSON.parse(props.artifact.content)
    if (Array.isArray(parsed)) return parsed as BundleFile[]
    if (parsed && typeof parsed === 'object') {
      return Object.entries(parsed).map(([path, content]) => ({
        path,
        content: typeof content === 'string' ? content : JSON.stringify(content),
      }))
    }
  } catch { /* fall through */ }
  return [{ path: 'bundle', content: props.artifact.content }]
})

async function copyFile(content: string) {
  const idx = bundleFiles.value.findIndex(f => f.content === content)
  try {
    await navigator.clipboard.writeText(content)
    copiedIndex.value = idx
    setTimeout(() => { copiedIndex.value = null }, 1500)
  } catch { /* ignore */ }
}
</script>
