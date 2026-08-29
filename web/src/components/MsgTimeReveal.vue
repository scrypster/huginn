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
    <div class="msg-time-body min-w-0" data-testid="msg-time-body">
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
/* The stamp is taken completely out of normal flow (position: absolute) so it
   can never change the width available to .msg-time-body. Only `opacity`
   transitions on hover/reveal — nothing that affects layout (width, margin,
   transform-driven reflow) — so message text never rewraps when the
   timestamp appears. The row stays a plain flex container purely for the
   is-end (right-aligned bubble) alignment that already existed; the stamp
   overlays the trailing corner of the bubble/text instead of pushing it. */
.msg-time-row {
  position: relative;
  display: flex;
  min-width: 0;
  width: 100%;
}
.msg-time-row.is-end {
  justify-content: flex-end;
}
.msg-time-body {
  min-width: 0;
  width: 100%;
}
.msg-time-row.is-end .msg-time-body {
  width: auto;
  max-width: 100%;
}
.msg-time-stamp {
  position: absolute;
  right: 8px;
  bottom: 3px;
  padding: 1px 6px;
  border-radius: 6px;
  font-size: 11px;
  line-height: 1.4;
  color: rgba(139, 148, 158, 0.85);
  background: rgba(13, 17, 23, 0.55);
  backdrop-filter: blur(3px);
  -webkit-backdrop-filter: blur(3px);
  white-space: nowrap;
  pointer-events: none;
  opacity: 0;
  transition: opacity 150ms ease;
  font-variant-numeric: tabular-nums;
}
.msg-time-row.is-revealed .msg-time-stamp {
  opacity: 1;
}
</style>
