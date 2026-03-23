<template>
  <div class="h-full flex flex-col">
    <div class="flex items-center justify-between px-6 py-4 border-b border-huginn-border flex-shrink-0">
      <div class="flex items-center gap-3">
        <h1 class="text-sm font-semibold text-huginn-text uppercase tracking-widest">Inbox</h1>
        <span v-if="pendingCount > 0"
          class="text-[10px] px-1.5 py-0.5 rounded-full bg-huginn-blue text-white font-medium">
          {{ pendingCount }}
        </span>
      </div>
      <div class="flex items-center gap-3">
        <button v-if="snoozedIds.size > 0" data-testid="unsnooze-btn" @click="clearSnooze"
          class="text-xs text-huginn-muted hover:text-huginn-blue transition-colors">
          Show snoozed ({{ snoozedIds.size }})
        </button>
        <button data-testid="mark-all-seen-btn" @click="markAllSeen"
          :disabled="isBulkProcessing"
          class="text-xs text-huginn-muted hover:text-huginn-text transition-colors disabled:opacity-40 disabled:cursor-not-allowed">
          Mark all seen
        </button>
        <button data-testid="dismiss-all-btn" @click="dismissAll"
          :disabled="isBulkProcessing"
          class="text-xs text-huginn-muted hover:text-huginn-red transition-colors disabled:opacity-40 disabled:cursor-not-allowed">
          Dismiss all
        </button>
      </div>
    </div>

    <!-- Inbox action error banner -->
    <div v-if="inboxError"
      data-testid="inbox-error-banner"
      class="flex items-center justify-between gap-2 px-6 py-2 bg-huginn-red/10 border-b border-huginn-red/30 text-huginn-red text-xs flex-shrink-0">
      <span>{{ inboxError }}</span>
      <button @click="inboxError = null" class="opacity-60 hover:opacity-100">✕</button>
    </div>

    <!-- Severity filter chips -->
    <div class="flex items-center gap-1.5 px-6 py-2 border-b border-huginn-border/50 flex-shrink-0">
      <button
        v-for="chip in severityChips"
        :key="chip.value"
        @click="severityFilter = chip.value"
        class="text-[10px] px-2.5 py-1 rounded-full border transition-all duration-150"
        :class="severityFilter === chip.value
          ? chip.activeClass
          : 'border-huginn-border text-huginn-muted/60 hover:text-huginn-muted hover:border-huginn-border/80'"
      >
        {{ chip.label }}
        <span v-if="chip.count > 0" class="ml-1 opacity-70">{{ chip.count }}</span>
      </button>
    </div>

    <div class="flex-1 overflow-y-auto px-6 py-4">
      <div v-if="loading" class="flex items-center justify-center py-16">
        <div class="w-5 h-5 border-2 border-huginn-border border-t-huginn-blue rounded-full animate-spin" />
      </div>

      <div v-else-if="visible.length === 0" class="flex flex-col items-center justify-center py-16 gap-3">
        <svg class="w-10 h-10 text-huginn-muted opacity-30" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5">
          <path d="M18 8A6 6 0 006 8c0 7-3 9-3 9h18s-3-2-3-9" />
          <path d="M13.73 21a2 2 0 01-3.46 0" />
        </svg>
        <p class="text-huginn-muted text-sm">No notifications</p>
      </div>

      <div v-else class="space-y-3 max-w-2xl" data-testid="notification-list">
        <div v-for="n in visible" :key="n.id" data-testid="notification-item" class="group relative">
          <NotificationCard
            :notification="n"
            @action="handleAction"
            @chat="handleChat"
          />
          <!-- Snooze button (appears on hover, session-local) -->
          <button
            data-testid="snooze-btn"
            @click="snooze(n.id)"
            class="absolute top-2 right-2 opacity-0 group-hover:opacity-100 transition-opacity text-[10px] text-huginn-muted hover:text-huginn-yellow px-1.5 py-0.5 rounded border border-huginn-border/60 hover:border-huginn-yellow/30 bg-huginn-surface"
            title="Snooze for this session"
          >snooze</button>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, ref, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { useNotifications } from '../composables/useNotifications'
