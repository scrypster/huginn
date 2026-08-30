<template>
  <div data-testid="welcome-card"
    class="mx-5 mt-5 rounded-2xl border border-huginn-blue/20 p-5"
    style="background:linear-gradient(135deg,rgba(88,166,255,0.08),rgba(88,166,255,0.02))">
    <div class="flex items-start gap-3">
      <div class="w-10 h-10 rounded-xl flex items-center justify-center flex-shrink-0 select-none"
        :style="`background:${agentColor}22;border:1px solid ${agentColor}40`">
        <span class="font-bold text-lg" :style="`color:${agentColor}`">{{ agentIcon || agentName[0]?.toUpperCase() }}</span>
      </div>
      <div class="flex-1 min-w-0">
        <h2 class="text-sm font-semibold text-huginn-text">{{ agentName }} is your Chief of Staff</h2>
        <p class="text-xs text-huginn-muted mt-1 leading-relaxed">
          Ask for anything — {{ agentName }} can answer directly, or bring in a specialist teammate for
          coding, research, or ops work.
        </p>
      </div>
      <button data-testid="welcome-card-dismiss" @click="$emit('dismiss')"
        class="w-6 h-6 rounded flex items-center justify-center text-huginn-muted/50 hover:text-huginn-muted hover:bg-huginn-surface transition-colors flex-shrink-0"
        title="Dismiss">
        <svg class="w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
          <line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/>
        </svg>
      </button>
    </div>

    <div class="mt-4 flex flex-wrap gap-2">
      <button v-for="ex in examples" :key="ex"
        type="button"
        data-testid="welcome-card-example"
        @click="$emit('use-example', ex)"
        class="text-xs px-3 py-1.5 rounded-lg border border-huginn-border text-huginn-muted hover:text-huginn-text hover:border-huginn-blue/40 transition-colors">
        "{{ ex }}"
      </button>
    </div>

    <div v-if="!modelConfigured" data-testid="welcome-card-model-hint"
      class="mt-4 flex items-center gap-2 text-[11px] text-huginn-yellow">
      <svg class="w-3.5 h-3.5 flex-shrink-0" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
        <path d="M10.29 3.86L1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0z"/><line x1="12" y1="9" x2="12" y2="13"/><line x1="12" y1="17" x2="12.01" y2="17"/>
      </svg>
      <span>No model connected yet — <router-link to="/settings" class="underline hover:text-huginn-text">set one up in Settings</router-link> before you send a message.</span>
    </div>
  </div>
</template>

<script setup lang="ts">
withDefaults(defineProps<{
  agentName: string
  agentIcon?: string
  agentColor?: string
  modelConfigured?: boolean
}>(), {
  agentIcon: '',
  agentColor: '#58A6FF',
  modelConfigured: true,
})

defineEmits<{
  dismiss: []
  'use-example': [text: string]
}>()

const examples = [
  'hire a teammate',
  'ask me the time',
  'give me a coding task',
]
</script>
