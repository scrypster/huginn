<template>
  <Transition name="backdrop">
    <div
      class="fixed inset-0 z-[110] flex items-end justify-center sm:items-center p-4"
      style="background:rgba(0,0,0,0.72);backdrop-filter:blur(6px)"
      data-testid="checkpoint-revert-dialog"
      @click.self="close"
    >
      <div
        class="w-[480px] max-w-[95vw] rounded-2xl overflow-hidden flex flex-col"
        style="background:#13181f;border:1px solid rgba(48,54,61,0.9);box-shadow:0 24px 80px rgba(0,0,0,0.6),0 0 0 1px rgba(255,255,255,0.04) inset"
        @click.stop
      >
        <div class="px-6 pt-6 pb-4 flex items-start justify-between" style="border-bottom:1px solid rgba(48,54,61,0.55)">
          <div class="flex items-center gap-3 min-w-0">
            <div class="w-9 h-9 rounded-xl flex items-center justify-center flex-shrink-0 text-huginn-yellow"
              style="background:rgba(210,153,34,0.12);border:1px solid rgba(210,153,34,0.25)">
              <svg class="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
                <polyline points="1 4 1 10 7 10" /><path d="M3.51 15a9 9 0 102.13-9.36L1 10" />
              </svg>
            </div>
            <div class="min-w-0">
              <h2 class="text-sm font-semibold text-white leading-tight">
                {{ result ? 'Revert result' : 'Undo this run' }}
              </h2>
              <p class="text-[11px] mt-0.5 text-huginn-muted/70 truncate" :title="run?.TaskSummary">
                {{ run?.AgentID || 'agent' }} · {{ run?.TaskSummary || run?.ThreadID }}
              </p>
            </div>
          </div>
          <button type="button" class="w-7 h-7 rounded-lg flex items-center justify-center text-huginn-muted/60 hover:text-huginn-text hover:bg-huginn-surface/60 flex-shrink-0"
            data-testid="checkpoint-revert-close" @click="close">
            <svg class="w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round">
              <line x1="18" y1="6" x2="6" y2="18" /><line x1="6" y1="6" x2="18" y2="18" />
            </svg>
          </button>
        </div>

        <div class="px-6 py-5 flex flex-col gap-4" style="max-height:70vh;overflow-y:auto">
          <!-- ── Confirmation state ─────────────────────────────────── -->
          <template v-if="!result">
            <div class="text-xs text-huginn-text/90 leading-relaxed space-y-2">
              <p>Revert rewrites the files this run touched back to how they were before it started. It only ever touches
                <strong class="text-huginn-text font-medium">your working tree</strong> — never your real git history.</p>
              <p>By default, a file this run touched but that you (or another run) then hand-edited afterward is
                <strong class="text-huginn-text font-medium">left alone</strong>, not overwritten.</p>
            </div>

            <div class="space-y-2">
              <label class="flex items-start gap-2 text-xs text-huginn-text cursor-pointer">
                <input type="radio" :checked="!restoreAll" @change="restoreAll = false" class="mt-0.5" data-testid="checkpoint-revert-mode-preserve" />
                <span>
                  <span class="font-medium">Preserve hand-edits</span> (default) — only restore files nobody has touched since
                </span>
              </label>
              <label class="flex items-start gap-2 text-xs text-huginn-text cursor-pointer">
                <input type="radio" :checked="restoreAll" @change="restoreAll = true" class="mt-0.5" data-testid="checkpoint-revert-mode-all" />
                <span>
                  <span class="font-medium text-huginn-yellow">Restore everything</span> — overwrite hand-edited files too, no exceptions
                </span>
              </label>
            </div>

            <div v-if="run?.Pushed"
              class="px-3 py-2.5 rounded-xl border border-huginn-red/30 text-xs text-huginn-text/90 space-y-2"
              style="background:rgba(248,81,73,0.06)">
              <p>
                <strong class="text-huginn-red">This run was already pushed</strong> (or has an open PR). Reverting only
                changes local files — it can never rewrite the pushed commit. You'll still need a forward commit to undo
                the remote side.
              </p>
              <label class="flex items-start gap-2 cursor-pointer">
                <input type="checkbox" v-model="allowAfterPush" class="mt-0.5" data-testid="checkpoint-revert-allow-pushed" />
                <span>I understand this run was pushed and want to revert local files anyway</span>
              </label>
            </div>

            <label class="flex items-start gap-2 text-xs text-huginn-text/90 cursor-pointer pt-1 border-t border-huginn-border">
              <input type="checkbox" v-model="confirmed" class="mt-0.5" data-testid="checkpoint-revert-confirm" />
              <span>I understand this will modify files on disk for this run</span>
            </label>

            <p v-if="revertError" class="text-xs text-huginn-red">{{ revertError }}</p>

            <div class="flex gap-2 pt-1">
              <button type="button"
                class="flex-1 px-4 py-2 rounded-lg text-xs font-medium border border-huginn-border text-huginn-muted hover:bg-huginn-surface transition-all"
                @click="close">
                Cancel
              </button>
              <button type="button"
                data-testid="checkpoint-revert-confirm-btn"
                class="flex-1 px-4 py-2 rounded-lg text-xs font-semibold text-white transition-all disabled:opacity-40 disabled:cursor-not-allowed"
                style="background:rgba(248,81,73,0.85)"
                :disabled="!canRevert || reverting"
                @click="doRevert">
                {{ reverting ? 'Reverting…' : 'Revert' }}
              </button>
            </div>
          </template>

          <!-- ── Result state — full honesty disclosure, never a truncated toast ── -->
          <template v-else>
            <div v-if="result.NothingCaptured"
              class="px-3 py-2.5 rounded-xl border border-huginn-yellow/40 text-xs text-huginn-yellow"
              style="background:rgba(210,153,34,0.08)" data-testid="checkpoint-result-nothing-captured">
              Nothing was captured for this run — there was nothing to restore. This can happen when a run touched only
              gitignored files or ran outside the snapshot scope.
            </div>

            <div v-if="result.Warning" class="px-3 py-2.5 rounded-xl border border-huginn-border text-xs text-huginn-text/90 bg-huginn-surface/40" data-testid="checkpoint-result-warning">
              {{ result.Warning }}
            </div>

            <div class="grid grid-cols-2 gap-2 text-center">
              <div class="rounded-xl border border-huginn-green/30 py-2.5" style="background:rgba(63,185,80,0.06)">
                <div class="text-lg font-bold text-huginn-green tabular-nums" data-testid="checkpoint-result-restored-count">{{ (result.Restored || []).length }}</div>
                <div class="text-[10px] uppercase tracking-wide text-huginn-muted">restored</div>
              </div>
              <div class="rounded-xl border border-huginn-red/30 py-2.5" style="background:rgba(248,81,73,0.06)">
                <div class="text-lg font-bold text-huginn-red tabular-nums" data-testid="checkpoint-result-deleted-count">{{ (result.Deleted || []).length }}</div>
                <div class="text-[10px] uppercase tracking-wide text-huginn-muted">deleted</div>
              </div>
            </div>

            <ResultFileList label="Restored" :paths="result.Restored" tone="green" />
            <ResultFileList label="Deleted" :paths="result.Deleted" tone="red" />
            <ResultFileList
              label="Preserved (hand-edited since — not restored)"
              :paths="result.SkippedEdited"
              tone="yellow"
              test-id="checkpoint-result-skipped-edited"
            />
            <ResultFileList
              label="Not restorable (outside snapshot scope)"
              :paths="result.NotRestorable"
              tone="muted"
              test-id="checkpoint-result-not-restorable"
            />

            <div v-if="failedEntries.length" class="space-y-1.5" data-testid="checkpoint-result-failed">
              <p class="text-[10px] uppercase tracking-wide text-huginn-red">Failed to restore</p>
              <div v-for="[path, msg] in failedEntries" :key="path" class="text-[11px] font-mono text-huginn-text/90 bg-huginn-red/8 border border-huginn-red/25 rounded-lg px-2.5 py-1.5">
                <div class="truncate">{{ path }}</div>
                <div class="text-huginn-red/90">{{ msg }}</div>
              </div>
            </div>

            <button type="button"
              class="mt-1 px-4 py-2 rounded-lg text-xs font-medium border border-huginn-border text-huginn-muted hover:bg-huginn-surface transition-all self-start"
              @click="close">
              Done
            </button>
          </template>
        </div>
      </div>
    </div>
  </Transition>
