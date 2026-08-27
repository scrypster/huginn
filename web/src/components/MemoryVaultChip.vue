<template>
  <div
    data-testid="memory-vault-chip"
    class="flex-shrink-0 px-5 py-1.5 border-b border-huginn-border/50"
    style="background:rgba(255,255,255,0.02)"
  >
    <div class="flex items-center gap-2">
      <button
        type="button"
        data-testid="memory-vault-chip-action"
        class="flex-1 text-left text-[11px] text-huginn-muted leading-snug hover:text-huginn-text transition-colors"
        @click="onChipClick"
      >{{ chip.text }}</button>
      <button
        type="button"
        class="text-huginn-muted/40 hover:text-huginn-muted text-xs leading-none px-1"
        aria-label="Dismiss memory hint"
        data-testid="memory-vault-chip-dismiss"
        @click="$emit('dismiss')"
      >✕</button>
    </div>
  </div>

  <div
    v-if="open"
    data-testid="memory-vault-modal"
    class="fixed inset-0 z-50 flex items-center justify-center"
    @click.self="close"
  >
    <div class="relative bg-huginn-surface border border-huginn-border rounded-xl shadow-2xl w-96 p-5">
      <p class="text-[12px] text-huginn-text font-medium leading-snug">Memory for {{ agentName }}</p>
      <p
        class="text-[11px] text-huginn-muted leading-snug mt-1 mb-3"
        :data-testid="!busy && !localConnected ? 'memory-vault-offline' : undefined"
      >
        {{ statusLine }}
      </p>

      <p v-if="busy" data-testid="memory-vault-busy" class="text-[11px] text-huginn-muted">Connecting local Muninn…</p>
      <p v-if="error" data-testid="memory-vault-error" class="text-[11px] text-huginn-muted mb-2">{{ error }}</p>

      <template v-if="!busy && localConnected">
        <div v-if="mode === 'existing' && availableVaults.length" class="space-y-1.5">
          <label class="text-[10px] text-huginn-muted uppercase tracking-wide">Existing vault</label>
          <select
            v-model="selectedVault"
            data-testid="memory-vault-existing"
            class="w-full bg-huginn-bg border border-huginn-border rounded-lg px-2 py-1.5 text-[12px] text-huginn-text outline-none"
          >
            <option v-for="v in availableVaults" :key="v" :value="v">{{ v }}</option>
          </select>
          <button
            type="button"
            class="text-[10px] text-huginn-blue/80 hover:text-huginn-blue"
            data-testid="memory-vault-switch-new"
            @click="switchToNew"
          >Create a new vault</button>
        </div>

        <div v-if="mode === 'new'" class="space-y-1.5">
          <label class="text-[10px] text-huginn-muted uppercase tracking-wide">New vault name</label>
          <input
            v-model="newVaultName"
            data-testid="memory-vault-new-name"
            type="text"
            placeholder="Vault name"
            class="w-full bg-huginn-bg border border-huginn-border rounded-lg px-2 py-1.5 text-[12px] text-huginn-text outline-none"
          />
          <p v-if="newNameBlocked" data-testid="memory-vault-in-use" class="text-[10px] text-huginn-muted">
            That vault is already in use.
          </p>
          <button
            v-if="availableVaults.length"
            type="button"
            class="text-[10px] text-huginn-blue/80 hover:text-huginn-blue"
            data-testid="memory-vault-switch-existing"
            @click="mode = 'existing'"
          >Use an existing vault</button>
        </div>

        <div class="space-y-1.5 mt-4" data-testid="memory-vault-modes">
          <p class="text-[10px] text-huginn-muted uppercase tracking-wide">Memory mode</p>
          <div class="space-y-1.5">
            <button
              v-for="m in memoryModes"
              :key="m.value"
              type="button"
              :data-testid="'memory-vault-mode-' + m.value"
              class="w-full flex items-start gap-2.5 px-2.5 py-2 rounded-lg border text-left transition-all"
              :class="memoryMode === m.value
                ? 'border-huginn-blue/50 bg-huginn-blue/5'
                : 'border-huginn-border/30 hover:border-huginn-border/60'"
              @click="memoryMode = m.value"
            >
              <div
                class="w-3.5 h-3.5 rounded-full border-2 flex items-center justify-center shrink-0 mt-0.5"
                :class="memoryMode === m.value ? 'border-huginn-blue' : 'border-huginn-muted/40'"
              >
                <div v-if="memoryMode === m.value" class="w-1.5 h-1.5 rounded-full bg-huginn-blue" />
              </div>
              <div class="min-w-0">
                <p
                  class="text-[11px] font-medium"
                  :class="memoryMode === m.value ? 'text-huginn-text' : 'text-huginn-muted'"
                >{{ m.label }}</p>
                <p class="text-[10px] text-huginn-muted/70 leading-snug mt-0.5">{{ m.description }}</p>
              </div>
            </button>
          </div>
        </div>
      </template>

      <div class="flex justify-end gap-2 mt-4">
        <button
          type="button"
          class="text-[11px] text-huginn-muted px-2 py-1"
          data-testid="memory-vault-cancel"
          @click="close"
        >Cancel</button>
        <button
          v-if="localConnected"
          type="button"
          data-testid="memory-vault-confirm"
          class="text-[11px] text-huginn-text px-2.5 py-1 rounded-lg border border-huginn-border hover:border-huginn-blue/50 disabled:opacity-40"
          :disabled="!canConfirm || busy"
          @click="confirm"
        >{{ confirmLabel }}</button>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { api } from '../composables/useApi'
