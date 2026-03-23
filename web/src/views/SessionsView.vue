<template>
  <div class="flex flex-col h-full">
    <div class="flex items-center justify-between px-4 py-2 border-b border-huginn-border bg-huginn-surface">
      <span class="text-huginn-blue text-sm font-bold">sessions</span>
      <button @click="refresh" class="text-huginn-muted text-xs hover:text-huginn-blue">refresh</button>
    </div>
    <div class="flex-1 overflow-y-auto p-4">
      <div v-if="loading" class="text-huginn-muted text-sm">Loading sessions...</div>
      <div v-else-if="error" class="text-huginn-red text-sm">{{ error }}</div>
      <div v-else-if="sessions.length === 0" class="text-huginn-muted text-sm">No sessions found.</div>
      <div v-else class="space-y-2">
        <div
          v-for="sess in sessions"
          :key="sess.id"
          class="border border-huginn-border rounded p-3 bg-huginn-surface cursor-pointer hover:border-huginn-blue transition-colors"
          @click="toggleExpand(sess.id)"
        >
          <div class="flex items-center justify-between">
            <span class="text-huginn-text text-sm font-mono">{{ sess.id?.slice(0, 12) }}...</span>
            <span class="text-huginn-muted text-xs">{{ formatDate(sess.created_at || sess.updated_at) }}</span>
          </div>
          <div v-if="sess.model" class="text-huginn-muted text-xs mt-1">model: {{ sess.model }}</div>
          <div v-if="expanded === sess.id" class="mt-3 pt-3 border-t border-huginn-border">
            <pre class="text-huginn-muted text-xs overflow-x-auto whitespace-pre-wrap">{{ JSON.stringify(sess, null, 2) }}</pre>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { api } from '../composables/useApi'

interface Session {
  id: string
  created_at?: string
  updated_at?: string
  model?: string
  [key: string]: unknown
}

const sessions = ref<Session[]>([])
const loading = ref(true)
const error = ref('')
const expanded = ref('')

function toggleExpand(id: string) {
  expanded.value = expanded.value === id ? '' : id
}

function formatDate(dateStr?: string): string {
  if (!dateStr) return ''
  try {
    return new Date(dateStr).toLocaleString()
  } catch {
    return dateStr
  }
}

async function refresh() {
  loading.value = true
  error.value = ''
  try {
    const data = await api.sessions.list()
    sessions.value = data as Session[]
  } catch (e: unknown) {
    error.value = e instanceof Error ? e.message : 'Failed to load sessions'
  } finally {
    loading.value = false
  }
}

onMounted(refresh)
</script>