</template>

<script setup lang="ts">
import { ref, computed, defineComponent, h } from 'vue'
import { useCheckpoints, type RunRecord, type RevertResult } from '../composables/useCheckpoints'

const props = defineProps<{
  threadId: string
  run?: RunRecord
}>()

const emit = defineEmits<{
  close: []
  reverted: [RevertResult]
}>()

const { revert } = useCheckpoints()

const restoreAll = ref(false)
const allowAfterPush = ref(false)
const confirmed = ref(false)
const reverting = ref(false)
const revertError = ref('')
const result = ref<RevertResult | null>(null)

// Revert button is disabled until the human explicitly confirms — and, for
// a pushed run, until they've also checked the pushed-specific box. Never
// a silent one-click destructive revert.
const canRevert = computed(() => {
  if (!confirmed.value) return false
  if (props.run?.Pushed && !allowAfterPush.value) return false
  return true
})

const failedEntries = computed(() => Object.entries(result.value?.Failed || {}))

async function doRevert() {
  if (!canRevert.value || reverting.value) return
  reverting.value = true
  revertError.value = ''
  try {
    result.value = await revert(props.threadId, {
      all: restoreAll.value,
      allow_after_push: allowAfterPush.value,
    })
    emit('reverted', result.value)
  } catch (e) {
    revertError.value = e instanceof Error ? e.message : 'Revert failed'
  } finally {
    reverting.value = false
  }
}

function close() {
  emit('close')
}

// Small local subcomponent for the honesty file lists (Restored / Deleted /
// SkippedEdited / NotRestorable) — same shape, four times, kept inline
// rather than a separate file since it's only ever used here.
const toneClass: Record<string, string> = {
  green: 'text-huginn-green',
  red: 'text-huginn-red',
  yellow: 'text-huginn-yellow',
  muted: 'text-huginn-muted',
}
const ResultFileList = defineComponent({
  props: { label: String, paths: { type: Array as () => string[] | null, default: null }, tone: String, testId: String },
  setup(p) {
    return () => {
      const paths = p.paths || []
      if (!paths.length) return null
      return h('div', { class: 'space-y-1', 'data-testid': p.testId }, [
        h('p', { class: `text-[10px] uppercase tracking-wide ${toneClass[p.tone || 'muted']}` }, `${p.label} (${paths.length})`),
        h('div', { class: 'max-h-32 overflow-y-auto space-y-0.5' }, paths.map((path) =>
          h('div', { key: path, class: 'text-[11px] font-mono text-huginn-text/80 truncate', title: path }, path)
        )),
      ])
    }
  },
})
</script>

<style scoped>
.backdrop-enter-active,
.backdrop-leave-active {
  transition: opacity 0.15s ease;
}
.backdrop-enter-from,
.backdrop-leave-to {
  opacity: 0;
}
</style>
