<template>
  <div class="px-3 pt-2.5 pb-1" data-testid="company-switcher">
    <div class="flex items-center justify-between mb-1.5">
      <div class="text-[10px] font-semibold text-huginn-muted uppercase tracking-widest select-none">
        Company
      </div>
      <button
        type="button"
        data-testid="company-new"
        aria-label="New company"
        title="New company"
        class="w-5 h-5 rounded flex items-center justify-center text-huginn-muted hover:text-huginn-blue hover:bg-huginn-bg transition-colors"
        @click="openCreate"
      >
        <svg class="w-3 h-3" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round">
          <line x1="12" y1="5" x2="12" y2="19"/><line x1="5" y1="12" x2="19" y2="12"/>
        </svg>
      </button>
    </div>
    <div class="relative" ref="rootEl">
      <button
        type="button"
        data-testid="company-switcher-trigger"
        aria-haspopup="listbox"
        :aria-expanded="open"
        aria-label="Company"
        @click="open = !open"
        class="w-full flex items-center gap-2 px-2 py-1.5 rounded-md text-left
               bg-huginn-bg/70 hover:bg-huginn-bg border border-huginn-border/50
               hover:border-huginn-border transition-colors"
      >
        <span
          class="w-4 h-4 rounded-[4px] flex items-center justify-center text-[9px] font-semibold flex-shrink-0"
          :style="swatchStyle"
        >{{ glyph }}</span>
        <span class="flex-1 min-w-0 text-xs text-huginn-text truncate">{{ currentLabel }}</span>
        <span
          v-if="selectedFollowUnread"
          data-testid="company-switcher-current-unread"
          class="w-1.5 h-1.5 rounded-full bg-huginn-blue flex-shrink-0"
        />
        <svg class="w-2.5 h-2.5 text-huginn-muted/45 flex-shrink-0 transition-transform duration-150"
          :class="open ? 'rotate-90' : ''"
          viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round">
          <polyline points="9 18 15 12 9 6" />
        </svg>
      </button>

      <div
        v-if="open"
        data-testid="company-switcher-menu"
        role="listbox"
        class="absolute left-0 right-0 top-full mt-1 z-50 rounded-md border border-huginn-border shadow-xl overflow-hidden py-0.5"
        style="background:rgba(22,27,34,0.98)"
      >
        <button
          type="button"
          role="option"
          data-testid="company-switcher-desk"
          :aria-selected="isDesk"
          @click="pick(null)"
          class="w-full flex items-center gap-2 px-2 py-1.5 text-left text-xs transition-colors"
          :class="isDesk ? 'text-huginn-text bg-huginn-blue/8' : 'text-huginn-muted hover:text-huginn-text hover:bg-huginn-bg/60'"
        >
          <span class="w-4 h-4 rounded-[4px] flex items-center justify-center text-[9px] text-huginn-muted/70 bg-huginn-bg flex-shrink-0">·</span>
          <span class="flex-1 truncate">Desk</span>
          <span v-if="isDesk" class="text-huginn-blue text-[10px]">✓</span>
        </button>
        <button
          v-for="c in companies"
          :key="c.id"
          type="button"
          role="option"
          :data-testid="`company-switcher-option-${c.id}`"
          :aria-selected="effectiveCompanyId === c.id"
          @click="pick(c.id)"
          class="w-full flex items-center gap-2 px-2 py-1.5 text-left text-xs transition-colors"
          :class="effectiveCompanyId === c.id
            ? 'text-huginn-text bg-huginn-blue/8'
            : 'text-huginn-muted hover:text-huginn-text hover:bg-huginn-bg/60'"
        >
          <span
            class="w-4 h-4 rounded-[4px] flex items-center justify-center text-[9px] font-semibold flex-shrink-0"
            :style="companySwatch(c)"
          >{{ companyGlyph(c) }}</span>
          <span class="flex-1 truncate">{{ c.name }}</span>
          <span
            v-if="companyUnread(c.id)"
            :data-testid="`company-switcher-unread-${c.id}`"
            class="w-1.5 h-1.5 rounded-full bg-huginn-blue flex-shrink-0"
          />
          <span v-if="effectiveCompanyId === c.id" class="text-huginn-blue text-[10px]">✓</span>
        </button>
        <div class="mx-1.5 my-0.5 border-t border-huginn-border/40" />
        <button
          type="button"
          data-testid="company-switcher-new"
          class="w-full flex items-center gap-2 px-2 py-1.5 text-left text-xs text-huginn-muted hover:text-huginn-text hover:bg-huginn-bg/60"
          @click="openCreate"
        >
          <span class="w-4 h-4 rounded-[4px] flex items-center justify-center text-huginn-blue/80 bg-huginn-blue/10 flex-shrink-0 text-[11px] font-semibold">+</span>
          <span>New company</span>
        </button>
        <button
          v-if="selectedCompany"
          type="button"
          data-testid="company-switcher-manage"
          class="w-full flex items-center gap-2 px-2 py-1.5 text-left text-xs text-huginn-muted hover:text-huginn-text hover:bg-huginn-bg/60"
          @click="openManage"
        >
          <span class="w-4 h-4 rounded-[4px] flex items-center justify-center text-huginn-muted/70 bg-huginn-bg flex-shrink-0">
            <svg class="w-2.5 h-2.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
              <path d="M17 21v-2a4 4 0 00-4-4H5a4 4 0 00-4 4v2"/><circle cx="9" cy="7" r="4"/>
            </svg>
          </span>
          <span>Manage people</span>
        </button>
      </div>
    </div>

    <CompanyCreateModal
      v-if="modalOpen"
      :manage-id="manageId || undefined"
      @close="modalOpen = false; manageId = ''"
      @created="onCreated"
    />
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted } from 'vue'
import { useCompanies, type Company } from '../composables/useCompanies'
import { useSpaces } from '../composables/useSpaces'
import CompanyCreateModal from './CompanyCreateModal.vue'

