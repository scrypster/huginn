<template>
  <Transition name="backdrop">
    <div
      class="fixed inset-0 z-[110] flex items-end justify-center sm:items-center p-4"
      style="background:rgba(0,0,0,0.72);backdrop-filter:blur(6px)"
      data-testid="company-seat-picker"
      @click.self="onBackdrop"
      @keydown="onKey"
    >
      <div
        class="w-[420px] max-w-[95vw] rounded-2xl overflow-hidden flex flex-col"
        style="background:#13181f;border:1px solid rgba(48,54,61,0.9);box-shadow:0 24px 80px rgba(0,0,0,0.6),0 0 0 1px rgba(255,255,255,0.04) inset"
        @click.stop
      >
        <div class="px-6 pt-6 pb-4 flex items-start justify-between" style="border-bottom:1px solid rgba(48,54,61,0.55)">
          <div class="flex items-center gap-3 min-w-0">
            <div class="w-9 h-9 rounded-xl flex items-center justify-center flex-shrink-0 text-sm font-bold"
              :style="headerSwatch">
              {{ headerGlyph }}
            </div>
            <div class="min-w-0">
              <h2 class="text-sm font-semibold text-white leading-tight truncate">{{ title }}</h2>
              <p
                v-if="statusLine"
                data-testid="company-seat-status"
                class="text-[11px] mt-0.5 text-huginn-muted truncate"
              >{{ statusLine }}</p>
              <p v-else class="text-[11px] mt-0.5 text-huginn-muted">{{ subtitle }}</p>
            </div>
          </div>
          <button type="button" class="w-7 h-7 rounded-lg flex items-center justify-center text-huginn-muted hover:text-huginn-text hover:bg-huginn-surface/60"
            data-testid="company-seat-close" @click="$emit('close')">
            <svg class="w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round">
              <line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/>
            </svg>
          </button>
        </div>

        <div class="px-6 py-5 flex flex-col gap-4" style="max-height:62vh;overflow:hidden">
          <template v-if="pending">
            <div data-testid="company-seat-confirm" class="flex flex-col gap-4">
              <p class="text-sm text-huginn-text">{{ pending.prompt }}</p>
              <div class="flex items-center justify-end gap-2">
                <button
                  type="button"
                  data-testid="company-seat-confirm-cancel"
                  class="px-3 py-2 rounded-xl text-xs text-huginn-muted hover:text-huginn-text"
                  @click="cancelPending"
                >Cancel</button>
                <button
                  type="button"
                  :data-testid="pending.action === 'unseat' ? 'company-seat-confirm-remove' : 'company-seat-confirm-add'"
                  class="px-4 py-2 rounded-xl text-xs font-semibold text-white transition-colors"
                  :class="pending.action === 'unseat' ? 'bg-huginn-red hover:bg-huginn-red/90' : 'bg-huginn-blue hover:bg-huginn-blue/90'"
                  @click="commitPending"
                >{{ pending.action === 'unseat' ? 'Remove' : 'Add' }}</button>
              </div>
            </div>
          </template>

          <template v-else-if="resolvedMode === 'people'">
            <CompanyRosterPicker
              :agents="agents"
              :seated="seatedNames"
              :companies="companies"
              :company-id="companyId || ''"
              mode="seat"
              confirm-externally
              @seat="requestSeat($event, companyId || '')"
              @unseat="requestUnseat($event, companyId || '')"
            />
          </template>

          <template v-else>
            <div class="relative">
              <svg class="w-3 h-3 text-huginn-muted absolute left-3 top-1/2 -translate-y-1/2 pointer-events-none"
                viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round">
                <circle cx="11" cy="11" r="8"/><line x1="21" y1="21" x2="16.65" y2="16.65"/>
              </svg>
              <input
                ref="searchInput"
                v-model="query"
                data-testid="company-seat-search"
                type="text"
                placeholder="Search companies…"
                class="w-full pl-8 pr-3 py-2 rounded-xl text-xs outline-none text-huginn-text"
                style="background:rgba(22,27,34,0.8);border:1px solid rgba(48,54,61,0.8)"
              />
            </div>

            <div class="flex flex-col gap-3 min-h-0" style="max-height:42vh;overflow-y:auto;scrollbar-width:thin">
              <div v-if="!companies.length" class="px-1 py-3 text-[11px] text-huginn-muted italic">
                No companies yet
              </div>

              <div v-else-if="!filteredJoinable.length && !filteredSeated.length" class="px-1 py-3 text-[11px] text-huginn-muted italic">
                No matches
              </div>

              <div v-if="filteredJoinable.length" class="flex flex-col gap-1">
                <div class="px-1 text-[10px] font-semibold text-huginn-muted uppercase tracking-widest">
                  Available
                </div>
                <button
                  v-for="(c, i) in filteredJoinable"
                  :key="c.id"
                  type="button"
                  :data-testid="`company-seat-join-${c.id}`"
                  class="w-full flex items-center gap-2.5 px-2.5 py-2 rounded-xl text-left transition-colors"
                  :class="i === highlight ? 'bg-huginn-blue/8 text-huginn-text' : 'text-huginn-text hover:bg-huginn-bg/50'"
                  @click="requestSeat(agentName, c.id)"
                  @mouseenter="highlight = i"
                >
                  <span
                    class="w-7 h-7 rounded-lg flex items-center justify-center text-[11px] font-bold flex-shrink-0"
                    :style="swatch(c)"
                  >{{ glyph(c) }}</span>
                  <span class="flex-1 min-w-0 text-xs truncate">{{ c.name }}</span>
                </button>
              </div>

              <div v-if="filteredSeated.length" class="flex flex-col gap-1">
                <div class="px-1 text-[10px] font-semibold text-huginn-muted uppercase tracking-widest">
                  Seated
                </div>
                <div
                  v-for="c in filteredSeated"
                  :key="c.id"
                  :data-testid="`company-seat-seated-${c.id}`"
                  class="w-full flex items-center gap-2.5 px-2.5 py-2 rounded-xl"
                >
                  <span
                    class="w-7 h-7 rounded-lg flex items-center justify-center text-[11px] font-bold flex-shrink-0"
                    :style="swatch(c)"
                  >{{ glyph(c) }}</span>
                  <span class="flex-1 min-w-0 text-xs text-huginn-text truncate">{{ c.name }}</span>
                  <span class="text-huginn-blue text-[11px]" aria-hidden="true">✓</span>
                  <button
                    type="button"
                    :data-testid="`company-seat-unseat-${c.id}`"
                    class="text-[11px] text-huginn-muted hover:text-huginn-red transition-colors"
                    @click="requestUnseat(agentName, c.id)"
                  >Remove</button>
                </div>
              </div>
            </div>
          </template>
        </div>
      </div>
    </div>
  </Transition>
