<template>
  <div class="flex flex-col gap-3">
    <!-- Error state: shown when the catalog fetch failed -->
    <div
      v-if="error"
      class="text-[11px] text-huginn-red bg-huginn-red/10 border border-huginn-red/30 rounded-lg px-3 py-2"
      data-testid="catalog-error"
    >
      Could not load form. Please close and try again.
    </div>

    <!-- Loading state -->
    <div
      v-else-if="loading"
      class="text-[11px] text-huginn-muted"
      data-testid="catalog-loading"
    >
      Loading…
    </div>

    <!-- Fields -->
    <template v-else>
      <div
        v-for="field in fields"
        :key="field.key"
        :data-testid="`field-wrapper-${field.key}`"
      >
        <label class="text-[10px] text-huginn-muted mb-1 block">
          {{ field.label }}
          <span v-if="!field.required" class="opacity-60">(optional)</span>
        </label>

        <!-- select + optional __custom__ URL -->
        <template v-if="field.type === 'select'">
          <select
            v-model="values[field.key]"
            class="field"
            :disabled="testing || saving"
            :data-testid="`field-${field.key}`"
          >
            <option
              v-for="opt in field.options"
              :key="opt.value"
              :value="opt.value"
            >{{ opt.label }}</option>
          </select>
          <!-- Secondary input shown when __custom__ sentinel is selected -->
          <input
            v-if="values[field.key] === '__custom__'"
            v-model="customUrls[field.key]"
            type="url"
            class="field mt-2"
            placeholder="https://…"
            :disabled="testing || saving"
            :data-testid="`field-${field.key}-custom`"
          />
        </template>

        <!-- subdomain with inline suffix -->
        <div v-else-if="field.type === 'subdomain'" class="flex items-center">
          <input
            v-model="values[field.key]"
            type="text"
            class="field rounded-r-none flex-1"
            :placeholder="field.placeholder ?? ''"
            :disabled="testing || saving"
            :data-testid="`field-${field.key}`"
          />
          <span class="text-[11px] text-huginn-muted bg-huginn-surface border border-l-0 border-huginn-border rounded-r-lg px-2 py-[7px] whitespace-nowrap">
            .{{ subdomainSuffix(field.key) }}
          </span>
        </div>

        <!-- password -->
        <input
          v-else-if="field.type === 'password'"
          v-model="values[field.key]"
          type="password"
          class="field"
          :placeholder="field.placeholder ?? ''"
          :disabled="testing || saving"
          :data-testid="`field-${field.key}`"
        />

        <!-- url -->
        <input
          v-else-if="field.type === 'url'"
          v-model="values[field.key]"
          type="url"
          class="field"
          :placeholder="field.placeholder ?? ''"
          :disabled="testing || saving"
          :data-testid="`field-${field.key}`"
        />

        <!-- email -->
        <input
          v-else-if="field.type === 'email'"
          v-model="values[field.key]"
          type="email"
          class="field"
          :placeholder="field.placeholder ?? ''"
          :disabled="testing || saving"
          :data-testid="`field-${field.key}`"
        />

        <!-- text (default) -->
        <input
          v-else
          v-model="values[field.key]"
          type="text"
          class="field"
          :placeholder="field.placeholder ?? ''"
          :disabled="testing || saving"
          :data-testid="`field-${field.key}`"
        />

        <!-- help text -->
        <p
          v-if="field.help_text"
          class="text-[10px] text-huginn-muted mt-1"
          :data-testid="`help-${field.key}`"
        >{{ field.help_text }}</p>
      </div>

      <!-- Label -->
      <div>
        <label class="text-[10px] text-huginn-muted mb-1 block">Label <span class="opacity-60">(optional)</span></label>
        <input
          v-model="label"
          type="text"
          class="field"
          :placeholder="defaultLabel || 'e.g. prod'"
          :disabled="testing || saving"
          data-testid="field-label"
        />
      </div>
    </template>
  </div>
</template>

<script setup lang="ts">
import { reactive, ref } from 'vue'
import type { FieldDefinition } from '../../composables/useCredentialCatalog'

const props = defineProps<{
  fields: FieldDefinition[]
  defaultLabel?: string
  testing: boolean
  saving: boolean
  loading?: boolean
  error?: boolean
}>()

// Per-field values keyed by FieldDef.key, pre-populated with catalog defaults.
const values = reactive<Record<string, string>>(
  Object.fromEntries(props.fields.map((f) => [f.key, f.default ?? ''])),
)

// Separate storage for custom URL inputs that appear after __custom__ is selected.
const customUrls = reactive<Record<string, string>>({})

const label = ref('')

/**
 * Return the suffix shown next to subdomain fields.
 * Derived from the provider's first select-type option that contains a dot,
 * or falls back to a generic ".example.com" hint.
 * Currently only Zendesk uses subdomain fields; its suffix is .zendesk.com.
 */
function subdomainSuffix(key: string): string {
  const field = props.fields.find((f) => f.key === key)
  if (field?.help_text) {
    // Extract "*.zendesk.com" pattern from help text.
    const m = field.help_text.match(/\w+\.\w+\.\w+/)
    if (m) {
      const parts = m[0].split('.')
      return parts.slice(1).join('.')
    }
  }
  return 'example.com'
}

defineExpose({
  /**
   * Returns a flat map of field key → value ready to POST to the server.
   * For select fields with __custom__ selected, substitutes the custom URL.
   * Always includes the label key.
   */
  getPayload(): Record<string, string> {
    const payload: Record<string, string> = {}
    for (const f of props.fields) {
      const v = values[f.key] ?? ''
      if (f.type === 'select' && v === '__custom__') {
        payload[f.key] = customUrls[f.key] ?? ''
      } else {
        payload[f.key] = v
      }
    }
    payload['label'] = label.value
    return payload
  },
})
</script>
