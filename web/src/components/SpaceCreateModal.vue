<template>
  <!-- Backdrop -->
  <Transition name="backdrop">
    <div
      class="fixed inset-0 z-50 flex items-center justify-center p-4"
      style="background:rgba(0,0,0,0.72);backdrop-filter:blur(4px)"
      @click.self="$emit('close')"
    >
      <!-- Modal card -->
      <Transition name="modal">
        <div
          class="w-[460px] max-w-[95vw] rounded-2xl shadow-2xl overflow-hidden flex flex-col"
          style="background:#13181f;border:1px solid rgba(48,54,61,0.9);box-shadow:0 24px 80px rgba(0,0,0,0.6),0 0 0 1px rgba(255,255,255,0.04) inset"
          @click.stop
        >
          <!-- Header -->
          <div class="px-6 pt-6 pb-5 flex items-start justify-between" style="border-bottom:1px solid rgba(48,54,61,0.6)">
            <div class="flex items-center gap-3">
              <div class="w-9 h-9 rounded-xl flex items-center justify-center flex-shrink-0"
                style="background:rgba(88,166,255,0.12);border:1px solid rgba(88,166,255,0.2)">
                <svg class="w-4 h-4 text-huginn-blue" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
                  <path d="M21 15a2 2 0 01-2 2H7l-4 4V5a2 2 0 012-2h14a2 2 0 012 2z"/>
                </svg>
              </div>
              <div>
                <h2 class="text-sm font-semibold text-white leading-tight" style="font-family:system-ui,-apple-system,sans-serif">New Channel</h2>
                <p class="text-[11px] mt-0.5" style="color:rgba(139,148,158,0.8);font-family:system-ui,-apple-system,sans-serif">Create a shared space for your agent team</p>
              </div>
            </div>
            <button @click="$emit('close')"
              class="w-7 h-7 rounded-lg flex items-center justify-center transition-colors duration-150 flex-shrink-0 mt-0.5"
              style="color:rgba(139,148,158,0.7)"
              onmouseover="this.style.background='rgba(48,54,61,0.8)';this.style.color='rgba(230,237,243,0.9)'"
              onmouseout="this.style.background='transparent';this.style.color='rgba(139,148,158,0.7)'"
            >
              <svg class="w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round">
                <line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/>
              </svg>
            </button>
          </div>

          <!-- Body -->
          <div class="px-6 py-5 flex flex-col gap-5 overflow-y-auto" style="max-height:60vh">

            <!-- Channel name -->
            <div class="flex flex-col gap-2">
              <label class="text-[11px] font-semibold uppercase tracking-widest" style="color:rgba(139,148,158,0.6);font-family:system-ui,-apple-system,sans-serif">
                Channel Name
              </label>
              <input
                ref="nameInputRef"
                v-model="form.name"
                data-testid="space-name-input"
                class="w-full px-4 py-2.5 rounded-xl text-sm outline-none transition-all duration-150"
                style="background:rgba(22,27,34,0.8);border:1px solid rgba(48,54,61,0.8);color:#e6edf3;font-family:system-ui,-apple-system,sans-serif"
                placeholder="e.g. Product Planning"
                maxlength="80"
                @focus="e => (e.target as HTMLElement).style.borderColor='rgba(88,166,255,0.5)'"
                @blur="e => (e.target as HTMLElement).style.borderColor='rgba(48,54,61,0.8)'"
                @keydown.enter="canCreate && create()"
              />
            </div>

            <!-- Lead agent -->
            <div class="flex flex-col gap-2">
              <label class="text-[11px] font-semibold uppercase tracking-widest" style="color:rgba(139,148,158,0.6);font-family:system-ui,-apple-system,sans-serif">
                Lead Agent
              </label>
              <!-- Custom dropdown -->
              <div class="relative" ref="leadDropdownRef">
                <button
                  type="button"
                  data-testid="lead-agent-select"
                  @click="leadOpen = !leadOpen"
                  class="w-full flex items-center gap-3 px-4 py-2.5 rounded-xl text-sm text-left outline-none transition-all duration-150"
                  :style="`background:rgba(22,27,34,0.8);border:1px solid ${leadOpen ? 'rgba(88,166,255,0.5)' : 'rgba(48,54,61,0.8)'};color:#e6edf3;font-family:system-ui,-apple-system,sans-serif`"
                >
                  <template v-if="form.leadAgent">
                    <span class="w-6 h-6 rounded-lg flex items-center justify-center text-[11px] font-bold flex-shrink-0"
                      :style="`background:${leadAgentObj?.color ?? '#58a6ff'}22;color:${leadAgentObj?.color ?? '#58a6ff'}`">
                      {{ leadAgentObj?.icon ?? form.leadAgent[0] }}
                    </span>
                    <span class="flex-1 font-medium">{{ form.leadAgent }}</span>
                  </template>
                  <span v-else class="flex-1" style="color:rgba(139,148,158,0.5)">Select lead agent…</span>
                  <svg class="w-4 h-4 flex-shrink-0 transition-transform duration-150" :class="leadOpen ? 'rotate-180' : ''"
                    style="color:rgba(139,148,158,0.5)" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
                    <polyline points="6 9 12 15 18 9"/>
                  </svg>
                </button>

                <!-- Dropdown list -->
                <Transition name="dropdown">
                  <div v-if="leadOpen"
                    class="absolute top-full mt-1.5 left-0 right-0 rounded-xl overflow-hidden z-10"
                    style="background:#1c2129;border:1px solid rgba(48,54,61,0.9);box-shadow:0 12px 40px rgba(0,0,0,0.5)">
                    <div class="max-h-[180px] overflow-y-auto p-1">
                      <button
                        v-for="a in agents"
                        :key="a.name"
                        type="button"
                        @click="selectLead(a)"
                        class="w-full flex items-center gap-3 px-3 py-2.5 rounded-lg text-sm text-left transition-colors duration-100"
                        :style="`color:${form.leadAgent === a.name ? '#e6edf3' : 'rgba(139,148,158,0.9)'};background:${form.leadAgent === a.name ? 'rgba(88,166,255,0.1)' : 'transparent'};font-family:system-ui,-apple-system,sans-serif`"
                        @mouseover="e => { if(form.leadAgent !== a.name)(e.currentTarget as HTMLElement).style.background='rgba(48,54,61,0.5)' }"
                        @mouseout="e => { if(form.leadAgent !== a.name)(e.currentTarget as HTMLElement).style.background='transparent' }"
                      >
                        <span class="w-6 h-6 rounded-lg flex items-center justify-center text-[11px] font-bold flex-shrink-0"
                          :style="`background:${a.color}22;color:${a.color}`">{{ a.icon }}</span>
                        <span class="flex-1 font-medium">{{ a.name }}</span>
                        <svg v-if="form.leadAgent === a.name" class="w-3.5 h-3.5 flex-shrink-0"
                          style="color:#58a6ff" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round">
                          <polyline points="20 6 9 17 4 12"/>
                        </svg>
                      </button>
                    </div>
                  </div>
                </Transition>
              </div>
            </div>

            <!-- Member agents -->
            <div class="flex flex-col gap-2">
              <div class="flex items-center justify-between">
                <label class="text-[11px] font-semibold uppercase tracking-widest" style="color:rgba(139,148,158,0.6);font-family:system-ui,-apple-system,sans-serif">
                  Member Agents
                </label>
                <span v-if="form.members.length > 0" class="text-[11px] px-1.5 py-0.5 rounded-md font-medium" style="background:rgba(88,166,255,0.12);color:#58a6ff;font-family:system-ui,-apple-system,sans-serif">
                  {{ form.members.length }} selected
                </span>
              </div>

              <div class="rounded-xl overflow-hidden" style="border:1px solid rgba(48,54,61,0.8)">
                <div v-if="otherAgents.length === 0" class="px-4 py-5 text-center">
                  <p class="text-xs" style="color:rgba(139,148,158,0.5);font-family:system-ui,-apple-system,sans-serif">
                    {{ agents.length <= 1 ? 'Add more agents to build a team.' : 'Select a lead agent first.' }}
                  </p>
                </div>
                <div v-else class="max-h-[180px] overflow-y-auto">
                  <button
                    v-for="(a, i) in otherAgents"
                    :key="a.name"
                    type="button"
                    :data-testid="`member-agent-${a.name}`"
                    @click="toggleMember(a.name)"
                    class="w-full flex items-center gap-3 px-4 py-2.5 text-left transition-colors duration-100"
                    :style="`background:${form.members.includes(a.name) ? 'rgba(88,166,255,0.07)' : 'transparent'};border-top:${i > 0 ? '1px solid rgba(48,54,61,0.4)' : 'none'}`"
                    @mouseover="e => { if(!form.members.includes(a.name))(e.currentTarget as HTMLElement).style.background='rgba(48,54,61,0.4)' }"
                    @mouseout="e => { if(!form.members.includes(a.name))(e.currentTarget as HTMLElement).style.background='transparent' }"
                  >
                    <span class="w-7 h-7 rounded-lg flex items-center justify-center text-[11px] font-bold flex-shrink-0"
                      :style="`background:${a.color}22;color:${a.color}`">{{ a.icon }}</span>
                    <span class="flex-1 text-sm font-medium" style="color:#e6edf3;font-family:system-ui,-apple-system,sans-serif">{{ a.name }}</span>

                    <!-- Check indicator -->
                    <div class="w-5 h-5 rounded-md flex items-center justify-center flex-shrink-0 transition-all duration-150"
                      :style="form.members.includes(a.name)
                        ? 'background:rgba(88,166,255,0.2);border:1.5px solid rgba(88,166,255,0.6)'
                        : 'background:rgba(22,27,34,0.8);border:1.5px solid rgba(48,54,61,0.8)'">
                      <svg v-if="form.members.includes(a.name)" class="w-3 h-3" style="color:#58a6ff"
                        viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="3" stroke-linecap="round">
                        <polyline points="20 6 9 17 4 12"/>
                      </svg>
                    </div>
                  </button>
                </div>
              </div>
            </div>

            <!-- Error -->
            <div v-if="createError"
              class="flex items-center gap-2.5 px-4 py-3 rounded-xl text-xs"
              style="background:rgba(248,81,73,0.08);border:1px solid rgba(248,81,73,0.25);color:#f85149;font-family:system-ui,-apple-system,sans-serif">
              <svg class="w-3.5 h-3.5 flex-shrink-0" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
                <circle cx="12" cy="12" r="10"/><line x1="12" y1="8" x2="12" y2="12"/><line x1="12" y1="16" x2="12.01" y2="16"/>
              </svg>
              {{ createError }}
            </div>
          </div>

          <!-- Footer -->
          <div class="px-6 py-4 flex items-center justify-end gap-3" style="border-top:1px solid rgba(48,54,61,0.6)">
            <button
              @click="$emit('close')"
              data-testid="space-create-cancel"
              class="px-4 py-2 rounded-xl text-sm font-medium transition-all duration-150"
              style="color:rgba(139,148,158,0.8);background:transparent;border:1px solid rgba(48,54,61,0.7);font-family:system-ui,-apple-system,sans-serif"
              @mouseover="e => { (e.currentTarget as HTMLElement).style.borderColor='rgba(139,148,158,0.5)'; (e.currentTarget as HTMLElement).style.color='rgba(230,237,243,0.9)' }"
              @mouseout="e => { (e.currentTarget as HTMLElement).style.borderColor='rgba(48,54,61,0.7)'; (e.currentTarget as HTMLElement).style.color='rgba(139,148,158,0.8)' }"
            >
              Cancel
            </button>
            <button
              :disabled="!canCreate || creating"
              data-testid="space-create-submit"
              @click="create"
              class="px-5 py-2 rounded-xl text-sm font-semibold transition-all duration-150 flex items-center gap-2"
              :style="canCreate && !creating
                ? 'background:rgba(88,166,255,0.15);border:1px solid rgba(88,166,255,0.4);color:#58a6ff;font-family:system-ui,-apple-system,sans-serif'
                : 'background:rgba(88,166,255,0.05);border:1px solid rgba(88,166,255,0.1);color:rgba(88,166,255,0.3);cursor:not-allowed;font-family:system-ui,-apple-system,sans-serif'"
              @mouseover="e => { if(canCreate && !creating){ (e.currentTarget as HTMLElement).style.background='rgba(88,166,255,0.22)'; (e.currentTarget as HTMLElement).style.borderColor='rgba(88,166,255,0.55)' } }"
              @mouseout="e => { if(canCreate && !creating){ (e.currentTarget as HTMLElement).style.background='rgba(88,166,255,0.15)'; (e.currentTarget as HTMLElement).style.borderColor='rgba(88,166,255,0.4)' } }"
            >
              <svg v-if="creating" class="w-3.5 h-3.5 animate-spin" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round">
                <path d="M21 12a9 9 0 11-6.219-8.56"/>
              </svg>
              {{ creating ? 'Creating…' : 'Create Channel' }}
            </button>
          </div>
        </div>
      </Transition>
    </div>
  </Transition>
