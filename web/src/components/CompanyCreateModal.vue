<template>
  <Transition name="backdrop">
    <div
      class="fixed inset-0 z-[110] flex items-end justify-center sm:items-center p-4"
      style="background:rgba(0,0,0,0.72);backdrop-filter:blur(6px)"
      data-testid="company-create-modal"
      @click.self="$emit('close')"
    >
      <div
        class="w-[440px] max-w-[95vw] rounded-2xl overflow-hidden flex flex-col"
        style="background:#13181f;border:1px solid rgba(48,54,61,0.9);box-shadow:0 24px 80px rgba(0,0,0,0.6),0 0 0 1px rgba(255,255,255,0.04) inset"
        @click.stop
      >
        <div class="px-6 pt-6 pb-4 flex items-start justify-between" style="border-bottom:1px solid rgba(48,54,61,0.55)">
          <div class="flex items-center gap-3">
            <div class="w-9 h-9 rounded-xl flex items-center justify-center flex-shrink-0 text-sm font-bold"
              :style="`background:${swatch}22;color:${swatch};border:1px solid ${swatch}33`">
              {{ glyph }}
            </div>
            <div>
              <h2 class="text-sm font-semibold text-white leading-tight">
                {{ managing ? 'Company roster' : step === 1 ? 'New company' : 'Seat people' }}
              </h2>
              <p class="text-[11px] mt-0.5 text-huginn-muted/70">
                {{ managing
                  ? (companyName || 'Who is seated here')
                  : step === 1
                    ? 'A company is an isolation boundary — roster, vault, connections.'
                    : 'Pick known people. Desk people can sit in many companies.' }}
              </p>
            </div>
          </div>
          <button type="button" class="w-7 h-7 rounded-lg flex items-center justify-center text-huginn-muted/60 hover:text-huginn-text hover:bg-huginn-surface/60"
            data-testid="company-create-close" @click="$emit('close')">
            <svg class="w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round">
              <line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/>
            </svg>
          </button>
        </div>

        <div class="px-6 py-5 flex flex-col gap-5" style="max-height:62vh;overflow-y:auto">
          <template v-if="step === 1 && !managing">
            <div class="flex flex-col gap-2">
              <label class="text-[11px] font-semibold uppercase tracking-widest text-huginn-muted/55">Company name</label>
              <input
                ref="nameInput"
                v-model="name"
                data-testid="company-name-input"
                maxlength="80"
                placeholder="e.g. Huginn"
                class="w-full px-4 py-2.5 rounded-xl text-sm outline-none"
                style="background:rgba(22,27,34,0.8);border:1px solid rgba(48,54,61,0.8);color:#e6edf3"
                @keydown.enter="canContinue && (step = 2)"
              />
            </div>

            <button type="button"
              data-testid="company-vault-toggle"
              class="flex items-center gap-1.5 text-[11px] text-huginn-muted/50 hover:text-huginn-muted transition-colors self-start"
              @click="showVault = !showVault">
              <svg class="w-2.5 h-2.5 transition-transform" :class="showVault ? 'rotate-90' : ''"
                viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round">
                <polyline points="9 18 15 12 9 6"/>
              </svg>
              Advanced — vault
            </button>
            <div v-if="showVault" class="flex flex-col gap-1.5">
              <input
                v-model="vault"
                data-testid="company-vault-input"
                placeholder="Leave empty — empty stays empty"
                class="w-full px-3 py-2 rounded-xl text-xs outline-none"
                style="background:rgba(22,27,34,0.8);border:1px solid rgba(48,54,61,0.8);color:#e6edf3"
              />
              <p class="text-[10px] text-huginn-muted/45">Optional. An empty vault stays empty — Huginn will not substitute one.</p>
            </div>
          </template>

          <template v-else>
            <CompanyRosterPicker
              :agents="agents"
              :seated="seated"
              :companies="companies"
              :company-id="manageId || ''"
              mode="seat"
              @seat="onSeat"
              @unseat="onUnseat"
            />
          </template>

          <p v-if="error" class="text-[11px] text-huginn-red" data-testid="company-create-error">{{ error }}</p>
        </div>

        <div class="px-6 py-4 flex items-center justify-between gap-3" style="border-top:1px solid rgba(48,54,61,0.55)">
          <button type="button" class="text-xs text-huginn-muted hover:text-huginn-text" @click="onBack">
            {{ managing || step === 1 ? 'Cancel' : 'Back' }}
          </button>
          <button
            v-if="!managing && step === 1"
            type="button"
            data-testid="company-create-continue"
            :disabled="!canContinue"
            class="px-4 py-2 rounded-xl text-xs font-semibold transition-colors"
            :class="canContinue ? 'text-white bg-huginn-blue hover:bg-huginn-blue/90' : 'text-huginn-muted/40 bg-huginn-bg cursor-not-allowed'"
            @click="step = 2"
          >Continue</button>
          <button
            v-else-if="!managing"
            type="button"
            data-testid="company-create-submit"
            :disabled="busy"
            class="px-4 py-2 rounded-xl text-xs font-semibold text-white bg-huginn-blue hover:bg-huginn-blue/90 disabled:opacity-50"
            @click="submit"
          >{{ busy ? 'Creating…' : seated.length ? `Create with ${seated.length}` : 'Create company' }}</button>
          <span v-else class="text-[11px] text-huginn-muted/45">{{ seated.length }} seated</span>
        </div>
      </div>
    </div>
  </Transition>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useAgents } from '../composables/useAgents'
