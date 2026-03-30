<template>
  <Teleport to="body">
    <Transition name="modal">
      <div
        v-if="provider"
        class="fixed inset-0 z-50 flex items-center justify-center p-4 backdrop-blur-sm"
        style="background: rgba(0,0,0,0.5);"
        @click.self="handleBackdropClick"
      >
        <div
          class="modal-panel w-full max-w-[440px] bg-huginn-surface border border-huginn-border rounded-xl shadow-2xl"
          data-testid="modal-panel"
        >
          <!-- Header -->
          <div class="flex items-center gap-3 px-5 py-4 border-b border-huginn-border">
            <div
              class="w-7 h-7 rounded-lg flex items-center justify-center text-[10px] font-bold text-white flex-shrink-0"
              :style="{ backgroundColor: meta.iconColor }"
            >{{ meta.icon }}</div>
            <span class="text-sm font-semibold text-huginn-text flex-1" data-testid="modal-title">
              Connect {{ meta.name }}
            </span>
            <button
              data-testid="btn-close"
              @click="handleClose"
              class="text-huginn-muted hover:text-huginn-text transition-colors text-xl leading-none"
            >×</button>
          </div>

          <!-- Body: catalog-driven generic form OR legacy per-provider form -->
          <div class="px-5 py-4">
            <!-- Async loading state while catalog is being fetched -->
            <div
              v-if="catalogLoading"
              class="text-[11px] text-huginn-muted"
              data-testid="catalog-loading"
            >Loading…</div>

            <!-- Error state: shown when the catalog fetch failed -->
            <div
              v-else-if="catalogError"
              class="text-[11px] text-huginn-red bg-huginn-red/10 border border-huginn-red/30 rounded-lg px-3 py-2"
              data-testid="catalog-error"
            >Could not load form. Please close and try again.</div>

            <!-- Catalog-driven generic form (provider is in the catalog) -->
            <GenericCredentialForm
              v-else-if="catalogEntry != null"
              ref="formRef"
              :fields="catalogEntry.fields"
              :default-label="catalogEntry.default_label"
              :testing="testing"
              :saving="saving"
            />

            <!-- Provider not found in catalog -->
            <div v-else class="text-[11px] text-huginn-red">Unknown provider: {{ provider }}</div>
          </div>

          <!-- Footer: Test btn | feedback message | Connect btn -->
          <div class="flex items-center gap-3 px-5 pb-5">
            <button
              data-testid="btn-test"
              @click="handleTest"
              :disabled="testing || saving || catalogLoading || catalogError"
              class="text-xs text-huginn-muted border border-huginn-border rounded-lg px-4 py-2 hover:bg-huginn-surface/80 transition-colors disabled:opacity-50 flex-shrink-0"
            >{{ testing ? 'Testing…' : 'Test' }}</button>

            <!-- Inline feedback -->
            <div class="flex-1 min-w-0">
              <div
                v-if="testResult"
                data-testid="test-result"
                class="text-[10px] truncate"
                :class="testResult.ok ? 'text-huginn-green' : 'text-huginn-red'"
              >{{ testResult.ok ? '✓ Connection successful' : '✗ ' + testResult.error }}</div>
              <div
                v-else-if="saveMsg"
                data-testid="save-msg"
                class="text-[10px] truncate"
                :class="saveMsg === 'Connected!' ? 'text-huginn-green' : 'text-huginn-red'"
              >{{ saveMsg }}</div>
            </div>

            <button
              data-testid="btn-connect"
              @click="handleConnect"
              :disabled="testing || saving || catalogLoading || catalogError"
              class="text-xs bg-huginn-blue text-huginn-bg font-semibold rounded-lg px-4 py-2 hover:opacity-90 transition-opacity disabled:opacity-50 flex-shrink-0"
            >{{ saving ? 'Connecting…' : 'Connect' }}</button>
          </div>
        </div>
      </div>
    </Transition>
  </Teleport>
</template>

<script setup lang="ts">
import { ref, computed, watch, onMounted, onUnmounted } from 'vue'
import { api } from '../../composables/useApi'
import {
  getCredentialCatalogEntry,
  type CredentialCatalogEntry,
} from '../../composables/useCredentialCatalog'
import GenericCredentialForm from './GenericCredentialForm.vue'

const props = defineProps<{
  provider: string | null
}>()

const emit = defineEmits<{
  close: []
  connected: []
}>()

const formRef = ref<{ getPayload(): Record<string, string> } | null>(null)

