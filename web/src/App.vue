<template>
  <div class="flex h-screen bg-huginn-bg text-huginn-text font-mono overflow-hidden">

    <!-- WS degradation banner: shown after 4 s of non-connected state -->
    <Teleport to="body">
      <div v-if="showDegradedBanner"
        data-testid="ws-degraded-banner"
        class="fixed top-0 inset-x-0 z-[9999] flex items-center justify-center gap-3 px-4 py-2
               bg-huginn-red/90 text-white text-xs font-medium backdrop-blur-sm">
        <span>⚠ Connection lost — reconnecting…</span>
        <button @click="showDegradedBanner = false"
          class="ml-auto opacity-70 hover:opacity-100 transition-opacity text-base leading-none">✕</button>
      </div>
    </Teleport>

    <!-- ── Column 1: Icon strip (48px) ─────────────────────────────── -->
    <nav class="w-12 flex-shrink-0 flex flex-col items-center py-3 gap-1 border-r border-huginn-border" style="background:#090e14">

      <!-- Logo mark -->
      <div class="w-8 h-8 rounded-xl flex items-center justify-center mb-3 select-none cursor-default"
        style="background:linear-gradient(135deg,rgba(88,166,255,0.2),rgba(88,166,255,0.05));border:1px solid rgba(88,166,255,0.3)">
        <span class="text-huginn-blue font-bold text-sm leading-none">H</span>
      </div>

      <!-- Nav icons -->
      <button
        v-for="item in navItems"
        :key="item.section"
        @click="goToSection(item.path)"
        class="relative w-8 h-8 rounded-lg flex items-center justify-center transition-all duration-150 group"
        :class="activeSection === item.section
          ? 'bg-huginn-blue/20 text-huginn-blue'
          : 'text-huginn-muted hover:text-huginn-text hover:bg-huginn-surface'"
        :title="item.label"
      >
        <!-- Active left bar -->
        <div v-if="activeSection === item.section"
          class="absolute -left-3 top-1/2 -translate-y-1/2 w-0.5 h-5 bg-huginn-blue rounded-r" />

        <!-- Badge overlay for inbox -->
        <span v-if="item.section === 'inbox' && pendingCount > 0"
          class="absolute -top-0.5 -right-0.5 w-3.5 h-3.5 rounded-full bg-huginn-red text-white text-[8px] font-bold flex items-center justify-center leading-none">
          {{ pendingCount > 9 ? '9+' : pendingCount }}
        </span>

        <!-- Badge overlay for chat -->
        <span v-if="item.section === 'chat' && chatDoneCount > 0"
          class="absolute -top-0.5 -right-0.5 w-3.5 h-3.5 rounded-full bg-huginn-red text-white text-[8px] font-bold flex items-center justify-center leading-none">
          {{ chatDoneCount > 9 ? '9+' : chatDoneCount }}
        </span>

        <!-- Icon -->
        <svg class="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
          <path v-if="item.icon === 'chat'" d="M21 15a2 2 0 01-2 2H7l-4 4V5a2 2 0 012-2h14a2 2 0 012 2z" />
          <g v-else-if="item.icon === 'agents'">
            <circle cx="12" cy="8" r="4" />
            <path d="M6 21v-2a4 4 0 014-4h4a4 4 0 014 4v2" />
          </g>
          <g v-else-if="item.icon === 'models'">
            <path d="M12 2L2 7l10 5 10-5-10-5z" />
            <path d="M2 17l10 5 10-5" />
            <path d="M2 12l10 5 10-5" />
          </g>
          <g v-else-if="item.icon === 'connections'">
            <path d="M10 13a5 5 0 007.54.54l3-3a5 5 0 00-7.07-7.07l-1.72 1.71" />
            <path d="M14 11a5 5 0 00-7.54-.54l-3 3a5 5 0 007.07 7.07l1.71-1.71" />
          </g>
          <g v-else-if="item.icon === 'cloud'">
            <path d="M18 10h-1.26A8 8 0 1 0 9 20h9a5 5 0 0 0 0-10z"/>
          </g>
          <g v-else-if="item.icon === 'skills'">
            <path d="M12 2l3.09 6.26L22 9.27l-5 4.87 1.18 6.88L12 17.77l-6.18 3.25L7 14.14 2 9.27l6.91-1.01L12 2z"/>
          </g>
          <g v-else-if="item.icon === 'settings'">
            <circle cx="12" cy="12" r="3" />
            <path d="M19.4 15a1.65 1.65 0 00.33 1.82l.06.06a2 2 0 010 2.83 2 2 0 01-2.83 0l-.06-.06a1.65 1.65 0 00-1.82-.33 1.65 1.65 0 00-1 1.51V21a2 2 0 01-4 0v-.09A1.65 1.65 0 009 19.4a1.65 1.65 0 00-1.82.33l-.06.06a2 2 0 01-2.83-2.83l.06-.06A1.65 1.65 0 004.68 15a1.65 1.65 0 00-1.51-1H3a2 2 0 010-4h.09A1.65 1.65 0 004.6 9a1.65 1.65 0 00-.33-1.82l-.06-.06a2 2 0 012.83-2.83l.06.06A1.65 1.65 0 009 4.68a1.65 1.65 0 001-1.51V3a2 2 0 014 0v.09a1.65 1.65 0 001 1.51 1.65 1.65 0 001.82-.33l.06-.06a2 2 0 012.83 2.83l-.06.06A1.65 1.65 0 0019.4 9a1.65 1.65 0 001.51 1H21a2 2 0 010 4h-.09a1.65 1.65 0 00-1.51 1z" />
          </g>
          <g v-else-if="item.icon === 'logs'">
            <path d="M14 2H6a2 2 0 00-2 2v16a2 2 0 002 2h12a2 2 0 002-2V8z" />
            <polyline points="14 2 14 8 20 8" />
            <line x1="16" y1="13" x2="8" y2="13" />
            <line x1="16" y1="17" x2="8" y2="17" />
          </g>
          <g v-else-if="item.icon === 'stats'">
            <line x1="18" y1="20" x2="18" y2="10" />
            <line x1="12" y1="20" x2="12" y2="4" />
            <line x1="6" y1="20" x2="6" y2="14" />
          </g>
          <!-- Inbox icon (bell) -->
          <g v-else-if="item.icon === 'inbox'">
            <path d="M18 8A6 6 0 006 8c0 7-3 9-3 9h18s-3-2-3-9" />
            <path d="M13.73 21a2 2 0 01-3.46 0" />
          </g>
          <!-- Automation icon (bolt/lightning) -->
          <g v-else-if="item.icon === 'automation'">
            <polygon points="13 2 3 14 12 14 11 22 21 10 12 10 13 2" />
          </g>
        </svg>

        <!-- Tooltip -->
        <div class="absolute left-full ml-2 px-2 py-1 bg-huginn-surface border border-huginn-border rounded text-xs text-huginn-text whitespace-nowrap pointer-events-none z-50
                    opacity-0 group-hover:opacity-100 transition-opacity duration-100">
          {{ item.label }}
        </div>
      </button>

      <!-- Spacer -->
      <div class="flex-1" />

      <!-- Profile / Cloud button -->
      <div class="relative mb-1" ref="profileButtonRef">
        <button
          @click="toggleProfilePopover"
          class="relative w-8 h-8 rounded-lg flex items-center justify-center transition-all duration-150 group text-huginn-muted hover:text-huginn-text hover:bg-huginn-surface"
          :class="profilePopoverOpen ? 'bg-huginn-surface text-huginn-text' : ''"
          title="Account & Cloud"
        >
          <svg class="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
            <circle cx="12" cy="8" r="4" />
            <path d="M6 21v-2a4 4 0 014-4h4a4 4 0 014 4v2" />
          </svg>
          <!-- Status dot: green=local, blue=cloud, red=unreachable -->
          <span data-testid="ws-status-dot" class="absolute -top-0.5 -right-0.5 w-2.5 h-2.5 rounded-full border-2 border-[#090e14] transition-colors duration-300"
            :class="!wsConnected ? 'bg-huginn-red' : cloudConnected ? 'bg-huginn-blue' : 'bg-huginn-green'"
            :style="!wsConnected ? '' : cloudConnected ? 'box-shadow:0 0 4px rgba(88,166,255,0.5)' : 'box-shadow:0 0 4px rgba(63,185,80,0.5)'" />
        </button>

        <!-- Popover (opens upward + right) -->
        <div v-if="profilePopoverOpen"
          class="absolute bottom-full left-full ml-2 mb-1 w-64 bg-huginn-surface border border-huginn-border rounded-xl shadow-2xl z-50 overflow-hidden"
        >
          <!-- Cloud status -->
          <div class="px-4 py-3">
            <div class="flex items-center gap-2 mb-1">
              <div class="w-2 h-2 rounded-full flex-shrink-0 transition-colors duration-300"
                :class="cloudConnected ? 'bg-huginn-blue' : 'bg-huginn-muted/40'"
                :style="cloudConnected ? 'box-shadow:0 0 4px rgba(88,166,255,0.5)' : ''" />
              <span class="text-xs font-semibold text-huginn-text">
                {{ cloudConnected ? 'Huginn Cloud' : 'Not connected' }}
              </span>
            </div>
            <template v-if="cloudConnected">
              <p v-if="cloudStatus.machine_id" class="text-[11px] text-huginn-muted ml-4 truncate">{{ cloudStatus.machine_id }}</p>
              <p v-if="cloudStatus.cloud_url"   class="text-[11px] text-huginn-muted ml-4 truncate">{{ cloudStatus.cloud_url }}</p>
              <button @click="disconnectCloud" :disabled="cloudDisconnecting"
                class="mt-2 ml-4 text-[10px] text-huginn-muted hover:text-huginn-red transition-colors disabled:opacity-50">
                {{ cloudDisconnecting ? 'Disconnecting...' : 'Disconnect' }}
              </button>
            </template>
            <template v-else>
              <p class="text-[11px] text-huginn-muted ml-4 mb-2 leading-relaxed">Access your agents from anywhere</p>
              <button @click="connectCloud" :disabled="cloudConnecting"
                class="ml-4 px-3 py-1.5 text-[10px] rounded-lg bg-huginn-blue text-white hover:bg-huginn-blue/80 disabled:opacity-50 transition-colors">
                {{ cloudConnecting ? 'Connecting...' : 'Connect to Huginn Cloud' }}
              </button>
            </template>
          </div>

          <!-- Divider -->
          <div class="border-t border-huginn-border" />

          <!-- Local server status -->
          <div class="px-4 py-2.5 flex items-center gap-2">
            <div class="w-1.5 h-1.5 rounded-full flex-shrink-0 transition-colors duration-300"
              :class="wsConnected ? 'bg-huginn-green' : 'bg-huginn-red'" />
            <span class="text-[11px] text-huginn-muted">
              Local server {{ wsConnected ? 'reachable' : 'unreachable' }}
            </span>
          </div>
        </div>
      </div>
    </nav>

    <!-- ── Column 2: Context panel (240px, conditional) ─────────────── -->
    <transition
      enter-active-class="transition-[width,opacity] duration-200 ease-out"
      enter-from-class="opacity-0"
      enter-to-class="opacity-100"
      leave-active-class="transition-[width,opacity] duration-150 ease-in"
      leave-from-class="opacity-100"
      leave-to-class="opacity-0"
    >
      <aside v-if="showPanel"
        class="w-60 flex-shrink-0 flex flex-col bg-huginn-surface border-r border-huginn-border overflow-hidden">

        <!-- Panel header -->
        <div class="flex items-center gap-2 px-3 h-11 border-b border-huginn-border flex-shrink-0">
          <!-- Chat: search bar -->
          <template v-if="activeSection === 'chat'">
            <svg class="w-3 h-3 text-huginn-muted/40 flex-shrink-0" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
              <circle cx="11" cy="11" r="8"/><line x1="21" y1="21" x2="16.65" y2="16.65"/>
            </svg>
            <input
              v-model="sidebarSearch"
              placeholder="Search…"
              class="flex-1 bg-transparent text-xs text-huginn-text placeholder-huginn-muted/35 outline-none min-w-0"
            />
            <button v-if="sidebarSearch" @click="sidebarSearch = ''"
              class="w-4 h-4 flex items-center justify-center text-huginn-muted/40 hover:text-huginn-muted transition-colors flex-shrink-0">
              <svg class="w-2.5 h-2.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round">
                <line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/>
              </svg>
            </button>
          </template>
          <!-- Agents: search bar + new button -->
          <template v-else-if="activeSection === 'agents'">
            <svg class="w-3 h-3 text-huginn-muted/40 flex-shrink-0" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
              <circle cx="11" cy="11" r="8"/><line x1="21" y1="21" x2="16.65" y2="16.65"/>
            </svg>
            <input
              v-model="agentSearch"
              placeholder="Search…"
              class="flex-1 bg-transparent text-xs text-huginn-text placeholder-huginn-muted/35 outline-none min-w-0"
            />
            <button v-if="agentSearch" @click="agentSearch = ''"
              class="w-4 h-4 flex items-center justify-center text-huginn-muted/40 hover:text-huginn-muted transition-colors flex-shrink-0">
              <svg class="w-2.5 h-2.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round">
                <line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/>
              </svg>
            </button>
            <button @click="handleNewItem"
              class="w-6 h-6 rounded flex items-center justify-center text-huginn-muted hover:text-huginn-blue hover:bg-huginn-bg transition-all duration-150 flex-shrink-0"
              title="New">
              <svg class="w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round">
                <line x1="12" y1="5" x2="12" y2="19" />
                <line x1="5" y1="12" x2="19" y2="12" />
              </svg>
            </button>
          </template>
          <!-- Other sections: title + new button -->
          <template v-else>
            <span class="flex-1 text-[11px] font-semibold text-huginn-muted uppercase tracking-widest select-none">
              {{ panelTitle }}
            </span>
            <button v-if="activeSection !== 'automation'" @click="handleNewItem"
              class="w-6 h-6 rounded flex items-center justify-center text-huginn-muted hover:text-huginn-blue hover:bg-huginn-bg transition-all duration-150"
              title="New">
              <svg class="w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round">
                <line x1="12" y1="5" x2="12" y2="19" />
                <line x1="5" y1="12" x2="19" y2="12" />
              </svg>
            </button>
          </template>
        </div>

        <!-- ── Chat panel: Channels + DMs + Sessions ── -->
        <div v-if="activeSection === 'chat'" class="flex-1 overflow-y-auto">

          <!-- Spaces loading spinner -->
          <div v-if="spacesLoading && channels.length === 0 && dms.length === 0"
            class="flex items-center justify-center py-6">
            <div class="w-3.5 h-3.5 border border-huginn-border border-t-huginn-blue rounded-full animate-spin" />
          </div>

          <!-- Spaces fetch error -->
          <div v-if="spacesError && !spacesLoading"
            class="mx-3 my-2 px-3 py-2 rounded bg-huginn-red/10 border border-huginn-red/30">
            <p class="text-[11px] text-huginn-red">{{ spacesError }}</p>
          </div>

          <template v-else>

            <!-- ── Channels ───────────────────────────────────── -->
            <div v-if="channels.length > 0 || true">
              <!-- Section header -->
              <div class="group w-full flex items-center gap-1.5 px-3 py-2 hover:bg-huginn-bg/40 transition-colors duration-100">
                <button @click="channelSectionOpen = !channelSectionOpen"
                  class="flex items-center gap-1.5 flex-1 min-w-0">
                  <svg class="w-2.5 h-2.5 text-huginn-muted/50 transition-transform duration-150 flex-shrink-0"
                    :class="channelSectionOpen || sidebarSearch ? 'rotate-90' : ''"
                    viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round">
                    <polyline points="9 18 15 12 9 6" />
                  </svg>
                  <span class="text-[10px] font-semibold text-huginn-muted/60 group-hover:text-huginn-muted uppercase tracking-widest">
                    Channels
                  </span>
                </button>
                <span v-if="channels.length" class="text-[10px] text-huginn-muted/30 tabular-nums">{{ channels.length }}</span>
                <button @click="showCreateChannelModal = true"
                  data-testid="create-channel-btn"
                  class="w-4 h-4 flex items-center justify-center text-huginn-muted/30 hover:text-huginn-blue transition-colors flex-shrink-0 ml-1"
                  title="New channel">
                  <svg class="w-3 h-3" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round">
                    <line x1="12" y1="5" x2="12" y2="19" /><line x1="5" y1="12" x2="19" y2="12" />
                  </svg>
                </button>
              </div>

              <template v-if="channelSectionOpen || sidebarSearch">
                <div v-if="filteredChannels.length === 0" class="px-4 pb-1">
                  <p class="text-[11px] text-huginn-muted/35 italic pl-4">{{ sidebarSearch ? 'No matches' : 'No channels yet' }}</p>
                </div>
                <button
                  v-for="sp in filteredChannels"
                  :key="sp.id"
                  :data-testid="`channel-item-${sp.id}`"
                  @click="selectSpace(sp.id)"
                  class="relative w-full flex items-center gap-2 px-3 py-1.5 text-left transition-colors duration-100 group/item"
                  :class="activeSpaceId === sp.id
                    ? 'bg-huginn-blue/8 text-huginn-text'
                    : 'text-huginn-muted hover:bg-huginn-bg/40 hover:text-huginn-text'"
                >
                  <div v-if="activeSpaceId === sp.id"
                    class="absolute left-0 top-1/2 -translate-y-1/2 w-0.5 h-4 bg-huginn-blue rounded-r" />
                  <!-- Channel # icon with activity pulse when lead agent is active -->
                  <div class="relative flex-shrink-0 w-4 text-center">
                    <span class="text-[13px] font-medium text-huginn-muted/60">#</span>
                    <span v-if="sp.leadAgent && isAgentActive(sp.leadAgent)"
                      class="absolute -bottom-0.5 -right-0.5 w-1.5 h-1.5 rounded-full bg-huginn-green border border-huginn-sidebar animate-pulse" />
                  </div>
                  <!-- Unseen count badge for channels -->
                  <span v-if="sp.unseenCount > 0"
                    :data-testid="`channel-unseen-${sp.id}`"
                    class="flex-shrink-0 min-w-[16px] h-4 px-1 rounded-full bg-huginn-blue text-white text-[9px] font-bold flex items-center justify-center leading-none">
                    {{ sp.unseenCount > 9 ? '9+' : sp.unseenCount }}
                  </span>
                  <div class="flex-1 min-w-0">
                    <!-- Inline rename input -->
                    <input v-if="renamingSpaceId === sp.id"
                      :ref="(el) => el && (el as HTMLInputElement).focus()"
                      v-model="spaceRenameValue"
                      @keydown.enter.stop="commitSpaceRename(sp.id)"
                      @keydown.escape.stop="renamingSpaceId = ''"
                      @blur="commitSpaceRename(sp.id)"
                      @click.stop
                      class="w-full bg-huginn-bg border border-huginn-blue/60 rounded px-1 text-xs text-huginn-text outline-none"
                    />
                    <span v-else class="text-xs truncate block"
                      :class="activeSpaceId === sp.id ? 'text-huginn-text font-medium' : ''">
                      {{ sp.name }}
                    </span>
                    <span v-if="!renamingSpaceId && spaceLastMessage(sp.id)"
                      class="text-[10px] text-huginn-muted/50 truncate block leading-tight">
                      {{ spaceLastMessage(sp.id)!.text }}
                    </span>
                  </div>
                  <!-- ⋯ menu -->
                  <div v-if="renamingSpaceId !== sp.id" class="relative flex-shrink-0 space-menu">
                    <div @click.stop="spaceMenuId = spaceMenuId === sp.id ? null : sp.id"
                      class="w-5 h-5 flex items-center justify-center text-huginn-muted/30 hover:text-huginn-muted cursor-pointer opacity-0 group-hover/item:opacity-100 transition-opacity rounded"
                      title="Channel options">
                      <svg class="w-3 h-3" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round">
                        <circle cx="12" cy="5" r="1"/><circle cx="12" cy="12" r="1"/><circle cx="12" cy="19" r="1"/>
                      </svg>
                    </div>
                    <div v-if="spaceMenuId === sp.id"
                      class="absolute right-0 top-full mt-1 w-32 rounded-lg border border-huginn-border shadow-xl overflow-hidden z-50 space-menu"
                      style="background:rgba(22,27,34,0.97)">
                      <div @click.stop="startSpaceRename(sp)"
                        class="flex items-center gap-2 px-3 py-2 text-xs text-huginn-muted hover:text-huginn-text hover:bg-huginn-surface cursor-pointer transition-colors">
                        <svg class="w-3 h-3" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
                          <path d="M11 4H4a2 2 0 00-2 2v14a2 2 0 002 2h14a2 2 0 002-2v-7"/><path d="M18.5 2.5a2.121 2.121 0 013 3L12 15l-4 1 1-4 9.5-9.5z"/>
                        </svg>
                        Rename
                      </div>
                      <div @click.stop="doDeleteSpace(sp.id)"
                        class="flex items-center gap-2 px-3 py-2 text-xs text-huginn-red/70 hover:text-huginn-red hover:bg-huginn-red/5 cursor-pointer transition-colors">
                        <svg class="w-3 h-3" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
                          <polyline points="3 6 5 6 21 6"/><path d="M19 6l-1 14a2 2 0 01-2 2H8a2 2 0 01-2-2L5 6m3 0V4a1 1 0 011-1h4a1 1 0 011 1v2"/>
                        </svg>
                        Delete
                      </div>
                    </div>
                  </div>
                </button>

              </template>
            </div>

            <!-- Divider -->
            <div class="mx-3 border-t border-huginn-border/30 my-1" />

            <!-- ── Direct Messages ────────────────────────────── -->
            <div>
              <button @click="dmSectionOpen = !dmSectionOpen"
                class="group w-full flex items-center gap-1.5 px-3 py-2 hover:bg-huginn-bg/40 transition-colors duration-100">
                <svg class="w-2.5 h-2.5 text-huginn-muted/50 transition-transform duration-150 flex-shrink-0"
                  :class="dmSectionOpen ? 'rotate-90' : ''"
                  viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round">
                  <polyline points="9 18 15 12 9 6" />
                </svg>
                <span class="text-[10px] font-semibold text-huginn-muted/60 group-hover:text-huginn-muted uppercase tracking-widest">
                  Direct Messages
                </span>
                <span v-if="dms.length" class="text-[10px] text-huginn-muted/30 tabular-nums ml-auto">{{ dms.length }}</span>
              </button>

              <template v-if="dmSectionOpen || sidebarSearch">
                <div v-if="filteredDMs.length === 0" class="px-4 pb-1">
                  <p class="text-[11px] text-huginn-muted/35 italic pl-4">{{ sidebarSearch ? 'No matches' : 'No agents configured' }}</p>
                </div>
                <button
                  v-for="sp in filteredDMs"
                  :key="sp.id"
                  :data-testid="`dm-item-${sp.id}`"
                  @click="selectSpace(sp.id)"
                  class="relative w-full flex items-center gap-2 px-3 py-1.5 text-left transition-colors duration-100 group/item"
                  :class="activeSpaceId === sp.id
                    ? 'bg-huginn-blue/8 text-huginn-text'
                    : 'text-huginn-muted hover:bg-huginn-bg/40 hover:text-huginn-text'"
                >
                  <div v-if="activeSpaceId === sp.id"
                    class="absolute left-0 top-1/2 -translate-y-1/2 w-0.5 h-4 bg-huginn-blue rounded-r" />
                  <!-- Agent color dot with activity indicator -->
                  <div class="relative flex-shrink-0">
                    <div class="w-5 h-5 rounded-md flex items-center justify-center text-[10px] font-bold"
                      :style="{ background: (agentColorMap[sp.leadAgent] || '#58a6ff') + '33', color: agentColorMap[sp.leadAgent] || '#58a6ff' }">
                      {{ sp.leadAgent?.[0]?.toUpperCase() ?? '?' }}
                    </div>
                    <span v-if="sp.leadAgent && isAgentActive(sp.leadAgent)"
                      class="absolute -bottom-0.5 -right-0.5 w-2 h-2 rounded-full bg-huginn-green border border-huginn-sidebar animate-pulse" />
                  </div>
                  <div class="flex-1 min-w-0">
                    <span class="text-xs truncate block"
                      :class="activeSpaceId === sp.id ? 'text-huginn-text font-medium' : ''">
                      {{ sp.leadAgent }}
                    </span>
                    <span v-if="spaceLastMessage(sp.id)"
                      class="text-[10px] text-huginn-muted/50 truncate block leading-tight">
                      {{ spaceLastMessage(sp.id)!.text }}
                    </span>
                  </div>
                  <div class="flex flex-col items-end gap-0.5 flex-shrink-0">
                    <span v-if="sp.unseenCount > 0"
                      :data-testid="`dm-unseen-${sp.id}`"
                      class="min-w-[16px] h-4 px-1 rounded-full bg-huginn-blue text-white text-[9px] font-bold flex items-center justify-center leading-none">
                      {{ sp.unseenCount > 9 ? '9+' : sp.unseenCount }}
                    </span>
                    <span v-else-if="spaceLastMessage(sp.id)?.relTime"
                      class="text-[10px] text-huginn-muted/40">
                      {{ spaceLastMessage(sp.id)!.relTime }}
                    </span>
                  </div>
                  <!-- ⋯ menu (DM: delete only) -->
                  <div class="relative flex-shrink-0 space-menu">
                    <div @click.stop="spaceMenuId = spaceMenuId === sp.id ? null : sp.id"
                      class="w-5 h-5 flex items-center justify-center text-huginn-muted/30 hover:text-huginn-muted cursor-pointer opacity-0 group-hover/item:opacity-100 transition-opacity rounded"
                      title="DM options">
                      <svg class="w-3 h-3" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round">
                        <circle cx="12" cy="5" r="1"/><circle cx="12" cy="12" r="1"/><circle cx="12" cy="19" r="1"/>
                      </svg>
                    </div>
                    <div v-if="spaceMenuId === sp.id"
                      class="absolute right-0 top-full mt-1 w-28 rounded-lg border border-huginn-border shadow-xl overflow-hidden z-50 space-menu"
                      style="background:rgba(22,27,34,0.97)">
                      <div @click.stop="doDeleteSpace(sp.id)"
                        class="flex items-center gap-2 px-3 py-2 text-xs text-huginn-red/70 hover:text-huginn-red hover:bg-huginn-red/5 cursor-pointer transition-colors">
                        <svg class="w-3 h-3" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
                          <polyline points="3 6 5 6 21 6"/><path d="M19 6l-1 14a2 2 0 01-2 2H8a2 2 0 01-2-2L5 6m3 0V4a1 1 0 011-1h4a1 1 0 011 1v2"/>
                        </svg>
                        Delete
                      </div>
                    </div>
                  </div>
                </button>
              </template>
            </div>


          </template>
        </div>

        <!-- ── Automation stacked sections ── -->
        <div v-else-if="activeSection === 'automation'" class="flex-1 overflow-y-auto">
          <!-- Loading spinner -->
          <div v-if="automationLoading" class="flex items-center justify-center py-10">
            <div class="w-4 h-4 border border-huginn-border border-t-huginn-blue rounded-full animate-spin" />
          </div>

          <template v-else>

            <!-- ── Notifications ─────────────────────────────── -->
            <div class="mt-1">
              <!-- Section header row -->
              <button @click="router.push('/inbox')"
                class="group w-full flex items-center gap-2 px-3 py-2 transition-colors duration-100"
                :class="route.path === '/inbox' ? 'bg-huginn-blue/8' : 'hover:bg-huginn-bg/50'">
                <!-- icon -->
                <div class="w-5 h-5 rounded flex items-center justify-center flex-shrink-0"
                  :class="route.path === '/inbox' ? 'text-huginn-blue' : 'text-huginn-muted/60 group-hover:text-huginn-muted'">
                  <svg class="w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M18 8A6 6 0 0 0 6 8c0 7-3 9-3 9h18s-3-2-3-9"/><path d="M13.73 21a2 2 0 0 1-3.46 0"/></svg>
                </div>
                <span class="text-[11px] font-semibold flex-1 text-left"
                  :class="route.path === '/inbox' ? 'text-huginn-blue' : 'text-huginn-muted group-hover:text-huginn-text'">
                  Notifications
                </span>
                <span v-if="pendingCount > 0"
                  class="min-w-[16px] h-4 px-1 rounded-full bg-huginn-red text-white text-[9px] font-bold flex items-center justify-center leading-none flex-shrink-0">
                  {{ pendingCount > 9 ? '9+' : pendingCount }}
                </span>
                <svg v-else class="w-2.5 h-2.5 text-huginn-muted/30 group-hover:text-huginn-muted/60 transition-colors flex-shrink-0" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round"><polyline points="9 18 15 12 9 6"/></svg>
              </button>

              <!-- Items -->
              <div v-if="!notifications.length" class="px-4 pb-1 pt-0.5">
                <p class="text-[11px] text-huginn-muted/40 italic pl-7">No notifications yet</p>
              </div>
              <button
                v-for="n in notifications.slice(0, 3)"
                :key="n.id"
                @click="router.push('/inbox')"
                class="w-full flex items-center gap-2.5 pl-10 pr-3 py-1.5 text-left transition-colors duration-100 hover:bg-huginn-bg/40 group/item">
                <div class="w-1.5 h-1.5 rounded-full flex-shrink-0"
                  :class="{ 'bg-huginn-red': n.severity==='urgent', 'bg-yellow-400': n.severity==='warning', 'bg-huginn-blue/60': n.severity==='info' }"
                  :style="n.status==='pending' && n.severity==='urgent' ? 'box-shadow:0 0 4px rgba(248,81,73,0.5)' : ''" />
                <span class="text-[11px] flex-1 truncate"
                  :class="n.status==='pending' ? 'text-huginn-text font-medium' : 'text-huginn-muted group-hover/item:text-huginn-text/80'">
                  {{ n.summary }}
                </span>
              </button>
            </div>

            <!-- Section divider -->
            <div class="mx-3 border-t border-huginn-border/30 my-1" />

            <!-- ── Workflows ──────────────────────────────────── -->
            <div class="pb-2">
              <button @click="router.push('/workflows')"
                class="group w-full flex items-center gap-2 px-3 py-2 transition-colors duration-100"
                :class="route.path.startsWith('/workflows') && !route.params.id ? 'bg-huginn-blue/8' : 'hover:bg-huginn-bg/50'">
                <div class="w-5 h-5 rounded flex items-center justify-center flex-shrink-0"
                  :class="route.path.startsWith('/workflows') ? 'text-huginn-blue' : 'text-huginn-muted/60 group-hover:text-huginn-muted'">
                  <svg class="w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="3" width="7" height="7"/><rect x="14" y="3" width="7" height="7"/><rect x="14" y="14" width="7" height="7"/><rect x="3" y="14" width="7" height="7"/></svg>
                </div>
                <span class="text-[11px] font-semibold flex-1 text-left"
                  :class="route.path.startsWith('/workflows') ? 'text-huginn-blue' : 'text-huginn-muted group-hover:text-huginn-text'">
                  Workflows
                </span>
                <span class="text-[10px] text-huginn-muted/35 tabular-nums flex-shrink-0">{{ workflows.length || '' }}</span>
                <svg class="w-2.5 h-2.5 text-huginn-muted/30 group-hover:text-huginn-muted/60 transition-colors flex-shrink-0" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round"><polyline points="9 18 15 12 9 6"/></svg>
              </button>

              <div v-if="workflows.length === 0" class="pb-1">
                <p class="text-[11px] text-huginn-muted/40 italic pl-10 pr-3">No workflows yet</p>
              </div>
              <button
                v-for="wf in workflows.slice(0, 6)"
                :key="wf.id"
                @click="router.push('/workflows/' + wf.id)"
                class="relative w-full flex items-center gap-2.5 pl-10 pr-3 py-1.5 text-left transition-colors duration-100 group/item"
                :class="route.path.startsWith('/workflows') && route.params.id === wf.id
                  ? 'bg-huginn-blue/8'
                  : 'hover:bg-huginn-bg/40'"
              >
                <div v-if="route.path.startsWith('/workflows') && route.params.id === wf.id"
                  class="absolute left-0 top-1/2 -translate-y-1/2 w-0.5 h-4 bg-huginn-blue rounded-r" />
                <div class="w-1.5 h-1.5 rounded-full flex-shrink-0"
                  :class="wf.enabled ? 'bg-huginn-green' : 'bg-huginn-muted/30'"
                  :style="wf.enabled ? 'box-shadow:0 0 4px rgba(63,185,80,0.35)' : ''" />
                <span class="text-[11px] flex-1 truncate"
                  :class="route.path.startsWith('/workflows') && route.params.id === wf.id
                    ? 'text-huginn-text font-medium'
                    : 'text-huginn-muted/80 group-hover/item:text-huginn-text'">
                  {{ wf.name }}
                </span>
              </button>
            </div>
          </template>
        </div>

        <!-- ── Agents list ── -->
        <div v-else-if="activeSection === 'agents'" data-testid="agent-list" class="flex-1 overflow-y-auto py-1.5">
          <div v-if="agentsLoading" class="flex items-center justify-center py-10">
            <div class="w-4 h-4 border border-huginn-border border-t-huginn-blue rounded-full animate-spin" />
          </div>
          <div v-else-if="agents.length === 0" class="flex flex-col items-center justify-center py-10 px-4 text-center gap-2">
            <p class="text-huginn-muted text-xs">No agents configured</p>
          </div>
          <p v-else-if="agentSearch && !agents.some(a => String(a.name).toLowerCase().includes(agentSearch.toLowerCase()))"
            class="text-[11px] text-huginn-muted/35 italic pl-4 py-2">No matches</p>
          <button
            v-for="agent in agents.filter(a => !agentSearch || String(a.name).toLowerCase().includes(agentSearch.toLowerCase()))"
            :key="String(agent.name)"
            data-testid="agent-item"
            @click="router.push('/agents/' + agent.name)"
            class="relative w-full flex items-center gap-2.5 px-3 py-2 text-left transition-colors duration-100 group"
            :class="route.params.agentName === agent.name
              ? 'bg-huginn-bg/80 text-huginn-text'
              : 'text-huginn-muted hover:bg-huginn-bg/40 hover:text-huginn-text'"
          >
            <div v-if="route.params.agentName === agent.name"
              class="absolute left-0 top-1/2 -translate-y-1/2 w-0.5 h-4 bg-huginn-blue rounded-r" />
            <!-- Agent color dot -->
            <div class="w-5 h-5 rounded-md flex items-center justify-center text-[10px] font-bold text-white flex-shrink-0"
              :style="{ background: (agent.color as string) || '#58a6ff' }">
              {{ (agent.icon as string) || (agent.name as string)?.[0]?.toUpperCase() }}
            </div>
            <span class="text-xs flex-1 truncate">{{ agent.name }}</span>
          </button>
        </div>

        <!-- ── Skills navigation items ── -->
        <div v-else-if="activeSection === 'skills'" class="flex-1 py-3 space-y-0.5 px-2">
          <button v-for="item in skillsNavItems" :key="item.tab"
            @click="router.push('/skills/' + item.tab)"
            class="relative w-full flex items-center gap-2.5 px-2 py-2 rounded-lg text-left transition-colors duration-100"
            :class="(route.params.tab as string) === item.tab || (!route.params.tab && item.tab === 'installed')
              ? 'bg-huginn-bg/80 text-huginn-text'
              : 'text-huginn-muted hover:bg-huginn-bg/40 hover:text-huginn-text'"
          >
            <div class="absolute left-0 top-1/2 -translate-y-1/2 w-0.5 h-4 bg-huginn-blue rounded-r"
              v-if="(route.params.tab as string) === item.tab || (!route.params.tab && item.tab === 'installed')" />
            <span class="text-xs">{{ item.label }}</span>
          </button>
        </div>
      </aside>
    </transition>

    <!-- ── Space Create Modal ───────────────────────────────────────── -->
    <SpaceCreateModal
      v-if="showCreateChannelModal"
      @close="showCreateChannelModal = false"
      @created="(id) => { showCreateChannelModal = false; selectSpace(id) }"
    />

    <!-- ── Global Search Modal (Cmd+K) ──────────────────────────────── -->
    <Teleport to="body">
      <Transition
        enter-active-class="transition-all duration-150 ease-out"
        enter-from-class="opacity-0"
        enter-to-class="opacity-100"
        leave-active-class="transition-all duration-100 ease-in"
        leave-from-class="opacity-100"
        leave-to-class="opacity-0"
      >
        <div v-if="globalSearchOpen"
          class="fixed inset-0 z-50 flex items-start justify-center pt-24"
          style="background:rgba(0,0,0,0.6);backdrop-filter:blur(2px)"
          @click.self="globalSearchOpen = false"
          @keydown.escape="globalSearchOpen = false"
        >
          <div class="w-full max-w-xl mx-4 rounded-2xl overflow-hidden shadow-2xl border border-huginn-border"
            style="background:rgba(22,27,34,0.97)">
            <!-- Search input -->
            <div class="flex items-center gap-3 px-4 py-3 border-b border-huginn-border">
              <svg class="w-4 h-4 text-huginn-muted/60 flex-shrink-0" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
                <circle cx="11" cy="11" r="8"/><path d="m21 21-4.35-4.35"/>
              </svg>
              <input
                ref="globalSearchInputEl"
                v-model="globalSearchQuery"
                placeholder="Search all messages…"
                class="flex-1 bg-transparent text-sm text-huginn-text placeholder-huginn-muted/40 outline-none"
                data-testid="global-search-input"
              />
              <kbd class="text-[10px] px-1.5 py-0.5 rounded border border-huginn-border text-huginn-muted/50 font-mono">Esc</kbd>
            </div>

            <!-- Results -->
            <div class="max-h-80 overflow-y-auto" data-testid="global-search-results">
              <div v-if="!globalSearchQuery.trim()" class="px-4 py-8 text-center text-huginn-muted/50 text-sm">
                Type to search across all sessions
              </div>
              <div v-else-if="globalSearchResults.length === 0" class="px-4 py-8 text-center text-huginn-muted/50 text-sm">
                No messages match "{{ globalSearchQuery }}"
              </div>
              <button
                v-for="result in globalSearchResults"
                :key="`${result.sessionId}-${result.msgId}`"
                @click="navigateToSearchResult(result)"
                class="w-full flex flex-col gap-0.5 px-4 py-3 text-left transition-colors hover:bg-huginn-surface border-b border-huginn-border/40 last:border-0"
                data-testid="global-search-result"
              >
                <div class="flex items-center gap-2 mb-0.5">
                  <span class="text-[10px] font-semibold text-huginn-blue truncate">{{ result.sessionLabel }}</span>
                  <span v-if="result.agent" class="text-[10px] text-huginn-muted/60">· {{ result.agent }}</span>
                </div>
                <p class="text-xs text-huginn-text leading-relaxed line-clamp-2" v-html="result.snippet" />
              </button>
            </div>

            <!-- Footer hint -->
            <div class="flex items-center justify-between px-4 py-2 border-t border-huginn-border">
              <span class="text-[10px] text-huginn-muted/40">{{ globalSearchResults.length }} result{{ globalSearchResults.length !== 1 ? 's' : '' }}</span>
              <span class="text-[10px] text-huginn-muted/40">↵ to open · Esc to close</span>
            </div>
          </div>
        </div>
      </Transition>
    </Teleport>

    <!-- ── Keyboard Shortcuts Modal (?) ─────────────────────────────── -->
    <Teleport to="body">
      <Transition
        enter-active-class="transition-all duration-150 ease-out"
        enter-from-class="opacity-0"
        enter-to-class="opacity-100"
        leave-active-class="transition-all duration-100 ease-in"
        leave-from-class="opacity-100"
        leave-to-class="opacity-0"
      >
        <div v-if="shortcutsOpen"
          class="fixed inset-0 z-50 flex items-start justify-center pt-24"
          style="background:rgba(0,0,0,0.6);backdrop-filter:blur(2px)"
          @click.self="shortcutsOpen = false"
        >
          <div class="w-full max-w-md mx-4 rounded-2xl overflow-hidden shadow-2xl border border-huginn-border"
            style="background:rgba(22,27,34,0.97)">
            <!-- Header -->
            <div class="flex items-center justify-between px-5 py-4 border-b border-huginn-border">
              <h2 class="text-sm font-semibold text-huginn-text">Keyboard Shortcuts</h2>
              <button @click="shortcutsOpen = false" class="text-huginn-muted hover:text-huginn-text transition-colors">
                <svg class="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                  <line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/>
                </svg>
              </button>
            </div>
            <!-- Groups -->
            <div class="px-5 py-4 space-y-5">
              <div v-for="group in shortcutGroups" :key="group.label">
                <p class="text-[10px] font-semibold text-huginn-muted/50 uppercase tracking-wider mb-2">{{ group.label }}</p>
                <div v-for="s in group.shortcuts" :key="s.key"
                  class="flex items-center justify-between py-1.5 border-b border-huginn-border/30 last:border-0">
                  <span class="text-xs text-huginn-muted">{{ s.description }}</span>
                  <kbd class="text-[10px] px-2 py-0.5 rounded border border-huginn-border text-huginn-muted/70 font-mono bg-huginn-surface whitespace-nowrap">{{ s.key }}</kbd>
                </div>
              </div>
            </div>
            <!-- Footer -->
            <div class="px-5 py-2.5 border-t border-huginn-border">
              <p class="text-[10px] text-huginn-muted/30">Press <kbd class="font-mono text-huginn-muted/50">Esc</kbd> or <kbd class="font-mono text-huginn-muted/50">?</kbd> to close</p>
            </div>
          </div>
        </div>
      </Transition>
    </Teleport>

    <!-- ── Column 3: Main content ───────────────────────────────────── -->
    <main class="flex-1 overflow-hidden">
      <!-- App loading -->
      <div v-if="appLoading" class="flex flex-col items-center justify-center h-full gap-4">
        <div class="w-8 h-8 border-2 border-huginn-border border-t-huginn-blue rounded-full animate-spin" />
        <p class="text-huginn-muted text-sm">Starting huginn...</p>
      </div>

      <!-- App error -->
      <div v-else-if="appError" class="flex flex-col items-center justify-center h-full gap-3">
        <p class="text-huginn-red text-sm">{{ appError }}</p>
        <button @click="initApp" class="text-huginn-blue text-xs hover:underline">Retry connection</button>
      </div>

      <RouterView v-else />
    </main>
  </div>
