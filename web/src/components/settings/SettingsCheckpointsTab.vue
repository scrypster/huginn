<template>
  <div class="space-y-6">
    <section class="space-y-2">
      <h3 class="text-[11px] font-semibold text-huginn-muted uppercase tracking-widest">Run Checkpoints</h3>
      <p class="text-xs text-huginn-muted leading-relaxed">
        Every agent run is snapshotted before it starts. If a run goes wrong you can undo exactly the files it
        touched — hand-edits you made afterward are preserved unless you explicitly choose to override them.
        This never rewrites your real git history. Runs whose snapshot failed are marked
        <strong class="text-huginn-yellow font-medium">not protected</strong> — nothing is hidden.
      </p>
    </section>

    <div v-if="error" class="px-4 py-2.5 rounded-xl border border-huginn-red/40 text-huginn-red bg-huginn-red/8 text-xs" data-testid="checkpoints-load-error">
      {{ error }}
    </div>

    <div v-if="loading && !runs.length" class="py-8 text-center text-xs text-huginn-muted">Loading…</div>

    <div v-else-if="!runs.length" class="py-8 text-center">
      <p class="text-huginn-muted text-xs">No runs recorded yet.</p>
    </div>

    <div v-else class="space-y-2">
      <div
        v-for="r in sortedRuns"
        :key="r.ThreadID"
        data-testid="checkpoint-run-row"
        class="rounded-xl border border-huginn-border bg-huginn-surface/50 overflow-hidden"
      >
        <div class="px-4 py-3 flex items-start justify-between gap-3">
          <div class="flex-1 min-w-0 space-y-1.5">
            <div class="flex items-center gap-2 flex-wrap">
              <span class="text-xs font-medium text-huginn-text">{{ r.AgentID || 'agent' }}</span>
              <span class="text-[10px] text-huginn-muted font-mono truncate" :title="r.ThreadID">{{ r.ThreadID }}</span>
              <StatusChip :run="r" />
            </div>
            <p class="text-[11px] text-huginn-muted truncate" :title="r.TaskSummary">{{ r.TaskSummary || '—' }}</p>
            <p class="text-[11px] text-huginn-muted">
              {{ formatTime(r.CreatedAt) }} ·
              {{ (r.TouchedPaths || []).length }} file{{ (r.TouchedPaths || []).length === 1 ? '' : 's' }} touched
            </p>
          </div>
          <div class="flex flex-col items-end gap-1.5 flex-shrink-0">
            <button
              class="px-2 py-1 text-[10px] font-medium rounded border border-huginn-border text-huginn-muted hover:text-huginn-text hover:bg-huginn-surface transition-colors disabled:opacity-40"
              :disabled="r.Status === 'capture_failed' || diffLoading === r.ThreadID"
              data-testid="checkpoint-view-diff"
              @click="toggleDiff(r)"
            >
              {{ expandedDiff === r.ThreadID ? 'hide diff' : diffLoading === r.ThreadID ? 'loading…' : 'view diff' }}
            </button>
            <button
              class="px-2 py-1 text-[10px] font-medium rounded border border-huginn-red/30 text-huginn-red hover:bg-huginn-red/10 transition-colors disabled:opacity-40"
              :disabled="r.Status === 'capture_failed'"
              data-testid="checkpoint-revert-open"
              @click="revertTarget = r"
            >
              Revert
            </button>
          </div>
        </div>

        <div v-if="expandedDiff === r.ThreadID" class="border-t border-huginn-border" data-testid="checkpoint-diff-body">
          <div v-if="diffText[r.ThreadID] === ''" class="px-4 py-3 text-[11px] text-huginn-muted italic">No changes.</div>
          <div v-else class="diff-body text-xs font-mono overflow-x-auto px-4 py-3 leading-relaxed">
            <div v-for="(line, i) in diffLines(diffText[r.ThreadID] || '')" :key="i" :class="lineClass(line)">{{ line || ' ' }}</div>
          </div>
        </div>
      </div>
    </div>

    <CheckpointRevertDialog
      v-if="revertTarget"
      :thread-id="revertTarget.ThreadID"
      :run="revertTarget"
      @close="revertTarget = null"
    />
  </div>
</template>

<script setup lang="ts">
import { computed, defineComponent, h, onMounted, ref } from 'vue'
import { useCheckpoints, type RunRecord } from '../../composables/useCheckpoints'
import CheckpointRevertDialog from '../CheckpointRevertDialog.vue'

const { runs, loading, error, fetchRuns, fetchDiff } = useCheckpoints()

const expandedDiff = ref<string | null>(null)
const diffLoading = ref<string | null>(null)
const diffText = ref<Record<string, string>>({})
const revertTarget = ref<RunRecord | null>(null)

onMounted(() => fetchRuns())

const sortedRuns = computed(() =>
  [...runs.value].sort((a, b) => (b.CreatedAt || '').localeCompare(a.CreatedAt || '')),
)

function formatTime(iso: string): string {
  if (!iso) return '—'
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return iso
  return d.toLocaleString()
}

async function toggleDiff(r: RunRecord) {
  if (expandedDiff.value === r.ThreadID) {
    expandedDiff.value = null
    return
  }
  expandedDiff.value = r.ThreadID
  if (diffText.value[r.ThreadID] === undefined) {
    diffLoading.value = r.ThreadID
    try {
      diffText.value[r.ThreadID] = await fetchDiff(r.ThreadID)
    } catch (e) {
      diffText.value[r.ThreadID] = `(failed to load diff: ${e instanceof Error ? e.message : 'error'})`
    } finally {
      diffLoading.value = null
    }
  }
}

function diffLines(unified: string): string[] {
  return unified.split('\n')
}

function lineClass(line: string): string {
  if (line.startsWith('+') && !line.startsWith('+++')) return 'diff-add'
  if (line.startsWith('-') && !line.startsWith('---')) return 'diff-remove'
  if (line.startsWith('@@') || line.startsWith('diff --git')) return 'diff-hunk'
  return 'diff-context'
}

// StatusChip: protected / capture failed / reverted / pushed — the honest
// chip set the task calls for, kept local since it's a tiny presentational
// helper used only in this list.
const StatusChip = defineComponent({
  props: { run: { type: Object as () => RunRecord, required: true } },
  setup(p) {
    return () => {
      const chips = []
      switch (p.run.Status) {
        case 'capture_failed':
          chips.push(h('span', {
            class: 'px-1.5 py-0.5 rounded border border-huginn-yellow/40 text-[10px] text-huginn-yellow',
            'data-testid': 'checkpoint-chip-capture-failed',
            title: p.run.CaptureError || undefined,
          }, 'capture failed — not protected'))
          break
        case 'reverted':
          chips.push(h('span', {
            class: 'px-1.5 py-0.5 rounded border border-huginn-border text-[10px] text-huginn-muted',
            'data-testid': 'checkpoint-chip-reverted',
          }, 'reverted'))
          break
        default:
          chips.push(h('span', {
            class: 'px-1.5 py-0.5 rounded border border-huginn-green/30 text-[10px] text-huginn-green',
            'data-testid': 'checkpoint-chip-protected',
          }, 'protected'))
      }
      if (p.run.Pushed) {
        chips.push(h('span', {
          class: 'px-1.5 py-0.5 rounded border border-huginn-blue/30 text-[10px] text-huginn-blue',
          'data-testid': 'checkpoint-chip-pushed',
        }, 'pushed'))
      }
      return chips
    }
  },
})
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
</style>
