<template>
  <Transition name="backdrop" appear>
    <div
      class="fixed inset-0 z-[100] flex items-end justify-center sm:items-center p-4 sm:p-6"
      style="background:rgba(0,0,0,0.72);backdrop-filter:blur(6px)"
      @click.self="$emit('close')"
    >
      <Transition name="sheet" appear>
        <div
          class="relative w-full max-w-lg rounded-2xl overflow-hidden"
          style="background:#12171e;border:1px solid rgba(255,255,255,0.07);box-shadow:0 40px 120px rgba(0,0,0,0.8),0 0 0 1px rgba(255,255,255,0.04) inset"
        >
          <!-- ── Header ─────────────────────────────────────────────── -->
          <div class="flex items-center gap-3 px-5 py-4 border-b" style="border-color:rgba(255,255,255,0.06)">
            <div
              class="w-9 h-9 rounded-xl flex items-center justify-center flex-shrink-0 text-sm font-bold select-none"
              :style="`background:${space.color}18;color:${space.color};border:1px solid ${space.color}22`"
            >{{ space.icon || (space.kind === 'channel' ? '#' : '·') }}</div>
            <div class="flex-1 min-w-0">
              <h2 class="text-sm font-semibold text-huginn-text truncate">{{ space.name }}</h2>
              <p class="text-[11px] text-huginn-muted/70">{{ allAgentNames.length }} {{ allAgentNames.length === 1 ? 'agent' : 'agents' }}</p>
            </div>
            <button
              @click="$emit('close')"
              class="w-7 h-7 rounded-lg flex items-center justify-center text-huginn-muted/60 hover:text-huginn-text hover:bg-huginn-surface/60 transition-all duration-150 flex-shrink-0"
            >
              <svg class="w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round">
                <line x1="18" y1="6" x2="6" y2="18" /><line x1="6" y1="6" x2="18" y2="18" />
              </svg>
            </button>
          </div>

          <!-- ── Agent list ─────────────────────────────────────────── -->
          <div class="max-h-[60vh] overflow-y-auto" style="scrollbar-width:thin;scrollbar-color:rgba(255,255,255,0.06) transparent">
            <div v-for="agName in allAgentNames" :key="agName">

              <!-- Agent row -->
              <div
                class="flex items-center gap-3 px-5 py-3.5 border-b cursor-pointer select-none transition-colors duration-100"
                style="border-color:rgba(255,255,255,0.04)"
                :class="expandedAgent === agName ? 'bg-huginn-surface/20' : 'hover:bg-huginn-surface/10'"
                @click="toggleExpand(agName)"
              >
                <!-- Avatar -->
                <div
                  class="w-8 h-8 rounded-xl flex items-center justify-center text-sm font-bold flex-shrink-0"
                  :style="`background:${getAgentColor(agName)}18;color:${getAgentColor(agName)};border:1px solid ${getAgentColor(agName)}22`"
                >{{ getAgentIcon(agName) }}</div>

                <!-- Name -->
                <div class="flex-1 min-w-0">
                  <div class="text-sm font-medium text-huginn-text truncate">{{ agName }}</div>
                  <div class="text-[11px] text-huginn-muted/65 truncate">{{ getAgentModel(agName) }}</div>
                </div>

                <!-- Role badge -->
                <span
                  v-if="agName === space.leadAgent"
                  class="text-[10px] font-bold uppercase tracking-wider px-2 py-0.5 rounded-full flex-shrink-0"
                  :style="`background:${space.color}15;color:${space.color};border:1px solid ${space.color}20`"
                >Lead</span>
                <span v-else class="text-[10px] text-huginn-muted/55 flex-shrink-0">Member</span>

                <!-- Chevron -->
                <svg
                  class="w-3.5 h-3.5 text-huginn-muted/45 transition-transform duration-200 flex-shrink-0"
                  :class="expandedAgent === agName ? 'rotate-180 text-huginn-muted/60' : ''"
                  viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"
                >
                  <polyline points="6 9 12 15 18 9" />
                </svg>
              </div>

              <!-- Expanded details -->
              <Transition name="expand">
                <div
                  v-if="expandedAgent === agName"
                  class="border-b overflow-hidden"
                  style="border-color:rgba(255,255,255,0.04);background:rgba(0,0,0,0.15)"
                >
                  <!-- Loading state -->
                  <div v-if="loadingDetails.has(agName)" class="px-5 py-4 flex items-center gap-2">
                    <span class="w-1 h-1 rounded-full bg-huginn-muted/40 animate-bounce" style="animation-delay:0ms" />
                    <span class="w-1 h-1 rounded-full bg-huginn-muted/40 animate-bounce" style="animation-delay:120ms" />
                    <span class="w-1 h-1 rounded-full bg-huginn-muted/40 animate-bounce" style="animation-delay:240ms" />
                  </div>

                  <!-- Details content -->
                  <div v-else class="px-5 py-4 space-y-4">

                    <!-- Capability badges row -->
                    <div class="flex flex-wrap gap-1.5">
                      <!-- Model -->
                      <span class="inline-flex items-center gap-1 px-2 py-1 rounded-lg text-[11px] font-medium"
                        style="background:rgba(88,166,255,0.08);color:rgba(88,166,255,0.9);border:1px solid rgba(88,166,255,0.15)">
                        <svg class="w-3 h-3" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
                          <circle cx="12" cy="12" r="3"/><path d="M12 2v2M12 20v2M4.22 4.22l1.42 1.42M18.36 18.36l1.42 1.42M2 12h2M20 12h2M4.22 19.78l1.42-1.42M18.36 5.64l1.42-1.42"/>
                        </svg>
                        {{ getDetailModel(agName) }}
                      </span>

                      <!-- Memory mode -->
                      <span v-if="getDetailMemoryMode(agName)"
                        class="inline-flex items-center gap-1 px-2 py-1 rounded-lg text-[11px] font-medium"
                        style="background:rgba(63,185,80,0.08);color:rgba(63,185,80,0.9);border:1px solid rgba(63,185,80,0.15)">
                        <svg class="w-3 h-3" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
                          <path d="M12 2a9 9 0 110 18A9 9 0 0112 2z"/><path d="M9 9h6M9 12h6M9 15h4"/>
                        </svg>
                        {{ getDetailMemoryMode(agName) }}
                        <span v-if="getDetailVaultName(agName)" class="text-huginn-muted/60 font-mono"> · {{ getDetailVaultName(agName) }}</span>
                      </span>

                      <!-- Connections count -->
                      <span v-if="getDetailConnectionCount(agName) > 0"
                        class="inline-flex items-center gap-1 px-2 py-1 rounded-lg text-[11px] font-medium"
                        style="background:rgba(227,179,65,0.08);color:rgba(227,179,65,0.9);border:1px solid rgba(227,179,65,0.15)">
                        <svg class="w-3 h-3" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
                          <path d="M10 13a5 5 0 007.54.54l3-3a5 5 0 00-7.07-7.07l-1.72 1.71M14 11a5 5 0 00-7.54-.54l-3 3a5 5 0 007.07 7.07l1.71-1.71"/>
                        </svg>
                        {{ getDetailConnectionCount(agName) }} {{ getDetailConnectionCount(agName) === 1 ? 'connection' : 'connections' }}
                      </span>
                      <span v-else
                        class="inline-flex items-center gap-1 px-2 py-1 rounded-lg text-[11px]"
                        style="background:rgba(255,255,255,0.03);color:rgba(255,255,255,0.3);border:1px solid rgba(255,255,255,0.06)">
                        No connections
                      </span>
                    </div>

                    <!-- Connections list -->
                    <div v-if="getDetailConnections(agName).length > 0" class="space-y-1">
                      <p class="text-[10px] font-semibold text-huginn-muted/40 uppercase tracking-widest mb-2">Connections</p>
                      <div class="flex flex-wrap gap-1.5">
                        <span
                          v-for="conn in getDetailConnections(agName)"
                          :key="conn"
                          class="inline-flex items-center px-2 py-0.5 rounded-md text-[11px] text-huginn-muted/70"
                          style="background:rgba(255,255,255,0.04);border:1px solid rgba(255,255,255,0.07)"
                        >{{ conn }}</span>
                      </div>
                    </div>

                    <!-- System prompt preview -->
                    <div v-if="getDetailSystemPrompt(agName)" class="space-y-1.5">
                      <p class="text-[10px] font-semibold text-huginn-muted/40 uppercase tracking-widest">Persona</p>
                      <p class="text-[11px] text-huginn-muted/60 leading-relaxed line-clamp-2">{{ getDetailSystemPrompt(agName) }}</p>
                    </div>

                    <!-- Actions -->
                    <div v-if="space.kind === 'channel'" class="flex items-center gap-2 pt-1">
                      <!-- Set as Lead (members only) -->
                      <button
                        v-if="agName !== space.leadAgent"
                        @click.stop="setAsLead(agName)"
                        :disabled="saving"
                        class="flex items-center gap-1.5 px-3 py-1.5 rounded-lg text-xs font-medium transition-all duration-150 active:scale-95"
                        :class="saving ? 'text-huginn-muted/30 cursor-not-allowed border border-huginn-border/20' : 'text-huginn-blue border border-huginn-blue/25 hover:bg-huginn-blue/10 hover:border-huginn-blue/40'"
                      >
                        <svg class="w-3 h-3" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round">
                          <polygon points="12 2 15.09 8.26 22 9.27 17 14.14 18.18 21.02 12 17.77 5.82 21.02 7 14.14 2 9.27 8.91 8.26 12 2"/>
                        </svg>
                        Set as Lead
                      </button>

                      <div class="flex-1" />

                      <!-- Remove member -->
                      <button
                        v-if="agName !== space.leadAgent"
                        @click.stop="removeAgent(agName)"
                        :disabled="saving"
                        class="flex items-center gap-1.5 px-3 py-1.5 rounded-lg text-xs font-medium transition-all duration-150 active:scale-95"
                        :class="saving ? 'text-huginn-muted/20 cursor-not-allowed' : 'text-huginn-red/70 border border-huginn-red/20 hover:bg-huginn-red/10 hover:border-huginn-red/40 hover:text-huginn-red'"
                      >
                        <svg class="w-3 h-3" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round">
                          <path d="M16 21v-2a4 4 0 00-4-4H6a4 4 0 00-4 4v2"/><circle cx="9" cy="7" r="4"/><line x1="17" y1="8" x2="23" y2="14"/><line x1="23" y1="8" x2="17" y2="14"/>
                        </svg>
                        Remove
                      </button>
                    </div>
                  </div>
                </div>
              </Transition>

            </div><!-- end v-for -->
          </div>

          <!-- ── Add agent section (channels only) ─────────────────── -->
          <div v-if="space.kind === 'channel'" class="px-5 py-3 border-t" style="border-color:rgba(255,255,255,0.06)">
            <!-- All members already added -->
            <p v-if="availableToAdd.length === 0"
              class="text-center text-[11px] text-huginn-muted/55 py-1">
              All available agents are members
            </p>

            <!-- Search-as-you-type picker -->
            <div v-else ref="searchContainerRef">
              <!-- Input -->
              <div
                class="flex items-center gap-2 px-3 py-2 rounded-xl transition-all duration-200"
                :style="searchFocused
                  ? 'background:rgba(88,166,255,0.05);border:1px solid rgba(88,166,255,0.35)'
                  : 'background:rgba(255,255,255,0.04);border:1px solid rgba(255,255,255,0.07)'"
              >
                <svg class="w-3.5 h-3.5 flex-shrink-0 transition-colors duration-200"
                  :class="searchFocused ? 'text-huginn-blue/70' : 'text-huginn-muted/55'"
                  viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
                  <circle cx="11" cy="11" r="8"/><line x1="21" y1="21" x2="16.65" y2="16.65"/>
                </svg>
                <input
                  v-model="agentSearch"
                  placeholder="Add an agent to this channel…"
                  class="flex-1 bg-transparent text-xs text-huginn-text placeholder-huginn-muted/55 outline-none min-w-0"
                  @focus="onSearchFocus"
                  @blur="onSearchBlur"
                />
                <button v-if="agentSearch" @click="agentSearch = ''"
                  class="w-4 h-4 flex items-center justify-center text-huginn-muted/50 hover:text-huginn-muted/80 transition-colors flex-shrink-0">
                  <svg class="w-2.5 h-2.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round">
                    <line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/>
                  </svg>
                </button>
              </div>

              <!-- Results panel — teleported to body so it escapes overflow:hidden -->
              <Teleport to="body">
                <Transition name="dropdown">
                  <div v-if="searchFocused"
                    class="fixed rounded-2xl overflow-hidden"
                    :style="{
                      ...dropdownPos,
                      background: '#141b25',
                      border: '1px solid rgba(255,255,255,0.1)',
                      boxShadow: '0 -8px 40px rgba(0,0,0,0.7), 0 0 0 1px rgba(255,255,255,0.04) inset',
                      maxHeight: '240px',
                      overflowY: 'auto',
                      scrollbarWidth: 'thin',
                      zIndex: '9999',
                    }"
                  >
                    <div v-if="filteredAvailable.length === 0" class="px-4 py-4 text-center">
                      <p class="text-[12px] text-huginn-muted/40">No agents match "{{ agentSearch }}"</p>
                    </div>

                    <button
                      v-for="(agName, i) in filteredAvailable"
                      :key="agName"
                      @mousedown.prevent="quickAddAgent(agName)"
                      class="w-full flex items-center gap-3 px-4 py-2.5 text-left transition-all duration-100 group/row hover:bg-white/[0.04]"
                      :class="i > 0 ? 'border-t' : ''"
                      style="border-color:rgba(255,255,255,0.05)"
                    >
                      <!-- Avatar -->
                      <span
                        class="w-8 h-8 rounded-xl flex items-center justify-center text-[12px] font-bold flex-shrink-0"
                        :style="`background:${getAgentColor(agName)}1a;color:${getAgentColor(agName)};border:1px solid ${getAgentColor(agName)}30`"
                      >{{ getAgentIcon(agName) }}</span>

                      <!-- Info -->
                      <div class="flex-1 min-w-0">
                        <div class="text-[13px] font-medium text-huginn-text truncate">{{ agName }}</div>
                        <div class="text-[11px] text-huginn-muted/65 truncate">{{ getAgentModel(agName) }}</div>
                      </div>

                      <!-- Add chip — reveals on hover -->
                      <span
                        class="flex items-center gap-1 px-2 py-0.5 rounded-lg text-[11px] font-medium opacity-0 group-hover/row:opacity-100 transition-all duration-150 flex-shrink-0"
                        style="background:rgba(88,166,255,0.15);color:rgba(88,166,255,0.9);border:1px solid rgba(88,166,255,0.2)"
                      >
                        <svg class="w-2.5 h-2.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round">
                          <line x1="12" y1="5" x2="12" y2="19"/><line x1="5" y1="12" x2="19" y2="12"/>
                        </svg>
                        Add
                      </span>
                    </button>
                  </div>
                </Transition>
              </Teleport>
            </div>
          </div>

          <!-- DM note -->
          <div v-else-if="space.kind === 'dm'" class="px-5 py-4 border-t" style="border-color:rgba(255,255,255,0.06)">
            <p class="text-[11px] text-huginn-muted/55 text-center italic">DM conversations are fixed and cannot be modified</p>
          </div>
        </div>
      </Transition>
    </div>
  </Transition>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, nextTick } from 'vue'
