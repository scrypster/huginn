<template>
  <div class="flex flex-col h-full">
    <!-- Header -->
    <div class="flex items-center justify-between px-4 py-2 border-b border-huginn-border bg-huginn-surface flex-shrink-0">
      <div class="flex items-center gap-3">
        <span class="text-huginn-blue text-sm font-bold">logs</span>
        <span class="text-huginn-muted text-xs">{{ filteredLines.length }} / {{ parsedLines.length }} lines</span>
        <!-- Level filter pills -->
        <div class="flex items-center gap-1">
          <button
            v-for="lvl in levelFilters"
            :key="lvl"
            @click="activeLevel = lvl"
            class="text-[10px] px-2 py-0.5 rounded border transition-colors"
            :class="activeLevel === lvl
              ? 'bg-huginn-blue/15 text-huginn-blue border-huginn-blue/30'
              : 'text-huginn-muted border-huginn-border hover:text-huginn-text'"
          >{{ lvl }}</button>
        </div>
      </div>
      <div class="flex items-center gap-3">
        <!-- Search -->
        <input
          v-model="searchText"
          type="text"
          placeholder="search..."
          class="bg-huginn-surface border border-huginn-border rounded-lg px-3 py-1 text-xs text-huginn-text outline-none focus:border-huginn-blue/50 w-48"
        />
        <label class="flex items-center gap-1 text-xs text-huginn-muted">
          <input type="checkbox" v-model="autoRefresh" class="accent-huginn-blue" />
          auto-refresh
        </label>
        <button @click="fetchLogs" class="text-huginn-muted text-xs hover:text-huginn-blue">refresh</button>
      </div>
    </div>

    <!-- Date range filter bar -->
    <div class="flex items-center gap-2 px-4 py-1.5 border-b border-huginn-border/50 bg-huginn-surface/40 flex-shrink-0">
      <span class="text-[10px] text-huginn-muted uppercase tracking-widest flex-shrink-0">Date range</span>
      <input
        v-model="dateFrom"
        type="date"
        class="bg-huginn-surface border border-huginn-border rounded-lg px-2 py-0.5 text-xs text-huginn-text outline-none focus:border-huginn-blue/50 [color-scheme:dark]"
      />
      <span class="text-huginn-muted text-xs">→</span>
      <input
        v-model="dateTo"
        type="date"
        class="bg-huginn-surface border border-huginn-border rounded-lg px-2 py-0.5 text-xs text-huginn-text outline-none focus:border-huginn-blue/50 [color-scheme:dark]"
      />
      <button v-if="dateFrom || dateTo" @click="dateFrom = ''; dateTo = ''"
        class="text-[10px] text-huginn-muted hover:text-huginn-red transition-colors px-1">clear</button>
      <div class="flex-1" />
      <button @click="exportLogs"
        class="flex items-center gap-1 text-[10px] text-huginn-muted hover:text-huginn-blue transition-colors px-2 py-0.5 rounded border border-huginn-border hover:border-huginn-blue/30">
        <svg class="w-3 h-3" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
          <path d="M21 15v4a2 2 0 01-2 2H5a2 2 0 01-2-2v-4"/><polyline points="7 10 12 15 17 10"/><line x1="12" y1="15" x2="12" y2="3"/>
        </svg>
        export
      </button>
    </div>

    <!-- Log rows -->
    <div ref="logsEl" class="flex-1 overflow-y-auto">
      <div v-if="loading && parsedLines.length === 0" class="text-huginn-muted text-sm p-4">Loading logs...</div>
      <div v-else-if="error" class="text-huginn-red text-sm p-4">{{ error }}</div>
      <div v-else-if="filteredLines.length === 0" class="text-huginn-muted text-sm p-4">No log entries match.</div>
      <div v-else>
        <div
          v-for="(entry, i) in filteredLines"
          :key="i"
          class="flex items-start gap-3 px-4 py-1.5 border-b border-huginn-border/30 hover:bg-white/[0.02] font-mono"
        >
          <!-- Time -->
          <span class="w-16 flex-shrink-0 text-huginn-muted/60 text-[11px] pt-px">{{ entry.time }}</span>

          <!-- Level badge -->
          <span
            v-if="entry.level"
            class="w-12 flex-shrink-0 text-[10px] px-1 py-0.5 rounded border text-center"
            :class="levelClass(entry.level)"
          >{{ entry.level }}</span>
          <span v-else class="w-12 flex-shrink-0" />

          <!-- Message + extra fields -->
          <div class="flex-1 min-w-0">
            <span class="text-huginn-text text-xs break-words">{{ entry.msg }}</span>
            <span
              v-if="entry.extras"
              class="ml-2 text-huginn-muted/60 text-[11px] break-all"
            >{{ entry.extras }}</span>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted, watch, nextTick } from 'vue'
