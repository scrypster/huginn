<template>
  <div class="flex flex-shrink-0 transition-all duration-200"
       :style="open ? 'width:220px' : 'width:28px'">
    <!-- Toggle button (chevron on left edge of panel) -->
    <button
      data-testid="panel-toggle"
      @click="$emit('toggle')"
      class="flex-shrink-0 w-7 flex flex-col items-center justify-center gap-1 py-3 hover:bg-huginn-surface/60 transition-colors"
      :title="open ? 'Collapse member panel' : 'Expand member panel'"
      :aria-label="open ? 'Collapse member panel' : 'Expand member panel'"
    >
      <svg class="w-3 h-3 text-huginn-muted transition-transform" :class="open ? 'rotate-0' : 'rotate-180'"
           viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
        <polyline points="15 18 9 12 15 6" />
      </svg>
    </button>

    <!-- Panel content — only rendered when open -->
    <div v-if="open" class="flex-1 overflow-y-auto border-l border-huginn-border">
      <p class="text-[10px] font-semibold text-huginn-muted uppercase tracking-widest px-3 pt-3 pb-2">Members</p>

      <div v-for="member in members" :key="member.name" class="flex items-start gap-2 px-3 py-2">
        <!-- Avatar initial -->
        <div
          class="w-6 h-6 rounded-full flex-shrink-0 flex items-center justify-center text-[10px] font-bold text-white select-none"
          :style="{ background: member.color }"
        >{{ member.name[0]?.toUpperCase() }}</div>

        <!-- Info -->
        <div class="min-w-0 flex-1">
          <div class="flex items-center gap-1">
            <span class="text-xs font-medium truncate" style="color:var(--color-text,#e6edf3)">{{ member.name }}</span>
            <span
              v-if="member.isLead"
              data-testid="lead-badge"
              class="text-[9px] px-1 py-0.5 rounded flex-shrink-0"
              style="background:rgba(88,166,255,0.1);color:#58a6ff"
            >Lead</span>
          </div>
          <p class="text-[11px] mt-0.5 leading-snug"
             style="color:#8b949e;display:-webkit-box;-webkit-line-clamp:2;-webkit-box-orient:vertical;overflow:hidden">
            {{ member.description || 'No description' }}
          </p>
          <p v-if="member.vaultName" class="text-[10px] mt-0.5 truncate" style="color:#8b949e">
            🧠 {{ member.vaultName }}
          </p>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
interface SpaceMemberCard {
  name: string
  description: string
  vaultName: string
  isLead: boolean
  color: string
}

defineProps<{
  members: SpaceMemberCard[]
  open: boolean
}>()

defineEmits<{ (e: 'toggle'): void }>()
</script>
