<template>
  <div
    v-if="items.length > 0"
    class="rounded-xl border border-huginn-border overflow-hidden"
    style="background:rgba(22,27,34,0.97);min-width:180px"
  >
    <button
      v-for="(item, i) in items"
      :key="String(item.name)"
      @mousedown.prevent="selectItem(i)"
      class="w-full flex items-center gap-2.5 px-3 py-2 text-left text-xs transition-colors duration-100"
      :class="i === selectedIndex ? 'bg-huginn-blue/20 text-huginn-text' : 'text-huginn-muted hover:bg-huginn-surface'"
    >
      <div
        class="w-5 h-5 rounded-md flex items-center justify-center text-[10px] font-bold text-white flex-shrink-0"
        :style="{ background: (item.color as string) || '#58a6ff' }"
      >
        {{ (item.icon as string) || String(item.name)?.[0]?.toUpperCase() }}
      </div>
      <span>{{ item.name }}</span>
      <span v-if="i === 0" class="ml-auto text-[10px] text-huginn-muted/50">Tab</span>
    </button>
  </div>
</template>

<script setup lang="ts">
import { ref, watch } from 'vue'

const props = defineProps<{
  items: Array<Record<string, unknown>>
  command: (item: { id: string; label: string }) => void
}>()

const selectedIndex = ref(0)

watch(() => props.items, () => { selectedIndex.value = 0 })

function selectItem(index: number) {
  const item = props.items[index]
  if (item) {
    props.command({ id: String(item.name), label: String(item.name) })
  }
}

function onKeyDown({ event }: { event: KeyboardEvent }) {
  if (event.key === 'ArrowUp') {
    selectedIndex.value = (selectedIndex.value - 1 + props.items.length) % props.items.length
    return true
  }
  if (event.key === 'ArrowDown' || event.key === 'Tab') {
    selectedIndex.value = (selectedIndex.value + 1) % props.items.length
    return true
  }
  if (event.key === 'Enter') {
    selectItem(selectedIndex.value)
    return true
  }
  return false
}

defineExpose({ onKeyDown })
</script>