</template>

<script setup lang="ts">
import { ref, computed, nextTick, onMounted, onBeforeUnmount } from 'vue'
import { api } from '../composables/useApi'
import { useSpaces } from '../composables/useSpaces'

const emit = defineEmits<{
  close: []
  created: [spaceId: string]
}>()

interface Agent { name: string; color: string; icon: string; model?: string }

const spacesApi = useSpaces()
const nameInputRef = ref<HTMLInputElement | null>(null)
const leadDropdownRef = ref<HTMLElement | null>(null)
const agents = ref<Agent[]>([])
const form = ref({ name: '', leadAgent: '', members: [] as string[] })
const creating = ref(false)
const createError = ref<string | null>(null)
const leadOpen = ref(false)

const leadAgentObj = computed(() => agents.value.find(a => a.name === form.value.leadAgent))
const otherAgents = computed(() => agents.value.filter(a => a.name !== form.value.leadAgent))
const canCreate = computed(() => form.value.name.trim().length > 0 && form.value.leadAgent !== '')

function selectLead(a: Agent) {
  form.value.leadAgent = a.name
  // Remove from members if selected as lead
  form.value.members = form.value.members.filter(m => m !== a.name)
  leadOpen.value = false
}

function toggleMember(name: string) {
  const idx = form.value.members.indexOf(name)
  if (idx >= 0) form.value.members.splice(idx, 1)
  else form.value.members.push(name)
}