</template>

<script setup lang="ts">
import { ref, shallowRef, provide, computed, nextTick, onMounted, onUnmounted, watch } from 'vue'
import { RouterView, useRoute, useRouter } from 'vue-router'
import { useHuginnWS, type HuginnWS } from './composables/useHuginnWS'
import { setToken, fetchToken, api } from './composables/useApi'
import { useSessions } from './composables/useSessions'
import { useNotifications } from './composables/useNotifications'
import { useWorkflows } from './composables/useWorkflows'
import { wireThreadDetailWS } from './composables/useThreadDetail'
import { useCloud } from './composables/useCloud'
import { useSpaces, wireSpaceWS } from './composables/useSpaces'
import { wireSpaceTimelineWS, getSpaceLastMessage } from './composables/useSpaceTimeline'
import { wireSwarmWS } from './composables/useSwarmStatus'
import SpaceCreateModal from './components/SpaceCreateModal.vue'
import { useAgents } from './composables/useAgents'
import { useThreads } from './composables/useThreads'

const route = useRoute()
const router = useRouter()
const { sessions, fetchSessions, createSession, formatSessionLabel, getMessages } = useSessions()
const { notifications, pendingCount, fetchSummary, fetchNotifications, wireWS } = useNotifications()
const { isAgentActive } = useThreads()