</template>

<script setup lang="ts">
import { computed, nextTick, onMounted, ref, watch } from 'vue'
import type { Company } from '../composables/useCompanies'
import CompanyRosterPicker, { type RosterAgent } from './CompanyRosterPicker.vue'

const props = withDefaults(defineProps<{
  agent?: string
  companyId?: string
  companies: Company[]
  agents?: RosterAgent[]
  mode?: 'companies' | 'people' | 'confirm'
}>(), { companies: () => [], agents: () => [] })

const emit = defineEmits<{
  close: []
  seat: [agent: string, companyId: string]
  unseat: [agent: string, companyId: string]
}>()

const query = ref('')
const highlight = ref(0)
const searchInput = ref<HTMLInputElement | null>(null)

const resolvedMode = computed(() => {
  if (props.mode) return props.mode
  if (props.agent && props.companyId) return 'confirm'
  if (props.companyId && !props.agent) return 'people'
  return 'companies'
})

const agentName = computed(() => (props.agent || '').trim())

function seatedIn(company: Company, agent: string): boolean {
  if (!agent) return false
  const lower = agent.toLowerCase()
  return company.members.some(m => m.toLowerCase() === lower)
}

const seatedCompanies = computed(() => {
  const agent = agentName.value
  if (!agent) return []
  return props.companies.filter(c => seatedIn(c, agent))
})

const joinableCompanies = computed(() => {
  const agent = agentName.value
  return props.companies.filter(c => !seatedIn(c, agent))
})

function matchesQuery(name: string): boolean {
  const q = query.value.trim().toLowerCase()
  return !q || name.toLowerCase().includes(q)
}

const filteredJoinable = computed(() =>
  joinableCompanies.value.filter(c => matchesQuery(c.name)),
)
const filteredSeated = computed(() =>
  seatedCompanies.value.filter(c => matchesQuery(c.name)),
)

const seatedNames = computed(() => {
  if (resolvedMode.value !== 'people' || !props.companyId) return []
  return props.companies.find(c => c.id === props.companyId)?.members ?? []
})

const companyRec = computed(() =>
  props.companies.find(c => c.id === props.companyId) ?? null,
)