import { useAgents } from '../composables/useAgents'
import { useSpaces, type Space } from '../composables/useSpaces'
import { api } from '../composables/useApi'

const props = defineProps<{ space: Space }>()
const emit = defineEmits<{ (e: 'close'): void }>()

const { agents, fetchAgents } = useAgents()
const { updateSpace } = useSpaces()

onMounted(() => {
  if (agents.value.length === 0) fetchAgents()
})

const saving             = ref(false)
const agentSearch        = ref('')
const searchFocused      = ref(false)
const expandedAgent      = ref<string | null>(null)
const searchContainerRef = ref<HTMLElement | null>(null)
const dropdownPos        = ref<Record<string, string>>({})
const agentDetails  = ref<Record<string, Record<string, unknown>>>({})
const loadingDetails = ref<Set<string>>(new Set())

// ── Agent name list ───────────────────────────────────────────────────
const allAgentNames = computed(() => {
  const { leadAgent, memberAgents } = props.space
  return [leadAgent, ...memberAgents.filter(m => m !== leadAgent)]
})

const availableToAdd = computed(() =>
  agents.value.filter(a => !!a.model && !allAgentNames.value.includes(a.name)).map(a => a.name)
)

const filteredAvailable = computed(() => {
  const q = agentSearch.value.trim().toLowerCase()
  if (!q) return availableToAdd.value
  return availableToAdd.value.filter(n => n.toLowerCase().includes(q))
})