// ── Nav structure ────────────────────────────────────────────────────
const navItems = [
  { section: 'inbox',      label: 'Inbox',      path: '/inbox',      icon: 'inbox'      },
  { section: 'chat',       label: 'Chat',       path: '/chat',       icon: 'chat'       },
  { section: 'agents',     label: 'Agents',     path: '/agents',     icon: 'agents'     },
  { section: 'models',     label: 'Models',     path: '/models',     icon: 'models'     },
  { section: 'automation', label: 'Automation', path: '/workflows',  icon: 'automation' },
  { section: 'connections',label: 'Connections',path: '/connections', icon: 'connections'},
  { section: 'skills',     label: 'Skills',     path: '/skills',     icon: 'skills'     },
  { section: 'stats',      label: 'Stats',      path: '/stats',      icon: 'stats'      },
  { section: 'settings',   label: 'Settings',   path: '/settings',   icon: 'settings'   },
  { section: 'logs',       label: 'Logs',       path: '/logs',       icon: 'logs'       },
]

const activeSection    = computed(() => {
  const seg = route.path.split('/')[1] || 'chat'
  if (seg === 'routines' || seg === 'workflows') return 'automation'
  if (seg === 'space') return 'chat'  // space view lives within the chat section
  return seg
})
const activeSessionId  = computed(() => route.params.sessionId as string || '')
const showPanel        = computed(() => ['chat', 'agents', 'automation', 'skills'].includes(activeSection.value))
const panelTitle       = computed(() => {
  if (activeSection.value === 'chat') return 'Sessions'
  if (activeSection.value === 'automation') return 'Automation'
  if (activeSection.value === 'skills') return 'Skills'
  return 'Agents'
})

