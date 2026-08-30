<template>
  <div
    v-if="visible"
    data-testid="reply-thread-drawer"
    class="flex flex-col h-full border-l border-huginn-border flex-shrink-0 overflow-hidden"
    style="width:400px;background:rgba(18,23,30,0.98);backdrop-filter:blur(12px)"
  >
    <div class="flex items-center gap-2 px-3 h-11 border-b border-huginn-border flex-shrink-0"
      style="background:rgba(22,27,34,0.6)">
      <span class="text-xs font-semibold text-huginn-text tracking-wide flex-1 truncate">Thread</span>
      <button
        data-testid="reply-thread-close"
        type="button"
        class="w-6 h-6 rounded-lg flex items-center justify-center text-huginn-muted/40 hover:text-huginn-muted hover:bg-huginn-surface"
        title="Close"
        @click="$emit('close')"
      >
        <svg class="w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
          <line x1="18" y1="6" x2="6" y2="18" /><line x1="6" y1="6" x2="18" y2="18" />
        </svg>
      </button>
    </div>

    <div ref="listEl" class="flex-1 overflow-y-auto py-4 px-4 space-y-3" data-testid="reply-thread-list"
      @touchstart.passive="onThreadTouchStart" @touchmove.passive="onThreadTouchMove">
      <div v-if="parentDayLabel" class="flex items-center gap-3" data-testid="reply-day-sep">
        <div class="flex-1 h-px bg-huginn-border/40" />
        <span class="text-[11px] text-huginn-muted/50 font-medium select-none">{{ parentDayLabel }}</span>
        <div class="flex-1 h-px bg-huginn-border/40" />
      </div>
      <div v-if="parent" data-testid="reply-thread-parent" class="pb-3 border-b border-huginn-border/60">
        <div class="text-[11px] font-semibold text-huginn-muted mb-1" data-testid="reply-thread-author">{{ speaker(parent) }}</div>
        <MsgTimeReveal :created-at="createdAtOf(parent)" :revealed="threadTimesRevealed">
          <SystemFailLine v-if="classifyReplySpeech(parent.content).kind === 'fail'" :content="parent.content" />
          <p v-else-if="classifyReplySpeech(parent.content).kind === 'speech'" data-testid="reply-speech" class="text-sm text-huginn-text leading-relaxed whitespace-pre-wrap break-words">{{ classifyReplySpeech(parent.content).text }}</p>
          <p v-else data-testid="reply-hidden" class="text-xs text-huginn-muted/50 italic">Hidden</p>
        </MsgTimeReveal>
      </div>

      <div v-if="loading" class="text-xs text-huginn-muted">Loading replies…</div>
      <div v-else-if="error" class="text-xs text-huginn-red">{{ error }}</div>

      <template v-for="msg in repliesWithDays" :key="msg.id">
        <div v-if="msg.dateLabel" class="flex items-center gap-3" data-testid="reply-day-sep">
          <div class="flex-1 h-px bg-huginn-border/40" />
          <span class="text-[11px] text-huginn-muted/50 font-medium select-none">{{ msg.dateLabel }}</span>
          <div class="flex-1 h-px bg-huginn-border/40" />
        </div>
        <div
          :data-reply-id="msg.id"
          data-testid="reply-thread-msg"
          class="space-y-0.5"
        >
          <div class="text-[11px] font-semibold text-huginn-muted">{{ speaker(msg) }}</div>
          <MsgTimeReveal :created-at="createdAtOf(msg)" :revealed="threadTimesRevealed">
            <SystemFailLine v-if="classifyReplySpeech(msg.content).kind === 'fail'" :content="msg.content" />
            <p v-else-if="classifyReplySpeech(msg.content).kind === 'speech'" data-testid="reply-speech" class="text-sm text-huginn-text leading-relaxed whitespace-pre-wrap break-words">{{ classifyReplySpeech(msg.content).text }}</p>
            <p v-else data-testid="reply-hidden" class="text-xs text-huginn-muted/50 italic">Hidden</p>
          </MsgTimeReveal>
        </div>
      </template>

      <div
        v-if="streamSpeech"
        data-testid="reply-thread-stream"
        class="space-y-0.5"
      >
        <div class="text-[11px] font-semibold text-huginn-muted">{{ streamAgent || typingAgent || fallbackAgent || 'Teammate' }}</div>
        <p data-testid="reply-speech" class="text-sm text-huginn-text leading-relaxed whitespace-pre-wrap break-words">{{ streamSpeech }}</p>
      </div>
      <div v-else-if="typingAgent" data-testid="reply-thread-writing" class="text-xs text-huginn-muted italic">
        {{ typingAgent }} is writing
      </div>
      <div
        v-if="snagAgent"
        data-testid="reply-thread-snag"
        class="text-xs text-huginn-muted italic"
        :title="snagReason || 'diagnose'"
      >
        {{ snagAgent }} hit a snag
      </div>
    </div>

    <form class="px-3 py-3 border-t border-huginn-border flex-shrink-0" data-testid="reply-thread-composer" @submit.prevent="send">
      <p
        v-if="unknownMentionHint"
        data-testid="reply-unknown-mention-hint"
        class="text-[11px] mb-2"
        style="color:rgba(227,179,65,0.92)"
      >{{ unknownMentionHint }}</p>
      <div
        v-if="mentionHits.length"
        data-testid="reply-mention-picker"
        class="mb-2 rounded-xl border border-huginn-border overflow-hidden"
        style="background:rgba(22,27,34,0.97)"
      >
        <button
          v-for="name in mentionHits"
          :key="name"
          type="button"
          class="w-full text-left px-3 py-1.5 text-xs text-huginn-text hover:bg-huginn-blue/20"
          @mousedown.prevent="pickMention(name)"
        >@{{ name }}</button>
      </div>
      <div class="flex gap-2">
        <input
          ref="inputEl"
          v-model="draft"
          data-testid="reply-thread-input"
          type="text"
          class="flex-1 bg-huginn-surface border border-huginn-border rounded-lg px-3 py-2 text-sm text-huginn-text"
          placeholder="Reply…"
          :disabled="sending"
        />
        <button
          data-testid="reply-thread-send"
          type="submit"
          class="px-3 py-2 rounded-lg text-sm font-medium border border-huginn-blue/40 text-huginn-blue disabled:opacity-40"
          :disabled="sending || !draft.trim()"
        >
          Send
        </button>
      </div>
    </form>
  </div>