import { api } from '../composables/useApi'

interface ParsedLine {
  time: string
  isoDate: string  // YYYY-MM-DD for date filtering
  level: string
  msg: string
  extras: string
  raw: string
  levelRaw: string
}

const rawLines = ref<string[]>([])
const loading = ref(true)
const error = ref('')
const autoRefresh = ref(true)
const logsEl = ref<HTMLElement>()
const activeLevel = ref('ALL')
const searchText = ref('')
const dateFrom = ref('')
const dateTo = ref('')
let timer: ReturnType<typeof setInterval> | null = null

const levelFilters = ['ALL', 'ERROR', 'WARN', 'INFO', 'DEBUG']

function extractTime(iso: string): string {
  // Extract HH:MM:SS from ISO timestamp
  const match = iso.match(/T(\d{2}:\d{2}:\d{2})/)
  return match?.[1] ?? iso.slice(0, 8)
}

function levelClass(level: string): string {
  switch (level.toUpperCase()) {
    case 'ERROR': return 'text-huginn-red border-huginn-red/30 bg-huginn-red/10'
    case 'WARN':  return 'text-huginn-yellow border-huginn-yellow/30 bg-huginn-yellow/10'
    case 'INFO':  return 'text-huginn-muted border-huginn-border'
    case 'DEBUG': return 'text-huginn-muted/50 border-huginn-border/50'
    default:      return 'text-huginn-muted border-huginn-border'
  }
}

const parsedLines = computed<ParsedLine[]>(() => {
  return rawLines.value.map((line) => {
    const trimmed = line.trim()
    if (!trimmed) return { time: '', isoDate: '', level: '', msg: trimmed, extras: '', raw: line, levelRaw: '' }
    try {
      const obj = JSON.parse(trimmed)
      const { time, level, msg, ...rest } = obj as Record<string, unknown>
      const extras = Object.entries(rest)
        .map(([k, v]) => `${k}=${typeof v === 'object' ? JSON.stringify(v) : v}`)
        .join(' ')
      const rawTime = time != null ? String(time) : ''
      return {
        time: rawTime ? extractTime(rawTime) : '',
        isoDate: rawTime ? rawTime.slice(0, 10) : '',
        level: level != null ? String(level).toUpperCase() : '',
        msg: msg != null ? String(msg) : trimmed,
        extras,
        raw: line,
        levelRaw: level != null ? String(level).toUpperCase() : '',
      }
    } catch {
      return { time: '', isoDate: '', level: '', msg: trimmed, extras: '', raw: line, levelRaw: '' }
    }
  })
})

const filteredLines = computed<ParsedLine[]>(() => {
  let lines = parsedLines.value
  if (activeLevel.value !== 'ALL') {
    lines = lines.filter((l) => l.levelRaw === activeLevel.value)
  }
  if (searchText.value.trim()) {
    const q = searchText.value.trim().toLowerCase()
    lines = lines.filter((l) => l.msg.toLowerCase().includes(q) || l.extras.toLowerCase().includes(q))
  }
  if (dateFrom.value) {
    lines = lines.filter((l) => !l.isoDate || l.isoDate >= dateFrom.value)
  }
  if (dateTo.value) {
    lines = lines.filter((l) => !l.isoDate || l.isoDate <= dateTo.value)
  }
  return lines
})

function exportLogs() {
  const text = filteredLines.value.map((l) => l.raw).join('\n')
  const blob = new Blob([text], { type: 'text/plain' })
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = `huginn-logs-${new Date().toISOString().slice(0, 19).replace(/:/g, '-')}.txt`
  a.click()
  URL.revokeObjectURL(url)
}

async function fetchLogs() {
  try {
    const data = await api.logs(500)
    rawLines.value = data.lines ?? []
    error.value = ''
    await nextTick()
    if (logsEl.value) {
      logsEl.value.scrollTop = logsEl.value.scrollHeight
    }
  } catch (e: unknown) {
    error.value = e instanceof Error ? e.message : 'Failed to load logs'
  } finally {
    loading.value = false
  }
}

function startTimer() {
  if (timer) clearInterval(timer)
  timer = setInterval(fetchLogs, 5000)
}

function stopTimer() {
  if (timer) {
    clearInterval(timer)
    timer = null
  }
}

watch(autoRefresh, (val) => {
  if (val) startTimer()
  else stopTimer()
})

onMounted(() => {
  fetchLogs()
  if (autoRefresh.value) startTimer()
})

onUnmounted(stopTimer)
</script>