import { useSessions } from '../composables/useSessions'
import NotificationCard from '../components/NotificationCard.vue'

const router = useRouter()
const { notifications, pendingCount, loading, fetchNotifications, applyAction } = useNotifications()
const { createSession } = useSessions()

// ── Snooze (session-local) ───────────────────────────────────────────
const snoozedIds = ref<Set<string>>(new Set())

function snooze(id: string) {
  snoozedIds.value = new Set([...snoozedIds.value, id])
}

function clearSnooze() {
  snoozedIds.value = new Set()
}

// ── Severity filter ──────────────────────────────────────────────────
type SeverityFilter = 'all' | 'urgent' | 'warning' | 'info'
const severityFilter = ref<SeverityFilter>('all')

const activeNotifications = computed(() =>
  notifications.value.filter(
    (n: any) => !['dismissed', 'executed'].includes(n.status) && !snoozedIds.value.has(n.id)
  )
)

const severityChips = computed(() => [
  { value: 'all' as SeverityFilter, label: 'All', count: activeNotifications.value.length, activeClass: 'border-huginn-blue/60 text-huginn-blue bg-huginn-blue/10' },
  { value: 'urgent' as SeverityFilter, label: 'Urgent', count: activeNotifications.value.filter((n: any) => n.severity === 'urgent').length, activeClass: 'border-huginn-red/60 text-huginn-red bg-huginn-red/10' },
  { value: 'warning' as SeverityFilter, label: 'Warning', count: activeNotifications.value.filter((n: any) => n.severity === 'warning').length, activeClass: 'border-huginn-yellow/60 text-huginn-yellow bg-huginn-yellow/10' },
  { value: 'info' as SeverityFilter, label: 'Info', count: activeNotifications.value.filter((n: any) => n.severity === 'info').length, activeClass: 'border-huginn-muted/60 text-huginn-muted bg-huginn-muted/10' },
])

const visible = computed(() => {
  const base = activeNotifications.value
  if (severityFilter.value === 'all') return base
  return base.filter((n: any) => n.severity === severityFilter.value)
})

const inboxError = ref<string | null>(null)
const isBulkProcessing = ref(false)

async function handleAction(id: string, action: string) {
  inboxError.value = null
  try {
    await applyAction(id, action)
  } catch (e) {
    inboxError.value = e instanceof Error ? e.message : 'Action failed'
  }
}

async function handleChat(n: any) {
  inboxError.value = null
  try {
    const sess = await createSession()
    await applyAction(n.id, 'seen')
    router.push(`/chat/${sess.id}`)
  } catch (e) {
    inboxError.value = e instanceof Error ? e.message : 'Action failed'
  }
}

async function markAllSeen() {
  if (isBulkProcessing.value) return
  isBulkProcessing.value = true
  inboxError.value = null
  try {
    for (const n of notifications.value.filter((x: any) => x.status === 'pending')) {
      await applyAction(n.id, 'seen')
    }
  } catch (e) {
    inboxError.value = e instanceof Error ? e.message : 'Failed to mark all seen'
  } finally {
    isBulkProcessing.value = false
  }
}

async function dismissAll() {
  if (isBulkProcessing.value) return
  isBulkProcessing.value = true
  inboxError.value = null
  try {
    const pending = notifications.value.filter(
      (x: any) => !['dismissed', 'executed'].includes(x.status)
    )
    for (const n of pending) {
      await applyAction(n.id, 'dismissed')
    }
  } catch (e) {
    inboxError.value = e instanceof Error ? e.message : 'Failed to dismiss all'
  } finally {
    isBulkProcessing.value = false
  }
}

onMounted(() => {
  fetchNotifications()
})
</script>
