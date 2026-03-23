<template>
  <div class="relative" ref="root">
    <!-- Input -->
    <div class="relative">
      <input
        ref="inputEl"
        :value="modelValue"
        @input="onInput"
        @focus="onFocus"
        @keydown="onKeydown"
        placeholder="@agent-name"
        class="w-full bg-huginn-bg border rounded-lg px-3 py-1.5 text-huginn-text placeholder-huginn-muted/50 focus:outline-none hover:border-huginn-border/80 transition-colors text-xs"
        :class="invalid
          ? 'border-red-500/60 focus:border-red-500/80'
          : 'border-huginn-border focus:border-huginn-blue/60'"
        autocomplete="off"
      />
    </div>

    <!-- Validation hint -->
    <p v-if="invalid" class="text-[11px] text-red-400 pl-1 mt-1">
      Unknown agent "{{ modelValue }}" — pick from the list or clear
    </p>

    <!-- Dropdown -->
    <Transition name="dropdown">
      <div v-if="open && filtered.length"
        class="absolute left-0 right-0 top-full mt-1 bg-huginn-surface border border-huginn-border rounded-xl shadow-xl z-50 overflow-hidden">
        <div
          v-for="(agent, i) in filtered"
          :key="String(agent.name)"
          @mousedown.prevent="select(agent)"
          class="flex items-center gap-2.5 px-3 py-2 cursor-pointer transition-colors duration-100"
          :class="i === cursor ? 'bg-huginn-blue/15 text-huginn-text' : 'text-huginn-muted hover:bg-huginn-bg/60 hover:text-huginn-text'"
        >
          <!-- Color avatar -->
          <div class="w-5 h-5 rounded-md flex items-center justify-center text-[10px] font-bold text-white flex-shrink-0"
            :style="{ background: (agent.color as string) || '#58a6ff' }">
            {{ (agent.icon as string) || String(agent.name)[0]?.toUpperCase() }}
          </div>
          <div class="flex flex-col min-w-0">
            <span class="text-xs">{{ agent.name }}</span>
            <span class="text-[10px] text-huginn-muted/60 font-mono ml-1">{{ agent.model || 'no model set' }}</span>
          </div>
        </div>
      </div>
    </Transition>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, watch, onMounted, onBeforeUnmount } from 'vue'
import { api } from '../composables/useApi'

const props = defineProps<{ modelValue: string }>()
const emit = defineEmits<{
  'update:modelValue': [v: string]
  'update:valid': [v: boolean]
  'select:agent': [agent: Record<string, unknown>]
}>()

const root = ref<HTMLElement | null>(null)
const open = ref(false)
const cursor = ref(0)
const agents = ref<Array<Record<string, unknown>>>([])

onMounted(async () => {
  try { agents.value = await api.agents.list() } catch {}
  document.addEventListener('mousedown', onOutside)
})
onBeforeUnmount(() => document.removeEventListener('mousedown', onOutside))

const query = computed(() => {
  const v = props.modelValue ?? ''
  return v.startsWith('@') ? v.slice(1).toLowerCase() : v.toLowerCase()
})

const filtered = computed(() => {
  const q = query.value
  if (!q && !open.value) return agents.value.slice(0, 8)
  return agents.value.filter(a => String(a.name).toLowerCase().includes(q)).slice(0, 8)
})

// Valid = empty (optional field) OR exactly matches a known agent name
const isValid = computed(() => {
  const v = (props.modelValue ?? '').trim()
  if (!v) return true
  return agents.value.some(a => String(a.name) === v)
})

// Only show invalid state after the agents list has loaded
const invalid = computed(() => agents.value.length > 0 && !isValid.value)

watch(isValid, (v) => emit('update:valid', v), { immediate: true })

function onInput(e: Event) {
  const val = (e.target as HTMLInputElement).value
  emit('update:modelValue', val)
  open.value = true
  cursor.value = 0
}

function onFocus() {
  open.value = true
  cursor.value = 0
}

function onKeydown(e: KeyboardEvent) {
  if (!open.value) return
  if (e.key === 'ArrowDown') { e.preventDefault(); cursor.value = Math.min(cursor.value + 1, filtered.value.length - 1) }
  if (e.key === 'ArrowUp')   { e.preventDefault(); cursor.value = Math.max(cursor.value - 1, 0) }
  if (e.key === 'Enter')     { e.preventDefault(); const a = filtered.value[cursor.value]; if (a) select(a) }
  if (e.key === 'Escape')    { open.value = false }
}

function select(agent: Record<string, unknown>) {
  emit('update:modelValue', String(agent.name))
  emit('select:agent', agent)
  open.value = false
}

function onOutside(e: MouseEvent) {
  if (!root.value?.contains(e.target as Node)) open.value = false
}
</script>

<style scoped>
.dropdown-enter-active, .dropdown-leave-active { transition: opacity 0.12s ease, transform 0.12s ease; }
.dropdown-enter-from, .dropdown-leave-to { opacity: 0; transform: translateY(-4px); }
</style>
