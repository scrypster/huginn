<!-- web/src/components/connections/CategoryNav.vue -->
<template>
  <nav class="flex flex-col h-full py-3 overflow-y-auto">
    <!-- Special items -->
    <button
      v-for="item in specialItems"
      :key="item.id"
      @click="$emit('update:category', item.id)"
      class="flex items-center justify-between px-3 py-1.5 mx-2 rounded-lg text-xs transition-colors"
      :class="category === item.id
        ? 'bg-huginn-blue/15 text-huginn-blue'
        : 'text-huginn-muted hover:text-huginn-text hover:bg-huginn-surface'"
    >
      <span>{{ item.label }}</span>
      <span
        v-if="item.count != null"
        class="text-[10px] px-1.5 py-0.5 rounded-full"
        :class="category === item.id
          ? 'bg-huginn-blue/20 text-huginn-blue'
          : 'bg-huginn-surface text-huginn-muted'"
      >{{ item.count }}</span>
    </button>

    <!-- Divider -->
    <div class="mx-3 my-2 border-t border-huginn-border/50" />

    <!-- Category items -->
    <button
      v-for="item in categoryItems"
      :key="item.id"
      @click="$emit('update:category', item.id)"
      class="flex items-center justify-between px-3 py-1.5 mx-2 rounded-lg text-xs transition-colors"
      :class="category === item.id
        ? 'bg-huginn-blue/15 text-huginn-blue'
        : 'text-huginn-muted hover:text-huginn-text hover:bg-huginn-surface'"
    >
      <span>{{ item.label }}</span>
      <span
        v-if="item.count"
        class="text-[10px] px-1 text-huginn-muted/60"
      >{{ item.count }}</span>
    </button>
  </nav>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { CATEGORY_LABELS, type ConnectionCategory, type CatalogConnection } from '../../composables/useConnectionsCatalog'

const props = defineProps<{
  category: ConnectionCategory
  connections: CatalogConnection[]
}>()

defineEmits<{
  'update:category': [value: ConnectionCategory]
}>()

const connectedCount = computed(() =>
  props.connections.filter(c => c.state?.connected && c.type !== 'coming_soon').length
)

const specialItems = computed(() => [
  { id: 'all' as ConnectionCategory,            label: 'All',              count: null },
  { id: 'my_connections' as ConnectionCategory, label: 'My Connections',   count: connectedCount.value },
])

const categoryOrder: ConnectionCategory[] = [
  'communication', 'dev_tools', 'cloud', 'productivity', 'databases', 'system',
]

const categoryItems = computed(() =>
  categoryOrder.map(id => {
    const items = props.connections.filter(c => c.category === id && c.type !== 'coming_soon')
    const connected = items.filter(c => c.state?.connected).length
    return {
      id,
      label: CATEGORY_LABELS[id],
      count: connected > 0 ? connected : null,
    }
  })
)
</script>
