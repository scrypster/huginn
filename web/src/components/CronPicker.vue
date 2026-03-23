<template>
  <div class="space-y-2">
    <!-- Input row -->
    <input
      :value="modelValue"
      @input="$emit('update:modelValue', ($event.target as HTMLInputElement).value)"
      placeholder="0 8 * * 1-5"
      class="w-full bg-huginn-bg border border-huginn-border rounded-lg px-3 py-1.5 text-huginn-text placeholder-huginn-muted/50 focus:outline-none focus:border-huginn-blue/60 hover:border-huginn-border/80 transition-colors text-xs font-mono"
    />

    <!-- Human-readable description -->
    <p v-if="description" class="text-[11px] text-huginn-blue/80 pl-1">{{ description }}</p>
    <p v-else-if="modelValue" class="text-[11px] text-huginn-muted/60 pl-1 italic">Custom schedule</p>

    <!-- Quick presets -->
    <div class="flex flex-wrap gap-1.5 pt-0.5">
      <button
        v-for="p in presets"
        :key="p.value"
        type="button"
        @click="$emit('update:modelValue', p.value)"
        class="px-2 py-0.5 rounded-md text-[11px] border transition-colors duration-100"
        :class="modelValue === p.value
          ? 'border-huginn-blue/60 bg-huginn-blue/10 text-huginn-blue'
          : 'border-huginn-border text-huginn-muted hover:border-huginn-blue/40 hover:text-huginn-text'"
      >{{ p.label }}</button>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'

const props = defineProps<{ modelValue: string }>()
defineEmits<{ 'update:modelValue': [v: string] }>()

const presets = [
  { label: 'Every weekday at 8 AM', value: '0 8 * * 1-5' },
  { label: 'Every morning',         value: '0 8 * * *'   },
  { label: 'Every Monday 9 AM',     value: '0 9 * * 1'   },
  { label: 'Hourly',                value: '0 * * * *'   },
  { label: 'Every 30 min',          value: '*/30 * * * *' },
  { label: '1st of month',          value: '0 8 1 * *'   },
]

const description = computed(() => describeCron(props.modelValue ?? ''))

function describeCron(expr: string): string {
  const parts = expr.trim().split(/\s+/)
  if (parts.length !== 5) return ''
  const [min, hour, dom, , dow] = parts as [string, string, string, string, string]

  const hourNum = parseInt(hour)
  const minNum  = parseInt(min)
  const validHour = !isNaN(hourNum) && hour === String(hourNum)
  const validMin  = !isNaN(minNum)  && min  === String(minNum)

  const timeStr = validHour
    ? `at ${fmt12(hourNum, validMin ? minNum : 0)}`
    : hour === '*' ? 'every hour' : ''

  // Every N minutes
  if (min.startsWith('*/') && hour === '*' && dom === '*' && dow === '*') {
    const n = min.slice(2)
    return `Every ${n} minute${n === '1' ? '' : 's'}`
  }
  // Every N hours
  if (min === '0' && hour.startsWith('*/') && dom === '*' && dow === '*') {
    const n = hour.slice(2)
    return `Every ${n} hour${n === '1' ? '' : 's'}`
  }

  const dayDesc = describeDow(dow, dom)
  if (!dayDesc || !timeStr) return ''
  return `${dayDesc} ${timeStr}`
}

function describeDow(dow: string, dom: string): string {
  const days = ['Sunday','Monday','Tuesday','Wednesday','Thursday','Friday','Saturday']
  if (dow === '*' && dom === '*') return 'Every day'
  if (dow === '*' && dom !== '*') {
    const d = parseInt(dom)
    return isNaN(d) ? '' : `Monthly on the ${ordinal(d)}`
  }
  if (dow === '1-5') return 'Every weekday'
  if (dow === '6,0' || dow === '0,6') return 'Every weekend'
  const n = parseInt(dow)
  if (!isNaN(n) && n >= 0 && n <= 6) return `Every ${days[n]}`
  return ''
}

function fmt12(h: number, m: number): string {
  const ampm = h < 12 ? 'AM' : 'PM'
  const h12 = h % 12 === 0 ? 12 : h % 12
  const mm = m === 0 ? '' : `:${String(m).padStart(2, '0')}`
  return `${h12}${mm} ${ampm}`
}

function ordinal(n: number): string {
  const s = ['th','st','nd','rd']
  const v = n % 100
  return n + (s[(v - 20) % 10] ?? s[v] ?? 'th')
}
</script>