// ── Summary helpers (from useAgents) ─────────────────────────────────
function getAgentColor(name: string): string {
  return agents.value.find(a => a.name === name)?.color ?? '#58a6ff'
}
function getAgentIcon(name: string): string {
  const icon = agents.value.find(a => a.name === name)?.icon ?? ''
  // Use icon only if it's a short glyph (emoji = 2 chars via surrogate pair, or 1-char symbol)
  if (icon && icon.length <= 2) return icon
  // Derive initials from name (handles kebab-case, snake_case, spaces)
  const parts = name.split(/[-_\s]+/).filter(Boolean)
  if (parts.length >= 2) return (parts[0]![0]! + parts[1]![0]!).toUpperCase()
  return (name.slice(0, 2).toUpperCase() || '?')
}
function getAgentModel(name: string): string {
  return agents.value.find(a => a.name === name)?.model ?? ''
}

// ── Detail helpers (from lazy-loaded full agent data) ─────────────────
function getDetailModel(name: string): string {
  const d = agentDetails.value[name]
  return (d?.model as string) || getAgentModel(name) || '—'
}
function getDetailMemoryMode(name: string): string {
  const d = agentDetails.value[name]
  return (d?.memory_mode as string) || ''
}
function getDetailVaultName(name: string): string {
  const d = agentDetails.value[name]
  return (d?.vault_name as string) || ''
}
function getDetailConnectionCount(name: string): number {
  const d = agentDetails.value[name]
  if (!d) return 0
  return Array.isArray(d.toolbelt) ? (d.toolbelt as unknown[]).length : 0
}
function getDetailConnections(name: string): string[] {
  const d = agentDetails.value[name]
  if (!d || !Array.isArray(d.toolbelt)) return []
  return (d.toolbelt as Record<string, string>[]).map(t => t.connection_id ?? t.provider ?? '').filter(Boolean)
}
function getDetailSystemPrompt(name: string): string {
  const d = agentDetails.value[name]
  const p = (d?.system_prompt as string) || ''
  return p.trim().slice(0, 200)
}

