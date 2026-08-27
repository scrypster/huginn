<template>
  <div
    class="msg-time-row"
    :class="{ 'is-end': align === 'end', 'is-revealed': revealed || hover }"
    data-testid="msg-time-row"
    @mouseenter="hover = true"
    @mouseleave="hover = false"
    @touchstart.passive="onTouchStart"
    @touchmove.passive="onTouchMove"
    @touchend="onTouchEnd"
  >
    <div class="msg-time-body min-w-0">
      <slot />
    </div>
    <span
      v-if="label"
      class="msg-time-stamp"
      data-testid="msg-rel-time"
      :title="clock || undefined"
    >{{ label }}</span>
  </div>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { formatClockTime, formatRelativeTime } from '../utils/relativeTime'

const props = withDefaults(defineProps<{
  createdAt?: string
  revealed?: boolean
  align?: 'start' | 'end'
}>(), {
  revealed: false,
  align: 'start',
})

const hover = ref(false)
let startX = 0

const label = computed(() => formatRelativeTime(props.createdAt))
const clock = computed(() => formatClockTime(props.createdAt))

function onTouchStart(e: TouchEvent) {
  startX = e.touches[0]?.clientX ?? 0
}

function onTouchMove(e: TouchEvent) {
  const x = e.touches[0]?.clientX ?? startX
  if (x < startX - 24) hover.value = true
  if (x > startX + 24) hover.value = false
}

function onTouchEnd() {
  // hover/reveal persists until swipe-right or mouseleave
}
</script>

<style scoped>
.msg-time-row {
  display: flex;
  align-items: center;
  min-width: 0;
  width: 100%;
}
.msg-time-row.is-end {
  justify-content: flex-end;
}
.msg-time-body {
  min-width: 0;
  transition: transform 180ms ease;
}
.msg-time-row.is-revealed .msg-time-body {
  transform: translateX(-6px);
}
.msg-time-stamp {
  flex: 0 0 auto;
  max-width: 0;
  margin-left: 0;
  overflow: hidden;
  opacity: 0;
  transform: translateX(10px);
  transition: opacity 180ms ease, transform 180ms ease, max-width 180ms ease, margin 180ms ease;
  font-size: 11px;
  line-height: 1;
  color: rgba(139, 148, 158, 0.72);
  white-space: nowrap;
  pointer-events: none;
  font-variant-numeric: tabular-nums;
}
.msg-time-row.is-revealed .msg-time-stamp {
  max-width: 8rem;
  margin-left: 8px;
  opacity: 1;
  transform: translateX(0);
}
</style>