// ── Skills navigation items ─────────────────────────────────────────
const skillsNavItems = [
  { tab: 'installed', label: 'Installed' },
  { tab: 'browse',    label: 'Browse' },
  { tab: 'create',    label: 'Create' },
]

// ── Agents list (for sidebar panel) ─────────────────────────────────
const { agents, loading: agentsLoading, fetchAgents } = useAgents()

async function loadAgents() {
  await fetchAgents()
}

// ── Chat unseen tracking ─────────────────────────────────────────────
// Persisted to localStorage so badge state survives page refresh.
// UI-layer state belongs here, not in Pebble (which is for business data).
const UNSEEN_KEY = 'huginn:unseen_sessions'

function loadUnseenFromStorage(): string[] {
  try { return JSON.parse(localStorage.getItem(UNSEEN_KEY) ?? '[]') } catch { return [] }
}
function saveUnseenToStorage(ids: string[]) {
  try { localStorage.setItem(UNSEEN_KEY, JSON.stringify(ids)) } catch { /* quota exceeded */ }
}

const unseenSessionIds = ref<string[]>(loadUnseenFromStorage())
const chatDoneCount = computed(() => unseenSessionIds.value.length)

function markUnseen(id: string) {
  if (!unseenSessionIds.value.includes(id)) {
    unseenSessionIds.value.push(id)
    saveUnseenToStorage(unseenSessionIds.value)
  }
}
function clearUnseen(id: string) {
  unseenSessionIds.value = unseenSessionIds.value.filter(x => x !== id)
  saveUnseenToStorage(unseenSessionIds.value)
}