// ── Expand / lazy load ────────────────────────────────────────────────
async function loadAgentDetails(name: string) {
  if (agentDetails.value[name] || loadingDetails.value.has(name)) return
  loadingDetails.value = new Set([...loadingDetails.value, name])
  try {
    const data = await api.agents.get(name)
    agentDetails.value = { ...agentDetails.value, [name]: data }
  } catch { /* ignore */ } finally {
    const next = new Set(loadingDetails.value)
    next.delete(name)
    loadingDetails.value = next
  }
}

function toggleExpand(name: string) {
  expandedAgent.value = expandedAgent.value === name ? null : name
  if (expandedAgent.value === name) loadAgentDetails(name)
}

// ── Space mutations ───────────────────────────────────────────────────
async function removeAgent(name: string) {
  if (saving.value) return
  saving.value = true
  const newMembers = props.space.memberAgents.filter(m => m !== name)
  await updateSpace(props.space.id, { memberAgents: newMembers })
  if (expandedAgent.value === name) expandedAgent.value = null
  saving.value = false
}

function onSearchFocus() {
  searchFocused.value = true
  nextTick(() => {
    if (!searchContainerRef.value) return
    const rect = searchContainerRef.value.getBoundingClientRect()
    dropdownPos.value = {
      left:   `${rect.left}px`,
      width:  `${rect.width}px`,
      bottom: `${window.innerHeight - rect.top + 8}px`,
    }
  })
}

