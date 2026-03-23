<template>
  <div class="border border-huginn-border rounded-lg p-4 bg-huginn-surface transition-colors"
    :class="{ 'border-huginn-red/40': notification.severity === 'urgent', 'border-yellow-500/30': notification.severity === 'warning' }">
    <div class="flex items-start gap-3">
      <div class="w-2 h-2 rounded-full mt-2 flex-shrink-0"
        :class="{
          'bg-huginn-red': notification.severity === 'urgent',
          'bg-yellow-400': notification.severity === 'warning',
          'bg-huginn-blue': notification.severity === 'info',
        }" />
      <div class="flex-1 min-w-0">
        <div class="flex items-center justify-between gap-2">
          <span class="text-xs font-medium text-huginn-text truncate">{{ notification.summary }}</span>
          <span class="text-[10px] text-huginn-muted flex-shrink-0">{{ formatTime(notification.created_at) }}</span>
        </div>
        <div class="mt-1 flex items-center gap-2">
          <span class="text-[10px] text-huginn-muted uppercase tracking-wide">{{ notification.severity }}</span>
        </div>
        <div v-if="expanded" class="mt-3 text-xs text-huginn-muted whitespace-pre-wrap border-t border-huginn-border pt-3">
          {{ notification.detail }}
        </div>
        <div class="mt-3 flex items-center gap-2 flex-wrap">
          <button @click="expanded = !expanded"
            class="text-[11px] px-2 py-1 rounded border border-huginn-border text-huginn-muted hover:text-huginn-text hover:border-huginn-blue/40 transition-colors">
            {{ expanded ? 'Collapse' : 'View Detail' }}
          </button>
          <button @click="$emit('action', notification.id, 'dismiss')"
            class="text-[11px] px-2 py-1 rounded border border-huginn-border text-huginn-muted hover:text-huginn-red hover:border-huginn-red/40 transition-colors">
            Dismiss
          </button>
          <button @click="$emit('chat', notification)"
            class="text-[11px] px-2 py-1 rounded border border-huginn-blue/40 text-huginn-blue hover:bg-huginn-blue/10 transition-colors">
            → Chat
          </button>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import type { Notification } from '../composables/useNotifications'

defineProps<{ notification: Notification }>()
defineEmits<{
  action: [id: string, action: string]
  chat: [notification: Notification]
}>()

const expanded = ref(false)

function formatTime(ts: string) {
  const d = new Date(ts)
  return isNaN(d.getTime()) ? '' : d.toLocaleTimeString('en-US', { hour: 'numeric', minute: '2-digit' })
}
</script>