import type { MemoryChip, MuninnPresence } from '../utils/memoryChip'
import { collectInUseVaults, defaultVaultName, pickPreferredVault, vaultNameInUse } from '../utils/memoryVaultFlow'
import { MEMORY_MODES, normalizeMemoryMode, type MemoryMode } from '../utils/memoryModes'

const props = defineProps<{
  chip: MemoryChip
  agentName: string
  agentVaultName?: string
  agentMemoryMode?: string
  knownAgents?: Array<{ name?: string; vault_name?: string }>
}>()

const emit = defineEmits<{
  dismiss: []
  connected: []
  status: [MuninnPresence]
}>()

const memoryModes = MEMORY_MODES

const open = ref(false)
const busy = ref(false)
const error = ref('')
const mode = ref<'existing' | 'new'>('new')
const availableVaults = ref<string[]>([])
const inUseVaults = ref<string[]>([])
const selectedVault = ref('')
const newVaultName = ref('')
const localConnected = ref(false)
const memoryMode = ref<MemoryMode>(normalizeMemoryMode(props.agentMemoryMode))

const statusLine = computed(() => {
  if (busy.value) return 'Looking for a local Muninn daemon.'
  if (!localConnected.value) return "Muninn isn't running"
  if (mode.value === 'existing') return 'Connect this chat to an existing vault.'
  return 'Name a vault to use with the local daemon.'
})

const confirmLabel = computed(() => (mode.value === 'new' ? 'Create and connect' : 'Connect'))

const newNameBlocked = computed(() =>
  vaultNameInUse(newVaultName.value, inUseVaults.value, props.agentVaultName),
)

const canConfirm = computed(() => {
  if (!localConnected.value) return false
  if (mode.value === 'existing') return !!selectedVault.value.trim()
  const name = newVaultName.value.trim()
  return !!name && !newNameBlocked.value
})

function suggestedName() {
  return defaultVaultName(props.agentName)
}

function switchToNew() {
  mode.value = 'new'
  if (!newVaultName.value.trim()) newVaultName.value = suggestedName()
}

function close() {
  open.value = false
  busy.value = false
  error.value = ''
}

function chosenVault(): string {
  return mode.value === 'existing' ? selectedVault.value.trim() : newVaultName.value.trim()
}

async function onChipClick() {
  open.value = true
  busy.value = true
  error.value = ''
  localConnected.value = false
  newVaultName.value = suggestedName()
  memoryMode.value = normalizeMemoryMode(props.agentMemoryMode)
  try {
    const local = await api.muninn.connectLocal()
    localConnected.value = !!local?.connected || !!local?.ok
    emit('status', {
      connected: !!local?.connected,
      installed: local?.installed,
      running: local?.running,
      detected: local?.detected,
    })
    if (!localConnected.value) return
    const [vaultsRes, agents] = await Promise.all([
      api.muninn.vaults().catch(() => ({ vaults: local?.vaults || [] })),
      api.agents.list().catch(() => props.knownAgents || []),
    ])
    const fromApi = [...new Set([...(local?.vaults || []), ...(vaultsRes.vaults || [])])]
    const collected = collectInUseVaults(fromApi, agents, props.agentName)
    inUseVaults.value = collected.inUse
    availableVaults.value = collected.available
    const pick = pickPreferredVault({
      agentVaultName: props.agentVaultName,
      availableVaults: collected.available,
    })
    mode.value = pick.mode
    selectedVault.value = pick.vault
    if (pick.mode === 'new' && !newVaultName.value.trim()) {
      newVaultName.value = suggestedName()
    }
    if (props.agentVaultName && props.agentVaultName.trim()) {
      await attachVault(props.agentVaultName.trim(), false)
      close()
      emit('connected')
      return
    }
  } catch {
    localConnected.value = false
    error.value = ''
  } finally {
    busy.value = false
  }
}

async function attachVault(vaultName: string, createIfMissing: boolean) {
  if (createIfMissing) {
    try {
      const created = await api.muninn.createVault({
        vault_name: vaultName,
        agent_label: `huginn-${props.agentName}`,
      })
      if (created?.vault_name) vaultName = created.vault_name
    } catch {
      // Local MCP daemon may not expose the old HTTP create path.
      // Still attach the name — the persisted daemon token is enough.
    }
  }
  const current = await api.agents.get(props.agentName)
  await api.agents.update(props.agentName, {
    ...current,
    vault_name: vaultName,
    memory_type: 'muninndb',
    memory_enabled: true,
    context_notes_enabled: false,
    memory_mode: memoryMode.value,
  })
}

async function confirm() {
  const vault = chosenVault()
  if (!vault || (mode.value === 'new' && newNameBlocked.value)) return
  busy.value = true
  error.value = ''
  try {
    if (!localConnected.value) {
      const local = await api.muninn.connectLocal()
      localConnected.value = !!local?.connected || !!local?.ok
      emit('status', {
        connected: !!local?.connected,
        installed: local?.installed,
        running: local?.running,
        detected: local?.detected,
      })
    }
    if (!localConnected.value) return
    await attachVault(vault, mode.value === 'new')
    close()
    emit('connected')
  } catch {
    error.value = "Couldn't connect that vault yet."
  } finally {
    busy.value = false
  }
}
</script>