function onSearchBlur() {
  // Delay so @mousedown.prevent on results fires before blur hides them
  setTimeout(() => { searchFocused.value = false }, 200)
}

async function quickAddAgent(name: string) {
  if (saving.value) return
  saving.value = true
  const newMembers = [...props.space.memberAgents, name]
  await updateSpace(props.space.id, { memberAgents: newMembers })
  agentSearch.value = ''
  searchFocused.value = false
  saving.value = false
}

async function setAsLead(name: string) {
  if (saving.value) return
  saving.value = true
  // New lead: promoted member. Old lead demoted to regular member.
  const oldLead = props.space.leadAgent
  const currentMembers = props.space.memberAgents
  const newMembers = [oldLead, ...currentMembers.filter(m => m !== name)]
  await updateSpace(props.space.id, { leadAgent: name, memberAgents: newMembers })
  expandedAgent.value = null
  saving.value = false
}
</script>

<style scoped>
.backdrop-enter-active, .backdrop-leave-active { transition: opacity 220ms ease; }
.backdrop-enter-from, .backdrop-leave-to { opacity: 0; }

.sheet-enter-active { transition: transform 310ms cubic-bezier(0.34, 1.56, 0.64, 1), opacity 210ms ease; }
.sheet-leave-active { transition: transform 200ms ease, opacity 180ms ease; }
.sheet-enter-from { transform: translateY(28px) scale(0.95); opacity: 0; }
.sheet-leave-to { transform: translateY(12px) scale(0.98); opacity: 0; }

.expand-enter-active { transition: max-height 280ms cubic-bezier(0.4, 0, 0.2, 1), opacity 220ms ease; max-height: 400px; }
.expand-leave-active { transition: max-height 220ms cubic-bezier(0.4, 0, 0.2, 1), opacity 180ms ease; }
.expand-enter-from { max-height: 0; opacity: 0; }
.expand-leave-to { max-height: 0; opacity: 0; }

.dropdown-enter-active { transition: transform 170ms cubic-bezier(0.34, 1.4, 0.64, 1), opacity 140ms ease; }
.dropdown-leave-active { transition: transform 120ms ease, opacity 110ms ease; }
.dropdown-enter-from { transform: translateY(6px) scale(0.96); opacity: 0; }
.dropdown-leave-to { transform: translateY(4px) scale(0.98); opacity: 0; }
</style>