// Clear a session's unseen state when the user navigates into it
watch(activeSessionId, (id) => { if (id) clearUnseen(id) })

// Sync activeSpaceId from route so sidebar highlights the correct space
// when navigating via router.push('/space/:id').
watch(() => route.path, (path) => {
  if (path.startsWith('/space/')) {
    const spaceId = path.split('/')[2]
    if (spaceId && spaceId !== activeSpaceId.value) setActiveSpace(spaceId)
  } else if (activeSpaceId.value && !path.startsWith('/space/')) {
    // Navigating away from a space — clear the active space highlight.
    setActiveSpace(null)
  }
})


watch(activeSection, (s, prev) => {
  if (s === 'agents') loadAgents()
  if (s === 'automation') loadAutomation()
  if (s === 'chat') fetchSpaces()
  if (prev === 'agents') agentSearch.value = ''
})

// ── Automation lists (workflows) ─────────────────────────────────────
const { workflows, fetchWorkflows } = useWorkflows()
const automationLoading = ref(false)

async function loadAutomation() {
  automationLoading.value = true
  try {
    await Promise.all([fetchNotifications(), fetchWorkflows()])
  } catch { /* ignore */ }
  finally { automationLoading.value = false }
}

// ── Panel actions ────────────────────────────────────────────────────
function goToSection(path: string) { router.push(path) }

