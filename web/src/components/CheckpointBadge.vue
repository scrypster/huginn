<template>
  <div v-if="run" class="mt-1 pl-2 flex items-center gap-2" data-testid="checkpoint-badge">
    <template v-if="run.Status === 'capture_failed'">
      <span
        class="inline-flex items-center gap-1 text-[10px] text-huginn-yellow"
        data-testid="checkpoint-badge-not-protected"
        :title="run.CaptureError || 'Snapshot capture failed — this run is not protected'"
      >
        <svg class="w-3 h-3 flex-shrink-0" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
          <path d="M10.29 3.86L1.82 18a2 2 0 001.71 3h16.94a2 2 0 001.71-3L13.71 3.86a2 2 0 00-3.42 0z" />
          <line x1="12" y1="9" x2="12" y2="13" /><line x1="12" y1="17" x2="12.01" y2="17" />
        </svg>
        Not snapshotted
      </span>
    </template>
    <template v-else-if="run.Status === 'reverted'">
      <span class="inline-flex items-center gap-1 text-[10px] text-huginn-muted" data-testid="checkpoint-badge-reverted">
        <svg class="w-3 h-3 flex-shrink-0" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
          <polyline points="1 4 1 10 7 10" /><path d="M3.51 15a9 9 0 102.13-9.36L1 10" />
        </svg>
        Reverted
      </span>
    </template>
    <template v-else>
      <span class="inline-flex items-center gap-1 text-[10px] text-huginn-green/80" data-testid="checkpoint-badge-protected" title="This run's file changes were snapshotted and can be undone">
        <svg class="w-3 h-3 flex-shrink-0" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
          <path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z" />
        </svg>
        Snapshotted
      </span>
      <button
        type="button"
        class="text-[10px] font-medium text-huginn-blue hover:underline"
        data-testid="checkpoint-badge-undo"
        @click="showDialog = true"
      >
        Undo
      </button>
      <span v-if="run.Pushed" class="text-[10px] text-huginn-muted/70" title="This run was pushed — undo still restores local files, but you'll need a forward commit to undo the remote side">
        · pushed
      </span>
    </template>
  </div>

  <CheckpointRevertDialog
    v-if="showDialog && run"
    :thread-id="threadId"
    :run="run"
    @close="showDialog = false"
  />
</template>

<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useCheckpoints } from '../composables/useCheckpoints'
import CheckpointRevertDialog from './CheckpointRevertDialog.vue'

const props = defineProps<{
  threadId: string
  // Mirrors DelegatedThread.done (useSessions.ts) — the same WS-driven
  // completion flag ChatView already uses for delegatedThreadStatusLabel.
  // The checkpoints list is fetched once (module-singleton, see
  // useCheckpoints.ts); a run that finishes AFTER that first fetch would
  // otherwise never get a badge until a page reload. Refetching on this
  // thread's own done transition is the cheapest correct trigger — no
  // polling loop, and it fires only for a thread whose badge is actually
  // mounted right now.
  done?: boolean
}>()

const { ensureLoaded, getRunForThread, fetchRuns } = useCheckpoints()
const showDialog = ref(false)

onMounted(ensureLoaded)

watch(
  () => props.done,
  (isDone, wasDone) => {
    if (isDone && !wasDone) fetchRuns()
  },
)

const run = computed(() => getRunForThread(props.threadId))
</script>
