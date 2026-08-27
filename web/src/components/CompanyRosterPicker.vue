<template>
  <div data-testid="company-roster-picker" class="flex flex-col gap-3">
    <div v-if="mode === 'seat'" class="relative">
      <svg class="w-3 h-3 text-huginn-muted/40 absolute left-3 top-1/2 -translate-y-1/2 pointer-events-none"
        viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round">
        <circle cx="11" cy="11" r="8"/><line x1="21" y1="21" x2="16.65" y2="16.65"/>
      </svg>
      <input
        v-model="query"
        data-testid="company-roster-search"
        type="text"
        placeholder="Search people…"
        class="w-full pl-8 pr-3 py-2 rounded-xl text-xs outline-none"
        style="background:rgba(22,27,34,0.8);border:1px solid rgba(48,54,61,0.8);color:#e6edf3"
      />
    </div>

    <div v-if="mode === 'roster' && visible.length === 0"
      class="px-1 py-3 text-[11px] text-huginn-muted/50 italic"
      data-testid="company-roster-empty">
      No one seated yet
    </div>

    <div class="flex flex-col gap-1 max-h-64 overflow-y-auto" style="scrollbar-width:thin">
      <div
        v-for="agent in visible"
        :key="agent.name"
        :data-testid="`company-roster-agent-${agent.name}`"
        class="flex items-center gap-2.5 px-2.5 py-2 rounded-xl transition-colors"
        :class="isSeated(agent.name)
          ? 'bg-huginn-blue/8'
          : 'hover:bg-huginn-bg/50'"
      >
        <div class="w-7 h-7 rounded-lg flex items-center justify-center text-[11px] font-bold flex-shrink-0"
          :style="`background:${agent.color || '#58a6ff'}22;color:${agent.color || '#58a6ff'}`">
          {{ agent.icon || agent.name.slice(0, 1).toUpperCase() }}
        </div>
        <div class="flex-1 min-w-0">
          <div class="text-xs text-huginn-text truncate">{{ agent.name }}</div>
          <div class="text-[10px] text-huginn-muted/55 truncate">
            <template v-if="isSeated(agent.name)">Seated</template>
            <template v-else-if="otherCompanyName(agent.name)">
              In {{ otherCompanyName(agent.name) }}
            </template>
            <template v-else>{{ agent.model || 'No model set' }}</template>
          </div>
        </div>

        <button
          v-if="mode === 'seat' && !isSeated(agent.name)"
          type="button"
          :data-testid="`company-roster-seat-${agent.name}`"
          class="px-2 py-1 rounded-lg text-[11px] font-medium text-huginn-blue hover:bg-huginn-blue/10 transition-colors"
          @click="$emit('seat', agent.name)"
        >Seat</button>

        <template v-else-if="mode === 'seat' || mode === 'roster'">
          <span
            v-if="isSeated(agent.name) && pendingUnseat !== agent.name"
            class="text-[10px] text-huginn-muted uppercase tracking-wider"
          >Seated</span>
          <button
            v-if="isSeated(agent.name) && pendingUnseat !== agent.name"
            type="button"
            :data-testid="`company-roster-unseat-${agent.name}`"
            class="w-6 h-6 rounded-md flex items-center justify-center text-huginn-muted hover:text-huginn-red hover:bg-huginn-red/10 transition-colors"
            title="Unseat"
            @click="confirmExternally ? $emit('unseat', agent.name) : (pendingUnseat = agent.name)"
          >
            <svg class="w-3 h-3" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round">
              <line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/>
            </svg>
          </button>
          <div v-if="pendingUnseat === agent.name" class="flex items-center gap-1">
            <button
              type="button"
              :data-testid="`company-roster-unseat-confirm-${agent.name}`"
              class="px-2 py-1 rounded-md text-[10px] font-semibold text-huginn-red bg-huginn-red/10 hover:bg-huginn-red/20"
              @click="confirmUnseat(agent.name)"
            >Unseat</button>
            <button
              type="button"
              class="px-1.5 py-1 rounded-md text-[10px] text-huginn-muted hover:text-huginn-text"
              @click="pendingUnseat = ''"
            >No</button>
          </div>
        </template>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import type { Company } from '../composables/useCompanies'

export interface RosterAgent {
  name: string
  color?: string
  icon?: string
  model?: string
  is_default?: boolean
}

const props = withDefaults(defineProps<{
  agents: RosterAgent[]
  seated: string[]
  companies?: Company[]
  companyId?: string
  /** roster = only people already seated (Lab does not list Huginn-only Reggie). seat = known agents. */
  mode?: 'seat' | 'roster'
  /** Parent shows Add/Remove confirm. Skip the inline Unseat/No row. */
  confirmExternally?: boolean
}>(), { mode: 'seat', companies: () => [], seated: () => [], confirmExternally: false })

const emit = defineEmits<{
  seat: [name: string]
  unseat: [name: string]
}>()

const query = ref('')
const pendingUnseat = ref('')

function seatedSet(): Set<string> {
  return new Set(props.seated.map(n => n.toLowerCase()))
}

function isSeated(name: string): boolean {
  return seatedSet().has(name.toLowerCase())
}

const visible = computed(() => {
  const q = query.value.trim().toLowerCase()
  let list = props.agents
  if (props.mode === 'roster') {
    const seated = seatedSet()
    list = list.filter(a => seated.has(a.name.toLowerCase()))
  }
  if (q) list = list.filter(a => a.name.toLowerCase().includes(q))
  return [...list].sort((a, b) => {
    const as = isSeated(a.name) ? 0 : 1
    const bs = isSeated(b.name) ? 0 : 1
    if (as !== bs) return as - bs
    return a.name.localeCompare(b.name)
  })
})

function otherCompanyName(agent: string): string {
  if (!props.companies?.length) return ''
  const lower = agent.toLowerCase()
  const elsewhere = props.companies.filter(c =>
    c.id !== props.companyId && c.members.some(m => m.toLowerCase() === lower),
  )
  if (!elsewhere.length) return ''
  // Desk people (default / many companies) don't get a "stuck in one" hint.
  const agentRec = props.agents.find(a => a.name.toLowerCase() === lower)
  if (agentRec?.is_default) return ''
  if (elsewhere.length > 1) return ''
  return elsewhere[0]?.name ?? ''
}

function confirmUnseat(name: string) {
  pendingUnseat.value = ''
  emit('unseat', name)
}
</script>