async function handleNewItem() {
  if (activeSection.value === 'chat') {
    const session = await createSession()
    router.push(`/chat/${session.id}`)
  } else if (activeSection.value === 'agents') {
    router.push('/agents/new')
  }
}


// ── Space management ──────────────────────────────────────────────────
const spaceMenuId      = ref<string | null>(null)
const renamingSpaceId  = ref('')
const spaceRenameValue = ref('')

function startSpaceRename(sp: { id: string; name: string }) {
  spaceMenuId.value     = null
  renamingSpaceId.value = sp.id
  spaceRenameValue.value = sp.name
}

async function commitSpaceRename(id: string) {
  const name = spaceRenameValue.value.trim()
  renamingSpaceId.value = ''
  if (name) await updateSpace(id, { name })
}

async function doDeleteSpace(id: string) {
  spaceMenuId.value = null
  if (!window.confirm('Delete this space and all its sessions? This cannot be undone.')) return
  if (activeSpaceId.value === id) router.push('/chat')
  await deleteSpace(id)
}

// ── Keyboard shortcuts modal ──────────────────────────────────────────
const shortcutsOpen = ref(false)
const shortcutGroups = [
  {
    label: 'Navigation',
    shortcuts: [
      { key: 'Cmd+K', description: 'Global message search' },
      { key: '?', description: 'Show keyboard shortcuts' },
    ],
  },
  {
    label: 'Chat',
    shortcuts: [
      { key: 'Ctrl+F', description: 'Search current chat' },
      { key: 'Enter', description: 'Send message' },
      { key: 'Shift+Enter', description: 'New line in message' },
      { key: '/', description: 'Open slash commands' },
    ],
  },
  {
    label: 'General',
    shortcuts: [
      { key: 'Esc', description: 'Close modal / cancel' },
      { key: 'Double-click', description: 'Rename session (in sidebar)' },
    ],
  },
]

