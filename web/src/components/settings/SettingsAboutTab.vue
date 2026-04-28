<template>
  <div class="space-y-6" data-testid="settings-about-panel">
    <p class="text-xs text-huginn-muted">Build information for the running Huginn instance. Useful for confirming an upgrade has taken effect.</p>

    <section class="space-y-3">
      <h3 class="text-[11px] font-semibold text-huginn-muted uppercase tracking-widest">Application</h3>
      <div class="rounded-xl border border-huginn-border bg-huginn-surface/40 divide-y divide-huginn-border">
        <div class="flex items-center justify-between px-4 py-3">
          <span class="text-xs text-huginn-muted">Name</span>
          <span class="text-xs text-huginn-text">Huginn</span>
        </div>
        <div class="flex items-center justify-between px-4 py-3">
          <span class="text-xs text-huginn-muted">Version</span>
          <span class="text-xs font-mono text-huginn-text" data-testid="settings-version-value">{{ versionLabel }}</span>
        </div>
      </div>
    </section>
  </div>
</template>

<script setup lang="ts">
defineProps<{
  versionLabel: string
}>()
</script>