const statusLine = computed(() => {
  if (pending.value) return ''
  if (resolvedMode.value !== 'companies') return ''
  const names = seatedCompanies.value.map(c => c.name)
  if (!names.length) return ''
  return `In ${names.join(', ')}`
})

const title = computed(() => {
  if (resolvedMode.value === 'people') {
    return companyRec.value ? `Add people to ${companyRec.value.name}` : 'Add people'
  }
  if (resolvedMode.value === 'confirm') return agentName.value ? `Add ${agentName.value}` : 'Add to company'
  return agentName.value ? `Add ${agentName.value} to a company` : 'Add to company'
})

const subtitle = computed(() => {
  if (resolvedMode.value === 'people') return 'Known people. Confirm before they sit.'
  if (resolvedMode.value === 'confirm') return ''
  return 'Companies they can join'
})

const headerGlyph = computed(() => {
  if (resolvedMode.value === 'people') {
    const c = companyRec.value
    return c ? glyph(c) : '+'
  }
  const n = agentName.value
  return n ? n.slice(0, 1).toUpperCase() : '+'
})

const headerSwatch = computed(() => {
  if (resolvedMode.value === 'people' && companyRec.value) return swatch(companyRec.value)
  return { background: 'rgba(88,166,255,0.13)', color: '#58a6ff', border: '1px solid rgba(88,166,255,0.2)' }
})

function glyph(c: Company): string {
  return (c.icon && c.icon.length <= 2 ? c.icon : c.name.slice(0, 1)).toUpperCase()
}

function swatch(c: Company): Record<string, string> {
  const color = c.color || '#58a6ff'
  return { background: color + '22', color }
}

interface Pending {
  action: 'seat' | 'unseat'
  agent: string
  companyId: string
  companyName: string
  prompt: string
}

const pending = ref<Pending | null>(null)

function companyName(id: string): string {
  return props.companies.find(c => c.id === id)?.name || 'this company'
}

function requestSeat(agent: string, companyId: string) {
  const name = (agent || '').trim()
  if (!name || !companyId) return
  const rec = props.companies.find(c => c.id === companyId)
  if (rec && seatedIn(rec, name)) return
  const cname = companyName(companyId)
  pending.value = {
    action: 'seat',
    agent: name,
    companyId,
    companyName: cname,
    prompt: `Add ${name} to ${cname}?`,
  }
}

function requestUnseat(agent: string, companyId: string) {
  const name = (agent || '').trim()
  if (!name || !companyId) return
  const cname = companyName(companyId)
  pending.value = {
    action: 'unseat',
    agent: name,
    companyId,
    companyName: cname,
    prompt: `Remove ${name} from ${cname}?`,
  }
}

function cancelPending() {
  const wasConfirmMode = resolvedMode.value === 'confirm'
  pending.value = null
  if (wasConfirmMode) emit('close')
}

function commitPending() {
  const p = pending.value
  if (!p) return
  pending.value = null
  if (p.action === 'unseat') emit('unseat', p.agent, p.companyId)
  else emit('seat', p.agent, p.companyId)
}

function onBackdrop() {
  if (pending.value) {
    cancelPending()
    return
  }
  emit('close')
}

function onKey(e: KeyboardEvent) {
  if (e.key === 'Escape') {
    e.stopPropagation()
    if (pending.value) cancelPending()
    else emit('close')
    return
  }
  if (pending.value || resolvedMode.value !== 'companies') return
  const list = filteredJoinable.value
  if (e.key === 'ArrowDown') {
    e.preventDefault()
    if (!list.length) return
    highlight.value = (highlight.value + 1) % list.length
  } else if (e.key === 'ArrowUp') {
    e.preventDefault()
    if (!list.length) return
    highlight.value = (highlight.value - 1 + list.length) % list.length
  } else if (e.key === 'Enter') {
    e.preventDefault()
    const c = list[highlight.value]
    if (c) requestSeat(agentName.value, c.id)
  }
}

watch(query, () => { highlight.value = 0 })

onMounted(() => {
  if (resolvedMode.value === 'confirm' && agentName.value && props.companyId) {
    requestSeat(agentName.value, props.companyId)
  }
  nextTick(() => searchInput.value?.focus())
})
</script>

<style scoped>
.backdrop-enter-active, .backdrop-leave-active { transition: opacity 0.15s ease; }
.backdrop-enter-from, .backdrop-leave-to { opacity: 0; }
</style>
