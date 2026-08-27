<template>
  <button
    v-if="count > 0 || typingAgent"
    type="button"
    data-testid="space-reply-chip"
    class="mt-1 text-[11px] font-medium px-2 py-0.5 rounded-lg border border-huginn-blue/30 text-huginn-blue/90 hover:bg-huginn-blue/10 transition-colors text-left max-w-full"
    @click.stop="$emit('open')"
  >
    <span v-if="typingAgent" data-testid="space-reply-writing">{{ typingAgent }} is writing</span>
    <span v-else>
      {{ count === 1 ? '1 reply' : `${count} replies` }}
      <span v-if="safePreview" data-testid="space-reply-preview" class="text-huginn-muted font-normal"> · {{ safePreview }}</span>
      <span
        v-if="participant && newSinceCount > 0"
        data-testid="space-reply-new-since"
        class="text-huginn-muted font-normal"
      > · {{ newSinceCount === 1 ? '1 new since you were last here' : `${newSinceCount} new since you were last here` }}</span>
    </span>
  </button>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { classifyReplySpeech } from './replySpeech'

const props = defineProps<{
  count: number
  preview?: string
  typingAgent?: string
  participant?: boolean
  newSince?: number
}>()

defineEmits<{ (e: 'open'): void }>()

const newSinceCount = computed(() => props.newSince ?? 0)

const safePreview = computed(() => {
  const raw = props.preview ?? ''
  const c = classifyReplySpeech(raw)
  if (c.kind !== 'speech') return ''
  const t = c.text.trim()
  if (!t) return ''
  if (/^delegated to @/i.test(t)) return ''
  return t.length > 80 ? t.slice(0, 80) + '…' : t
})
</script>