import { useCompanies } from '../composables/useCompanies'
import CompanyRosterPicker from './CompanyRosterPicker.vue'

const PALETTE = ['#58a6ff', '#3fb950', '#d29922', '#f78166', '#a371f7', '#db61a2']

const props = defineProps<{
  manageId?: string
}>()

const emit = defineEmits<{
  close: []
  created: [id: string]
}>()

const { agents, fetchAgents } = useAgents()
const {
  companies, createCompany, seatMember, unseatMember, creating, selectCompany,
} = useCompanies()

const step = ref(1)
const name = ref('')
const vault = ref('')
const showVault = ref(false)
const seated = ref<string[]>([])
const error = ref('')
const nameInput = ref<HTMLInputElement | null>(null)

const managing = computed(() => !!props.manageId)
const busy = computed(() => creating.value)
const companyName = computed(() => {
  if (!props.manageId) return name.value.trim()
  return companies.value.find(c => c.id === props.manageId)?.name || ''
})
const glyph = computed(() => {
  const n = companyName.value || name.value
  return n ? n.slice(0, 1).toUpperCase() : '+'
})
const swatch = computed(() => {
  const n = companyName.value || name.value || 'C'
  return PALETTE[n.charCodeAt(0) % PALETTE.length]
})
const canContinue = computed(() => name.value.trim().length > 0)

onMounted(async () => {
  if (!agents.value.length) await fetchAgents()
  if (props.manageId) {
    step.value = 2
    const c = companies.value.find(x => x.id === props.manageId)
    seated.value = [...(c?.members ?? [])]
    name.value = c?.name ?? ''
  }
  nameInput.value?.focus()
})

function onBack() {
  if (!managing.value && step.value === 2) {
    step.value = 1
    return
  }
  emit('close')
}

function onSeat(agent: string) {
  if (seated.value.some(n => n.toLowerCase() === agent.toLowerCase())) return
  seated.value = [...seated.value, agent]
  if (props.manageId) void seatMember(props.manageId, agent)
}

function onUnseat(agent: string) {
  seated.value = seated.value.filter(n => n.toLowerCase() !== agent.toLowerCase())
  if (props.manageId) void unseatMember(props.manageId, agent)
}

async function submit() {
  error.value = ''
  const created = await createCompany({
    name: name.value.trim(),
    members: seated.value,
    vault: vault.value.trim(),
    color: swatch.value,
    icon: glyph.value,
  })
  if (!created) {
    error.value = 'Could not create company'
    return
  }
  selectCompany(created.id)
  emit('created', created.id)
}
</script>

<style scoped>
.backdrop-enter-active, .backdrop-leave-active { transition: opacity 0.15s ease; }
.backdrop-enter-from, .backdrop-leave-to { opacity: 0; }
</style>
