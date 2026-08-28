<template>
  <div v-if="files.length" class="mt-2 rounded-xl border border-huginn-border overflow-hidden">
    <button
      @click="expanded = !expanded"
      class="w-full flex items-center gap-2 px-3 py-1.5 hover:bg-huginn-surface/80 transition-colors duration-100"
    >
      <svg class="w-3.5 h-3.5 text-huginn-muted flex-shrink-0" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
        <path d="M14 2H6a2 2 0 00-2 2v16a2 2 0 002 2h12a2 2 0 002-2V8z" />
        <polyline points="14 2 14 8 20 8" />
      </svg>
      <span class="text-xs text-huginn-text">
        {{ files.length }} file{{ files.length === 1 ? '' : 's' }} changed
      </span>
      <span class="text-[11px] font-mono">
        <span class="text-huginn-green">+{{ totalAdded }}</span>
        <span class="text-huginn-muted"> </span>
        <span class="text-huginn-red">−{{ totalRemoved }}</span>
      </span>
      <span class="text-[11px] text-huginn-muted flex-1 text-left">· {{ expanded ? 'hide diff' : 'view diff' }}</span>
      <svg class="w-3 h-3 text-huginn-muted transition-transform duration-150 flex-shrink-0"
        :class="expanded ? 'rotate-180' : ''"
        viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
        <polyline points="6 9 12 15 18 9" />
      </svg>
    </button>

    <div v-if="expanded" class="border-t border-huginn-border divide-y divide-huginn-border">
      <div v-for="(f, idx) in files" :key="`${f.path}-${idx}`">
        <div class="flex items-center gap-2 px-3 py-1.5 bg-huginn-surface/40">
          <span class="text-xs font-mono text-huginn-text truncate flex-1" :title="f.path">{{ f.path }}</span>
          <span v-if="f.is_new" class="text-[10px] uppercase tracking-wide text-huginn-green">new</span>
          <span v-else-if="f.is_delete" class="text-[10px] uppercase tracking-wide text-huginn-red">deleted</span>
          <span class="text-[11px] font-mono text-huginn-muted">
            <span class="text-huginn-green">+{{ f.added }}</span> <span class="text-huginn-red">−{{ f.removed }}</span>
          </span>
        </div>
        <div class="diff-body text-xs font-mono overflow-x-auto px-3 py-2 leading-relaxed">
          <div v-for="(line, i) in diffLines(f.unified)" :key="i" :class="lineClass(line)">{{ line || ' ' }}</div>
        </div>
        <div v-if="f.truncated" class="px-3 pb-2 text-[11px] text-huginn-muted italic">diff truncated</div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import type { FileDiff } from '../composables/useApi'

const props = defineProps<{
  diffs: (FileDiff | undefined)[]
}>()

// Collapsed by default — a diff card should never surprise-expand under a
// message the human hasn't asked to inspect yet.
const expanded = ref(false)

const files = computed(() => (props.diffs.filter(Boolean) as FileDiff[]))
const totalAdded = computed(() => files.value.reduce((n, f) => n + (f.added || 0), 0))
const totalRemoved = computed(() => files.value.reduce((n, f) => n + (f.removed || 0), 0))

// diffLines splits a unified diff body into individual lines, dropping the
// leading "--- path" / "+++ path" header pair (the per-file path chip above
// already shows the filename, so the header text would be redundant).
function diffLines(unified: string): string[] {
  const lines = unified.split('\n')
  if (lines[0]?.startsWith('--- ') && lines[1]?.startsWith('+++ ')) {
    return lines.slice(2)
  }
  return lines
}

function lineClass(line: string): string {
  if (line.startsWith('+') && !line.startsWith('+++')) return 'diff-add'
  if (line.startsWith('-') && !line.startsWith('---')) return 'diff-remove'
  if (line.startsWith('@@')) return 'diff-hunk'
  if (line.startsWith('…')) return 'diff-truncated'
  return 'diff-context'
}
</script>

<style scoped>
.diff-body {
  background: #0d1117;
}
.diff-body > div {
  white-space: pre;
}
.diff-add {
  color: #3fb950;
  background: rgba(63, 185, 80, 0.12);
  display: block;
}
.diff-remove {
  color: #f85149;
  background: rgba(248, 81, 73, 0.12);
  display: block;
}
.diff-hunk {
  color: #58a6ff;
  opacity: 0.75;
  display: block;
}
.diff-context {
  color: #8b949e;
  display: block;
}
.diff-truncated {
  color: #8b949e;
  font-style: italic;
  display: block;
}
</style>