// ── WS + App init ────────────────────────────────────────────────────
const appLoading = ref(true)
const appError   = ref('')
const wsRef       = shallowRef<HuginnWS | null>(null)
provide('ws', wsRef)

const wsConnected = computed(() => wsRef.value?.connected.value ?? false)
const wsConnectionState = computed(() => wsRef.value?.connectionState.value ?? 'connecting')

// Show a banner after 4 s of non-connected state to avoid flicker on brief blips.
const showDegradedBanner = ref(false)
let degradedTimer: ReturnType<typeof setTimeout> | null = null

watch(wsConnectionState, (state) => {
  if (state === 'connected') {
    if (degradedTimer) { clearTimeout(degradedTimer); degradedTimer = null }
    showDegradedBanner.value = false
  } else if (!degradedTimer) {
    degradedTimer = setTimeout(() => {
      showDegradedBanner.value = true
      degradedTimer = null
    }, 4000)
  }
})

onUnmounted(() => {
  if (degradedTimer) { clearTimeout(degradedTimer); degradedTimer = null }
})

async function initApp() {
  appLoading.value = true
  appError.value = ''
  try {
    const tok = await fetchToken()
    setToken(tok)
    if (wsRef.value) wsRef.value.destroy()
    wsRef.value = useHuginnWS(tok)
    const ws = wsRef.value!
    appLoading.value = false
    await Promise.all([fetchSessions(), fetchSummary()])
    fetchSpaces()
    fetchAgents().catch(() => {})
    fetchCloudStatus().catch(() => {})
    wireWS(ws)
    wireThreadDetailWS(ws)
    wireSpaceWS(ws)
    wireSpaceTimelineWS(ws)
    wireSwarmWS(ws, () => activeSessionId.value)

    // Update session state live from WS events
    ws.on('token', (msg) => {
      if (msg.session_id) {
        const sess = sessions.value.find(s => s.id === msg.session_id)
        if (sess) sess.state = 'running'
      }
    })
    ws.on('done', (msg) => {
      if (msg.session_id) {
        const sess = sessions.value.find(s => s.id === msg.session_id)
        if (sess) sess.state = 'idle'
        // Mark unseen if user isn't currently viewing this session
        if (msg.session_id !== activeSessionId.value) markUnseen(msg.session_id)
      }
    })
  } catch (e: unknown) {
    appError.value = e instanceof Error ? e.message : 'Failed to initialize'
    appLoading.value = false
  }
}

