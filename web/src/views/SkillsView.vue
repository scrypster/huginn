<template>
  <div class="flex flex-col h-full overflow-y-auto bg-huginn-bg">

    <!-- ── Installed tab ──────────────────────────────────────────── -->
    <template v-if="tab === 'installed'">
      <div class="flex items-center justify-between px-6 py-4 border-b border-huginn-border flex-shrink-0">
        <div>
          <h1 class="text-huginn-text font-semibold text-sm">Installed Skills</h1>
          <p class="text-huginn-muted text-xs mt-0.5">Your skill library. Assign skills to agents from each agent's settings.</p>
        </div>
      </div>

      <div class="px-6 pt-3 pb-1 max-w-2xl">
        <input
          v-model="installedQuery"
          type="text"
          placeholder="Filter installed skills..."
          class="w-full px-3 py-2 text-xs bg-huginn-surface border border-huginn-border rounded-lg text-huginn-text placeholder-huginn-muted focus:outline-none focus:border-huginn-blue/50 transition-colors"
        />
      </div>

      <div v-if="actionError" class="mx-6 mt-3 px-3 py-2 rounded-lg bg-huginn-red/10 border border-huginn-red/30 text-huginn-red text-xs">
        {{ actionError }}
      </div>

      <div v-if="installed.loading.value" class="flex items-center justify-center py-16">
        <div class="w-5 h-5 border border-huginn-border border-t-huginn-blue rounded-full animate-spin" />
      </div>

      <div v-else-if="installed.error.value" class="flex flex-col items-center justify-center py-16 gap-3">
        <p class="text-huginn-red text-sm">{{ installed.error.value }}</p>
        <button @click="installed.load()" class="text-huginn-blue text-xs hover:underline">Retry</button>
      </div>

      <div v-else-if="installed.skills.value.length === 0" class="flex flex-col items-center justify-center py-16 gap-3">
        <p class="text-huginn-muted text-sm">No skills installed yet.</p>
        <button @click="router.push('/skills/browse')" class="text-huginn-blue text-xs hover:underline">Browse the registry →</button>
      </div>

      <div v-else class="px-6 py-4 grid grid-cols-1 gap-3 max-w-2xl">
        <div
          v-for="skill in filteredInstalled"
          :key="skill.name"
          class="flex items-start gap-3 px-4 py-3 rounded-xl border border-huginn-border bg-huginn-surface/50"
        >
          <!-- Enabled indicator -->
          <div class="flex-shrink-0 mt-0.5">
            <div class="w-2 h-2 rounded-full"
              :class="skill.enabled ? 'bg-huginn-green' : 'bg-huginn-muted/40'"
              :style="skill.enabled ? 'box-shadow:0 0 4px rgba(63,185,80,0.5)' : ''" />
          </div>

          <!-- Info -->
          <div class="flex-1 min-w-0">
            <div class="flex items-center gap-2">
              <button @click.stop="openUsageModal(skill)" class="text-xs font-semibold text-huginn-text hover:text-huginn-blue transition-colors text-left">{{ skill.name }}</button>
              <span class="text-[10px] px-1.5 py-0.5 rounded border border-huginn-border text-huginn-muted">
                v{{ skill.version }}
              </span>
              <span class="text-[10px] px-1.5 py-0.5 rounded border border-huginn-border"
                :class="skill.source === 'registry' ? 'text-huginn-blue border-huginn-blue/30' : 'text-huginn-muted'">
                {{ skill.source }}
              </span>
              <span v-if="skill.tool_count > 0"
                class="text-[10px] px-1.5 py-0.5 rounded border border-huginn-amber/40 text-huginn-amber"
                title="This skill includes callable tools">
                {{ skill.tool_count }} tool{{ skill.tool_count === 1 ? '' : 's' }}
              </span>
            </div>
            <p v-if="skill.author" class="text-[11px] text-huginn-muted mt-0.5">by {{ skill.author }}</p>
            <!-- Used by pill -->
            <button
              @click.stop="openUsageModal(skill)"
              class="mt-1.5 inline-flex items-center gap-1 text-[10px] transition-colors"
              :class="(agentsBySkill[skill.name]?.length ?? 0) > 0
                ? 'text-huginn-blue hover:text-huginn-blue/80'
                : 'text-huginn-muted/40 hover:text-huginn-muted'"
            >
              <svg class="w-2.5 h-2.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
                <path d="M17 21v-2a4 4 0 0 0-4-4H5a4 4 0 0 0-4 4v2"/>
                <circle cx="9" cy="7" r="4"/>
                <path d="M23 21v-2a4 4 0 0 0-3-3.87"/>
                <path d="M16 3.13a4 4 0 0 1 0 7.75"/>
              </svg>
              <span>
                {{ (agentsBySkill[skill.name]?.length ?? 0) === 0
                  ? 'Not assigned'
                  : `Used by ${agentsBySkill[skill.name]!.length} agent${agentsBySkill[skill.name]!.length === 1 ? '' : 's'}` }}
              </span>
            </button>
          </div>

          <!-- Actions -->
          <div class="flex items-center gap-1 flex-shrink-0">
            <button
              @click="openExecuteModal(skill.name)"
              class="px-2 py-1 text-[10px] rounded border border-huginn-border text-huginn-muted hover:text-huginn-text hover:border-huginn-border/70 transition-colors duration-150"
              title="Run skill"
            >
              Run
            </button>
            <button
              @click="toggleSkill(skill.name, !skill.enabled)"
              class="px-2 py-1 text-[10px] rounded border transition-colors duration-150"
              :class="skill.enabled
                ? 'border-huginn-border text-huginn-muted hover:text-huginn-red hover:border-huginn-red/40'
                : 'border-huginn-blue/30 text-huginn-blue hover:bg-huginn-blue/10'"
            >
              {{ skill.enabled ? 'Disable' : 'Enable' }}
            </button>
            <button
              @click="confirmUninstall(skill.name)"
              class="w-6 h-6 rounded flex items-center justify-center text-huginn-muted hover:text-huginn-red transition-colors"
              title="Uninstall"
            >
              <svg class="w-3 h-3" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round">
                <line x1="18" y1="6" x2="6" y2="18" /><line x1="6" y1="6" x2="18" y2="18" />
              </svg>
            </button>
          </div>
        </div>
        <p v-if="filteredInstalled.length === 0" class="text-huginn-muted text-xs text-center py-8 col-span-1">
          No skills match "{{ installedQuery }}".
        </p>
      </div>
    </template>

    <!-- ── Browse tab ─────────────────────────────────────────────── -->
    <template v-else-if="tab === 'browse'">
      <div class="flex flex-col h-full overflow-hidden">

        <!-- Top bar: title + refresh -->
        <div class="px-6 py-3 border-b border-huginn-border flex-shrink-0 flex items-center gap-3">
          <div>
            <h1 class="text-huginn-text font-semibold text-sm">Skills Marketplace</h1>
            <p class="text-huginn-muted text-xs mt-0.5">Browse and install skills from the community registry.</p>
          </div>
          <div class="ml-auto flex items-center gap-1.5">
            <button @click="registry.load(true)"
              class="w-7 h-7 rounded flex items-center justify-center text-huginn-muted hover:text-huginn-text hover:bg-huginn-surface transition-all"
              title="Refresh index">
              <svg class="w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                <polyline points="23 4 23 10 17 10" /><polyline points="1 20 1 14 7 14" />
                <path d="M3.51 9a9 9 0 0114.85-3.36L23 10M1 14l4.64 4.36A9 9 0 0020.49 15" />
              </svg>
            </button>
          </div>
        </div>

        <!-- Search bar -->
        <div class="px-4 py-2.5 border-b border-huginn-border flex-shrink-0">
          <div class="relative">
            <svg class="absolute left-3 top-1/2 -translate-y-1/2 w-3.5 h-3.5 text-huginn-muted pointer-events-none" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
              <circle cx="11" cy="11" r="8"/><line x1="21" y1="21" x2="16.65" y2="16.65"/>
            </svg>
            <input v-model="browseQuery" type="text" placeholder="Search skills and collections..."
              class="w-full pl-9 pr-8 py-2 text-xs bg-huginn-surface border border-huginn-border rounded-lg text-huginn-text placeholder-huginn-muted focus:outline-none focus:border-huginn-blue/50 transition-colors" />
            <button v-if="browseQuery" @click="browseQuery = ''"
              class="absolute right-2.5 top-1/2 -translate-y-1/2 text-huginn-muted hover:text-huginn-text transition-colors">
              <svg class="w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round">
                <line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/>
              </svg>
            </button>
          </div>
        </div>

        <!-- Loading -->
        <div v-if="registry.loading.value" class="flex-1 flex items-center justify-center">
          <div class="w-5 h-5 border border-huginn-border border-t-huginn-blue rounded-full animate-spin" />
        </div>

        <!-- Error -->
        <div v-else-if="registry.error.value" class="flex-1 flex flex-col items-center justify-center gap-3">
          <p class="text-huginn-muted text-sm">Registry unavailable</p>
          <p class="text-huginn-red text-xs">{{ registry.error.value }}</p>
          <button @click="registry.load()" class="text-huginn-blue text-xs hover:underline">Retry</button>
        </div>

        <!-- Two-column body -->
        <div v-else class="flex flex-1 min-h-0 overflow-hidden">

          <!-- ── Left panel: category nav ── -->
          <div class="w-44 border-r border-huginn-border flex-shrink-0 overflow-y-auto py-2">
            <div class="px-4 pt-2 pb-1.5">
              <span class="text-[10px] font-semibold uppercase tracking-widest text-huginn-muted">Category</span>
            </div>
            <button @click="setCategory('all')"
              class="w-full text-left px-4 py-2 flex items-center justify-between transition-all duration-150 border-l-2"
              :class="categoryFilter === 'all' ? 'border-l-huginn-blue bg-huginn-blue/5 text-huginn-text' : 'border-l-transparent text-huginn-muted hover:text-huginn-text hover:bg-huginn-surface/70'">
              <span class="text-xs font-medium">All</span>
              <span class="text-[10px] text-huginn-muted/60">{{ registry.index.value.length }}</span>
            </button>
            <button v-for="cat in registryCategories" :key="cat"
              @click="setCategory(cat)"
              class="w-full text-left px-4 py-2 flex items-center justify-between transition-all duration-150 border-l-2"
              :class="categoryFilter === cat ? 'border-l-huginn-blue bg-huginn-blue/5 text-huginn-text' : 'border-l-transparent text-huginn-muted hover:text-huginn-text hover:bg-huginn-surface/70'">
              <span class="text-xs font-medium">{{ categoryLabel(cat) }}</span>
              <span class="text-[10px] text-huginn-muted/60">{{ categorySkillCount[cat] ?? 0 }}</span>
            </button>
          </div>

          <!-- ── Right panel ── -->
          <div class="flex-1 overflow-y-auto relative">

            <!-- Skills grid / grouped (default view) -->
            <Transition name="detail-fade">
              <div v-if="browseKind !== 'skill' && browseKind !== 'collection'" class="flex flex-col">

                <!-- Sticky toolbar -->
                <div class="sticky top-0 z-10 flex items-center gap-3 px-5 py-2.5 border-b border-huginn-border/60 flex-shrink-0"
                  style="background:rgba(13,17,23,0.94);backdrop-filter:blur(8px)">
                  <span class="text-[10px] text-huginn-muted font-medium">
                    {{ filteredRegistry.length }} skill{{ filteredRegistry.length === 1 ? '' : 's' }}
                  </span>
                  <div class="ml-auto flex items-center gap-0.5 p-0.5 rounded-lg border border-huginn-border/60"
                    style="background:rgba(22,27,34,0.7)">
                    <button @click="viewMode = 'grid'"
                      class="px-3 py-1 text-[10px] font-medium rounded-md transition-all duration-150"
                      :class="viewMode === 'grid' ? 'bg-huginn-blue text-white shadow-sm' : 'text-huginn-muted hover:text-huginn-text'">
                      Grid
                    </button>
                    <button @click="viewMode = 'grouped'"
                      class="px-3 py-1 text-[10px] font-medium rounded-md transition-all duration-150"
                      :class="viewMode === 'grouped' ? 'bg-huginn-blue text-white shadow-sm' : 'text-huginn-muted hover:text-huginn-text'">
                      Grouped
                    </button>
                  </div>
                </div>

                <!-- ── Grid view ── -->
                <div v-if="viewMode === 'grid'" class="p-5">
                  <p v-if="filteredRegistry.length === 0" class="text-center text-huginn-muted text-sm py-16">
                    No results{{ browseQuery ? ` for "${browseQuery}"` : '' }}
                  </p>
                  <div v-else class="grid gap-2.5" style="grid-template-columns: repeat(auto-fill, minmax(260px, 1fr))">
                    <div v-for="skill in filteredRegistry" :key="skill.name"
                      @click="selectSkill(skill)"
                      class="group flex flex-col gap-2 px-4 py-3 rounded-xl border border-huginn-border bg-huginn-surface/30 hover:bg-huginn-surface/70 hover:border-huginn-border/80 cursor-pointer transition-all duration-150">
                      <div class="flex items-start justify-between gap-2">
                        <div class="flex-1 min-w-0">
                          <div class="flex items-center gap-1.5">
                            <div class="w-1.5 h-1.5 rounded-full flex-shrink-0 transition-colors"
                              :class="isInstalled(skill.name) ? 'bg-huginn-green' : 'bg-huginn-border'" />
                            <span class="text-xs font-medium text-huginn-text group-hover:text-huginn-blue transition-colors truncate">{{ skill.display_name || skill.name }}</span>
                          </div>
                          <p class="text-[10px] text-huginn-muted mt-1 leading-relaxed line-clamp-2">{{ skill.description }}</p>
                        </div>
                        <div class="flex-shrink-0 pt-0.5">
                          <button v-if="!isInstalled(skill.name)"
                            @click.stop="requestInstall(skill)"
                            :disabled="registry.isInstalling(skill.name)"
                            class="px-2.5 py-1 text-[10px] rounded-lg bg-huginn-blue text-white hover:bg-huginn-blue/80 disabled:opacity-50 transition-colors whitespace-nowrap">
                            {{ registry.isInstalling(skill.name) ? '…' : 'Install' }}
                          </button>
                          <div v-else class="text-[10px] text-huginn-green flex items-center gap-1">
                            <svg class="w-3 h-3" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5"><polyline points="20 6 9 17 4 12"/></svg>
                          </div>
                        </div>
                      </div>
                      <div v-if="(skill.tags ?? []).length" class="flex flex-wrap gap-1">
                        <span v-for="tag in (skill.tags ?? []).slice(0,3)" :key="tag"
                          class="text-[9px] px-1.5 py-0.5 rounded border border-huginn-border text-huginn-muted">{{ tag }}</span>
                      </div>
                    </div>
                  </div>
                </div>

                <!-- ── Grouped view ── -->
                <div v-else class="p-5 space-y-4">
                  <p v-if="filteredRegistry.length === 0" class="text-center text-huginn-muted text-sm py-16">
                    No results{{ browseQuery ? ` for "${browseQuery}"` : '' }}
                  </p>
                  <template v-else>

                    <!-- Collection groups -->
                    <div v-for="group in groupedSkills.groups" :key="group.col.id"
                      class="rounded-xl border border-huginn-border overflow-hidden">

                      <!-- Collection header (click to collapse) -->
                      <div class="flex items-center gap-2.5 px-4 py-3 select-none cursor-pointer transition-colors duration-150 hover:bg-huginn-surface/60"
                        style="background:rgba(22,27,34,0.85)"
                        @click="toggleCollapse(group.col.id)">
                        <!-- Chevron -->
                        <svg class="w-3.5 h-3.5 flex-shrink-0 transition-transform duration-200"
                          :class="collapsedCollections.has(group.col.id) ? '-rotate-90' : 'rotate-0'"
                          style="color:rgba(139,92,246,0.6)" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round">
                          <polyline points="6 9 12 15 18 9"/>
                        </svg>
                        <!-- Star icon -->
                        <svg class="w-3 h-3 flex-shrink-0" viewBox="0 0 24 24" fill="none" stroke="rgba(192,132,252,0.7)" stroke-width="2" stroke-linecap="round">
                          <polygon points="12 2 15.09 8.26 22 9.27 17 14.14 18.18 21.02 12 17.77 5.82 21.02 7 14.14 2 9.27 8.91 8.26 12 2"/>
                        </svg>
                        <!-- Name + count -->
                        <span class="text-xs font-semibold text-huginn-text flex-1 min-w-0 truncate">{{ group.col.display_name || group.col.name }}</span>
                        <span class="text-[10px] text-huginn-muted flex-shrink-0">{{ group.skills.length }} skill{{ group.skills.length === 1 ? '' : 's' }}</span>
                        <!-- Install All / All Installed (stop propagation so header click still collapses) -->
                        <div class="flex-shrink-0 ml-1" @click.stop>
                          <button v-if="!isCollectionInstalled(group.col)"
                            @click="installCollection(group.col)"
                            class="px-2.5 py-1 text-[10px] rounded-lg font-medium transition-all duration-150 border"
                            style="background:rgba(88,166,255,0.1);color:#58a6ff;border-color:rgba(88,166,255,0.25)"
                            @mouseenter="(e:MouseEvent) => ((e.currentTarget as HTMLElement).style.background='rgba(88,166,255,0.2)')"
                            @mouseleave="(e:MouseEvent) => ((e.currentTarget as HTMLElement).style.background='rgba(88,166,255,0.1)')">
                            Install All
                          </button>
                          <div v-else class="flex items-center gap-1 text-[10px] text-huginn-green">
                            <svg class="w-3 h-3" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5"><polyline points="20 6 9 17 4 12"/></svg>
                            All Installed
                          </div>
                        </div>
                      </div>

                      <!-- Collapsible skills grid -->
                      <Transition
                        @enter="onCollapseEnter" @after-enter="onCollapseAfterEnter"
                        @leave="onCollapseLeave" @after-leave="onCollapseAfterLeave">
                        <div v-show="!collapsedCollections.has(group.col.id)"
                          class="border-t border-huginn-border/40">
                          <div class="p-3 grid gap-2" style="grid-template-columns: repeat(auto-fill, minmax(230px, 1fr))">
                            <div v-for="skill in group.skills" :key="skill.name"
                              @click="selectSkill(skill)"
                              class="group flex flex-col gap-1.5 px-3 py-2.5 rounded-lg border border-huginn-border bg-huginn-surface/20 hover:bg-huginn-surface/60 hover:border-huginn-border/70 cursor-pointer transition-all duration-150">
                              <div class="flex items-start justify-between gap-2">
                                <div class="flex-1 min-w-0">
                                  <div class="flex items-center gap-1.5">
                                    <div class="w-1.5 h-1.5 rounded-full flex-shrink-0"
                                      :class="isInstalled(skill.name) ? 'bg-huginn-green' : 'bg-huginn-border'" />
                                    <span class="text-xs font-medium text-huginn-text group-hover:text-huginn-blue transition-colors truncate">{{ skill.display_name || skill.name }}</span>
                                  </div>
                                  <p class="text-[10px] text-huginn-muted mt-0.5 leading-relaxed line-clamp-2">{{ skill.description }}</p>
                                </div>
                                <div class="flex-shrink-0 pt-0.5">
                                  <button v-if="!isInstalled(skill.name)"
                                    @click.stop="requestInstall(skill)"
                                    :disabled="registry.isInstalling(skill.name)"
                                    class="px-2 py-0.5 text-[10px] rounded-md bg-huginn-blue text-white hover:bg-huginn-blue/80 disabled:opacity-50 transition-colors">
                                    {{ registry.isInstalling(skill.name) ? '…' : 'Install' }}
                                  </button>
                                  <div v-else class="text-[10px] text-huginn-green">
                                    <svg class="w-3 h-3" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5"><polyline points="20 6 9 17 4 12"/></svg>
                                  </div>
                                </div>
                              </div>
                            </div>
                          </div>
                        </div>
                      </Transition>
                    </div>

                    <!-- Uncollected skills divider + grid -->
                    <div v-if="groupedSkills.uncollected.length > 0">
                      <div class="flex items-center gap-3 mb-3">
                        <div class="h-px flex-1" style="background:rgba(48,54,61,0.6)" />
                        <span class="text-[10px] font-semibold uppercase tracking-widest" style="color:rgba(139,148,158,0.5)">Individual Skills</span>
                        <div class="h-px flex-1" style="background:rgba(48,54,61,0.6)" />
                      </div>
                      <div class="grid gap-2.5" style="grid-template-columns: repeat(auto-fill, minmax(260px, 1fr))">
                        <div v-for="skill in groupedSkills.uncollected" :key="skill.name"
                          @click="selectSkill(skill)"
                          class="group flex flex-col gap-2 px-4 py-3 rounded-xl border border-huginn-border bg-huginn-surface/30 hover:bg-huginn-surface/70 hover:border-huginn-border/80 cursor-pointer transition-all duration-150">
                          <div class="flex items-start justify-between gap-2">
                            <div class="flex-1 min-w-0">
                              <div class="flex items-center gap-1.5">
                                <div class="w-1.5 h-1.5 rounded-full flex-shrink-0"
                                  :class="isInstalled(skill.name) ? 'bg-huginn-green' : 'bg-huginn-border'" />
                                <span class="text-xs font-medium text-huginn-text group-hover:text-huginn-blue transition-colors truncate">{{ skill.display_name || skill.name }}</span>
                              </div>
                              <p class="text-[10px] text-huginn-muted mt-1 leading-relaxed line-clamp-2">{{ skill.description }}</p>
                            </div>
                            <div class="flex-shrink-0 pt-0.5">
                              <button v-if="!isInstalled(skill.name)"
                                @click.stop="requestInstall(skill)"
                                :disabled="registry.isInstalling(skill.name)"
                                class="px-2.5 py-1 text-[10px] rounded-lg bg-huginn-blue text-white hover:bg-huginn-blue/80 disabled:opacity-50 transition-colors whitespace-nowrap">
                                {{ registry.isInstalling(skill.name) ? '…' : 'Install' }}
                              </button>
                              <div v-else class="text-[10px] text-huginn-green">
                                <svg class="w-3 h-3" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5"><polyline points="20 6 9 17 4 12"/></svg>
                              </div>
                            </div>
                          </div>
                          <div v-if="(skill.tags ?? []).length" class="flex flex-wrap gap-1">
                            <span v-for="tag in (skill.tags ?? []).slice(0,3)" :key="tag"
                              class="text-[9px] px-1.5 py-0.5 rounded border border-huginn-border text-huginn-muted">{{ tag }}</span>
                          </div>
                        </div>
                      </div>
                    </div>

                  </template>
                </div>

              </div>
            </Transition>

            <!-- Collection detail -->
            <Transition name="detail-fade">
              <div v-if="browseKind === 'collection' && selectedCollection" class="p-8">

                <!-- Header -->
                <div class="flex items-start justify-between gap-6 mb-6">
                  <div class="flex items-start gap-4">
                    <div class="w-14 h-14 rounded-2xl flex-shrink-0 flex items-center justify-center"
                      style="background:linear-gradient(135deg,rgba(139,92,246,0.18),rgba(109,40,217,0.32));border:1px solid rgba(139,92,246,0.3)">
                      <svg class="w-6 h-6" viewBox="0 0 24 24" fill="none" stroke="rgba(192,132,252,0.9)" stroke-width="1.5" stroke-linecap="round">
                        <polygon points="12 2 15.09 8.26 22 9.27 17 14.14 18.18 21.02 12 17.77 5.82 21.02 7 14.14 2 9.27 8.91 8.26 12 2"/>
                      </svg>
                    </div>
                    <div>
                      <div class="text-[10px] font-semibold uppercase tracking-widest mb-1.5" style="color:rgba(192,132,252,0.7)">Collection</div>
                      <h1 class="text-xl font-bold text-huginn-text leading-tight">{{ selectedCollection.display_name || selectedCollection.name }}</h1>
                      <p class="text-xs text-huginn-muted mt-1">
                        by {{ selectedCollection.author }} &nbsp;·&nbsp;
                        {{ selectedCollection.skills.length }} skills &nbsp;·&nbsp;
                        {{ collectionInstalledCount(selectedCollection) }} installed
                      </p>
                    </div>
                  </div>
                  <div class="flex-shrink-0 pt-1">
                    <button v-if="!isCollectionInstalled(selectedCollection)"
                      @click="installCollection(selectedCollection)"
                      :disabled="installLoading"
                      class="px-4 py-2 text-xs rounded-lg font-medium transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
                      style="background:#58a6ff;color:#0d1117"
                      onmouseenter="this.style.opacity='0.85'" onmouseleave="this.style.opacity='1'">
                      {{ installLoading ? 'Installing…' : `Install All (${selectedCollection.skills.length})` }}
                    </button>
                    <div v-else class="flex items-center gap-1.5 text-huginn-green text-xs font-medium">
                      <svg class="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5"><polyline points="20 6 9 17 4 12"/></svg>
                      All Installed
                    </div>
                  </div>
                </div>

                <!-- Install error -->
                <div v-if="installError"
                  class="flex items-center justify-between gap-2 mb-4 px-3 py-2 rounded-lg bg-huginn-red/10 border border-huginn-red/30 text-huginn-red text-xs">
                  <span>{{ installError }}</span>
                  <button @click="installError = null" class="opacity-60 hover:opacity-100">✕</button>
                </div>

                <p v-if="selectedCollection.description" class="text-sm text-huginn-muted leading-relaxed mb-6">{{ selectedCollection.description }}</p>

                <div class="border-t border-huginn-border mb-5" />

                <!-- Skills in collection -->
                <div class="space-y-2">
                  <div v-for="skill in collectionSkillItems(selectedCollection)" :key="skill.name"
                    @click="selectSkill(skill)"
                    class="group flex items-start justify-between gap-3 px-4 py-3 rounded-xl border border-huginn-border bg-huginn-surface/30 hover:bg-huginn-surface/70 hover:border-huginn-border/80 cursor-pointer transition-all duration-150">
                    <div class="flex items-start gap-3 flex-1 min-w-0">
                      <div class="w-1.5 h-1.5 rounded-full flex-shrink-0 mt-1.5"
                        :class="isInstalled(skill.name) ? 'bg-huginn-green' : 'bg-huginn-border'" />
                      <div class="flex-1 min-w-0">
                        <div class="flex items-center gap-2">
                          <span class="text-xs font-medium text-huginn-text group-hover:text-huginn-blue transition-colors">{{ skill.display_name || skill.name }}</span>
                          <span class="text-[10px] text-huginn-muted">v{{ skill.version }}</span>
                        </div>
                        <p class="text-[11px] text-huginn-muted mt-0.5 leading-relaxed">{{ skill.description }}</p>
                        <div v-if="(skill.tags ?? []).length" class="flex flex-wrap gap-1 mt-1.5">
                          <span v-for="tag in (skill.tags ?? []).slice(0,3)" :key="tag"
                            class="text-[9px] px-1.5 py-0.5 rounded border border-huginn-border text-huginn-muted">{{ tag }}</span>
                        </div>
                      </div>
                    </div>
                    <div class="flex-shrink-0 pt-0.5">
                      <button v-if="!isInstalled(skill.name)"
                        @click.stop="requestInstall(skill)"
                        :disabled="registry.isInstalling(skill.name)"
                        class="px-3 py-1.5 text-[10px] rounded-lg bg-huginn-blue text-white hover:bg-huginn-blue/80 disabled:opacity-50 transition-colors">
                        {{ registry.isInstalling(skill.name) ? '…' : 'Install' }}
                      </button>
                      <div v-else class="text-[10px] text-huginn-green flex items-center gap-1">
                        <svg class="w-3 h-3" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5"><polyline points="20 6 9 17 4 12"/></svg>
                        Installed
                      </div>
                    </div>
                  </div>
                </div>
              </div>
            </Transition>

            <!-- Skill detail -->
            <Transition name="detail-fade">
              <div v-if="browseKind === 'skill' && selectedSkillItem" class="p-8 max-w-xl">

                <!-- Header -->
                <div class="flex items-start justify-between gap-6 mb-5">
                  <div>
                    <div class="flex items-center gap-2 mb-2 flex-wrap">
                      <span class="text-[10px] px-2 py-0.5 rounded border font-medium"
                        :class="authorBadgeClass(selectedSkillItem.author)">{{ selectedSkillItem.author }}</span>
                      <span class="text-[10px] px-1.5 py-0.5 rounded border border-huginn-border text-huginn-muted">v{{ selectedSkillItem.version }}</span>
                      <span v-if="selectedSkillItem.category" class="text-[10px] px-1.5 py-0.5 rounded border border-huginn-border text-huginn-muted">{{ selectedSkillItem.category }}</span>
                    </div>
                    <h1 class="text-xl font-bold text-huginn-text leading-tight">{{ selectedSkillItem.display_name || selectedSkillItem.name }}</h1>
                  </div>
                  <div class="flex-shrink-0 pt-1">
                    <button v-if="!isInstalled(selectedSkillItem.name)"
                      @click="requestInstall(selectedSkillItem)"
                      :disabled="registry.isInstalling(selectedSkillItem.name)"
                      class="px-4 py-2 text-xs rounded-lg font-medium disabled:opacity-50 transition-colors"
                      style="background:#58a6ff;color:#0d1117">
                      {{ registry.isInstalling(selectedSkillItem.name) ? 'Installing…' : 'Install' }}
                    </button>
                    <div v-else class="flex items-center gap-1.5 text-huginn-green text-xs font-medium">
                      <svg class="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5"><polyline points="20 6 9 17 4 12"/></svg>
                      Installed
                    </div>
                  </div>
                </div>

                <!-- Collection link -->
                <div v-if="skillParentCollection" class="mb-4">
                  <button @click="selectCollection(skillParentCollection)"
                    class="flex items-center gap-1.5 text-[11px] text-huginn-muted hover:text-purple-400 transition-colors">
                    <svg class="w-3 h-3" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
                      <polygon points="12 2 15.09 8.26 22 9.27 17 14.14 18.18 21.02 12 17.77 5.82 21.02 7 14.14 2 9.27 8.91 8.26 12 2"/>
                    </svg>
                    Part of {{ skillParentCollection.display_name || skillParentCollection.name }} collection →
                  </button>
                </div>

                <div class="border-t border-huginn-border mb-5" />

                <p class="text-sm text-huginn-muted leading-relaxed mb-6">{{ selectedSkillItem.description }}</p>

                <!-- Tags -->
                <div v-if="(selectedSkillItem.tags ?? []).length" class="flex flex-wrap gap-1.5">
                  <span v-for="tag in selectedSkillItem.tags" :key="tag"
                    class="text-[10px] px-2.5 py-1 rounded-full border border-huginn-border text-huginn-muted hover:border-huginn-blue/30 hover:text-huginn-blue cursor-pointer transition-colors"
                    @click="browseQuery = tag">
                    {{ tag }}
                  </span>
                </div>
              </div>
            </Transition>

          </div>
        </div>
      </div>
    </template>

    <!-- ── Create tab ─────────────────────────────────────────────── -->
    <template v-else-if="tab === 'create'">
      <div class="flex items-center justify-between px-6 py-4 border-b border-huginn-border flex-shrink-0">
        <div>
          <h1 class="text-huginn-text font-semibold text-sm">Create Skill</h1>
          <p class="text-huginn-muted text-xs mt-0.5">Write a custom SKILL.md and add it to your workspace.</p>
        </div>
      </div>

      <div class="px-6 py-4 max-w-2xl flex flex-col gap-4">
        <p class="text-huginn-muted text-xs">
          Paste or write your SKILL.md below. Required frontmatter: <code class="text-huginn-blue">name</code> and <code class="text-huginn-blue">version</code>.
          The body before <code class="text-huginn-blue">## Rules</code> becomes the prompt fragment; content after becomes rules.
        </p>
        <textarea
          v-model="createContent"
          class="w-full h-72 px-3 py-2 text-xs font-mono bg-huginn-surface border border-huginn-border rounded-lg text-huginn-text placeholder-huginn-muted focus:outline-none focus:border-huginn-blue/50 resize-none"
          placeholder="---&#10;name: my-skill&#10;version: 0.1.0&#10;author: you&#10;---&#10;&#10;You are an expert in..."
          spellcheck="false"
        />
        <div class="flex items-center gap-2">
          <button
            @click="saveSkill"
            :disabled="saving || !createContent.trim()"
            class="px-4 py-2 text-xs rounded-lg bg-huginn-blue text-white hover:bg-huginn-blue/80 disabled:opacity-50 transition-colors"
          >
            {{ saving ? 'Saving...' : 'Save Skill' }}
          </button>
          <p v-if="createError" class="text-huginn-red text-xs">{{ createError }}</p>
          <p v-if="createSuccess" class="text-huginn-green text-xs">✓ Skill "{{ createSuccess }}" saved</p>
        </div>
        <div class="px-4 py-3 rounded-xl border border-huginn-amber/20 bg-huginn-amber/5">
          <div class="flex items-start gap-2">
            <svg class="w-3.5 h-3.5 text-huginn-amber flex-shrink-0 mt-0.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
              <polygon points="13 2 3 14 12 14 11 22 21 10 12 10 13 2" />
            </svg>
            <div>
              <p class="text-[11px] font-semibold text-huginn-amber">Phase 2: Callable Tools</p>
              <p class="text-[11px] text-huginn-muted mt-0.5">
                Skills can also define LLM-callable tools in <code class="text-huginn-text bg-huginn-surface px-0.5 rounded">tools/*.md</code> files.
                Each tool file is a Markdown doc with YAML frontmatter (<code class="text-huginn-text bg-huginn-surface px-0.5 rounded">name</code>, <code class="text-huginn-text bg-huginn-surface px-0.5 rounded">description</code>, <code class="text-huginn-text bg-huginn-surface px-0.5 rounded">schema</code>, <code class="text-huginn-text bg-huginn-surface px-0.5 rounded">mode</code>).
                Tools support template substitution, shell execution, and nested agent calls.
              </p>
            </div>
          </div>
        </div>
      </div>
    </template>

  <!-- ── Skill Usage Modal ── -->
  <Teleport to="body">
    <Transition name="modal-fade">
      <div v-if="showUsageModal && usageModalSkill"
        class="fixed inset-0 flex items-center justify-center z-50"
        style="background:rgba(0,0,0,0.6);backdrop-filter:blur(4px)"
        @click.self="closeUsageModal">
        <div class="relative rounded-2xl overflow-hidden w-[480px] max-h-[70vh] flex flex-col"
          style="background:#13151a;border:1px solid rgba(255,255,255,0.07);box-shadow:0 25px 60px rgba(0,0,0,0.55)">

          <!-- Purple accent line -->
          <div class="absolute top-0 left-0 right-0 h-px"
            style="background:linear-gradient(90deg,transparent,rgba(139,92,246,0.5),transparent)" />

          <!-- Header -->
          <div class="flex items-start gap-3 px-5 pt-5 pb-4">
            <div class="w-8 h-8 rounded-lg flex items-center justify-center flex-shrink-0"
              style="background:rgba(139,92,246,0.15);border:1px solid rgba(139,92,246,0.25)">
              <svg class="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="rgba(167,139,250,0.9)" stroke-width="2" stroke-linecap="round">
                <path d="M17 21v-2a4 4 0 0 0-4-4H5a4 4 0 0 0-4 4v2"/>
                <circle cx="9" cy="7" r="4"/>
                <path d="M23 21v-2a4 4 0 0 0-3-3.87"/>
                <path d="M16 3.13a4 4 0 0 1 0 7.75"/>
              </svg>
            </div>
            <div class="flex-1 min-w-0">
              <h3 class="text-sm font-semibold" style="color:rgba(255,255,255,0.92)">{{ usageModalSkill.name }}</h3>
              <p class="text-[11px] mt-0.5" style="color:rgba(255,255,255,0.38)">
                {{ (agentsBySkill[usageModalSkill.name]?.length ?? 0) === 0
                  ? 'No agents assigned'
                  : `${agentsBySkill[usageModalSkill.name]!.length} agent${agentsBySkill[usageModalSkill.name]!.length === 1 ? '' : 's'} using this skill` }}
              </p>
            </div>
            <button @click="closeUsageModal"
              class="flex-shrink-0 w-6 h-6 rounded flex items-center justify-center transition-colors"
              style="color:rgba(255,255,255,0.3)"
              @mouseenter="e => (e.currentTarget as HTMLElement).style.color='rgba(255,255,255,0.7)'"
              @mouseleave="e => (e.currentTarget as HTMLElement).style.color='rgba(255,255,255,0.3)'">
              <svg class="w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round">
                <line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/>
              </svg>
            </button>
          </div>

          <!-- Divider -->
          <div style="height:1px;background:rgba(255,255,255,0.06)" />

          <!-- Agent list -->
          <div class="flex-1 overflow-y-auto px-5 py-4">
            <!-- Empty state -->
            <div v-if="(agentsBySkill[usageModalSkill.name]?.length ?? 0) === 0"
              class="flex flex-col items-center justify-center py-10 gap-3">
              <svg class="w-10 h-10" viewBox="0 0 24 24" fill="none" stroke="rgba(255,255,255,0.15)" stroke-width="1" stroke-linecap="round">
                <path d="M17 21v-2a4 4 0 0 0-4-4H5a4 4 0 0 0-4 4v2"/>
                <circle cx="9" cy="7" r="4"/>
                <path d="M23 21v-2a4 4 0 0 0-3-3.87"/>
                <path d="M16 3.13a4 4 0 0 1 0 7.75"/>
              </svg>
              <p class="text-xs text-center" style="color:rgba(255,255,255,0.3)">
                No agents have this skill assigned.<br>
                Add it from an agent's settings.
              </p>
              <router-link to="/agents" @click="closeUsageModal"
                class="text-[11px] transition-colors"
                style="color:rgba(88,166,255,0.8)"
                @mouseenter="e => (e.currentTarget as HTMLElement).style.color='rgba(88,166,255,1)'"
                @mouseleave="e => (e.currentTarget as HTMLElement).style.color='rgba(88,166,255,0.8)'">
                Go to Agents →
              </router-link>
            </div>

            <!-- Agent rows -->
            <div v-else class="space-y-2">
              <div v-for="agent in agentsBySkill[usageModalSkill.name]" :key="agent.name"
                class="group flex items-center gap-3 px-3 py-2.5 rounded-xl transition-colors"
                style="background:rgba(255,255,255,0.04);border:1px solid rgba(255,255,255,0.07)">
                <!-- Agent color badge -->
                <div class="w-7 h-7 rounded-full flex items-center justify-center flex-shrink-0 text-[11px] font-bold"
                  :style="`background:${agent.color}22;border:1px solid ${agent.color}55;color:${agent.color}`">
                  {{ agent.icon || agent.name.charAt(0).toUpperCase() }}
                </div>
                <span class="flex-1 text-xs font-medium" style="color:rgba(255,255,255,0.82)">{{ agent.name }}</span>
                <!-- Remove button -->
                <button
                  @click="removeSkillFromAgent(agent.name)"
                  :disabled="removingFromAgent === agent.name"
                  class="flex items-center gap-1 px-2 py-1 rounded-md text-[10px] transition-all duration-150 disabled:opacity-30"
                  :class="removingFromAgent === agent.name ? 'opacity-100' : 'opacity-0 group-hover:opacity-100'"
                  style="color:rgba(248,81,73,0.7);border:1px solid rgba(248,81,73,0.2);background:transparent"
                  @mouseenter="e => { if (removingFromAgent !== agent.name) (e.currentTarget as HTMLElement).style.background='rgba(248,81,73,0.1)' }"
                  @mouseleave="e => (e.currentTarget as HTMLElement).style.background='transparent'"
                  title="Remove skill from this agent">
                  <span v-if="removingFromAgent === agent.name">…</span>
                  <template v-else>
                    <svg class="w-2.5 h-2.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round">
                      <line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/>
                    </svg>
                    Remove
                  </template>
                </button>
              </div>
            </div>
          </div>

          <!-- Remove error -->
          <div v-if="removeError" class="mx-5 mb-3 px-3 py-2 rounded-lg text-[11px]"
            style="background:rgba(248,81,73,0.1);border:1px solid rgba(248,81,73,0.25);color:rgba(248,81,73,0.9)">
            {{ removeError }}
          </div>

          <!-- Footer -->
          <div style="border-top:1px solid rgba(255,255,255,0.06)" class="px-5 py-3 flex justify-end">
            <button @click="closeUsageModal"
              class="px-4 py-1.5 text-xs rounded-lg transition-colors"
              style="color:rgba(255,255,255,0.5)"
              @mouseenter="e => (e.currentTarget as HTMLElement).style.color='rgba(255,255,255,0.8)'"
              @mouseleave="e => (e.currentTarget as HTMLElement).style.color='rgba(255,255,255,0.5)'">
              Close
            </button>
          </div>
        </div>
      </div>
    </Transition>
  </Teleport>

  <!-- ── Install Confirmation Modal ───────────────────────────────── -->
  <Teleport to="body">
    <Transition name="modal-fade">
      <div v-if="pendingInstall"
        class="fixed inset-0 z-[300] flex items-center justify-center p-4"
        @mousedown.self="pendingInstall = null">
        <div class="absolute inset-0 bg-black/60 backdrop-blur-sm" />
        <div class="relative w-full max-w-sm bg-[#13151a] border border-white/[0.07] rounded-2xl flex flex-col overflow-hidden" style="box-shadow:0 25px 60px rgba(0,0,0,0.55)">
          <div class="h-px flex-shrink-0" style="background:linear-gradient(90deg,transparent,rgba(63,185,80,0.5),transparent)" />
          <div class="px-6 pt-5 pb-4">
            <div class="flex items-start gap-3.5 mb-4">
              <div class="w-10 h-10 rounded-xl flex items-center justify-center flex-shrink-0" style="background:rgba(63,185,80,0.12);border:1px solid rgba(63,185,80,0.2)">
                <svg class="w-5 h-5" style="color:rgba(63,185,80,0.85)" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round">
                  <path d="M12 2l3.09 6.26L22 9.27l-5 4.87 1.18 6.88L12 17.77l-6.18 3.25L7 14.14 2 9.27l6.91-1.01L12 2z"/>
                </svg>
              </div>
              <div class="flex-1 min-w-0">
                <p class="text-sm font-semibold" style="color:rgba(255,255,255,0.92)">{{ pendingInstall.display_name || pendingInstall.name }}</p>
                <p class="text-[11px] mt-0.5" style="color:rgba(255,255,255,0.35)">by {{ pendingInstall.author }}</p>
              </div>
            </div>
            <p class="text-xs leading-relaxed mb-5" style="color:rgba(255,255,255,0.5)">{{ pendingInstall.description }}</p>
            <div class="flex items-center gap-2.5">
              <button @click="pendingInstall = null"
                class="flex-1 py-2 text-xs font-medium rounded-lg transition-all duration-150"
                style="color:rgba(255,255,255,0.45);border:1px solid rgba(255,255,255,0.1)"
                @mouseenter="e => { (e.currentTarget as HTMLElement).style.background='rgba(255,255,255,0.05)'; (e.currentTarget as HTMLElement).style.color='rgba(255,255,255,0.65)' }"
                @mouseleave="e => { (e.currentTarget as HTMLElement).style.background='transparent'; (e.currentTarget as HTMLElement).style.color='rgba(255,255,255,0.45)' }">
                Cancel
              </button>
              <button @click="confirmInstall"
                class="flex-1 py-2 text-xs font-semibold text-white rounded-lg transition-all duration-150 active:scale-[0.97]"
                style="background:linear-gradient(135deg,rgba(63,185,80,0.9),rgba(46,160,67,0.9));box-shadow:0 2px 14px rgba(63,185,80,0.25)">
                Install
              </button>
            </div>
          </div>
        </div>
      </div>
    </Transition>
  </Teleport>

  <!-- ── Execute Skill Modal ───────────────────────────────────────── -->
  <Teleport to="body">
    <Transition name="modal-fade">
      <div v-if="executeTarget"
        class="fixed inset-0 z-[300] flex items-center justify-center p-4"
        @mousedown.self="closeExecuteModal">
        <div class="absolute inset-0 bg-black/60 backdrop-blur-sm" />
        <div class="relative w-full max-w-lg bg-[#13151a] border border-white/[0.07] rounded-2xl flex flex-col overflow-hidden" style="box-shadow:0 25px 60px rgba(0,0,0,0.55)">
          <div class="h-px flex-shrink-0" style="background:linear-gradient(90deg,transparent,rgba(88,166,255,0.5),transparent)" />
          <div class="px-5 pt-4 pb-3 flex items-center justify-between">
            <h3 class="text-xs font-semibold text-huginn-text">Run: {{ executeTarget }}</h3>
            <button @click="closeExecuteModal" class="text-huginn-muted hover:text-huginn-text transition-colors">
              <svg class="w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round">
                <line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/>
              </svg>
            </button>
          </div>
          <div style="height:1px;background:rgba(255,255,255,0.06)" />
          <div class="px-5 py-4 flex flex-col gap-3">
            <textarea
              v-model="executeInput"
              :disabled="executing"
              maxlength="32000"
              rows="5"
              placeholder="Input for the skill..."
              class="w-full px-3 py-2 text-xs font-mono bg-huginn-surface border border-huginn-border rounded-lg text-huginn-text placeholder-huginn-muted focus:outline-none focus:border-huginn-blue/50 resize-none disabled:opacity-60"
              spellcheck="false"
            />
            <div v-if="executeError" class="px-3 py-2 rounded-lg text-[11px]"
              style="background:rgba(248,81,73,0.1);border:1px solid rgba(248,81,73,0.25);color:rgba(248,81,73,0.9)">
              {{ executeError }}
            </div>
            <pre v-if="executeOutput" class="px-3 py-2 rounded-lg text-xs font-mono text-huginn-text overflow-auto max-h-48"
              style="background:rgba(255,255,255,0.04);border:1px solid rgba(255,255,255,0.08)">{{ executeOutput }}</pre>
          </div>
          <div style="border-top:1px solid rgba(255,255,255,0.06)" class="px-5 py-3 flex justify-end gap-2">
            <button v-if="executing" @click="cancelExecute"
              class="px-3 py-1.5 text-xs rounded-lg border border-huginn-red/30 text-huginn-red hover:bg-huginn-red/10 transition-colors">
              Cancel
            </button>
            <button @click="closeExecuteModal"
              class="px-3 py-1.5 text-xs rounded-lg transition-colors"
              style="color:rgba(255,255,255,0.5)"
              @mouseenter="e => (e.currentTarget as HTMLElement).style.color='rgba(255,255,255,0.8)'"
              @mouseleave="e => (e.currentTarget as HTMLElement).style.color='rgba(255,255,255,0.5)'">
              Close
            </button>
            <button @click="runExecute" :disabled="executing || !executeInput.trim()"
              class="px-4 py-1.5 text-xs font-semibold rounded-lg transition-colors disabled:opacity-50"
              style="background:rgba(88,166,255,0.15);border:1px solid rgba(88,166,255,0.3);color:rgba(88,166,255,0.9)">
              {{ executing ? 'Running…' : 'Run' }}
            </button>
          </div>
        </div>
      </div>
    </Transition>
  </Teleport>

  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted, watch } from 'vue'
import { useRouter } from 'vue-router'
import { type InstalledSkill, type RegistrySkill, type RegistryCollection, useInstalledSkills, useRegistrySkills, createSkill } from '../composables/useSkills'
import { api } from '../composables/useApi'

const props = defineProps<{ tab?: string }>()
const router = useRouter()

const tab = computed(() => props.tab || 'installed')

const installed = useInstalledSkills()
const registry = useRegistrySkills()

// ── Agent data for skill usage tracking ───────────────────────────────────
interface AgentSummary { name: string; color: string; icon: string; skills: string[] }
const allAgents = ref<AgentSummary[]>([])

async function loadAgents() {
  try {
    const raw = await api.agents.list() as Array<Record<string, unknown>>
    allAgents.value = raw.map(a => ({
      name: String(a.name ?? ''),
      color: String(a.color ?? '#58a6ff'),
      icon: String(a.icon ?? ''),
      skills: Array.isArray(a.skills) ? (a.skills as string[]) : [],
    }))
  } catch (e) { console.warn('skills: failed to load agents for usage map', e) }
}

// Map: skill name → agents that have it explicitly assigned
const agentsBySkill = computed(() => {
  const map: Record<string, AgentSummary[]> = {}
  for (const agent of allAgents.value) {
    for (const skillName of agent.skills) {
      if (!map[skillName]) map[skillName] = []
      map[skillName]!.push(agent)
    }
  }
  return map
})

// ── Skill Usage modal ─────────────────────────────────────────────────────
const showUsageModal = ref(false)
const usageModalSkill = ref<InstalledSkill | null>(null)

function openUsageModal(skill: InstalledSkill) {
  usageModalSkill.value = skill
  removeError.value = null
  showUsageModal.value = true
}

function closeUsageModal() {
  showUsageModal.value = false
  usageModalSkill.value = null
}

function onKeydown(e: KeyboardEvent) {
  if (e.key === 'Escape') {
    if (pendingInstall.value) { pendingInstall.value = null; return }
    if (executeTarget.value) { closeExecuteModal(); return }
    if (showUsageModal.value) { closeUsageModal() }
  }
}

const removingFromAgent = ref<string | null>(null)
const removeError = ref<string | null>(null)

async function removeSkillFromAgent(agentName: string) {
  if (!usageModalSkill.value) return
  const skillName = usageModalSkill.value.name
  removingFromAgent.value = agentName
  try {
    const agentData = await api.agents.get(agentName) as Record<string, unknown>
    const currentSkills: string[] = Array.isArray(agentData.skills) ? (agentData.skills as string[]) : []
    const updatedSkills = currentSkills.filter(s => s !== skillName)
    await api.agents.update(agentName, { ...agentData, skills: updatedSkills })
    // Update local state reactively so pill + modal update without re-fetching
    const idx = allAgents.value.findIndex(a => a.name === agentName)
    if (idx !== -1) {
      allAgents.value[idx] = { ...allAgents.value[idx]!, skills: updatedSkills }
    }
  } catch (e) {
    console.error('Failed to unassign skill from agent:', e)
    removeError.value = e instanceof Error ? e.message : 'Failed to remove skill'
  } finally {
    removingFromAgent.value = null
  }
}

const actionError      = ref<string | null>(null)
const pendingUninstall = ref<string | null>(null)

onMounted(() => {
  installed.load()
  loadAgents()
  if (tab.value === 'browse') registry.load()
  window.addEventListener('keydown', onKeydown)
})

onUnmounted(() => {
  window.removeEventListener('keydown', onKeydown)
})

watch(tab, (t) => {
  if (t === 'browse' && registry.index.value.length === 0) registry.load()
})

// Installed tab search
const installedQuery = ref('')
const filteredInstalled = computed(() =>
  installed.skills.value.filter(s =>
    !installedQuery.value.trim() ||
    s.name.toLowerCase().includes(installedQuery.value.toLowerCase())
  )
)

// Browse state
const browseQuery = ref('')
const categoryFilter = ref('all')
const browseKind = ref<'skill' | 'collection' | null>(null)
const selectedSkillItem = ref<RegistrySkill | null>(null)
const selectedCollection = ref<RegistryCollection | null>(null)

const CATEGORY_LABELS: Record<string, string> = {
  workflow: 'Workflow',
  language: 'Languages',
  devops: 'DevOps',
  data: 'Data',
  ai: 'AI & LLM',
  testing: 'Testing',
  documentation: 'Docs',
  debugging: 'Debugging',
  security: 'Security',
  git: 'Git',
  meta: 'Meta',
}

function categoryLabel(cat: string): string {
  return CATEGORY_LABELS[cat] ?? cat
}

function setCategory(cat: string) {
  categoryFilter.value = cat
  browseKind.value = null
  selectedSkillItem.value = null
  selectedCollection.value = null
}

const registryCategories = computed(() => {
  const cats = new Set(registry.index.value.map(s => s.category).filter(Boolean))
  // Sort by defined order, then alphabetically for unknowns
  const order = ['workflow', 'language', 'devops', 'data', 'ai', 'testing', 'documentation', 'debugging', 'security', 'git', 'meta']
  return [...order.filter(c => cats.has(c)), ...Array.from(cats).filter(c => !order.includes(c)).sort()]
})

const categorySkillCount = computed(() => {
  const counts: Record<string, number> = {}
  for (const s of registry.index.value) {
    if (s.category) counts[s.category] = (counts[s.category] ?? 0) + 1
  }
  return counts
})

// ── View mode toggle (Grid | Grouped) ─────────────────────────────────────
const viewMode = ref<'grid' | 'grouped'>('grid')
const collapsedCollections = ref(new Set<string>())

function toggleCollapse(colId: string) {
  const next = new Set(collapsedCollections.value)
  if (next.has(colId)) next.delete(colId)
  else next.add(colId)
  collapsedCollections.value = next
}

const groupedSkills = computed(() => {
  // Build groups by resolving each collection's skill ID list against filteredRegistry
  const inACollection = new Set<string>()
  const groups: { col: RegistryCollection; skills: RegistrySkill[] }[] = []

  for (const col of registry.collections.value) {
    const skills = col.skills
      .map(id => filteredRegistry.value.find(s => s.id === id))
      .filter((s): s is RegistrySkill => !!s)
    if (skills.length > 0) {
      groups.push({ col, skills })
      skills.forEach(s => inACollection.add(s.id))
    }
  }

  const uncollected = filteredRegistry.value.filter(s => !inACollection.has(s.id))
  return { groups, uncollected }
})

// Smooth height collapse transition hooks
function onCollapseEnter(el: Element) {
  const h = el as HTMLElement
  h.style.height = '0'
  h.style.overflow = 'hidden'
  requestAnimationFrame(() => {
    h.style.transition = 'height 0.22s cubic-bezier(0.4,0,0.2,1)'
    h.style.height = h.scrollHeight + 'px'
  })
}
function onCollapseAfterEnter(el: Element) {
  const h = el as HTMLElement
  h.style.height = 'auto'
  h.style.overflow = ''
  h.style.transition = ''
}
function onCollapseLeave(el: Element) {
  const h = el as HTMLElement
  h.style.height = h.scrollHeight + 'px'
  h.style.overflow = 'hidden'
  requestAnimationFrame(() => {
    h.style.transition = 'height 0.18s cubic-bezier(0.4,0,0.2,1)'
    h.style.height = '0'
  })
}
function onCollapseAfterLeave(el: Element) {
  const h = el as HTMLElement
  h.style.height = ''
  h.style.overflow = ''
  h.style.transition = ''
}

function selectSkill(skill: RegistrySkill) {
  selectedSkillItem.value = skill
  selectedCollection.value = null
  browseKind.value = 'skill'
}

function selectCollection(col: RegistryCollection) {
  selectedCollection.value = col
  selectedSkillItem.value = null
  browseKind.value = 'collection'
}

function collectionSkillItems(col: RegistryCollection): RegistrySkill[] {
  return col.skills
    .map(id => registry.index.value.find(s => s.id === id))
    .filter((s): s is RegistrySkill => !!s)
}

const skillParentCollection = computed(() => {
  if (!selectedSkillItem.value?.collection) return null
  return registry.collections.value.find(c => c.id === selectedSkillItem.value!.collection) ?? null
})

function collectionInstalledCount(col: RegistryCollection): number {
  return col.skills.filter(id => {
    const skill = registry.index.value.find(s => s.id === id)
    return skill && isInstalled(skill.name)
  }).length
}


const filteredRegistry = computed(() => {
  const q = browseQuery.value.toLowerCase().trim()
  let items = registry.index.value
  if (q) {
    items = items.filter(s =>
      (s.display_name || s.name).toLowerCase().includes(q) ||
      s.description.toLowerCase().includes(q) ||
      (s.tags ?? []).some(t => t.toLowerCase().includes(q))
    )
  }
  if (categoryFilter.value !== 'all') {
    items = items.filter(s => s.category === categoryFilter.value)
  }
  return items
})

function isCollectionInstalled(col: RegistryCollection): boolean {
  return col.skills.every(id => {
    const skill = registry.index.value.find(s => s.id === id)
    return skill && isInstalled(skill.name)
  })
}

const installError = ref<string | null>(null)
const installLoading = ref(false)

async function installCollection(col: RegistryCollection) {
  const skillsToInstall = col.skills
    .map(id => registry.index.value.find(s => s.id === id))
    .filter((s): s is RegistrySkill => !!s && !isInstalled(s.name))
  installError.value = null
  installLoading.value = true
  const failures: string[] = []
  try {
    for (const skill of skillsToInstall) {
      try {
        await registry.install(skill.name)
      } catch {
        failures.push(skill.name)
      }
    }
    await installed.load()
  } finally {
    installLoading.value = false
    if (failures.length) {
      installError.value = `Failed to install: ${failures.join(', ')}`
    }
  }
}

function isInstalled(name: string): boolean {
  return installed.skills.value.some(s => s.name === name)
}

const pendingInstall = ref<RegistrySkill | null>(null)

function requestInstall(skill: RegistrySkill) {
  pendingInstall.value = skill
}

async function confirmInstall() {
  if (!pendingInstall.value) return
  const name = pendingInstall.value.name
  pendingInstall.value = null
  await installSkill(name)
}

async function installSkill(name: string) {
  installError.value = null
  installLoading.value = true
  try {
    await registry.install(name)
    await installed.load()
  } catch (e) {
    installError.value = e instanceof Error ? e.message : 'Install failed'
  } finally {
    installLoading.value = false
  }
}

function authorBadgeClass(author: string): string {
  switch (author) {
    case 'official': return 'border-huginn-blue/30 text-huginn-blue'
    case 'superpowers': return 'border-purple-500/30 text-purple-400'
    default: return 'border-huginn-border text-huginn-muted'
  }
}

function confirmUninstall(name: string) {
  actionError.value = null
  pendingUninstall.value = name
}

async function doUninstall() {
  if (!pendingUninstall.value) return
  const name = pendingUninstall.value
  pendingUninstall.value = null
  try {
    await installed.uninstall(name)
  } catch (e: any) {
    actionError.value = `Failed to uninstall "${name}": ${e.message}`
  }
}

async function toggleSkill(name: string, enabled: boolean) {
  actionError.value = null
  try {
    await installed.toggleEnabled(name, enabled)
  } catch (e: any) {
    actionError.value = `Failed to ${enabled ? 'enable' : 'disable'} "${name}": ${e.message}`
  }
}

// ── Execute modal ─────────────────────────────────────────────────────────
const executeTarget = ref<string | null>(null)
const executeInput = ref('')
const executeOutput = ref<string | null>(null)
const executeError = ref<string | null>(null)
const executing = ref(false)
let executeAbort: AbortController | null = null

function openExecuteModal(name: string) {
  executeTarget.value = name
  executeInput.value = ''
  executeOutput.value = null
  executeError.value = null
}

function closeExecuteModal() {
  cancelExecute()
  executeTarget.value = null
}

function cancelExecute() {
  if (executeAbort) {
    executeAbort.abort()
    executeAbort = null
  }
  executing.value = false
}

async function runExecute() {
  if (!executeTarget.value || !executeInput.value.trim()) return
  executeOutput.value = null
  executeError.value = null
  executing.value = true
  executeAbort = new AbortController()
  try {
    executeOutput.value = await installed.execute(executeTarget.value, executeInput.value, executeAbort.signal)
  } catch (e: any) {
    if (e.name !== 'AbortError') {
      executeError.value = e.message ?? 'Execution failed'
    }
  } finally {
    executing.value = false
    executeAbort = null
  }
}

// Create state
const createContent = ref(`---\nname: my-skill\nversion: 0.1.0\nauthor: your-name\n---\n\nYou are an expert in...\n\n## Rules\n\n- Rule one\n`)
const saving = ref(false)
const createError = ref<string | null>(null)
const createSuccess = ref<string | null>(null)

async function saveSkill() {
  saving.value = true
  createError.value = null
  createSuccess.value = null
  try {
    const name = await createSkill(createContent.value)
    createSuccess.value = name
    await installed.load()
  } catch (e: any) {
    createError.value = e.message
  } finally {
    saving.value = false
  }
}

defineExpose({
  pendingUninstall,
  confirmUninstall,
  doUninstall,
  toggleSkill,
  actionError,
})
</script>

<style scoped>
/* Modal transition */
.modal-fade-enter-active, .modal-fade-leave-active { transition: opacity 0.15s ease, transform 0.15s ease; }
.modal-fade-enter-from, .modal-fade-leave-to { opacity: 0; }
.modal-fade-enter-from .relative, .modal-fade-leave-to .relative { transform: scale(0.96) translateY(6px); }

/* Detail panel transition */
.detail-fade-enter-active { transition: opacity 0.18s ease, transform 0.18s ease; }
.detail-fade-leave-active { transition: opacity 0.12s ease; position: absolute; inset: 0; }
.detail-fade-enter-from { opacity: 0; transform: translateY(6px); }
.detail-fade-leave-to { opacity: 0; }
</style>