const {
  companies,
  selectedCompany,
  effectiveCompanyId,
  isDesk,
  fetchCompanies,
  selectCompany,
  companyFollowUnread,
} = useCompanies()
const { fetchSpaces, spaces } = useSpaces()

const open = ref(false)
const rootEl = ref<HTMLElement | null>(null)
const modalOpen = ref(false)
const manageId = ref('')

const currentLabel = computed(() => selectedCompany.value?.name || 'Desk')
const glyph = computed(() => {
  const name = selectedCompany.value?.name
  return name ? name.slice(0, 1).toUpperCase() : '·'
})
const swatchStyle = computed(() => companySwatch(selectedCompany.value))

const followSpaces = computed(() =>
  spaces.value.map(s => ({
    id: s.id,
    companyId: s.companyId,
    kind: s.kind,
    unseenCount: s.unseenCount,
    forYou: s.forYou,
  })),
)

function companyUnread(id: string): boolean {
  return companyFollowUnread(id, followSpaces.value)
}

const selectedFollowUnread = computed(() => {
  const id = effectiveCompanyId.value
  return id ? companyUnread(id) : false
})

function companyGlyph(c: Company): string {
  return (c.icon && c.icon.length <= 2 ? c.icon : c.name.slice(0, 1)).toUpperCase()
}

function companySwatch(c: Company | null): Record<string, string> {
  const color = c?.color
  if (color) {
    return { background: color + '22', color }
  }
  return { background: '#161b22', color: '#8b949e' }
}

async function pick(id: string | null) {
  selectCompany(id)
  open.value = false
  if (id) await fetchSpaces({ companyId: id })
  else await fetchSpaces()
}

function openCreate() {
  manageId.value = ''
  modalOpen.value = true
  open.value = false
}

function openManage() {
  manageId.value = effectiveCompanyId.value || ''
  if (!manageId.value) return
  modalOpen.value = true
  open.value = false
}

async function onCreated(id: string) {
  modalOpen.value = false
  manageId.value = ''
  selectCompany(id)
  await fetchSpaces({ companyId: id })
}

function onDocClick(e: MouseEvent) {
  if (rootEl.value && !rootEl.value.contains(e.target as Node)) open.value = false
}

function onKey(e: KeyboardEvent) {
  if (e.key === 'Escape') open.value = false
}

onMounted(() => {
  void fetchCompanies()
  document.addEventListener('click', onDocClick, true)
  document.addEventListener('keydown', onKey)
})
onUnmounted(() => {
  document.removeEventListener('click', onDocClick, true)
  document.removeEventListener('keydown', onKey)
})
</script>