// ── Profile popover ──────────────────────────────────────────────────
const profilePopoverOpen = ref(false)
const profileButtonRef   = ref<HTMLElement | null>(null)

const {
  status: cloudStatus,
  connecting: cloudConnecting,
  disconnecting: cloudDisconnecting,
  fetchStatus: fetchCloudStatus,
  connect: connectCloud,
  disconnect: disconnectCloud,
} = useCloud()

const cloudConnected = computed(() => cloudStatus.value.connected)

// ── Spaces ───────────────────────────────────────────────────────────
const {
  channels, dms, activeSpaceId, loading: spacesLoading, error: spacesError,
  fetchSpaces, setActiveSpace, markRead,
  updateSpace, deleteSpace,
} = useSpaces()

const showCreateChannelModal = ref(false)
const channelSectionOpen = ref(true)
const dmSectionOpen = ref(true)
const sidebarSearch = ref('')
const agentSearch = ref('')

// ── Sidebar FTS session search (debounced, AbortController-guarded) ───
// When sidebarSearch is non-empty we hit the server FTS endpoint instead of
// the client-side filter so that message content is also searchable.
const sessionSearchResults = ref<Array<Record<string, unknown>>>([])
let _searchDebounce: ReturnType<typeof setTimeout> | null = null
let _searchController: AbortController | null = null

watch(sidebarSearch, (q) => {
  if (_searchDebounce !== null) clearTimeout(_searchDebounce)
  _searchController?.abort()
  if (!q.trim()) {
    sessionSearchResults.value = []
    return
  }
  _searchDebounce = setTimeout(async () => {
    _searchController = new AbortController()
    try {
      const results = await api.sessions.search(q.trim(), _searchController.signal)
      sessionSearchResults.value = results
    } catch (e: unknown) {
      // Ignore AbortError (superseded by a newer search)
      if (e instanceof Error && e.name !== 'AbortError') {
        sessionSearchResults.value = []
      }
    }
  }, 300)
})

// ── Sidebar last-message preview ──────────────────────────────────────
// Returns a { text, relTime } snippet for the most recent message in a space.
// Reads from the space timeline cache (populated when user visits the space).
// Returns null if the space hasn't been opened yet or has no messages.
function spaceLastMessage(spaceId: string): { text: string; relTime: string } | null {
  return getSpaceLastMessage(spaceId)
}

const filteredChannels = computed(() => {
  const q = sidebarSearch.value.trim().toLowerCase()
  if (!q) return channels.value
  return channels.value.filter(s =>
    s.name.toLowerCase().includes(q) ||
    s.leadAgent.toLowerCase().includes(q) ||
    s.memberAgents.some(m => m.toLowerCase().includes(q))
  )
})

const filteredDMs = computed(() => {
  const q = sidebarSearch.value.trim().toLowerCase()
  // Exclude DMs whose lead agent has no model configured
  const agentsWithModel = new Set(agents.value.filter(a => !!a.model).map(a => a.name))
  const capable = dms.value.filter(s => agentsWithModel.size === 0 || agentsWithModel.has(s.leadAgent))
  if (!q) return capable
  return capable.filter(s => s.leadAgent.toLowerCase().includes(q))
})

const agentColorMap = computed(() => {
  const m: Record<string, string> = {}
  for (const a of agents.value) if (a.color) m[a.name] = a.color as string
  return m
})

function selectSpace(id: string) {
  setActiveSpace(id)
  markRead(id)
  router.push(`/space/${id}`)
}


function toggleProfilePopover() {
  profilePopoverOpen.value = !profilePopoverOpen.value
  if (profilePopoverOpen.value) fetchCloudStatus()
}

function onDocClick(e: MouseEvent) {
  if (profileButtonRef.value && !profileButtonRef.value.contains(e.target as Node)) {
    profilePopoverOpen.value = false
  }
  if (spaceMenuId.value && !(e.target as HTMLElement).closest('.space-menu')) {
    spaceMenuId.value = null
  }
}

// ── Global search (Cmd+K) ────────────────────────────────────────────
const globalSearchOpen = ref(false)
const globalSearchQuery = ref('')
const globalSearchInputEl = ref<HTMLInputElement | null>(null)

interface SearchResult {
  sessionId: string
  sessionLabel: string
  msgId: string
  agent: string
  snippet: string
}

const globalSearchResults = computed((): SearchResult[] => {
  const q = globalSearchQuery.value.trim().toLowerCase()
  if (!q || q.length < 2) return []
  const results: SearchResult[] = []
  for (const session of sessions.value) {
    const msgs = getMessages(session.id)
    for (const msg of msgs) {
      if (!msg.content || (msg.role !== 'user' && msg.role !== 'assistant')) continue
      const lower = msg.content.toLowerCase()
      const idx = lower.indexOf(q)
      if (idx === -1) continue
      const start = Math.max(0, idx - 40)
      const end = Math.min(msg.content.length, idx + q.length + 80)
      const raw = (start > 0 ? '…' : '') + msg.content.slice(start, end) + (end < msg.content.length ? '…' : '')
      // Bold the match
      const escaped = q.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
      const snippet = raw.replace(new RegExp(`(${escaped})`, 'gi'), '<strong class="text-huginn-blue">$1</strong>')
      results.push({
        sessionId: session.id,
        sessionLabel: formatSessionLabel(session),
        msgId: msg.id,
        agent: msg.agent ?? '',
        snippet,
      })
      if (results.length >= 30) break
    }
    if (results.length >= 30) break
  }
  return results
})

function openGlobalSearch() {
  globalSearchOpen.value = true
  globalSearchQuery.value = ''
  nextTick(() => globalSearchInputEl.value?.focus())
}

function navigateToSearchResult(result: SearchResult) {
  globalSearchOpen.value = false
  router.push(`/chat/${result.sessionId}`)
}

function handleGlobalAppKeydown(e: KeyboardEvent) {
  if ((e.metaKey || e.ctrlKey) && e.key === 'k') {
    e.preventDefault()
    if (globalSearchOpen.value) {
      globalSearchOpen.value = false
    } else {
      openGlobalSearch()
    }
  }
  // ? opens keyboard shortcuts — skip when focus is inside an editable element
  if (e.key === '?' && !e.ctrlKey && !e.metaKey && !e.altKey) {
    const tag = (e.target as HTMLElement).tagName
    if (tag !== 'INPUT' && tag !== 'TEXTAREA' && !(e.target as HTMLElement).isContentEditable) {
      e.preventDefault()
      shortcutsOpen.value = !shortcutsOpen.value
    }
  }
  if (e.key === 'Escape') {
    if (shortcutsOpen.value) shortcutsOpen.value = false
  }
}

onMounted(() => {
  initApp()
  document.addEventListener('click', onDocClick, true)
  document.addEventListener('keydown', handleGlobalAppKeydown)
})
onUnmounted(() => {
  wsRef.value?.destroy()
  document.removeEventListener('click', onDocClick, true)
  document.removeEventListener('keydown', handleGlobalAppKeydown)
})
</script>
