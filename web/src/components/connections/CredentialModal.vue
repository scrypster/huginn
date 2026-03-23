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

          <!-- Body: dynamic form component -->
          <div class="px-5 py-4">
            <component
              :is="FORM_COMPONENTS[provider]"
              ref="formRef"
              :testing="testing"
              :saving="saving"
            />
          </div>

          <!-- Footer: Test btn | feedback message | Connect btn -->
          <div class="flex items-center gap-3 px-5 pb-5">
            <button
              data-testid="btn-test"
              @click="handleTest"
              :disabled="testing || saving"
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
              :disabled="testing || saving"
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
import { FORM_COMPONENTS, PROVIDER_META, type CredentialProvider } from './forms/index'

const props = defineProps<{
  provider: CredentialProvider | null
}>()

const emit = defineEmits<{
  close: []
  connected: []
}>()

const formRef = ref<{ getPayload(): Record<string, string> } | null>(null)

const meta = computed(() => {
  if (!props.provider) return { name: '', icon: '', iconColor: '#444' }
  return PROVIDER_META[props.provider]
})

const testing    = ref(false)
const saving     = ref(false)
const testResult = ref<{ ok: boolean; error?: string } | null>(null)
const saveMsg    = ref('')
const _successTimer = ref<ReturnType<typeof setTimeout> | null>(null)

watch(() => props.provider, (p) => {
  if (p) {
    testResult.value = null
    saveMsg.value    = ''
    testing.value    = false
    saving.value     = false
  }
})

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

type TestFn = (p: Record<string, string>) => Promise<{ ok: boolean; error?: string }>
type SaveFn = (p: Record<string, string>) => Promise<unknown>
const API_MAP: Record<CredentialProvider, { test: TestFn; save: SaveFn }> = {
  muninn:      { test: api.muninn.test as unknown as TestFn,           save: api.muninn.connect as unknown as SaveFn },
  datadog:     { test: api.credentials.datadogTest as unknown as TestFn,  save: api.credentials.datadogSave as unknown as SaveFn },
  splunk:      { test: api.credentials.splunkTest as unknown as TestFn,   save: api.credentials.splunkSave as unknown as SaveFn },
  slack_bot:   { test: api.credentials.slackBotTest as unknown as TestFn,   save: api.credentials.slackBotSave as unknown as SaveFn },
  jira_sa:     { test: api.credentials.jiraSATest as unknown as TestFn,     save: api.credentials.jiraSASave as unknown as SaveFn },
  linear:      { test: api.credentials.linearTest as unknown as TestFn,     save: api.credentials.linearSave as unknown as SaveFn },
  gitlab:      { test: api.credentials.gitlabTest as unknown as TestFn,     save: api.credentials.gitlabSave as unknown as SaveFn },
  discord:     { test: api.credentials.discordTest as unknown as TestFn,    save: api.credentials.discordSave as unknown as SaveFn },
  vercel:      { test: api.credentials.vercelTest as unknown as TestFn,     save: api.credentials.vercelSave as unknown as SaveFn },
  stripe:      { test: api.credentials.stripeTest as unknown as TestFn,     save: api.credentials.stripeSave as unknown as SaveFn },
  pagerduty:   { test: api.credentials.pagerdutyTest as unknown as TestFn,  save: api.credentials.pagerdutySave as unknown as SaveFn },
  newrelic:    { test: api.credentials.newrelicTest as unknown as TestFn,   save: api.credentials.newrelicSave as unknown as SaveFn },
  elastic:     { test: api.credentials.elasticTest as unknown as TestFn,    save: api.credentials.elasticSave as unknown as SaveFn },
  grafana:     { test: api.credentials.grafanaTest as unknown as TestFn,    save: api.credentials.grafanaSave as unknown as SaveFn },
  crowdstrike: { test: api.credentials.crowdstrikeTest as unknown as TestFn, save: api.credentials.crowdstrikeSave as unknown as SaveFn },
  terraform:   { test: api.credentials.terraformTest as unknown as TestFn,  save: api.credentials.terraformSave as unknown as SaveFn },
  servicenow:  { test: api.credentials.servicenowTest as unknown as TestFn, save: api.credentials.servicenowSave as unknown as SaveFn },
  notion:      { test: api.credentials.notionTest as unknown as TestFn,     save: api.credentials.notionSave as unknown as SaveFn },
  airtable:    { test: api.credentials.airtableTest as unknown as TestFn,   save: api.credentials.airtableSave as unknown as SaveFn },
  hubspot:     { test: api.credentials.hubspotTest as unknown as TestFn,    save: api.credentials.hubspotSave as unknown as SaveFn },
  zendesk:     { test: api.credentials.zendeskTest as unknown as TestFn,    save: api.credentials.zendeskSave as unknown as SaveFn },
  asana:       { test: api.credentials.asanaTest as unknown as TestFn,      save: api.credentials.asanaSave as unknown as SaveFn },
  monday:      { test: api.credentials.mondayTest as unknown as TestFn,     save: api.credentials.mondaySave as unknown as SaveFn },
}

async function handleTest() {
  if (!props.provider) return
  testing.value    = true
  testResult.value = null
  saveMsg.value    = ''
  try {
    const payload = formRef.value?.getPayload() ?? {}
    testResult.value = await API_MAP[props.provider].test(payload)
  } catch (e) {
    testResult.value = { ok: false, error: e instanceof Error ? e.message : String(e) }
  } finally {
    testing.value = false
  }
}

async function handleConnect() {
  if (!props.provider) return
  saving.value     = true
  saveMsg.value    = ''
  testResult.value = null
  try {
    const payload = formRef.value?.getPayload() ?? {}
    await API_MAP[props.provider].save(payload)
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
