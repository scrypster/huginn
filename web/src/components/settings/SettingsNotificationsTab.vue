<template>
  <div class="space-y-6">
    <p class="text-xs text-huginn-muted">Control when Huginn sends desktop notifications to your operating system.</p>

    <section class="space-y-3">
      <h3 class="text-[11px] font-semibold text-huginn-muted uppercase tracking-widest">Desktop Notifications</h3>

      <div v-if="!notif.isSupported" class="text-xs text-huginn-muted px-1">
        Browser notifications are not supported in this browser.
      </div>

      <template v-else>
        <SettingsToggleRow
          :model-value="notif.enabled.value"
          label="Desktop notifications"
          hint="Get notified when agents respond or workflows complete, even when this tab is in the background."
          @update:model-value="notif.toggle($event)"
        />

        <p v-if="notif.permission.value === 'denied'" class="text-[11px] text-amber-400 px-1">
          Notifications are blocked in browser settings. To enable, update your browser's site permissions for this page.
        </p>
      </template>
    </section>
  </div>
</template>

<script setup lang="ts">
import SettingsToggleRow from './SettingsToggleRow.vue'

defineProps<{
  notif: {
    isSupported: boolean
    enabled: { value: boolean }
    permission: { value: NotificationPermission }
    toggle: (value: boolean) => void
  }
}>()
</script>