// ── Catalog state ─────────────────────────────────────────────────────────────
// undefined  = still loading (shows GenericCredentialForm with loading=true)
// null       = not in catalog (legacy bespoke form rendered instead)
// Entry      = catalog entry found
const catalogEntry   = ref<CredentialCatalogEntry | null | undefined>(undefined)
const catalogLoading = ref(false)
const catalogError   = ref(false)

async function loadCatalogEntry(provider: string) {
  catalogEntry.value   = undefined
  catalogLoading.value = true
  catalogError.value   = false
  try {
    catalogEntry.value = await getCredentialCatalogEntry(provider)
  } catch {
    // Catalog fetch failed — show error state inside GenericCredentialForm.
    catalogEntry.value = undefined // keep "show generic form" branch
    catalogError.value = true
  } finally {
    catalogLoading.value = false
  }
}

// ── Provider meta ─────────────────────────────────────────────────────────────
const meta = computed(() => {
  if (!props.provider) return { name: '', icon: '', iconColor: '#444' }
  if (catalogEntry.value) {
    return {
      name:      catalogEntry.value.name,
      icon:      catalogEntry.value.icon,
      iconColor: catalogEntry.value.icon_color,
    }
  }
  return { name: props.provider, icon: '?', iconColor: '#444' }
})

// ── State reset ───────────────────────────────────────────────────────────────
const testing    = ref(false)
const saving     = ref(false)
const testResult = ref<{ ok: boolean; error?: string } | null>(null)
const saveMsg    = ref('')
const _successTimer = ref<ReturnType<typeof setTimeout> | null>(null)

watch(() => props.provider, (p) => {
  testResult.value = null
  saveMsg.value    = ''
  testing.value    = false
  saving.value     = false
  if (p) {
    loadCatalogEntry(p)
  }
}, { immediate: true })

// ── Keyboard ──────────────────────────────────────────────────────────────────
function onKeyDown(e: KeyboardEvent) {
  if (e.key === 'Escape' && props.provider && !saving.value) handleClose()
}
onMounted(()  => document.addEventListener('keydown', onKeyDown))
onUnmounted(() => {
  document.removeEventListener('keydown', onKeyDown)
  if (_successTimer.value) clearTimeout(_successTimer.value)
})

function handleClose() {
  if (saving.value) return
  emit('close')
}
function handleBackdropClick() { handleClose() }

// ── API routing ───────────────────────────────────────────────────────────────
// Two branches: database (muninn) → dedicated endpoint; everything else → generic.
function resolveApi(entryType: string): {
  test: (payload: Record<string, string>) => Promise<{ ok: boolean; error?: string }>
  save: (payload: Record<string, string>) => Promise<unknown>
} {
  if (entryType === 'database') {
    return {
      test: (p) => api.muninn.test(p),
      save: (p) => api.muninn.connect(p),
    }
  }
  return {
    test: (p) => api.credentials.testGeneric(props.provider!, p),
    save: (p) => api.credentials.saveGeneric(props.provider!, p),
  }
}

// ── Actions ───────────────────────────────────────────────────────────────────
async function handleTest() {
  if (!props.provider || !catalogEntry.value) return
  testing.value    = true
  testResult.value = null
  saveMsg.value    = ''
  try {
    const payload = formRef.value?.getPayload() ?? {}
    testResult.value = await resolveApi(catalogEntry.value.type).test(payload)
  } catch (e) {
    testResult.value = { ok: false, error: e instanceof Error ? e.message : String(e) }
  } finally {
    testing.value = false
  }
}

async function handleConnect() {
  if (!props.provider || !catalogEntry.value) return
  saving.value     = true
  saveMsg.value    = ''
  testResult.value = null
  try {
    const payload = formRef.value?.getPayload() ?? {}
    await resolveApi(catalogEntry.value.type).save(payload)
    saveMsg.value = 'Connected!'
    _successTimer.value = setTimeout(() => emit('connected'), 1500)
  } catch (e) {
    saveMsg.value = e instanceof Error ? e.message : String(e)
  } finally {
    saving.value = false
  }
}
</script>

<style scoped>
.modal-enter-from,
.modal-leave-to {
  opacity: 0;
}
.modal-enter-active {
  transition: opacity 200ms ease;
}
.modal-leave-active {
  transition: opacity 160ms ease;
}
.modal-enter-from .modal-panel {
  transform: translateY(16px) scale(0.97);
  opacity: 0;
}
.modal-leave-to .modal-panel {
  transform: translateY(8px) scale(0.98);
  opacity: 0;
}
.modal-enter-active .modal-panel {
  transition: transform 220ms ease-out, opacity 220ms ease-out;
}
.modal-leave-active .modal-panel {
  transition: transform 160ms ease-in, opacity 160ms ease-in;
}
</style>