</template>

<script setup lang="ts">
import { computed, nextTick, ref, watch } from 'vue'
import { api, type SpaceMessage } from '../composables/useApi'
import SystemFailLine from './SystemFailLine.vue'
import MsgTimeReveal from './MsgTimeReveal.vue'
import { classifyReplySpeech } from './replySpeech'
import { dropUnknownLeadMention, filterMentionSuggestions } from './ChatEditor/mentionSuggestions'
import { dateLabelFor, isSameDay, sortMessagesChronological } from '../composables/useMessageEnrichment'
import { messageCreatedAt } from '../utils/relativeTime'

const props = defineProps<{
  visible: boolean
  spaceId: string
  parent: SpaceMessage | { id: string; content: string; role: string; agent?: string; created_at?: string; ts?: string; createdAt?: string } | null
  incoming?: SpaceMessage | null
  typingAgent?: string
  streamAgent?: string
  streamText?: string
  memberNames?: string[]
  fallbackAgent?: string
  snagAgent?: string
  snagReason?: string
}>()

const emit = defineEmits<{
  (e: 'close'): void
  (e: 'posted', count: number, parentId: string): void
  (e: 'seen', parentId: string): void
}>()

const replies = ref<SpaceMessage[]>([])
const threadTimesRevealed = ref(false)
let threadTouchX = 0
const loading = ref(false)
const error = ref<string | null>(null)
const draft = ref('')
const sending = ref(false)
const listEl = ref<HTMLElement | null>(null)
const inputEl = ref<HTMLInputElement | null>(null)
const firstUnseenId = ref<string | null>(null)
const unknownMentionHint = ref('')
let unknownMentionTimer: ReturnType<typeof setTimeout> | null = null

const streamSpeech = computed(() => {
  const c = classifyReplySpeech(props.streamText)
  return c.kind === 'speech' ? c.text : ''
})

const mentionHits = computed(() => {
  const m = draft.value.match(/(?:^|\s)@([A-Za-z][\w-]*)?$/)
  if (m == null) return []
  const agents = (props.memberNames ?? []).map(name => ({ name }))
  return filterMentionSuggestions(agents, m[1] ?? '', props.memberNames).map(a => String(a.name ?? ''))
})

function pickMention(name: string) {
  draft.value = draft.value.replace(/@([A-Za-z][\w-]*)$/, '@' + name + ' ')
}

function showUnknownMentionHint(name: string) {
  unknownMentionHint.value = `${name} is not in this channel`
  if (unknownMentionTimer) clearTimeout(unknownMentionTimer)
  unknownMentionTimer = setTimeout(() => {
    unknownMentionHint.value = ''
    unknownMentionTimer = null
  }, 6000)
}