function onDocClick(e: MouseEvent) {
  if (leadOpen.value && leadDropdownRef.value && !leadDropdownRef.value.contains(e.target as Node)) {
    leadOpen.value = false
  }
}

async function create() {
  if (!canCreate.value || creating.value) return
  creating.value = true
  createError.value = null
  try {
    const sp = await spacesApi.createChannel({
      name: form.value.name.trim(),
      leadAgent: form.value.leadAgent,
      memberAgents: form.value.members,
    })
    if (sp) {
      emit('created', sp.id)
      emit('close')
    } else {
      createError.value = spacesApi.error.value ?? 'Failed to create channel.'
    }
  } catch {
    createError.value = 'An unexpected error occurred.'
  } finally {
    creating.value = false
  }
}

onMounted(async () => {
  try {
    const all = await api.agents.list() as unknown as Agent[]
    agents.value = all.filter(a => !!a.model)
  } catch { /* ignore */ }
  await nextTick()
  nameInputRef.value?.focus()
  document.addEventListener('click', onDocClick, true)
})

onBeforeUnmount(() => {
  document.removeEventListener('click', onDocClick, true)
})
</script>

<style scoped>
.backdrop-enter-active,
.backdrop-leave-active { transition: opacity 0.18s ease; }
.backdrop-enter-from,
.backdrop-leave-to { opacity: 0; }

.modal-enter-active { transition: opacity 0.2s ease, transform 0.2s cubic-bezier(0.34,1.2,0.64,1); }
.modal-leave-active { transition: opacity 0.15s ease, transform 0.15s ease; }
.modal-enter-from { opacity: 0; transform: scale(0.96) translateY(4px); }
.modal-leave-to { opacity: 0; transform: scale(0.97); }

.dropdown-enter-active { transition: opacity 0.12s ease, transform 0.12s cubic-bezier(0.34,1.2,0.64,1); }
.dropdown-leave-active { transition: opacity 0.08s ease; }
.dropdown-enter-from { opacity: 0; transform: scaleY(0.92) translateY(-4px); transform-origin: top; }
.dropdown-leave-to { opacity: 0; }

::-webkit-scrollbar { width: 4px; }
::-webkit-scrollbar-track { background: transparent; }
::-webkit-scrollbar-thumb { background: rgba(48,54,61,0.8); border-radius: 4px; }
</style>