function speaker(msg: { role: string; agent?: string }): string {
  if (msg.role === 'user') return 'You'
  const a = (msg.agent || '').trim()
  if (a && a.length > 1) return a
  return (props.fallbackAgent || '').trim() || a || 'Teammate'
}

function createdAtOf(msg: { created_at?: string; ts?: string; createdAt?: string } | null | undefined): string {
  return messageCreatedAt(msg)
}

const parentDayLabel = computed(() => dateLabelFor(createdAtOf(props.parent as { created_at?: string; ts?: string } | null)))

const repliesWithDays = computed(() => {
  const parentTs = createdAtOf(props.parent as { created_at?: string; ts?: string } | null)
  const ordered = sortMessagesChronological(replies.value)
  return ordered.map((msg, i) => {
    const ts = createdAtOf(msg)
    const prevTs = i === 0 ? parentTs : createdAtOf(ordered[i - 1])
    const dateLabel = ts && !isSameDay(ts, prevTs) ? dateLabelFor(ts) : undefined
    return { ...msg, dateLabel }
  })
})

function onThreadTouchStart(e: TouchEvent) {
  threadTouchX = e.touches[0]?.clientX ?? 0
}
function onThreadTouchMove(e: TouchEvent) {
  const x = e.touches[0]?.clientX ?? threadTouchX
  const dx = x - threadTouchX
  if (dx < -36) threadTimesRevealed.value = true
  if (dx > 36) threadTimesRevealed.value = false
}

async function load() {
  if (!props.visible || !props.spaceId || !props.parent?.id) return
  loading.value = true
  error.value = null
  try {
    const res = await api.spaces.replies(props.spaceId, props.parent.id)
    replies.value = Array.isArray(res?.messages) ? res.messages : []
    const unseen = typeof res?.unseen === 'number' ? res.unseen : 0
    if (res?.participant && unseen > 0 && replies.value.length) {
      const start = Math.max(0, replies.value.length - unseen)
      firstUnseenId.value = replies.value[start]?.id ?? null
    } else {
      firstUnseenId.value = null
    }
    await api.spaces.markThreadRead(props.spaceId, props.parent.id).catch(() => {})
    emit('seen', props.parent.id)
    await nextTick()
    if (firstUnseenId.value) {
      const el = listEl.value?.querySelector(`[data-reply-id="${firstUnseenId.value}"]`)
      el?.scrollIntoView({ block: 'start' })
    }
  } catch {
    error.value = 'Could not load replies.'
  } finally {
    loading.value = false
  }
}

watch(() => [props.visible, props.spaceId, props.parent?.id], load, { immediate: true })

const overlayBlockers = '[data-testid="memory-vault-modal"], [data-testid$="-modal"], [role="dialog"], [aria-modal="true"]'

function overlayHoldsFocus(): boolean {
  if (typeof document === 'undefined') return false
  const active = document.activeElement as HTMLElement | null
  if (active?.closest(overlayBlockers)) return true
  return !!document.querySelector(overlayBlockers)
}

function focusComposer() {
  if (!props.visible || overlayHoldsFocus()) return
  inputEl.value?.focus()
}

watch(
  () => [props.visible, props.parent?.id] as const,
  (curr, prev) => {
    const [visible, parentId] = curr
    if (!visible || !parentId) return
    if (prev && prev[0] && prev[1] === parentId) return
    nextTick(focusComposer)
  },
  { immediate: true },
)

watch(() => props.incoming, (msg) => {
  if (!msg?.id || !props.visible || !props.parent?.id) return
  if (msg.parent_id && msg.parent_id !== props.parent.id) return
  if (replies.value.some(r => r.id === msg.id)) return
  replies.value = [...replies.value, msg]
})

async function send() {
  const raw = draft.value.trim()
  if (!raw || !props.parent?.id || !props.spaceId) return
  const dropped = dropUnknownLeadMention(raw, props.memberNames)
  if (dropped.dropped) showUnknownMentionHint(dropped.dropped)
  const content = dropped.content.trim()
  if (!content) return
  sending.value = true
  try {
    const created = await api.spaces.postMessage(props.spaceId, {
      content,
      parent_id: props.parent.id,
    })
    if (!replies.value.some(r => r.id === created.id)) {
      replies.value = [...replies.value, created]
    }
    draft.value = ''
    emit('posted', replies.value.length, props.parent.id)
  } catch {
    error.value = 'Could not send reply.'
  } finally {
    sending.value = false
  }
}

</script>
