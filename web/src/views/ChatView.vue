<template>
  <div class="flex flex-col h-full bg-huginn-bg">

    <!-- ── Hydration overflow toast ────────────────────────────────── -->
    <Transition name="ws-banner">
      <div v-if="hydrationOverflowToastVisible"
        class="flex-shrink-0 flex items-center justify-between gap-3 px-4 py-2 text-xs font-medium"
        style="background:rgba(227,179,65,0.12);border-bottom:1px solid rgba(227,179,65,0.25);color:rgba(227,179,65,0.92)">
        <div class="flex items-center gap-2">
          <svg class="w-3.5 h-3.5 flex-shrink-0" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><path d="M10.29 3.86L1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0z"/><line x1="12" y1="9" x2="12" y2="13"/><line x1="12" y1="17" x2="12.01" y2="17"/></svg>
          <span>Some real-time updates were dropped while loading. Please refresh if data looks stale.</span>
        </div>
        <button @click="hydrationOverflowToastVisible = false"
          class="flex-shrink-0 px-2 py-0.5 rounded border border-huginn-amber/40 hover:bg-huginn-amber/10 transition-colors text-[11px]">
          Dismiss
        </button>
      </div>
    </Transition>

    <TransitionGroup name="ws-banner" tag="div" class="flex-shrink-0">
      <div v-for="toast in blockedThreadToasts" :key="toast.threadId"
        class="flex items-center justify-between gap-3 px-4 py-2 text-xs font-medium cursor-pointer"
        style="background:rgba(210,153,34,0.12);border-bottom:1px solid rgba(210,153,34,0.28);color:rgba(227,179,65,0.96)"
        @click="openThreadDetailById(toast.threadId)">
        <div class="flex items-center gap-2">
          <svg class="w-3.5 h-3.5 flex-shrink-0" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><path d="M10.29 3.86L1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0z"/><line x1="12" y1="9" x2="12" y2="13"/><line x1="12" y1="17" x2="12.01" y2="17"/></svg>
          <span>
            <strong>{{ toast.agent || 'Delegate' }}</strong> needs input
            <template v-if="toast.message"> — {{ toast.message }}</template>
          </span>
        </div>
        <button @click.stop="dismissBlockedThreadToast(toast.threadId)"
          class="flex-shrink-0 px-2 py-0.5 rounded border border-huginn-amber/40 hover:bg-huginn-amber/10 transition-colors text-[11px]">
          Dismiss
        </button>
      </div>
    </TransitionGroup>

    <Transition name="ws-banner">
      <div v-if="spaceMentionToast"
        data-testid="space-reply-mention-toast"
        class="flex-shrink-0 flex items-center justify-between gap-3 px-4 py-2 text-xs font-medium"
        style="background:rgba(88,166,255,0.12);border-bottom:1px solid rgba(88,166,255,0.25);color:rgba(88,166,255,0.96)">
        <span>You were mentioned in a thread — {{ spaceMentionToast }}</span>
        <button type="button" class="px-2 py-0.5 rounded border border-huginn-blue/40 text-[11px]" @click="spaceMentionToast = null">Dismiss</button>
      </div>
    </Transition>

    <!-- ── No session/space selected ──────────────────────────────── -->
    <div v-if="!sessionId && !spaceId" class="flex flex-col items-center justify-center h-full gap-6 pb-16">
      <div class="w-20 h-20 rounded-3xl flex items-center justify-center select-none"
        style="background:linear-gradient(135deg,rgba(88,166,255,0.15),rgba(88,166,255,0.04));border:1px solid rgba(88,166,255,0.25)">
        <span class="text-huginn-blue font-bold text-4xl leading-none">H</span>
      </div>

      <!-- No agents configured yet -->
      <template v-if="agentsList.length === 0">
        <div class="text-center space-y-1.5">
          <h1 class="text-huginn-text font-semibold text-lg tracking-tight">no agents yet</h1>
          <p class="text-huginn-muted text-sm">Create your first agent to get started.</p>
        </div>
        <a href="#/agents"
          class="flex items-center gap-2 px-5 py-2.5 rounded-xl text-sm font-medium text-huginn-blue transition-all duration-150
                 border border-huginn-blue/30 hover:bg-huginn-blue/10 hover:border-huginn-blue/50 active:scale-95">
          <svg class="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round">
            <line x1="12" y1="5" x2="12" y2="19" /><line x1="5" y1="12" x2="19" y2="12" />
          </svg>
          Create an agent
        </a>
      </template>

      <!-- Agents exist — prompt to pick a DM or channel -->
      <template v-else>
        <div class="text-center space-y-1.5">
          <h1 class="text-huginn-text font-semibold text-lg tracking-tight">huginn is ready</h1>
          <p class="text-huginn-muted text-sm">Pick a channel or DM from the sidebar to begin.</p>
        </div>
      </template>
    </div>

    <!-- ── Active session or space ───────────────────────────────── -->
    <template v-else>
      <div class="flex flex-1 min-h-0 overflow-hidden">
      <!-- Main chat column --><div class="flex flex-col flex-1 min-w-0">

      <!-- Header bar -->
      <div class="flex items-center gap-3 px-5 h-11 border-b border-huginn-border flex-shrink-0"
        style="background:rgba(22,27,34,0.6);backdrop-filter:blur(8px)">
        <div class="w-1.5 h-1.5 rounded-full flex-shrink-0 transition-all duration-300"
          :class="{
            'bg-huginn-green': runtimeState === 'running',
            'bg-huginn-blue': ['planning','coding'].includes(runtimeState),
            'bg-huginn-yellow': runtimeState === 'approval',
            'bg-huginn-muted/50': !runtimeState || runtimeState === 'idle',
          }"
          :style="runtimeState === 'running' ? 'box-shadow:0 0 6px rgba(63,185,80,0.5)' : ''"
        />
        <!-- Header title: space name when in a space, else inline-editable session label -->
        <template v-if="activeSpace">
          <span class="text-sm font-semibold text-huginn-text truncate select-none flex items-center gap-1">
            <span v-if="activeSpace.kind === 'channel'" class="text-huginn-muted/50 font-normal">#</span>
            {{ activeSpace.name }}
          </span>
          <span v-if="runtimeState && runtimeState !== 'idle'" class="text-xs text-huginn-muted">{{ runtimeState }}</span>
        </template>
        <template v-else-if="headerEditing">
          <input
            ref="headerInputEl"
            v-model="headerEditValue"
            :placeholder="sessionLabel"
            class="text-sm font-medium flex-1 min-w-0 bg-transparent border-b border-huginn-blue/60 outline-none text-huginn-text placeholder-huginn-muted/50"
            @keydown.enter="commitHeaderEdit"
            @keydown.esc="cancelHeaderEdit"
            @blur="commitHeaderEdit"
            @click.stop
          />
          <!-- Cancel ✕ -->
          <button
            @mousedown.prevent
            @click.stop="cancelHeaderEdit"
            class="w-5 h-5 rounded flex items-center justify-center text-huginn-muted hover:text-huginn-red flex-shrink-0 transition-colors"
            title="Cancel rename"
          >
            <svg class="w-3 h-3" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round">
              <line x1="18" y1="6" x2="6" y2="18" />
              <line x1="6" y1="6" x2="18" y2="18" />
            </svg>
          </button>
        </template>
        <template v-else>
          <span
            class="text-sm font-medium text-huginn-text truncate select-none cursor-text"
            @dblclick="startHeaderEdit"
            :title="'Double-click to rename'"
          >{{ sessionLabel }}</span>
          <span v-if="runtimeState && runtimeState !== 'idle'" class="text-xs text-huginn-muted">{{ runtimeState }}</span>
        </template>

        <!-- Thread activity badge (show when threads running) -->
        <button v-if="activeThreadCount > 0 || threadsError"
          @click="threadPanelOpen = !threadPanelOpen"
          class="relative flex items-center gap-1.5 px-2 py-1 rounded-lg text-xs transition-all duration-200 hover:bg-huginn-surface"
          style="color:rgba(88,166,255,0.9);border:1px solid rgba(88,166,255,0.2);background:rgba(88,166,255,0.06)"
          :title="threadsError ? 'Thread manager unavailable' : 'Toggle thread panel'"
        >
          <svg class="w-3 h-3" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
            <path d="M17 21v-2a4 4 0 00-4-4H5a4 4 0 00-4 4v2"/><circle cx="9" cy="7" r="4"/>
            <path d="M23 21v-2a4 4 0 00-3-3.87"/><path d="M16 3.13a4 4 0 010 7.75"/>
          </svg>
          <span class="font-bold tabular-nums">{{ activeThreadCount }}</span>
          <!-- Amber dot when thread manager is unavailable -->
          <span v-if="threadsError"
            class="absolute -top-0.5 -right-0.5 w-2 h-2 rounded-full bg-huginn-amber" />
        </button>
        <button v-if="blockedThreadCount > 0"
          @click="openBlockedThreadFocus()"
          class="flex items-center gap-1.5 px-2 py-1 rounded-lg text-xs transition-all duration-200 hover:bg-huginn-surface"
          style="color:rgba(227,179,65,0.96);border:1px solid rgba(210,153,34,0.28);background:rgba(210,153,34,0.10)"
          title="Blocked delegated threads need attention"
        >
          <svg class="w-3 h-3" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round">
            <path d="M10.29 3.86L1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0z"/><line x1="12" y1="9" x2="12" y2="13"/><line x1="12" y1="17" x2="12.01" y2="17"/>
          </svg>
          <span class="font-bold tabular-nums">{{ blockedThreadCount }}</span>
          <span>blocked</span>
        </button>

        <!-- Right side of header -->
        <div class="ml-auto flex items-center gap-2 flex-shrink-0">

          <!-- Agents chip (space context) -->
          <button v-if="activeSpace"
            @click="activeSpace.kind === 'channel' ? (rosterOpen = true) : toggleMemberPanel()"
            class="flex items-center gap-2 px-2.5 py-1 rounded-lg text-xs transition-all duration-150 hover:bg-huginn-surface active:scale-95"
            style="border:1px solid rgba(255,255,255,0.08)"
            title="Manage agents"
          >
            <!-- Stacked avatars -->
            <div class="flex -space-x-1.5">
              <div
                v-for="(ag, i) in spaceAgentPreviews"
                :key="ag.name"
                class="w-5 h-5 rounded-full flex items-center justify-center text-[9px] font-bold ring-1 ring-huginn-bg"
                :style="`background:${ag.color}22;color:${ag.color};z-index:${spaceAgentPreviews.length - i}`"
              >{{ ag.icon }}</div>
              <div v-if="spaceAgents.length > 3"
                class="w-5 h-5 rounded-full flex items-center justify-center text-[8px] font-bold ring-1 ring-huginn-bg text-huginn-muted"
                style="background:rgba(255,255,255,0.06);z-index:0"
              >+{{ spaceAgents.length - 3 }}</div>
            </div>
            <span class="text-huginn-text font-medium">{{ spaceAgents.length }} {{ spaceAgents.length === 1 ? 'agent' : 'agents' }}</span>
            <svg class="w-3 h-3 text-huginn-muted/50" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
              <polyline points="6 9 12 15 18 9" />
            </svg>
          </button>

          <!-- Memory replication chip (channel/space context only) -->
          <!-- Uses v-if with explicit activeSpace guard; agent picker below uses !activeSpace — mutually exclusive by domain -->
          <span v-if="replChipText && activeSpace" :class="['text-[10px] px-2 py-0.5 rounded-full', replChipClass]">
            {{ replChipText }}
          </span>

          <!-- Agent picker dropdown (standalone session context — no activeSpace) -->
          <div v-if="!activeSpace && agentsList.length" class="relative flex-shrink-0">
            <button
              @click="agentDropdownOpen = !agentDropdownOpen"
              class="flex items-center gap-1.5 px-2 py-1 rounded-lg text-xs transition-all duration-150 hover:bg-huginn-surface"
              :class="selectedAgent ? 'text-huginn-text' : 'text-huginn-muted border border-dashed border-huginn-border'"
              title="Switch agent"
            >
              <span v-if="selectedAgent"
                class="w-4 h-4 rounded flex items-center justify-center text-[10px] font-bold flex-shrink-0"
                :style="`background:${selectedAgent.color}22;color:${selectedAgent.color}`"
              >{{ selectedAgent.icon }}</span>
              <span>{{ selectedAgent?.name ?? 'No agent' }}</span>
              <svg class="w-3 h-3 opacity-50 transition-transform duration-150" :class="agentDropdownOpen ? 'rotate-180' : ''"
                viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
                <polyline points="6 9 12 15 18 9" />
              </svg>
            </button>

            <!-- Agent dropdown -->
            <div v-if="agentDropdownOpen"
              class="absolute right-0 top-full mt-1 w-48 rounded-xl border border-huginn-border shadow-xl overflow-hidden z-50"
              style="background:rgba(22,27,34,0.97);backdrop-filter:blur(8px)"
            >
              <button
                v-for="ag in agentsList"
                :key="ag.name"
                @click="selectAgent(ag.name)"
                class="w-full flex items-center gap-2.5 px-3 py-2.5 text-left text-sm transition-colors duration-100 hover:bg-huginn-surface"
                :class="ag.name === selectedAgentName ? 'text-huginn-text' : 'text-huginn-muted'"
              >
                <span class="w-5 h-5 rounded-md flex items-center justify-center text-[11px] font-bold flex-shrink-0"
                  :style="`background:${ag.color}22;color:${ag.color}`">{{ ag.icon }}</span>
                <div class="flex-1 min-w-0">
                  <div class="font-medium truncate">{{ ag.name }}</div>
                  <div class="text-[11px] text-huginn-muted truncate">{{ ag.model }}</div>
                </div>
                <svg v-if="ag.name === selectedAgentName" class="w-3.5 h-3.5 flex-shrink-0"
                  :style="`color:${ag.color}`"
                  viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round">
                  <polyline points="20 6 9 17 4 12" />
                </svg>
              </button>
            </div>
          </div>

          <!-- Export button -->
          <button
            v-if="messages.length > 0"
            @click="exportSession"
            class="w-7 h-7 rounded-lg flex items-center justify-center text-huginn-muted/50 hover:text-huginn-muted hover:bg-huginn-surface transition-all duration-150"
            title="Export chat as markdown"
          >
            <svg class="w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
              <path d="M21 15v4a2 2 0 01-2 2H5a2 2 0 01-2-2v-4"/>
              <polyline points="7 10 12 15 17 10"/>
              <line x1="12" y1="15" x2="12" y2="3"/>
            </svg>
          </button>

        </div>
      </div>

      <div v-if="displayAgentUnreliableTools"
        data-testid="chat-model-tools-warning"
        class="flex-shrink-0 px-5 py-1.5 border-b border-huginn-yellow/25 bg-huginn-yellow/8">
        <p class="text-[11px] text-huginn-yellow leading-snug">{{ MODEL_TOOL_WARNING }}</p>
      </div>

      <MemoryVaultChip
        v-if="memoryChip"
        :chip="memoryChip"
        :agent-name="memoryChip.agentName"
        :agent-vault-name="(memoryChipAgent as { vault_name?: string } | null)?.vault_name || ''"
        :agent-memory-mode="(memoryChipAgent as { memory_mode?: string } | null)?.memory_mode || ''"
        :known-agents="agentsList"
        @dismiss="dismissMemoryChip"
        @connected="onMemoryVaultConnected"
        @status="onMuninnChipStatus"
      />

      <!-- ── In-chat search bar (Ctrl+F) ────────────────────────── -->
      <Transition
        enter-active-class="transition-all duration-150 ease-out"
        enter-from-class="opacity-0 -translate-y-1"
        enter-to-class="opacity-100 translate-y-0"
        leave-active-class="transition-all duration-100 ease-in"
        leave-from-class="opacity-100 translate-y-0"
        leave-to-class="opacity-0 -translate-y-1"
      >
        <div v-if="chatSearchOpen"
          class="flex items-center gap-2 px-4 py-2 border-b border-huginn-border flex-shrink-0"
          style="background:rgba(22,27,34,0.8)"
        >
          <svg class="w-3.5 h-3.5 text-huginn-muted/50 flex-shrink-0" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
            <circle cx="11" cy="11" r="8"/><path d="m21 21-4.35-4.35"/>
          </svg>
          <input
            ref="chatSearchInputEl"
            v-model="chatSearchQuery"
            placeholder="Search messages…"
            class="flex-1 bg-transparent text-xs text-huginn-text placeholder-huginn-muted/40 outline-none min-w-0"
            @keydown.escape="closeChatSearch"
            @keydown.enter.exact="nextChatSearchMatch"
            @keydown.enter.shift="prevChatSearchMatch"
          />
          <span v-if="chatSearchQuery" class="text-[11px] text-huginn-muted/60 flex-shrink-0 tabular-nums">
            {{ chatSearchMatches.length ? `${chatSearchIndex + 1} / ${chatSearchMatches.length}` : '0 results' }}
          </span>
          <button v-if="chatSearchMatches.length > 1" @click="prevChatSearchMatch"
            class="w-5 h-5 rounded flex items-center justify-center text-huginn-muted hover:text-huginn-text transition-colors">
            <svg class="w-3 h-3" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round"><polyline points="18 15 12 9 6 15"/></svg>
          </button>
          <button v-if="chatSearchMatches.length > 1" @click="nextChatSearchMatch"
            class="w-5 h-5 rounded flex items-center justify-center text-huginn-muted hover:text-huginn-text transition-colors">
            <svg class="w-3 h-3" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round"><polyline points="6 9 12 15 18 9"/></svg>
          </button>
          <button @click="closeChatSearch"
            class="w-5 h-5 rounded flex items-center justify-center text-huginn-muted hover:text-huginn-text transition-colors">
            <svg class="w-3 h-3" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round">
              <line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/>
            </svg>
          </button>
        </div>
      </Transition>

      <!-- Messages scroll area (position:relative for unread pill) -->
      <div ref="messagesEl" class="flex-1 overflow-y-auto relative" @click="handleMessagesClick" @scroll="onMessagesScroll"
        @touchstart.passive="onHallwayTouchStart" @touchmove.passive="onHallwayTouchMove" @touchend="onHallwayTouchEnd">

        <!-- Infinite scroll sentinel — observed by IntersectionObserver to load older space messages -->
        <div v-if="spaceId" ref="topSentinelEl" class="h-1 w-full" />

        <!-- Space timeline loading state -->
        <div v-if="spaceId && spaceLoadingInitial" class="flex items-center justify-center py-10">
          <div class="w-4 h-4 border border-huginn-border border-t-huginn-blue rounded-full animate-spin" />
        </div>

        <!-- Space timeline error state -->
        <div v-else-if="spaceId && spaceError" class="flex flex-col items-center justify-center py-10 gap-3">
          <p class="text-huginn-red text-sm">{{ spaceError }}</p>
          <button @click="currentSpaceTimeline?.retryHydrate()"
            class="text-huginn-blue text-xs hover:underline">Retry</button>
        </div>

        <!-- Unread jump pill -->
        <Transition
          enter-active-class="transition-all duration-200 ease-out"
          enter-from-class="opacity-0 translate-y-2"
          enter-to-class="opacity-100 translate-y-0"
          leave-active-class="transition-all duration-150 ease-in"
          leave-from-class="opacity-100 translate-y-0"
          leave-to-class="opacity-0 translate-y-2"
        >
          <button v-if="unreadCount > 0 && !atBottom"
            @click="jumpToUnread"
            class="absolute bottom-4 left-1/2 -translate-x-1/2 z-10 flex items-center gap-1.5 px-3 py-1.5 rounded-full text-xs font-medium text-white shadow-lg transition-all duration-150 active:scale-95"
            style="background:rgba(88,166,255,0.9);backdrop-filter:blur(6px)"
            data-testid="unread-jump-pill"
          >
            <svg class="w-3 h-3" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round">
              <polyline points="6 9 12 15 18 9"/>
            </svg>
            {{ unreadCount }} new {{ unreadCount === 1 ? 'message' : 'messages' }}
          </button>
        </Transition>

        <!-- Loading skeleton (session switch) -->
        <div v-if="sessionSwitching && messages.length === 0" class="py-5 px-5 space-y-4 animate-pulse">
          <div v-for="i in 4" :key="i" class="flex gap-3" :class="i % 3 === 0 ? 'flex-row-reverse' : ''">
            <div class="w-7 h-7 rounded-lg flex-shrink-0 bg-huginn-border/40" />
            <div class="flex flex-col gap-1.5 flex-1" :class="i % 3 === 0 ? 'items-end' : ''">
              <div class="h-2.5 rounded-full bg-huginn-border/40" :style="`width:${30 + (i * 17) % 40}%`" />
              <div class="h-2.5 rounded-full bg-huginn-border/30" :style="`width:${20 + (i * 23) % 50}%`" />
            </div>
          </div>
        </div>

        <!-- Empty chat — hide while a space timeline is still hydrating so /space/:id
             does not flash "Send your first message" over the spinner. -->
        <div v-else-if="messages.length === 0 && !streaming && !spaceLoadingInitial" class="flex flex-col items-center justify-center h-full gap-3 pb-16">
          <div class="w-12 h-12 rounded-2xl flex items-center justify-center select-none"
            :style="displayAgent
              ? `background:${displayAgent.color}18;border:1px solid ${displayAgent.color}33`
              : 'background:rgba(88,166,255,0.08);border:1px solid rgba(88,166,255,0.15)'">
            <span v-if="displayAgent" class="font-bold text-lg" :style="`color:${displayAgent.color}`">{{ displayAgent.icon }}</span>
            <span v-else class="text-huginn-blue font-bold text-lg">H</span>
          </div>
          <p class="text-huginn-muted/60 text-sm">Send your first message</p>
        </div>

        <!-- Message list -->
        <div class="py-5 px-5 w-full">
          <template v-for="msg in enrichedMessages" :key="msg.id">
            <!-- Anchor for search/unread scroll targeting -->
            <div :data-msg-id="msg.id" style="position:relative;height:0" />

            <!-- Date divider -->
            <div v-if="msg.dateLabel" class="flex items-center gap-3 my-4" data-testid="hallway-day-sep">
              <div class="flex-1 h-px bg-huginn-border/40" />
              <span class="text-[11px] text-huginn-muted/50 font-medium select-none">{{ msg.dateLabel }}</span>
              <div class="flex-1 h-px bg-huginn-border/40" />
            </div>

            <!-- Thread completion summary card — visually distinct from regular messages -->
            <div v-if="msg.threadSummary"
              class="flex items-start gap-2.5 px-3.5 py-2.5 rounded-xl mx-2 mt-4"
              style="background:rgba(46,160,67,0.07);border:1px solid rgba(46,160,67,0.22)">
              <svg class="w-3.5 h-3.5 text-huginn-green/70 flex-shrink-0 mt-0.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round">
                <path d="M22 11.08V12a10 10 0 1 1-5.93-9.14"/><polyline points="22 4 12 14.01 9 11.01"/>
              </svg>
              <div class="flex-1 min-w-0">
                <div class="md-content text-xs leading-relaxed" style="color:rgba(46,160,67,0.85)"
                  v-html="renderWithMentions(msg.content)" />
                <button v-if="msg.threadSummaryThreadId"
                  @click="openThreadDetailById(msg.threadSummaryThreadId!)"
                  class="mt-1 text-[10px] font-medium hover:underline"
                  style="color:rgba(46,160,67,0.55)">
                  View thread →
                </button>
              </div>
            </div>

            <!-- Harness announcement — system/delegation row, not teammate voice -->
            <div v-else-if="msg.systemLine"
              class="flex items-center justify-center gap-2 px-3 my-2"
              data-testid="system-line">
              <div class="flex-1 h-px bg-huginn-border/40" />
              <span class="text-[11px] text-huginn-muted/70 text-center max-w-[80%]">{{ msg.content }}</span>
              <div class="flex-1 h-px bg-huginn-border/40" />
            </div>

            <!-- User message (right-aligned bubble) -->
            <div v-else-if="msg.role === 'user'" class="group flex flex-col items-end" :class="msg.showHeader ? 'mt-4' : 'mt-1'">
              <MsgTimeReveal :created-at="msg.createdAt" :revealed="hallwayTimesRevealed" align="end">
                <div class="md-content max-w-[75%] px-4 py-3 rounded-2xl rounded-tr-sm text-sm text-huginn-text leading-relaxed break-words cursor-pointer"
                  style="background:rgba(88,166,255,0.12);border:1px solid rgba(88,166,255,0.22)"
                  data-testid="space-root-bubble"
                  @click="spaceId && openReplyThread(msg)"
                  v-html="renderWithMentions(msg.content)" />
              </MsgTimeReveal>
              <p v-if="msg.id === lastSeenMessageId"
                 class="text-[10px] text-right pr-3 -mt-1"
                 style="color:#8b949e">
                Seen
              </p>
              <MessageActions
                class="opacity-0 group-hover:opacity-100 transition-opacity"
                :msg="msg"
                :agent-vault-name="''"
                :show-reply="!!spaceId"
                :show-diagnose="!!spaceId && !!msg.delegatedThreads?.length"
                @retry="handleRetry"
                @reply="openReplyThread(msg)"
                @diagnose="diagnoseMessage(msg)"
              />
              <SpaceReplyChip
                v-if="spaceId && !msg.parent_id"
                :count="msg.spaceReplyCount ?? 0"
                :preview="msg.lastPreview"
                :typing-agent="spaceReplyTyping[msg.id]"
                :participant="!!msg.spaceReplyParticipant || (msg.role === 'user')"
                :new-since="msg.newSince ?? 0"
                @open="openReplyThread(msg)"
              />
              <!-- Delegated thread activity rows (also shown for user-parented threads) -->
              <div v-if="msg.delegatedThreads?.length" class="mt-1.5 space-y-1 w-full max-w-[75%]">
                <div v-for="d in msg.delegatedThreads" :key="d.threadId" class="space-y-1">
                  <button
                    @click="openThreadDetail(d)"
                    class="group flex items-center gap-2 py-1 px-2 -ml-1 rounded-lg transition-all duration-150 hover:bg-huginn-surface/60 overflow-hidden min-w-0 w-full"
                    data-testid="delegation-activity-row"
                  >
                    <div class="relative w-4 h-4 flex-shrink-0">
                      <div class="w-4 h-4 rounded text-[9px] font-bold flex items-center justify-center"
                        :style="`background:${agentColorMap[d.agentId] ?? 'rgba(88,166,255,0.2)'}33;color:${agentColorMap[d.agentId] ?? 'rgba(88,166,255,0.8)'}`">
                        {{ agentIconMap[d.agentId] || d.agentId?.[0]?.toUpperCase() || '?' }}
                      </div>
                      <span v-if="['running','thinking','queued'].includes(getThreadById(d.threadId)?.Status ?? '')"
                        class="absolute inset-0 rounded animate-ping opacity-50"
                        :style="`background:${agentColorMap[d.agentId] ?? 'rgba(88,166,255,0.4)'}`" />
                    </div>
                    <span class="text-xs text-huginn-text/90 truncate">
                      <template v-if="d.agentId && d.agentId === msg.agent">Handling directly</template>
                      <template v-else>Delegated to
                        <span class="font-semibold" :style="`color:${agentColorMap[d.agentId] ?? 'rgba(88,166,255,0.8)'}`">@{{ d.agentId || 'agent' }}</span>
                      </template>
                    </span>
                    <span class="text-[11px] text-huginn-muted/70">
                      · {{ delegatedThreadStatusLabel(d) }}
                    </span>
                    <span v-if="delegatedThreadProgressLabel(d)" class="text-[11px] text-huginn-muted/60">
                      · {{ delegatedThreadProgressLabel(d) }}
                    </span>
                    <span v-if="d.task" class="text-[11px] text-huginn-muted/60 truncate min-w-0 flex-1">
                      · {{ d.task }}
                    </span>
                    <span v-else class="flex-1" />
                    <svg class="w-3 h-3 text-huginn-muted/30 opacity-0 group-hover:opacity-100 transition-opacity flex-shrink-0"
                      viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
                      <polyline points="9 18 15 12 9 6" />
                    </svg>
                  </button>
                  <!-- Delegated agent thinking indicator -->
                  <div
                    v-if="isThreadThinking(d.threadId)"
                    class="flex items-center gap-2 pl-2 py-0.5"
                  >
                    <div
                      class="w-4 h-4 rounded flex-shrink-0 text-[9px] font-bold flex items-center justify-center"
                      :style="`background:${agentColorMap[d.agentId] ?? 'rgba(88,166,255,0.2)'}33;color:${agentColorMap[d.agentId] ?? 'rgba(88,166,255,0.8)'}`"
                    >
                      {{ agentIconMap[d.agentId] || d.agentId?.[0]?.toUpperCase() || '?' }}
                    </div>
                    <span class="text-[11px] text-huginn-muted/70">thinking</span>
                    <span class="flex gap-0.5 ml-0.5">
                      <span class="w-1 h-1 rounded-full bg-huginn-muted/50 animate-bounce" style="animation-delay:0ms" />
                      <span class="w-1 h-1 rounded-full bg-huginn-muted/50 animate-bounce" style="animation-delay:75ms" />
                      <span class="w-1 h-1 rounded-full bg-huginn-muted/50 animate-bounce" style="animation-delay:150ms" />
                    </span>
                  </div>
                  <div v-if="d.inlineSummary"
                    @click="openThreadDetail(d)"
                    class="ml-5 pl-3 py-2 border-l-2 rounded-r-lg cursor-pointer hover:bg-huginn-surface/40 transition-colors"
                    :style="`border-color:${agentColorMap[d.agentId] ?? 'rgba(88,166,255,0.4)'}`">
                    <div class="flex items-center gap-1.5 mb-1">
                      <div class="w-4 h-4 rounded text-[9px] font-bold flex items-center justify-center"
                        :style="`background:${agentColorMap[d.agentId] ?? 'rgba(88,166,255,0.2)'}33;color:${agentColorMap[d.agentId] ?? 'rgba(88,166,255,0.8)'}`">
                        {{ agentIconMap[d.agentId] || d.agentId?.[0]?.toUpperCase() || '?' }}
                      </div>
                      <span class="text-xs font-semibold" :style="`color:${agentColorMap[d.agentId] ?? 'rgba(88,166,255,0.8)'}`">{{ d.agentId }}</span>
                    </div>
                    <div class="text-sm text-huginn-text/80 whitespace-pre-wrap">{{ d.inlineSummary }}</div>
                  </div>
                </div>
              </div>

              <div v-if="msg.permissionDenials?.length" class="mt-1.5 space-y-1.5 w-full max-w-[75%]">
                <div
                  v-for="pd in msg.permissionDenials"
                  :key="`${pd.threadId}:${pd.tool}`"
                  class="flex items-center gap-2 px-2.5 py-2 rounded-lg border border-huginn-amber/35 bg-huginn-amber/10"
                >
                  <span class="text-xs text-huginn-amber">
                    🔒 {{ pd.agentId || 'Agent' }} needs {{ formatDeniedTool(pd.tool) }} access to continue
                  </span>
                  <button
                    class="ml-auto px-2 py-0.5 rounded text-[11px] border border-huginn-amber/40 text-huginn-amber hover:bg-huginn-amber/15 transition-colors"
                    @click="openAgentAccess(pd.agentId)"
                  >
                    Grant
                  </button>
                </div>
              </div>
            </div>

            <!-- Assistant message (left-aligned) -->
            <div v-else-if="msg.role === 'assistant'" class="group flex gap-3" :class="msg.showHeader ? 'mt-4' : 'mt-1'">
              <!-- Agent avatar — visible only on first message of a run; placeholder spacer otherwise -->
              <div class="w-7 flex-shrink-0 mt-0.5">
                <div v-if="msg.showHeader"
                  class="w-7 h-7 rounded-lg flex items-center justify-center select-none"
                  :style="msg.agent && agentColorMap[msg.agent]
                    ? `background:${agentColorMap[msg.agent]}22;border:1px solid ${agentColorMap[msg.agent]}33`
                    : displayAgent
                      ? `background:${displayAgent.color}22;border:1px solid ${displayAgent.color}33`
                      : 'background:rgba(88,166,255,0.12);border:1px solid rgba(88,166,255,0.2)'">
                  <span class="text-xs font-bold"
                    :style="msg.agent && agentColorMap[msg.agent]
                      ? `color:${agentColorMap[msg.agent]}`
                      : displayAgent ? `color:${displayAgent.color}` : 'color:rgba(88,166,255,0.9)'">
                    {{ msg.agent ? (agentIconMap[msg.agent] || msg.agent[0]?.toUpperCase() || 'H') : (displayAgent?.icon ?? 'H') }}
                  </span>
                </div>
              </div>
              <div class="flex-1 min-w-0 pt-0.5">
                <!-- Per-message agent attribution header (only on first message of a run) -->
                <AgentMessageHeader
                  v-if="msg.showHeader && hallwayName(msg)"
                  :agent-name="hallwayName(msg)"
                  :created-at="msg.createdAt"
                  :agent-description="agentsList.find(a => a.name === hallwayName(msg))?.description"
                />
                <!-- Message text — system-fail prefixes are not teammate speech -->
                <SystemFailLine
                  v-if="isBareFailSpeech(visibleAssistantText(msg) || msg.content)"
                  :content="visibleAssistantText(msg) || msg.content"
                  :tool-name="failDisplayFor(msg.content, msg.toolCalls)?.toolName"
                />
                <MsgTimeReveal
                  v-else-if="visibleAssistantText(msg) && !msg.hideFailSpeech"
                  :created-at="msg.createdAt"
                  :revealed="hallwayTimesRevealed"
                  align="start"
                >
                  <div class="md-content text-sm text-huginn-text leading-relaxed break-words cursor-pointer"
                    data-testid="space-root-bubble"
                    @click="spaceId && openReplyThread(msg)"
                    v-html="renderWithMentions(visibleAssistantText(msg))" />
                </MsgTimeReveal>
                <!-- Active (in-flight) tool calls — anchored inside this message bubble so
                     it always appears below the content, never floating above it. -->
                <div v-if="msg.streaming && visibleToolCalls(activeToolCalls).length" class="mt-2">
                  <div class="inline-flex items-center gap-2 px-3 py-1.5 rounded-xl border border-huginn-border bg-huginn-surface/50">
                    <div class="flex gap-0.5 flex-shrink-0">
                      <span class="w-1 h-1 rounded-full bg-huginn-yellow animate-bounce" style="animation-delay:0ms" />
                      <span class="w-1 h-1 rounded-full bg-huginn-yellow animate-bounce" style="animation-delay:120ms" />
                      <span class="w-1 h-1 rounded-full bg-huginn-yellow animate-bounce" style="animation-delay:240ms" />
                    </div>
                    <svg class="w-3.5 h-3.5 text-huginn-yellow flex-shrink-0" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
                      <path d="M14.7 6.3a1 1 0 000 1.4l1.6 1.6a1 1 0 001.4 0l3.77-3.77a6 6 0 01-7.94 7.94l-6.91 6.91a2.12 2.12 0 01-3-3l6.91-6.91a6 6 0 017.94-7.94l-3.76 3.76z" />
                    </svg>
                    <span class="text-xs text-huginn-text">
                      {{ activeMemoryChipText || `${visibleToolCalls(activeToolCalls).length} tool call${visibleToolCalls(activeToolCalls).length === 1 ? '' : 's'}` }}
                    </span>
                    <span class="text-[11px] text-huginn-muted animate-pulse flex-shrink-0">· running</span>
                  </div>
                </div>
                <!-- Follow-up thinking indicator: lead agent is preparing their synthesis -->
                <div v-if="(msg as any).followUpThinking && !(msg as any).content"
                  class="flex items-center gap-1.5 py-1">
                  <span class="w-1.5 h-1.5 rounded-full bg-huginn-blue animate-bounce" style="animation-delay:0ms" />
                  <span class="w-1.5 h-1.5 rounded-full bg-huginn-blue animate-bounce" style="animation-delay:150ms" />
                  <span class="w-1.5 h-1.5 rounded-full bg-huginn-blue animate-bounce" style="animation-delay:300ms" />
                </div>

                <SpaceReplyChip
                  v-if="spaceId && !msg.parent_id"
                  :count="msg.spaceReplyCount ?? 0"
                  :preview="msg.lastPreview"
                  :typing-agent="spaceReplyTyping[msg.id]"
                  :participant="!!msg.spaceReplyParticipant"
                  :new-since="msg.newSince ?? 0"
                  @open="openReplyThread(msg)"
                />

                <!-- Delegated thread reply strips (Slack-style compact) -->
                <div v-if="msg.delegatedThreads?.length" class="mt-1.5 space-y-1">
                  <div v-for="d in msg.delegatedThreads" :key="d.threadId" class="space-y-1">
                    <button
                      @click="openThreadDetail(d)"
                      class="group flex items-center gap-2 py-1 px-2 -ml-1 rounded-lg transition-all duration-150 hover:bg-huginn-surface/60 overflow-hidden min-w-0"
                      data-testid="delegation-activity-row"
                    >
                      <!-- Agent avatar mini — animated pulse when thread is active -->
                      <div class="relative w-4 h-4 flex-shrink-0">
                        <div class="w-4 h-4 rounded text-[9px] font-bold flex items-center justify-center"
                          :style="`background:${agentColorMap[d.agentId] ?? 'rgba(88,166,255,0.2)'}33;color:${agentColorMap[d.agentId] ?? 'rgba(88,166,255,0.8)'}`">
                          {{ agentIconMap[d.agentId] || d.agentId?.[0]?.toUpperCase() || '?' }}
                        </div>
                        <!-- Active pulse ring when thread is running/thinking/queued -->
                        <span v-if="['running','thinking','queued'].includes(getThreadById(d.threadId)?.Status ?? '')"
                          class="absolute inset-0 rounded animate-ping opacity-50"
                          :style="`background:${agentColorMap[d.agentId] ?? 'rgba(88,166,255,0.4)'}`" />
                      </div>
                      <span class="text-xs text-huginn-text/90 truncate">
                        <template v-if="d.agentId && d.agentId === msg.agent">Handling directly</template>
                        <template v-else>Delegated to
                          <span class="font-semibold" :style="`color:${agentColorMap[d.agentId] ?? 'rgba(88,166,255,0.8)'}`">@{{ d.agentId || 'agent' }}</span>
                        </template>
                      </span>
                      <span class="text-[11px] text-huginn-muted/70">
                        · {{ delegatedThreadStatusLabel(d) }}
                      </span>
                      <span v-if="delegatedThreadProgressLabel(d)" class="text-[11px] text-huginn-muted/60">
                        · {{ delegatedThreadProgressLabel(d) }}
                      </span>
                      <!-- Task description — shown when available, truncated to keep strip compact -->
                      <span v-if="d.task" class="text-[11px] text-huginn-muted/60 truncate min-w-0 flex-1">
                        · {{ d.task }}
                      </span>
                      <span v-else class="flex-1" />
                      <!-- Chevron on hover -->
                      <svg class="w-3 h-3 text-huginn-muted/30 opacity-0 group-hover:opacity-100 transition-opacity flex-shrink-0"
                        viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
                        <polyline points="9 18 15 12 9 6" />
                      </svg>
                    </button>
                    <!-- Delegated agent thinking indicator -->
                    <div
                      v-if="isThreadThinking(d.threadId)"
                      class="flex items-center gap-2 pl-2 py-0.5"
                    >
                      <div
                        class="w-4 h-4 rounded flex-shrink-0 text-[9px] font-bold flex items-center justify-center"
                        :style="`background:${agentColorMap[d.agentId] ?? 'rgba(88,166,255,0.2)'}33;color:${agentColorMap[d.agentId] ?? 'rgba(88,166,255,0.8)'}`"
                      >
                        {{ agentIconMap[d.agentId] || d.agentId?.[0]?.toUpperCase() || '?' }}
                      </div>
                      <span class="text-[11px] text-huginn-muted/70">thinking</span>
                      <span class="flex gap-0.5 ml-0.5">
                        <span class="w-1 h-1 rounded-full bg-huginn-muted/50 animate-bounce" style="animation-delay:0ms" />
                        <span class="w-1 h-1 rounded-full bg-huginn-muted/50 animate-bounce" style="animation-delay:75ms" />
                        <span class="w-1 h-1 rounded-full bg-huginn-muted/50 animate-bounce" style="animation-delay:150ms" />
                      </span>
                    </div>
                    <!-- Inline thread reply preview: show agent's reply summary when thread completes -->
                    <div v-if="d.inlineSummary"
                      @click="openThreadDetail(d)"
                      class="ml-5 pl-3 py-2 border-l-2 rounded-r-lg cursor-pointer hover:bg-huginn-surface/40 transition-colors"
                      :style="`border-color:${agentColorMap[d.agentId] ?? 'rgba(88,166,255,0.4)'}`">
                      <div class="flex items-center gap-1.5 mb-1">
                        <div class="w-4 h-4 rounded text-[9px] font-bold flex items-center justify-center"
                          :style="`background:${agentColorMap[d.agentId] ?? 'rgba(88,166,255,0.2)'}33;color:${agentColorMap[d.agentId] ?? 'rgba(88,166,255,0.8)'}`">
                          {{ agentIconMap[d.agentId] || d.agentId?.[0]?.toUpperCase() || '?' }}
                        </div>
                        <span class="text-xs font-semibold" :style="`color:${agentColorMap[d.agentId] ?? 'rgba(88,166,255,0.8)'}`">{{ d.agentId }}</span>
                      </div>
                      <div class="text-sm text-huginn-text/80 whitespace-pre-wrap">{{ d.inlineSummary }}</div>
                    </div>
                  </div>
                </div>

                <!-- Delegated-thread permission denial cards -->
                <div v-if="msg.permissionDenials?.length" class="mt-1.5 space-y-1.5">
                  <div
                    v-for="pd in msg.permissionDenials"
                    :key="`${pd.threadId}:${pd.tool}`"
                    class="flex items-center gap-2 px-2.5 py-2 rounded-lg border border-huginn-amber/35 bg-huginn-amber/10"
                  >
                    <span class="text-xs text-huginn-amber">
                      🔒 {{ pd.agentId || 'Agent' }} needs {{ formatDeniedTool(pd.tool) }} access to continue
                    </span>
                    <button
                      class="ml-auto px-2 py-0.5 rounded text-[11px] border border-huginn-amber/40 text-huginn-amber hover:bg-huginn-amber/15 transition-colors"
                      @click="openAgentAccess(pd.agentId)"
                    >
                      Grant
                    </button>
                  </div>
                </div>

                <!-- Delegation error chips: shown when delegate_to_agent or tm.Create() failed -->
                <div v-if="(msg as any).delegationErrors?.length" class="mt-1.5 flex flex-wrap gap-1.5">
                  <div
                    v-for="e in (msg as any).delegationErrors"
                    :key="e.agent"
                    class="inline-flex items-center gap-1.5 px-2 py-1 rounded-lg text-[11px] font-medium
                           border border-huginn-red/30 bg-huginn-red/8 text-huginn-red"
                    :title="`Could not delegate to ${e.agent}: ${e.reason}`"
                  >
                    <svg class="w-3 h-3 flex-shrink-0" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round">
                      <circle cx="12" cy="12" r="10"/><line x1="12" y1="8" x2="12" y2="12"/><line x1="12" y1="16" x2="12.01" y2="16"/>
                    </svg>
                    <span>{{ e.agent }} unavailable</span>
                  </div>
                </div>

                <!-- Delegation warning chips: shown when heuristic detects a missed delegation -->
                <div v-if="(msg as any).delegationWarnings?.length" class="mt-1.5 flex flex-wrap gap-1.5">
                  <div
                    v-for="w in (msg as any).delegationWarnings"
                    :key="w.agent + ':' + w.reason"
                    class="inline-flex items-center gap-1.5 px-2 py-1 rounded-lg text-[11px] font-medium
                           border border-huginn-yellow/30 bg-huginn-yellow/8 text-huginn-yellow"
                    :title="w.reason === 'missing_mention_syntax' ? `${w.agent} was mentioned but not delegated — did you mean to assign them a task?` : `Unknown agent: ${w.agent}`"
                  >
                    <svg class="w-3 h-3 flex-shrink-0" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
                      <path d="M10.29 3.86L1.82 18a2 2 0 001.71 3h16.94a2 2 0 001.71-3L13.71 3.86a2 2 0 00-3.42 0z"/><line x1="12" y1="9" x2="12" y2="13"/><line x1="12" y1="17" x2="12.01" y2="17"/>
                    </svg>
                    <span v-if="w.reason === 'missing_mention_syntax'">{{ w.agent }} may have been missed</span>
                    <span v-else>Unknown: {{ w.agent }}</span>
                  </div>
                </div>

                <!-- Inline thread replies from agent_follow_up (Slack-style) -->
                <div v-if="msg.threadReplies?.length" class="mt-2 space-y-2">
                  <div v-for="reply in msg.threadReplies" :key="reply.id"
                    class="ml-5 pl-3 py-2 border-l-2 border-huginn-blue/40 rounded-r-lg bg-huginn-surface/20">
                    <div class="flex items-center gap-1.5 mb-1">
                      <div class="w-4 h-4 rounded text-[9px] font-bold flex items-center justify-center"
                        :style="`background:${agentColorMap[reply.agent] ?? 'rgba(88,166,255,0.2)'}33;color:${agentColorMap[reply.agent] ?? 'rgba(88,166,255,0.8)'}`">
                        {{ agentIconMap[reply.agent] || reply.agent?.[0]?.toUpperCase() || '?' }}
                      </div>
                      <span class="text-xs font-semibold" :style="`color:${agentColorMap[reply.agent] ?? 'rgba(88,166,255,0.8)'}`">{{ reply.agent }}</span>
                    </div>
                    <div class="text-sm text-huginn-text/90 whitespace-pre-wrap">{{ reply.content }}</div>
                  </div>
                </div>

                <!-- Tool call chip (completed, attached to this message).
                     Visible as soon as no tool calls are actively running, so the
                     chip persists below the content even while text is still streaming. -->
                <div v-if="msg.toolCalls?.length && !isBareFailSpeech(visibleAssistantText(msg) || msg.content) && (!msg.streaming || !visibleToolCalls(activeToolCalls).length)" class="mt-2">
                  <!-- Collapsed chip -->
                  <button @click="toggleMsgToolCalls(msg.id)"
                    class="flex items-center gap-2 px-3 py-1.5 rounded-xl border border-huginn-border hover:bg-huginn-surface/80 transition-colors duration-100"
                    :title="messageToolChipFailed(msg.content, msg.toolCalls) ? failDisplayFor(msg.content, msg.toolCalls)?.diagnostic : undefined"
                    :aria-description="messageToolChipFailed(msg.content, msg.toolCalls) ? failDisplayFor(msg.content, msg.toolCalls)?.diagnostic : undefined">
                    <svg class="w-3.5 h-3.5 text-huginn-yellow flex-shrink-0" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
                      <path d="M14.7 6.3a1 1 0 000 1.4l1.6 1.6a1 1 0 001.4 0l3.77-3.77a6 6 0 01-7.94 7.94l-6.91 6.91a2.12 2.12 0 01-3-3l6.91-6.91a6 6 0 017.94-7.94l-3.76 3.76z" />
                    </svg>
                    <span
                      v-if="isMemoryOnlyToolCalls(msg.toolCalls)"
                      class="text-xs text-huginn-text"
                    >
                      🧠 Memory: {{ summarizeMemoryToolCalls(msg.toolCalls) }}
                    </span>
                    <span
                      v-else-if="messageToolChipFailed(msg.content, msg.toolCalls)"
                      class="text-xs text-huginn-red"
                    >
                      {{ failChipLabel() }}
                    </span>
                    <template v-else>
                      <span class="text-xs text-huginn-text">
                        {{ `${msg.toolCalls.length} tool call${msg.toolCalls.length === 1 ? '' : 's'}` }}
                      </span>
                      <span class="text-[11px] text-huginn-green">· done</span>
                    </template>
                    <svg class="w-3 h-3 text-huginn-muted transition-transform duration-150 flex-shrink-0"
                      :class="expandedMsgCalls.has(msg.id) ? 'rotate-180' : ''"
                      viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
                      <polyline points="6 9 12 15 18 9" />
                    </svg>
                  </button>
                  <!-- Expanded detail list -->
                  <div v-if="expandedMsgCalls.has(msg.id)" class="mt-1.5 space-y-1.5">
                    <div v-for="tc in msg.toolCalls" :key="tc.id"
                      class="rounded-xl overflow-hidden border border-huginn-border">
                      <button @click="toggleToolCall(tc)"
                        class="w-full flex items-center gap-2 px-3 py-2 text-left hover:bg-huginn-surface/80 transition-colors duration-100">
                        <span
                          class="text-xs font-medium text-huginn-text flex-1"
                          :title="isFailedToolResult(tc.result) ? `${tc.name}${tc.result ? ` · ${tc.result}` : ''}` : undefined"
                        >{{ isFailedToolResult(tc.result) ? failChipLabel() : tc.name }}</span>
                        <svg class="w-3 h-3 text-huginn-muted transition-transform duration-150 flex-shrink-0"
                          :class="expandedToolCalls.has(tc.id) ? 'rotate-180' : ''"
                          viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
                          <polyline points="6 9 12 15 18 9" />
                        </svg>
                      </button>
                      <div v-if="expandedToolCalls.has(tc.id)"
                        class="border-t border-huginn-border px-3 py-2.5 space-y-2 bg-huginn-surface/30">
                        <div v-if="tc.args && Object.keys(tc.args).length">
                          <p class="text-[10px] text-huginn-muted uppercase tracking-wider mb-1.5">Input</p>
                          <pre class="text-xs text-huginn-muted overflow-x-auto leading-relaxed">{{ JSON.stringify(tc.args, null, 2) }}</pre>
                        </div>
                        <div v-if="tc.result">
                          <p class="text-[10px] text-huginn-muted uppercase tracking-wider mb-1.5">Output</p>
                          <pre class="text-xs text-huginn-muted overflow-x-auto max-h-40 leading-relaxed">{{ tc.result }}</pre>
                        </div>
                      </div>
                    </div>
                  </div>
                </div>
              <MessageActions
                class="mt-1 opacity-0 group-hover:opacity-100 transition-opacity"
                :msg="msg"
                :agent-vault-name="activeAgentVaultName"
                :show-reply="!!spaceId"
                :show-diagnose="!!spaceId && !!msg.delegatedThreads?.length"
                @reply="openReplyThread(msg)"
                @diagnose="diagnoseMessage(msg)"
              />
              </div>
            </div>
          </template>

        </div>
      </div>

      <!-- ── Permission banner ────────────────────────────────────── -->
      <div v-if="pendingPermission" class="px-4 pb-3 flex-shrink-0">
        <div class="rounded-xl px-4 py-3 border"
          style="background:rgba(210,153,34,0.07);border-color:rgba(210,153,34,0.35)">
          <div class="flex items-center gap-2 mb-1.5">
            <svg class="w-3.5 h-3.5 text-huginn-yellow flex-shrink-0" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
              <path d="M10.29 3.86L1.82 18a2 2 0 001.71 3h16.94a2 2 0 001.71-3L13.71 3.86a2 2 0 00-3.42 0z" />
              <line x1="12" y1="9" x2="12" y2="13" /><line x1="12" y1="17" x2="12.01" y2="17" />
            </svg>
            <span class="text-xs text-huginn-yellow font-semibold">Permission required</span>
          </div>
          <p class="text-xs text-huginn-muted mb-3 ml-5.5">{{ permissionDesc }}</p>
          <div class="flex gap-2 ml-5.5">
            <button @click="approvePermission(true)"
              class="px-3 py-1.5 rounded-lg text-xs font-medium text-huginn-green transition-all duration-150
                     border border-huginn-green/30 hover:bg-huginn-green/15 active:scale-95">Allow</button>
            <button @click="approvePermission(false)"
              class="px-3 py-1.5 rounded-lg text-xs font-medium text-huginn-red transition-all duration-150
                     border border-huginn-red/30 hover:bg-huginn-red/15 active:scale-95">Deny</button>
          </div>
        </div>
      </div>

      <!-- ── Delegation preview banners ─────────────────────────── -->
      <div v-if="sessionPreviews.length > 0" class="px-4 pb-2 flex flex-col gap-1.5" data-testid="delegation-preview-list">
        <div
          v-for="preview in sessionPreviews"
          :key="preview.threadId"
          :data-testid="`delegation-preview-${preview.threadId}`"
          class="flex items-start gap-3 px-3 py-2.5 rounded-lg border border-huginn-yellow/30 bg-huginn-yellow/5"
        >
          <svg class="w-3.5 h-3.5 text-huginn-yellow flex-shrink-0 mt-0.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
            <circle cx="12" cy="12" r="10"/><line x1="12" y1="8" x2="12" y2="12"/><line x1="12" y1="16" x2="12.01" y2="16"/>
          </svg>
          <div class="flex-1 min-w-0">
            <p class="text-xs text-huginn-yellow font-medium leading-snug" data-testid="delegation-preview-agent">
              Delegate to <span class="font-bold">{{ preview.agentId }}</span>?
            </p>
            <p class="text-[11px] text-huginn-muted/70 truncate mt-0.5" data-testid="delegation-preview-task">{{ preview.task }}</p>
            <p v-if="previewCountdownText(preview)" class="text-[10px] text-huginn-muted/55 mt-0.5">
              {{ previewCountdownText(preview) }}
            </p>
            <div v-if="preview.expiresAtMs && preview.expiresInSeconds"
              class="mt-1.5 h-1.5 rounded-full bg-huginn-surface/60 overflow-hidden">
              <div class="h-full transition-all duration-500" :style="previewProgressStyle(preview)" />
            </div>
          </div>
          <div class="flex gap-1.5 flex-shrink-0">
            <button
              data-testid="delegation-preview-allow"
              :disabled="!!preview.pendingDecision"
              @click="wsRef && ackPreview(wsRef, preview, true)"
              class="px-2 py-1 text-[10px] font-medium rounded border border-huginn-green/30 text-huginn-green hover:bg-huginn-green/15 transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
            >{{ preview.pendingDecision ? 'Sending...' : 'Allow' }}</button>
            <button
              data-testid="delegation-preview-deny"
              :disabled="!!preview.pendingDecision"
              @click="wsRef && ackPreview(wsRef, preview, false)"
              class="px-2 py-1 text-[10px] font-medium rounded border border-huginn-red/30 text-huginn-red hover:bg-huginn-red/15 transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
            >Deny</button>
          </div>
        </div>
      </div>

      <!-- ── Auto-approve notices ───────────────────────────────── -->
      <div v-if="autoApproveNotices.length > 0" class="px-4 pb-2 flex flex-col gap-1.5">
        <div
          v-for="notice in autoApproveNotices"
          :key="notice.id"
          class="flex items-center gap-2 px-3 py-1.5 rounded-lg text-xs"
          style="background:rgba(46,160,67,0.12);border:1px solid rgba(46,160,67,0.3);color:rgba(46,160,67,0.9)"
        >
          <svg class="w-3 h-3 flex-shrink-0" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round"><polyline points="20 6 9 17 4 12"/></svg>
          Auto-approved — <span class="font-semibold ml-1">@{{ notice.agentName }}</span> took over
        </div>
      </div>

      <!-- ── Swarm status panel ─────────────────────────────────── -->
      <Transition
        enter-active-class="transition-all duration-200 ease-out"
        enter-from-class="opacity-0 translate-y-2"
        enter-to-class="opacity-100 translate-y-0"
        leave-active-class="transition-all duration-150 ease-in"
        leave-from-class="opacity-100 translate-y-0"
        leave-to-class="opacity-0 translate-y-2"
      >
        <div v-if="swarmState && !swarmPanelDismissed" class="px-4 pb-2 flex-shrink-0">
          <div class="relative">
            <SwarmStatus />
            <button
              @click="swarmPanelDismissed = true"
              class="absolute top-2 right-2 w-5 h-5 rounded flex items-center justify-center text-huginn-muted hover:text-huginn-text transition-colors z-10"
              title="Dismiss swarm status"
            >
              <svg class="w-3 h-3" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round">
                <line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/>
              </svg>
            </button>
          </div>
        </div>
      </Transition>

      <!-- ── Streaming banner (above input) ─────────────────────── -->
      <Transition name="ws-banner">
        <div v-if="streaming"
          data-testid="streaming-banner"
          class="flex-shrink-0 relative overflow-hidden text-xs font-medium"
          style="background:rgba(88,166,255,0.08);border-top:1px solid rgba(88,166,255,0.18);color:rgba(88,166,255,0.85)">
          <!-- Indeterminate progress bar along the top edge -->
          <div class="absolute top-0 left-0 h-[2px] w-full overflow-hidden">
            <div class="h-full streaming-progress-bar" style="background:rgba(88,166,255,0.5)" />
          </div>
          <div class="flex items-center gap-2 px-4 py-1.5">
            <span>
              {{ displayAgent?.name ?? 'Agent' }} is responding…<template v-if="activeToolCalls.length"> · <span class="opacity-75">{{ activeToolCalls[0]?.name }}</span></template><template v-if="streamingElapsed >= 10"> ({{ formatElapsed(streamingElapsed) }})</template>
            </span>
          </div>
        </div>
      </Transition>

      <Transition name="ws-banner">
        <div v-if="prestreamThinking && !trivialAskPending"
          class="flex-shrink-0 text-xs px-4 py-1.5"
          style="background:rgba(88,166,255,0.06);border-top:1px solid rgba(88,166,255,0.14);color:rgba(139,148,158,0.95)">
          Preparing context and delegation plan…
        </div>
      </Transition>

      <!-- Interrupt / route diagnose stays offstage. @ is the room route.
           Mid-turn send defaults to a new hallway line (new_request / queue). -->
      <div
        v-if="streaming || queuedRunIds.length > 0"
        data-testid="composer-send-options"
        class="px-4 pb-1 flex-shrink-0"
      >
        <details
          class="text-[11px] text-huginn-muted/55"
          :open="sendOptionsOpen"
          @toggle="onSendOptionsToggle"
        >
          <summary
            data-testid="composer-send-options-summary"
            class="cursor-pointer select-none text-huginn-muted/45 hover:text-huginn-muted/80 list-none [&::-webkit-details-marker]:hidden"
          >
            <span v-if="queuedRunIds.length > 0">{{ queuedRunIds.length }} queued · </span>Send options
          </summary>
          <div v-if="sendOptionsOpen" class="mt-1.5 flex items-center gap-2">
            <span class="text-huginn-muted/80">When you send now:</span>
            <button
              type="button"
              class="px-2 py-1 rounded-md border transition-colors"
              :class="chatIntent === 'update_active_work'
                ? 'border-huginn-border text-huginn-text bg-huginn-surface/60'
                : 'border-huginn-border text-huginn-muted hover:text-huginn-text'"
              @click="chatIntent = 'update_active_work'"
            >
              Update active work
            </button>
            <button
              type="button"
              class="px-2 py-1 rounded-md border transition-colors"
              :class="chatIntent === 'new_request'
                ? 'border-huginn-border text-huginn-text bg-huginn-surface/60'
                : 'border-huginn-border text-huginn-muted hover:text-huginn-text'"
              @click="chatIntent = 'new_request'"
            >
              Start new request
            </button>
          </div>
          <div v-if="sendOptionsOpen && chatIntent === 'update_active_work'" class="mt-1.5 flex items-center gap-2">
            <span class="text-huginn-muted/70">Route:</span>
            <button
              type="button"
              class="px-2 py-1 rounded-md border transition-colors"
              :class="updateRoute === 'all_active'
                ? 'border-huginn-border text-huginn-text bg-huginn-surface/60'
                : 'border-huginn-border text-huginn-muted hover:text-huginn-text'"
              @click="updateRoute = 'all_active'"
            >
              All active delegates
            </button>
            <button
              type="button"
              class="px-2 py-1 rounded-md border transition-colors"
              :class="updateRoute === 'lead_only'
                ? 'border-huginn-border text-huginn-text bg-huginn-surface/60'
                : 'border-huginn-border text-huginn-muted hover:text-huginn-text'"
              @click="updateRoute = 'lead_only'"
            >
              Lead only
            </button>
            <button
              type="button"
              class="px-2 py-1 rounded-md border transition-colors"
              :class="updateRoute === 'specific_delegate'
                ? 'border-huginn-border text-huginn-text bg-huginn-surface/60'
                : 'border-huginn-border text-huginn-muted hover:text-huginn-text'"
              @click="updateRoute = 'specific_delegate'"
            >
              Specific delegate
            </button>
            <select
              v-if="updateRoute === 'specific_delegate' && activeDelegateAgents.length > 0"
              v-model="updateTargetAgent"
              class="ml-1 bg-huginn-surface/50 border border-huginn-border rounded px-2 py-1 text-[11px] text-huginn-text"
            >
              <option v-for="agent in activeDelegateAgents" :key="agent" :value="agent">
                {{ agent }}
              </option>
            </select>
            <span v-else-if="updateRoute === 'specific_delegate'" class="text-huginn-muted/60">
              no active delegates
            </span>
          </div>
        </details>
      </div>

      <!-- ── Input area ──────────────────────────────────────────── -->
      <div class="px-4 pb-4 flex-shrink-0">
        <p v-if="displayAgentUnreliableTools"
          data-testid="composer-model-tools-warning"
          class="text-[10px] text-huginn-yellow/90 leading-snug px-1 pb-2">
          {{ MODEL_TOOL_WARNING }}
        </p>
        <ChatEditor
          ref="chatEditorRef"
          :placeholder="activeSpace ? `Message ${activeSpace.name}...` : undefined"
          :member-names="mentionMemberNames"
          @send="handleEditorSend"
        />
      </div>
      </div><!-- end chat column -->

      <!-- Thread panel (slides in from right — secondary all-threads overview) -->
      <ThreadPanel
        :threads="sessionThreads"
        :agent-colors="agentColorMap"
        :agent-icons="agentIconMap"
        :visible="threadPanelOpen && sessionThreads.length > 0 && !threadDetail.isOpen.value"
        @collapse="threadPanelOpen = false"
        @cancel="cancelThread"
        @inject="(tid, content) => injectThread(tid, content)"
      />

      <!-- Thread detail (primary per-thread view — opened by clicking delegation strip) -->
      <ThreadDetail
        ref="threadDetailRef"
        :visible="threadDetail.isOpen.value"
        :messages="threadDetail.messages.value"
        :loading="threadDetail.loading.value"
        :error="threadDetail.error.value"
        :artifact="threadDetail.artifact.value"
        :thread-id="openThreadLiveId"
        :thread-status="getThreadById(openThreadLiveId)?.Status"
        @close="closeThreadDetail"
        @accept-artifact="threadDetail.handleAcceptArtifact"
        @reject-artifact="threadDetail.handleRejectArtifact"
        @inject="handleThreadDetailInject"
        @start-follow-up="handleThreadFollowUp"
      />

      <ReplyThread
        :visible="!!replyThreadParent && !!spaceId"
        :space-id="spaceId || ''"
        :parent="replyThreadParent"
        :incoming="spaceReplyIncoming"
        :typing-agent="replyThreadParent ? spaceReplyTyping[replyThreadParent.id] : ''"
        :stream-agent="replyThreadParent ? spaceReplyStream[replyThreadParent.id]?.agent : ''"
        :stream-text="replyThreadParent ? spaceReplyStream[replyThreadParent.id]?.text : ''"
        :member-names="mentionMemberNames"
        :fallback-agent="displayAgent?.name || activeSpace?.leadAgent || ''"
        :snag-agent="replyThreadParent ? spaceReplySnag[replyThreadParent.id]?.agent : ''"
        :snag-reason="replyThreadParent ? spaceReplySnag[replyThreadParent.id]?.reason : ''"
        @close="closeReplyThread"
        @posted="onSpaceReplyPosted"
        @seen="onSpaceThreadSeen"
      />

      <!-- Member panel (channel view only, right side) -->
      <ChannelMemberPanel
        v-if="activeSpace"
        :members="spaceMemberCards"
        :open="memberPanelOpen"
        @toggle="toggleMemberPanel"
      />
      </div><!-- end flex row -->
    </template>

    <!-- Tool call detail modal -->
    <ToolCallModal
      :open="selectedToolCall !== null"
      :tc="selectedToolCall"
      @close="selectedToolCall = null"
    />

    <!-- Agent roster modal -->
    <AgentRosterModal
      v-if="rosterOpen && activeSpace"
      :space="activeSpace"
      @close="rosterOpen = false"
    />

    <!-- Agent profile modal (read-only, opened by clicking @mention) -->
    <div
      v-if="agentProfile"
      class="fixed inset-0 z-50 flex items-center justify-center"
      @click.self="agentProfile = null"
    >
      <div class="relative bg-huginn-surface border border-huginn-border rounded-xl shadow-2xl w-80 p-6">
        <button
          class="absolute top-3 right-3 text-huginn-muted hover:text-huginn-text text-sm leading-none"
          @click="agentProfile = null"
        >✕</button>
        <!-- Avatar + name -->
        <div class="flex items-center gap-4 mb-5">
          <div
            class="w-16 h-16 rounded-full flex items-center justify-center text-2xl font-bold flex-shrink-0 select-none"
            :style="{ background: agentProfile.color || '#444', color: '#fff' }"
          >{{ agentProfile.icon || agentProfile.name?.[0]?.toUpperCase() }}</div>
          <div>
            <div class="text-huginn-text font-semibold text-lg leading-tight">{{ agentProfile.name }}</div>
            <div class="text-huginn-muted text-xs mt-1 font-mono truncate max-w-[160px]">{{ agentProfile.model }}</div>
          </div>
        </div>
        <!-- Details -->
        <div class="space-y-2 text-sm">
          <div v-if="agentProfile.description" class="text-huginn-muted">{{ agentProfile.description }}</div>
          <div v-if="agentProfile.memory_enabled" class="flex gap-2">
            <span class="text-huginn-text-dim w-20 flex-shrink-0">memory</span>
            <span class="text-huginn-muted">{{ agentProfile.vault_name || 'enabled' }}</span>
          </div>
          <div v-if="agentProfile.local_tools?.length" class="flex gap-2">
            <span class="text-huginn-text-dim w-20 flex-shrink-0">tools</span>
            <span class="text-huginn-muted">{{ agentProfile.local_tools?.join(', ') }}</span>
          </div>
          <div v-if="agentProfile.skills?.length" class="flex gap-2">
            <span class="text-huginn-text-dim w-20 flex-shrink-0">skills</span>
            <span class="text-huginn-muted">{{ agentProfile.skills?.length }} installed</span>
          </div>
          <div v-if="agentProfile.toolbelt?.length" class="flex gap-2">
            <span class="text-huginn-text-dim w-20 flex-shrink-0">connections</span>
            <span class="text-huginn-muted">{{ agentProfile.toolbelt?.map(t => t.provider).join(', ') }}</span>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { reactive, ref, shallowRef, computed, nextTick, inject, watch, onMounted, onUnmounted } from 'vue'
import type { Ref } from 'vue'
import { useRouter } from 'vue-router'
import { useSpaceTimeline, getSessionSpaceId, getSpaceTimelineState } from '../composables/useSpaceTimeline'
import { ChatEditor } from '../components/ChatEditor'
import { mentionableNames, spaceRosterNames } from '../components/ChatEditor/mentionSuggestions'
import { ThreadPanel } from '../components/ThreadPanel'
import SwarmStatus from '../components/SwarmStatus.vue'
import ThreadDetail from '../components/ThreadDetail.vue'
import ReplyThread from '../components/ReplyThread.vue'
import SpaceReplyChip from '../components/SpaceReplyChip.vue'
import AgentRosterModal from '../components/AgentRosterModal.vue'
import ToolCallModal from '../components/ToolCallModal.vue'
import AgentMessageHeader from '../components/AgentMessageHeader.vue'
import MsgTimeReveal from '../components/MsgTimeReveal.vue'
import MessageActions from '../components/MessageActions.vue'
import SystemFailLine from '../components/SystemFailLine.vue'
import type { HuginnWS, WSMessage } from '../composables/useHuginnWS'
import { api, apiFetch } from '../composables/useApi'
import MemoryVaultChip from '../components/MemoryVaultChip.vue'
import { isVaultMemoryWarning, resolveMemoryChip, type MuninnPresence } from '../utils/memoryChip'
import { useSessions, hydrationQueueOverflowed, type ToolCallRecord, type ChatMessage, type DelegatedThread, type PermissionDenial } from '../composables/useSessions'
import { useThreads, isRunning } from '../composables/useThreads'
import { useThreadDetail } from '../composables/useThreadDetail'
import { useSpaces } from '../composables/useSpaces'
import { useCompanies } from '../composables/useCompanies'
import { useSwarmStatus } from '../composables/useSwarmStatus'
import { adaptSpaceMessages, useMessageEnrichment } from '../composables/useMessageEnrichment'
import { useMarkdownRenderer } from '../composables/useMarkdownRenderer'
import { useChatSearch } from '../composables/useChatSearch'
import { useUnreadTracking } from '../composables/useUnreadTracking'
import { useChatStreaming } from '../composables/useChatStreaming'
import { useBrowserNotifications } from '../composables/useBrowserNotifications'
import { useReplicationStatus } from '../composables/useReplicationStatus'
import { useChatViewHeaderAndMembers } from './chat/useChatViewHeaderAndMembers'
import { hallwayAuthorName } from './chat/respondingAgent'
import { visibleAssistantContent } from '../utils/visibleAssistantContent'
import { isTrivialAsk } from '../utils/trivialAsk'
import { MODEL_TOOL_WARNING, modelUnreliableForTools } from './agents/modelToolCapabilities'
import ChannelMemberPanel from '../components/ChannelMemberPanel.vue'
import { failChipLabel, failDisplayFor, isBareFailSpeech, isFailedToolResult, messageToolChipFailed, visibleToolCalls } from '../utils/honesty'

interface Agent {
  name: string
  color: string
  icon: string
  model: string
  is_default?: boolean
  memory_enabled?: boolean
  vault_name?: string
  memory_mode?: string
  local_tools?: string[]
  skills?: unknown[]
  toolbelt?: Array<{ provider: string; [key: string]: unknown }>
  description?: string
  [key: string]: unknown
}

// ── Markdown rendering (extracted to useMarkdownRenderer) ────────────
// Initialized after agentsList is declared below.

const props = defineProps<{ sessionId?: string; spaceId?: string }>()

const router  = useRouter()
const wsRef   = inject<Ref<HuginnWS | null>>('ws')!
const markSpaceSeen = inject<(spaceId: string) => void>('markSpaceSeen')
const { getSessionThreads, getActiveThreadCount, loadThreads, wireWS: wireThreadWS, getSessionPreviews, clearSessionPreviews, ackPreview, threadsError } = useThreads()

// ── Space timeline mode ────────────────────────────────────────────────────────
// currentSpaceTimeline holds the active space timeline instance.
// It is re-created when spaceId changes.
const currentSpaceTimeline = shallowRef<ReturnType<typeof useSpaceTimeline> | null>(null)
const topSentinelEl = ref<HTMLElement | null>(null)
let intersectionObs: IntersectionObserver | null = null

// Derived space state (null-safe)
const spaceLoadingInitial = computed(() => currentSpaceTimeline.value?.getState().loadingInitial ?? false)
const spaceError = computed(() => currentSpaceTimeline.value?.getState().error ?? null)

watch(() => props.spaceId, async (newId) => {
  // Tear down old IntersectionObserver.
  if (intersectionObs) { intersectionObs.disconnect(); intersectionObs = null }

  if (!newId) {
    currentSpaceTimeline.value = null
    return
  }

  const tl = useSpaceTimeline(newId)
  currentSpaceTimeline.value = tl
  await tl.hydrate()
  // Clear stale unseen-badge entries for this space after hydration so
  // sessionToSpaceMap is populated and getSessionSpaceId returns correct results.
  markSpaceSeen?.(newId)
  // Load REST threads + reply badges for the active session and every
  // session_id on the timeline so ThreadPanel / strips work in space mode.
  for (const sid of collectSpaceSessionIds(tl)) {
    await loadThreads(sid)
    await hydrateThreadBadges(sid)
    applyLiveThreadSummaries(sid)
  }
  await scrollToBottom()
  markCurrentSessionSeen()

  // Set up IntersectionObserver on the top sentinel for infinite scroll.
  await nextTick()
  if (topSentinelEl.value) {
    intersectionObs = new IntersectionObserver(async ([entry]) => {
      if (!entry?.isIntersecting) return
      const anchorId = await tl.loadMore()
      if (anchorId) {
        await nextTick()
        messagesEl.value?.querySelector(`[data-msg-id="${anchorId}"]`)
          ?.scrollIntoView({ block: 'start', behavior: 'instant' })
      }
    }, { threshold: 0.1 })
    intersectionObs.observe(topSentinelEl.value)
  }
}, { immediate: true })

const { sessions, getMessages, fetchMessages, queueIfHydrating, formatSessionLabel, renameSession,
  getLastSeenMessageId, setLastSeenMessageId } = useSessions()
const { activeSpace, dms, openDM } = useSpaces()
const { companies } = useCompanies()
const mentionMemberNames = computed(() => {
  const space = activeSpace.value
  const roster = spaceRosterNames(space)
  if (roster === undefined) return undefined
  const companyId = space?.companyId?.trim()
  if (!companyId) return roster
  const seated = companies.value.find(c => c.id === companyId)?.members ?? []
  return mentionableNames(space, seated)
})

function isKnownSession(id: string): boolean {
  return sessions.value.some(s => s.id === id)
}

// ── Hydration overflow toast ──────────────────────────────────────────────────
// When the pre-hydration WS event queue overflows (> 500 events dropped while
// loading session history), we show a brief amber toast for 8 seconds so the
// user knows to refresh if data looks stale.
const hydrationOverflowToastVisible = ref(false)
let hydrationOverflowTimer: ReturnType<typeof setTimeout> | null = null
const blockedThreadToasts = ref<{ threadId: string; agent: string; message: string }[]>([])
const blockedThreadToastTimers = new Map<string, ReturnType<typeof setTimeout>>()
const previewNowMs = ref(Date.now())
let previewTicker: ReturnType<typeof setInterval> | null = null

const autoApproveNotices = ref<{ id: string; agentName: string }[]>([])
const autoApproveTimers = new Map<string, ReturnType<typeof setTimeout>>()

function showAutoApproveNotice(agentName: string) {
  const id = `aa-${Date.now()}`
  autoApproveNotices.value.push({ id, agentName })
  const t = setTimeout(() => {
    autoApproveNotices.value = autoApproveNotices.value.filter(n => n.id !== id)
    autoApproveTimers.delete(id)
  }, 4000)
  autoApproveTimers.set(id, t)
}

watch(hydrationQueueOverflowed, (overflowed) => {
  if (!overflowed) return
  hydrationOverflowToastVisible.value = true
  if (hydrationOverflowTimer) clearTimeout(hydrationOverflowTimer)
  hydrationOverflowTimer = setTimeout(() => {
    hydrationOverflowToastVisible.value = false
    hydrationOverflowTimer = null
  }, 8_000)
})

function dismissBlockedThreadToast(threadId?: string) {
  if (threadId === undefined) {
    blockedThreadToasts.value = []
    blockedThreadToastTimers.forEach(t => clearTimeout(t))
    blockedThreadToastTimers.clear()
    return
  }
  blockedThreadToasts.value = blockedThreadToasts.value.filter(t => t.threadId !== threadId)
  const timer = blockedThreadToastTimers.get(threadId)
  if (timer) {
    clearTimeout(timer)
    blockedThreadToastTimers.delete(threadId)
  }
}

function showBlockedThreadToast(payload: { threadId: string; agent: string; message: string }) {
  // Replace existing toast for same thread or add new one
  blockedThreadToasts.value = [
    ...blockedThreadToasts.value.filter(t => t.threadId !== payload.threadId),
    payload,
  ]
  const existing = blockedThreadToastTimers.get(payload.threadId)
  if (existing) clearTimeout(existing)
  blockedThreadToastTimers.set(
    payload.threadId,
    setTimeout(() => dismissBlockedThreadToast(payload.threadId), 10_000),
  )
}

function dismissAllBlockedThreadToasts() {
  blockedThreadToasts.value = []
  blockedThreadToastTimers.forEach(t => clearTimeout(t))
  blockedThreadToastTimers.clear()
}

function previewCountdownText(preview: { expiresAtMs?: number; mode?: string }): string {
  if (preview.expiresAtMs == null) return ''
  const secs = Math.max(0, Math.ceil((preview.expiresAtMs - previewNowMs.value) / 1000))
  const modeLabel = preview.mode === 'conditional' ? ' (conditional policy)' : ''
  if (secs === 0) return `Auto-approving now${modeLabel}`
  return `Auto-approves in ${secs}s${modeLabel}`
}

function previewProgressStyle(preview: { expiresAtMs?: number; expiresInSeconds?: number }): string {
  if (preview.expiresAtMs == null || !preview.expiresInSeconds || preview.expiresInSeconds <= 0) {
    return 'width:100%;background:rgba(88,166,255,0.55)'
  }
  const remainingMs = Math.max(0, preview.expiresAtMs - previewNowMs.value)
  const ratio = Math.max(0, Math.min(1, remainingMs / (preview.expiresInSeconds * 1000)))
  const pct = ratio * 100
  const color = ratio <= 0.2
    ? 'rgba(248,81,73,0.75)'
    : ratio <= 0.5
      ? 'rgba(210,153,34,0.75)'
      : 'rgba(88,166,255,0.75)'
  return `width:${pct}%;background:${color}`
}

// ── Session-switch loading state ─────────────────────────────────────
const sessionSwitching = ref(false)

// ── In-chat search (extracted to useChatSearch) ─────────────────────
// Initialized after messagesEl + messages are declared (see below).

// ── Unread tracking (extracted to useUnreadTracking) ─────────────────
// Initialized after messagesEl + messages are declared (see below).

// ── Streaming state (extracted to useChatStreaming) ──────────────────
const {
  activeToolCalls, expandedToolCalls, expandedMsgCalls,
  streaming, currentRunId, notifyStreaming,
  streamingElapsed,
  startStreamingWatchdog, clearStreamingWatchdog,
  startElapsedTimer, stopElapsedTimer, formatElapsed,
  toggleMsgToolCalls,
  resetStreaming,
} = useChatStreaming()
const messagesEl        = ref<HTMLElement>()
const hallwayTimesRevealed = ref(false)
let hallwayTouchX = 0
const pendingPermission = ref<WSMessage | null>(null)
const pendingPermissionBySpace = new Map<string, WSMessage>()

// Permission prompts are per owner space. Switching Tess → Steve must not
// keep Tess's modal on Steve; reopening Tess restores it.
watch(() => props.spaceId, (newId, oldId) => {
  if (oldId && oldId !== newId) {
    if (pendingPermission.value) pendingPermissionBySpace.set(oldId, pendingPermission.value)
    else pendingPermissionBySpace.delete(oldId)
  }
  pendingPermission.value = (newId && pendingPermissionBySpace.get(newId)) || null
})
const runtimeState      = ref('')
// pendingToolResults buffers tool results that arrive before the assistant message exists
// (e.g. prefetch tools like muninn_recall/muninn_where_left_off that fire before streaming starts).
const pendingToolResults = ref<Array<{ id: string; name: string; args: Record<string, unknown>; result: string }>>([])

// flushPendingToolResults attaches any buffered tool results to the current last assistant message.
function flushPendingToolResults(sessionId: string) {
  if (!pendingToolResults.value.length) return
  const msgs = getMessages(sessionId)
  const last = [...msgs].reverse().find(m => m.role === 'assistant')
  if (!last) return
  if (!last.toolCalls) last.toolCalls = []
  for (const tc of pendingToolResults.value) {
    last.toolCalls.push({ id: tc.id, name: tc.name, args: tc.args, result: tc.result, done: true })
  }
  pendingToolResults.value = []
}

// applyPermissionDenied attaches a permission denial entry to whichever
// message owns the delegated thread that was blocked. Uses getSourceMessages()
// so it works in both session mode and space mode.
function applyPermissionDenied(threadId: string, agentId: string, tool: string) {
  const msgs = getSourceMessages()
  const msg = msgs.find((m) =>
    m.delegatedThreads?.some((d) => d.threadId === threadId)
  )
  if (!msg) return
  if (!msg.permissionDenials) msg.permissionDenials = []
  // Frontend dedup: one entry per threadId:tool pair
  const key = `${threadId}:${tool}`
  if (msg.permissionDenials.some((d) => `${d.threadId}:${d.tool}` === key)) return
  msg.permissionDenials.push({ agentId, tool, threadId })
}

function delegatedThreadStatusLabel(d: DelegatedThread): string {
  if (d.done) {
    if ((d.replyCount ?? 0) < 1) return 'completed'
    return d.replyCount === 1 ? '1 reply' : `${d.replyCount} replies`
  }
  const status = getThreadById(d.threadId)?.Status ?? ''
  if (status && !['running', 'thinking', 'queued'].includes(status)) {
    return formatThreadStatus(status)
  }
  return 'working…'
}

function formatCompactDuration(ms: number): string {
  if (!Number.isFinite(ms) || ms <= 0) return ''
  const totalSeconds = Math.floor(ms / 1000)
  if (totalSeconds < 60) return `${totalSeconds}s`
  const minutes = Math.floor(totalSeconds / 60)
  const seconds = totalSeconds % 60
  if (minutes < 60) return `${minutes}m ${seconds}s`
  const hours = Math.floor(minutes / 60)
  const mins = minutes % 60
  return `${hours}h ${mins}m`
}

function delegatedThreadProgressLabel(d: DelegatedThread): string {
  const t = getThreadById(d.threadId)
  if (!t) return ''
  const parts: string[] = []
  const elapsed = formatCompactDuration(t.elapsedMs ?? 0)
  if (elapsed) parts.push(elapsed)
  const toolCallCount = t.toolCalls?.length ?? 0
  if (toolCallCount > 0) {
    parts.push(`${toolCallCount} tool call${toolCallCount === 1 ? '' : 's'}`)
  }
  return parts.join(' · ')
}

// ── Agent state ──────────────────────────────────────────────────────
const agentsList        = ref<Agent[]>([])
const selectedAgentName = ref('')
const agentDropdownOpen = ref(false)
const rosterOpen        = ref(false)
const muninnStatus = ref<MuninnPresence>({})
const memoryChipDismissed = ref(false)

function agentDMNeedle(id: string): string {
  return id.toLowerCase()
}

function findDMForAgent(name: string) {
  const needle = agentDMNeedle(name)
  return dms.value.find(dm => dm.leadAgent.toLowerCase() === needle)
}

function findAgentByName(name: string) {
  const needle = agentDMNeedle(name)
  return agentsList.value.find(a => a.name.toLowerCase() === needle)
}

// True when /chat/:sessionId is an agent name (Steve), not a real session id.
// Used to block send/fetch so we never persist a session named after the agent.
function isAgentDMAlias(id: string | undefined): boolean {
  if (!id || isKnownSession(id)) return false
  return !!findDMForAgent(id) || !!findAgentByName(id)
}

// Bookmarks like /#/chat/Steve are not session IDs — they collide with the
// agent DM at /space/<ulid>. Resolve via dms, then openDM for a known agent.
async function redirectUnknownSessionToAgentDM(sessionId: string): Promise<boolean> {
  if (!sessionId || props.spaceId || isKnownSession(sessionId)) return false
  const existing = findDMForAgent(sessionId)
  if (existing?.id) {
    await router.replace(`/space/${existing.id}`)
    return true
  }
  if (!findAgentByName(sessionId)) return false
  const space = await openDM(sessionId)
  if (space?.id) {
    await router.replace(`/space/${space.id}`)
    return true
  }
  return false
}

watch(
  [() => props.sessionId, agentsList, dms],
  async ([id]) => {
    if (!id || props.spaceId || isKnownSession(id)) return
    await redirectUnknownSessionToAgentDM(id)
  },
  { immediate: true },
)

// ── Extracted composables (depend on agentsList / messagesEl / messages) ──
const { renderWithMentions } = useMarkdownRenderer(agentsList)

// ── Swarm status panel ────────────────────────────────────────────────
const { swarmState } = useSwarmStatus()
const swarmPanelDismissed = ref(false)
// Reset dismiss when a new swarm session starts
watch(() => swarmState.value?.sessionId, (id) => {
  if (id) swarmPanelDismissed.value = false
})
const agentProfile      = ref<Agent | null>(null) // agent shown in read-only profile modal

// ── Thread panel state ────────────────────────────────────────────────
const threadPanelOpen   = ref(false)
const threadPanelPinned = ref(false) // true = don't auto-close when threads finish

// ── Thread detail (per-message thread slide-in) ───────────────────────
const threadDetail = useThreadDetail()
const threadDetailRef = ref<InstanceType<typeof ThreadDetail> | null>(null)
const replyThreadParent = ref<any>(null)
const spaceReplyIncoming = ref<any>(null)
const spaceReplyTyping = reactive<Record<string, string>>({})
const spaceReplyStream = reactive<Record<string, { agent: string; text: string }>>({})
const spaceReplySnag = reactive<Record<string, { agent: string; reason?: string }>>({})
const spaceMentionToast = ref<string | null>(null)

function diagnoseMessage(msg: any) {
  const d = msg?.delegatedThreads?.[0]
  if (d) openThreadDetail(d)
}

function hallwayName(msg: { agent?: string } | null | undefined): string {
  return hallwayAuthorName(msg?.agent, displayAgent.value?.name || activeSpace.value?.leadAgent || selectedAgentName.value)
}

function openReplyThread(msg: any) {
  if (!props.spaceId || !msg?.id) return
  closeThreadDetail()
  const named = hallwayName(msg)
  replyThreadParent.value = named && !msg.agent ? { ...msg, agent: named } : msg
}

function closeReplyThread() {
  replyThreadParent.value = null
}

function onSpaceReplyPosted(count: number, parentId: string) {
  applySpaceReplyMeta(parentId, { reply_count: count })
}

function onSpaceThreadSeen(parentId: string) {
  applySpaceReplyMeta(parentId, { new_since: 0 })
}

function applySpaceReplyMeta(parentId: string, patch: { reply_count?: number; last_preview?: string; new_since?: number }) {
  const tl = currentSpaceTimeline.value?.getState()
  const target = tl?.messages.find((m: any) => m.id === parentId)
  if (target) {
    if (patch.reply_count != null) {
      (target as any).reply_count = patch.reply_count
      ;(target as any).spaceReplyCount = patch.reply_count
    }
    if (patch.last_preview != null) (target as any).last_preview = patch.last_preview
    if (patch.new_since != null) (target as any).new_since = patch.new_since
  }
  if (replyThreadParent.value?.id === parentId) {
    replyThreadParent.value = {
      ...replyThreadParent.value,
      spaceReplyCount: patch.reply_count ?? replyThreadParent.value.spaceReplyCount,
      reply_count: patch.reply_count ?? replyThreadParent.value.reply_count,
      lastPreview: patch.last_preview ?? replyThreadParent.value.lastPreview,
      newSince: patch.new_since ?? replyThreadParent.value.newSince,
    }
  }
}
// Track live thread info for the currently-open ThreadDetail
const openThreadLiveId = ref<string>('')

function openThreadDetail(d: { threadId: string; agentId: string; msgId?: string }) {
  closeReplyThread()
  // msgId is the parent message ID for the API call (GET /api/v1/messages/{id}/thread).
  // Try badge's msgId first, then fall back to live thread state for older sessions.
  const msgId = d.msgId || getThreadById(d.threadId)?.parentMessageId
  if (!msgId) {
    // No parent message ID — fall back to global thread panel.
    threadPanelOpen.value = true
    return
  }
  // Close global ThreadPanel when opening specific thread detail
  threadPanelOpen.value = false
  openThreadLiveId.value = d.threadId
  threadDetail.open(msgId, d.agentId)
}

async function openThreadDetailById(threadId: string) {
  const thread = getThreadById(threadId)
  if (thread?.parentMessageId) {
    threadPanelOpen.value = false
    openThreadLiveId.value = threadId
    threadDetail.open(thread.parentMessageId, thread.AgentID ?? '')
    return
  }
  // Thread not in live state — fetch from API to get parentMessageId
  const lookupSessionId = resolvedThreadSessionId.value
  if (!lookupSessionId) {
    threadPanelOpen.value = true
    return
  }
  try {
    const fetched = await api.threads.get(lookupSessionId, threadId)
    if (fetched?.parentMessageId) {
      threadPanelOpen.value = false
      openThreadLiveId.value = threadId
      threadDetail.open(fetched.parentMessageId, fetched.AgentID ?? '')
      return
    }
  } catch {
    // fall through to panel
  }
  threadPanelOpen.value = true
}

function openBlockedThreadFocus() {
  const blocked = sessionThreads.value.find(t => t.Status === 'blocked')
  if (blocked) {
    openThreadDetailById(blocked.ID)
    return
  }
  threadPanelOpen.value = true
}

function closeThreadDetail() {
  threadDetail.close()
  openThreadLiveId.value = ''
}

const TOOL_LABELS: Record<string, string> = {
  bash: 'shell (bash)',
  computer: 'computer use',
  read_file: 'file read',
  write_file: 'file write',
  edit_file: 'file edit',
  glob: 'file search (glob)',
  grep: 'content search (grep)',
  web_fetch: 'web fetch',
  web_search: 'web search',
  mcp: 'MCP tool',
  list_mcp_resources: 'MCP resources',
  read_mcp_resource: 'MCP resource read',
}

function formatDeniedTool(tool: string): string {
  if (!tool) return 'required'
  if (TOOL_LABELS[tool]) return TOOL_LABELS[tool]!
  // Fallback: convert snake_case to space-separated words
  return tool.replace(/_/g, ' ')
}

function openAgentAccess(agentName: string) {
  if (!agentName) return
  router.push(`/agents/${encodeURIComponent(agentName)}`)
}

// hydrateThreadBadges restores thread reply badges from the DB after a page
// refresh. Calls GET /api/v1/containers/{id}/threads which returns root messages
// that have at least one reply (thread_reply_count > 0), then attaches
// delegatedThreads to the matching message in the UI.
function collectSpaceSessionIds(tl: { getState: () => { activeSessionId: string | null; messages: Array<{ session_id?: string }> } }): string[] {
  const state = tl.getState()
  const ids = new Set<string>()
  if (state.activeSessionId) ids.add(state.activeSessionId)
  for (const m of state.messages ?? []) {
    if (m.session_id) ids.add(m.session_id)
  }
  return [...ids]
}

function applyLiveThreadSummaries(sessionId: string) {
  if (!sessionId) return
  const threads = getSessionThreads(sessionId)
  if (!threads.length) return
  const msgs = getSourceMessages()
  for (const t of threads) {
    const summary = t.Summary?.Summary
    if (!summary) continue
    for (const m of msgs) {
      const entry = m.delegatedThreads?.find(d => d.threadId === t.ID)
      if (entry && !entry.inlineSummary) {
        entry.inlineSummary = summary
        if (!isRunning(t)) entry.done = true
      }
    }
  }
}

const hydratingBadgesFor = new Set<string>()
async function hydrateThreadBadges(sessionId: string) {
  if (!sessionId || hydratingBadgesFor.has(sessionId)) return
  hydratingBadgesFor.add(sessionId)
  try {
    type ContainerThreadRow = { id: string; thread_id?: string; agent: string; thread_reply_count: number; task?: string }
    const rows = await apiFetch<ContainerThreadRow[]>(`/api/v1/containers/${sessionId}/threads`)
    if (!Array.isArray(rows) || rows.length === 0) return
    const rowsPerParent = new Map<string, number>()
    for (const row of rows) {
      rowsPerParent.set(row.id, (rowsPerParent.get(row.id) ?? 0) + 1)
    }
    // Use the mutable source messages so the data survives computed re-renders.
    const msgs = getSourceMessages()
    for (const row of rows) {
      const msg = msgs.find((m: any) => m.id === row.id)
      if (!msg) continue
      if (!(msg as any).delegatedThreads) (msg as any).delegatedThreads = []
      const threadId = row.thread_id || row.id
      const dt = (msg as any).delegatedThreads as DelegatedThread[]
      const existing = dt.find((d) => d.threadId === threadId)
      // thread_reply_count is message-level and may represent multiple delegated
      // threads. When multiple rows share the same parent message, keep per-thread
      // hydration at 1 to avoid showing inflated counts on every badge.
      const hydratedReplies = (rowsPerParent.get(row.id) ?? 0) > 1
        ? 1
        : Math.max(row.thread_reply_count || 1, 1)
      if (existing) {
        if (hydratedReplies > 0) existing.replyCount = hydratedReplies
        if (row.task) existing.task = row.task
      } else {
        dt.push({
          threadId,
          agentId: row.agent,
          msgId: row.id,
          done: true,
          replyCount: hydratedReplies,
          task: row.task || undefined,
        })
      }
    }
  } catch {
    // Non-fatal: badges will be missing but session is still usable
  } finally {
    hydratingBadgesFor.delete(sessionId)
  }
}

// ── Computed ─────────────────────────────────────────────────────────

// getSourceMessages returns the *mutable* source message array for the current
// view mode. In space mode this is the SpaceMessage[] from the timeline state;
// in session mode it's the session message array from useSessions. Thread
// handlers must mutate these objects (not the adapted copies) so the data
// survives computed re-evaluations.
function getSourceMessages(): ChatMessage[] {
  if (props.spaceId) {
    return (currentSpaceTimeline.value?.getState().messages ?? []) as ChatMessage[]
  }
  return props.sessionId ? getMessages(props.sessionId) : []
}

// Owner-space routing: /#/space/:id has no route sessionId, so isForActiveSession
// is true for every event. Write timeline rows to the space that owns
// msg.session_id (via getSessionSpaceId), not the room currently on screen.
function getOwnerMessages(sessionId?: string): ChatMessage[] | null {
  if (props.sessionId) {
    if (sessionId && sessionId !== props.sessionId) return null
    return getMessages(props.sessionId)
  }
  if (!props.spaceId || !sessionId) return null
  const ownerSpaceId = getSessionSpaceId(sessionId)
  if (!ownerSpaceId) return null
  return getSpaceTimelineState(ownerSpaceId).messages as ChatMessage[]
}

function isOwnerView(sessionId?: string): boolean {
  if (props.sessionId) return !sessionId || sessionId === props.sessionId
  if (!props.spaceId || !sessionId) return false
  return getSessionSpaceId(sessionId) === props.spaceId
}

const messages = computed(() => {
  if (props.spaceId) {
    const spMsgs = currentSpaceTimeline.value?.getState().messages ?? []
    return adaptSpaceMessages(spMsgs) as ReturnType<typeof getMessages>
  }
  return props.sessionId ? getMessages(props.sessionId) : []
})

watch(messages, (next) => {
  if (!prestreamThinking.value) return
  const hasVisibleAssistantOutput = next.some(m =>
    m.role === 'assistant' &&
    !!m.streaming &&
    typeof m.content === 'string' &&
    m.content.trim().length > 0
  )
  if (hasVisibleAssistantOutput) {
    prestreamThinking.value = false
  }
}, { deep: true })


const lastSeenMessageId = computed(() =>
  props.sessionId ? getLastSeenMessageId(props.sessionId) : null
)

const activeAgentVaultName = computed(() => {
  const name = selectedAgentName.value || (activeSpace.value ? spaceAgents.value[0]?.name : '')
  if (!name) return ''
  const agent = agentsList.value.find(a => a.name === name)
  return (agent?.vault_name as string) ?? ''
})

// enrichedMessages (extracted to useMessageEnrichment)
const { enrichedMessages } = useMessageEnrichment(messages as any)

// ── Composables that depend on messages / messagesEl ─────────────────
const sessionIdRef = computed(() => props.sessionId)
const {
  chatSearchOpen, chatSearchQuery, chatSearchIndex,
  chatSearchMatches, openChatSearch, closeChatSearch,
  nextChatSearchMatch, prevChatSearchMatch,
  chatSearchInputEl, // template ref: bound via ref="chatSearchInputEl" in template
} = useChatSearch(messages as any, messagesEl)
// vue-tsc does not count template ref bindings as reads; this satisfies noUnusedLocals.
void (chatSearchInputEl satisfies unknown)

const spaceIdRef = computed(() => props.spaceId)
const {
  atBottom, unreadCount, onMessagesScroll, markCurrentSessionSeen, jumpToUnread,
} = useUnreadTracking(sessionIdRef, messages as any, messagesEl, spaceIdRef)

const selectedAgent = computed(() =>
  agentsList.value.find(a => a.name === selectedAgentName.value) ?? null
)

const inFlightUserContent = computed(() => {
  if (!streaming.value) return ''
  for (let i = messages.value.length - 1; i >= 0; i--) {
    const m = messages.value[i]
    if (m?.role === 'user') return m.content ?? ''
  }
  return ''
})

const {
  headerEditing,
  headerEditValue,
  headerInputEl,
  startHeaderEdit,
  commitHeaderEdit,
  cancelHeaderEdit,
  sessionLabel,
  spaceAgents,
  spaceAgentPreviews,
  spaceMemberCards,
  displayAgent,
  memberPanelOpen,
  toggleMemberPanel,
} = useChatViewHeaderAndMembers({
  sessions: sessions as Ref<Array<{ id: string; title?: string }>>,
  sessionId: computed(() => props.sessionId),
  spaceId: computed(() => props.spaceId),
  formatSessionLabel: formatSessionLabel as (s: { id: string; title?: string }) => string,
  renameSession,
  activeSpace: activeSpace as Ref<{ kind?: string; leadAgent: string; memberAgents: string[] } | null>,
  agentsList: agentsList as Ref<Array<{ name: string; icon?: string; model?: string; description?: string; vault_name?: string; color?: string }>>,
  selectedAgentName,
  threadPanelOpen,
  selectedAgent: selectedAgent as Ref<{ name: string; icon?: string; model?: string; description?: string; vault_name?: string; color?: string } | null>,
  streaming,
  inFlightUserContent,
})
// vue-tsc does not count template ref bindings as reads; this satisfies noUnusedLocals.
void (headerInputEl satisfies unknown)

const displayAgentUnreliableTools = computed(() =>
  modelUnreliableForTools({
    name: displayAgent.value?.model,
    supportsTools: (displayAgent.value as { supportsTools?: boolean } | null)?.supportsTools,
  }),
)

const memoryChipAgent = computed(() => {
  const shown = displayAgent.value
  const name = shown?.name || selectedAgentName.value || activeSpace.value?.leadAgent || ''
  if (!name) return null
  return agentsList.value.find(a => a.name === name) ?? shown ?? { name }
})

const memoryChip = computed(() => {
  const agent = memoryChipAgent.value
  return resolveMemoryChip({
    agentName: agent?.name,
    vaultName: (agent as { vault_name?: string } | null)?.vault_name,
    memoryType: (agent as { memory_type?: string } | null)?.memory_type,
    contextNotesEnabled: !!(agent as { context_notes_enabled?: boolean } | null)?.context_notes_enabled,
    muninn: muninnStatus.value,
    dismissed: memoryChipDismissed.value,
    inChat: !!(props.sessionId || props.spaceId),
  })
})

function dismissMemoryChip() {
  memoryChipDismissed.value = true
}

function onMuninnChipStatus(status: MuninnPresence) {
  muninnStatus.value = { ...muninnStatus.value, ...status }
}

async function onMemoryVaultConnected() {
  await Promise.all([loadMuninnStatus(), loadAgents()])
}

async function loadMuninnStatus() {
  try {
    const data = await api.muninn?.status?.()
    if (data) muninnStatus.value = data
  } catch { /* ignore */ }
}

watch(() => memoryChipAgent.value?.name, () => {
  memoryChipDismissed.value = false
})

function exportSession() {
  if (!messages.value.length) return
  const label = sessionLabel.value
  const lines: string[] = [`# ${label}`, '']
  for (const msg of messages.value) {
    if (msg.role !== 'user' && msg.role !== 'assistant') continue
    const who = msg.role === 'user' ? '**You**' : `**${msg.agent ?? 'Assistant'}**`
    const ts = msg.createdAt ? ` · ${new Date(msg.createdAt).toLocaleString()}` : ''
    lines.push(`### ${who}${ts}`)
    lines.push(msg.content ?? '')
    lines.push('')
  }
  const blob = new Blob([lines.join('\n')], { type: 'text/markdown' })
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = `${label.replace(/[^a-z0-9]+/gi, '-').toLowerCase()}.md`
  a.click()
  URL.revokeObjectURL(url)
}

// Session-mode uses props.sessionId. Space-mode DMs/channels have an empty
// sessionId prop — resolve to the timeline's active session (and every other
// session_id on the timeline) so the same thread helpers work.
const resolvedThreadSessionId = computed(() => {
  if (props.sessionId) return props.sessionId
  return currentSpaceTimeline.value?.getState().activeSessionId ?? ''
})

const spaceThreadSessionIds = computed(() => {
  if (props.sessionId) return [props.sessionId]
  if (!currentSpaceTimeline.value) return []
  return collectSpaceSessionIds(currentSpaceTimeline.value)
})

const sessionThreads = computed(() =>
  spaceThreadSessionIds.value.flatMap(id => getSessionThreads(id))
)

const activeThreadCount = computed(() =>
  spaceThreadSessionIds.value.reduce((n, id) => n + getActiveThreadCount(id), 0)
)

const blockedThreadCount = computed(() =>
  sessionThreads.value.filter(t => t.Status === 'blocked').length
)

// Pending delegation previews for this session (shown as approval banners).
const sessionPreviews = computed(() =>
  spaceThreadSessionIds.value.flatMap(id => getSessionPreviews(id))
)

watch(resolvedThreadSessionId, (sid, prev) => {
  if (!props.spaceId || !sid || sid === prev) return
  loadThreads(sid).then(() => {
    hydrateThreadBadges(sid)
    applyLiveThreadSummaries(sid)
  })
})

const agentColorMap = computed(() => {
  const m: Record<string, string> = {}
  for (const ag of agentsList.value) m[ag.name] = ag.color
  return m
})

const agentIconMap = computed(() => {
  const m: Record<string, string> = {}
  for (const ag of agentsList.value) m[ag.name] = ag.icon
  return m
})

// Replication status chip
const { chipText: replChipText, chipClass: replChipClass } = useReplicationStatus(spaceIdRef)

// Auto-show panel when threads appear; auto-hide 4s after all finish (unless pinned)
watch(activeThreadCount, (count) => {
  if (count > 0) {
    threadPanelOpen.value = true
  } else if (!threadPanelPinned.value && sessionThreads.value.length > 0) {
    setTimeout(() => {
      if (activeThreadCount.value === 0 && !threadPanelPinned.value) {
        threadPanelOpen.value = false
      }
    }, 4000)
  }
})

const permissionDesc = computed(() => {
  if (!pendingPermission.value?.payload) return ''
  const p = pendingPermission.value.payload as Record<string, string>
  return `${p.tool ?? ''}: ${p.command ?? p.args ?? ''}`
})


// ── Helpers ──────────────────────────────────────────────────────────
async function scrollToBottom() {
  await nextTick()
  if (messagesEl.value) messagesEl.value.scrollTop = messagesEl.value.scrollHeight
}

const selectedToolCall = ref<ToolCallRecord | null>(null)

function toggleToolCall(tc: ToolCallRecord) {
  selectedToolCall.value = tc
}

function isForActiveSession(msg: WSMessage): boolean {
  if (!props.sessionId) return true
  const sid = msg.session_id || props.sessionId
  return sid === props.sessionId
}

function visibleAssistantText(msg: { role?: string; content?: string; toolCalls?: unknown[] }): string {
  const content = msg.content ?? ''
  if (msg.role !== 'assistant') return content
  // Once tools ran on this message, leftover tool / result JSON is residue,
  // not speech; the chips carry the facts.
  return visibleAssistantContent(content, { afterTools: !!msg.toolCalls?.length })
}

function isMemoryToolName(name: string): boolean {
  return name.startsWith('muninn_')
}

function summarizeMemoryToolCalls(calls: Array<{ name: string }>): string {
  const names = calls.map(c => c.name)
  if (names.some(n => n === 'muninn_remember' || n === 'muninn_remember_batch' || n === 'muninn_remember_tree')) {
    return 'saved to memory'
  }
  if (names.some(n => n === 'muninn_session' || n === 'muninn_where_left_off')) {
    return 'resumed session'
  }
  return 'checked context'
}

function isMemoryOnlyToolCalls(calls?: ToolCallRecord[]): boolean {
  return !!calls?.length && calls.every(tc => isMemoryToolName(tc.name))
}

const activeMemoryChipText = computed(() => {
  if (!activeToolCalls.value.length) return ''
  if (!activeToolCalls.value.every(tc => isMemoryToolName(tc.name))) return ''
  return `🧠 Memory: ${summarizeMemoryToolCalls(activeToolCalls.value)}`
})

// ── Thread helpers ────────────────────────────────────────────────────
function getThreadById(threadId: string) {
  return sessionThreads.value.find(t => t.ID === threadId)
}

function isThreadThinking(threadId: string): boolean {
  const t = getThreadById(threadId)
  return !!t && isRunning(t) && !t.streamingContent
}

function formatThreadStatus(status: string): string {
  const map: Record<string, string> = {
    'done': 'done', 'completed': 'done', 'completed-with-timeout': 'done',
    'error': 'error', 'cancelled': 'cancelled', 'blocked': 'needs help',
    'thinking': 'thinking', 'queued': 'queued', 'running': 'running',
  }
  return map[status] ?? status
}

// ── Agent helpers ─────────────────────────────────────────────────────
async function loadAgents() {
  try {
    const data = await api.agents.list()
    agentsList.value = data as unknown as Agent[]
  } catch { /* ignore */ }
}

function selectAgent(name: string) {
  const ws = wsRef.value
  if (!props.sessionId) return
  selectedAgentName.value = name
  agentDropdownOpen.value = false
  ws?.send({ type: 'set_primary_agent', session_id: props.sessionId, payload: { agent: name } })
}

function syncSessionAgent() {
  if (!props.sessionId) return
  const sess = sessions.value.find(s => s.id === props.sessionId)
  if (sess?.agent) {
    selectedAgentName.value = sess.agent
  } else {
    // No agent recorded yet — default to the first is_default agent, else empty
    const def = agentsList.value.find(a => a.is_default)
    selectedAgentName.value = def?.name ?? ''
  }
}

// ── Chat editor ───────────────────────────────────────────────────────
const chatEditorRef = ref<{ focus: () => void; setText?: (content: string) => void; clear?: () => void } | null>(null)

// Clear the composer when the active DM / channel changes so the previous
// space's draft + placeholder don't leak into the new one.
watch(() => activeSpace.value?.id, (_newId, _oldId) => {
  nextTick(() => {
    chatEditorRef.value?.clear?.()
    chatEditorRef.value?.focus()
  })
})
type ChatIntent = 'update_active_work' | 'new_request'
type UpdateRoute = 'all_active' | 'lead_only' | 'specific_delegate'
const chatIntent = ref<ChatIntent>('new_request')
const updateRoute = ref<UpdateRoute>('all_active')
const updateTargetAgent = ref('')
const queuedRunIds = ref<string[]>([])
const prestreamThinking = ref(false)
const trivialAskPending = ref(false)
const sendOptionsOpen = ref(false)

function armTrivialAskPending(markdown: string) {
  trivialAskPending.value = isTrivialAsk(markdown)
}

function rearmDelegationPlanFromLiveWork() {
  if (!trivialAskPending.value) return
  trivialAskPending.value = false
  if (streaming.value) prestreamThinking.value = true
}

function onSendOptionsToggle(e: Event) {
  sendOptionsOpen.value = (e.target as HTMLDetailsElement).open
}

function resetComposerSendOptions() {
  sendOptionsOpen.value = false
  chatIntent.value = 'new_request'
  updateRoute.value = 'all_active'
}

// Per-space run chrome. ChatView is reused across /space/:id, so a single
// streaming / currentRunId / queuedRunIds / prestreamThinking would paint
// Steve's quiet DM as busy while Tess is the one running, and queue Steve's
// next send against Tess's turn.
type OwnerRunState = {
  streaming: boolean
  prestreamThinking: boolean
  trivialAskPending: boolean
  currentRunId: string
  queuedRunIds: string[]
}
const runByOwner = ref<Record<string, OwnerRunState>>({})

function snapshotOwnerRun(): OwnerRunState {
  return {
    streaming: streaming.value,
    prestreamThinking: prestreamThinking.value,
    trivialAskPending: trivialAskPending.value,
    currentRunId: currentRunId.value,
    queuedRunIds: [...queuedRunIds.value],
  }
}

function persistOwnerRun(key: string) {
  if (!key) return
  runByOwner.value = { ...runByOwner.value, [key]: snapshotOwnerRun() }
}

function applyOwnerRun(state: OwnerRunState) {
  streaming.value = state.streaming
  prestreamThinking.value = state.prestreamThinking
  trivialAskPending.value = state.trivialAskPending
  currentRunId.value = state.currentRunId
  queuedRunIds.value = [...state.queuedRunIds]
  if (state.streaming) {
    startStreamingWatchdog()
    startElapsedTimer()
  } else {
    clearStreamingWatchdog()
    stopElapsedTimer()
  }
}

function restoreOwnerRun(key: string) {
  if (!key) return
  const saved = runByOwner.value[key]
  applyOwnerRun(saved ?? { streaming: false, prestreamThinking: false, trivialAskPending: false, currentRunId: '', queuedRunIds: [] })
}

function finishStoredOwnerRun(key: string) {
  const saved = runByOwner.value[key]
  if (!saved) return
  if (saved.queuedRunIds.length > 0) {
    const next = saved.queuedRunIds[0]!
    const rest = saved.queuedRunIds.slice(1)
    runByOwner.value = {
      ...runByOwner.value,
      [key]: { streaming: true, prestreamThinking: true, trivialAskPending: false, currentRunId: next, queuedRunIds: rest },
    }
    return
  }
  runByOwner.value = {
    ...runByOwner.value,
    [key]: { streaming: false, prestreamThinking: false, trivialAskPending: false, currentRunId: '', queuedRunIds: [] },
  }
}

function finishBackgroundOwnerRun(runId: string): boolean {
  const key = Object.entries(runByOwner.value).find(([, s]) => s.currentRunId === runId)?.[0]
  if (!key) return false
  finishStoredOwnerRun(key)
  return true
}

watch(() => props.spaceId, (newId, oldId) => {
  if (oldId && oldId !== newId) persistOwnerRun(oldId)
  if (newId) restoreOwnerRun(newId)
  sendOptionsOpen.value = false
})

watch([streaming, queuedRunIds], ([isStreaming, queued]) => {
  if (!isStreaming && queued.length === 0) resetComposerSendOptions()
})

const activeDelegateAgents = computed(() => {
  const out = new Set<string>()
  for (const t of sessionThreads.value) {
    if (!t?.AgentID) continue
    if (['done', 'cancelled', 'error', 'completed', 'completed-with-timeout'].includes(t.Status)) continue
    out.add(t.AgentID)
  }
  return [...out]
})

watch(activeDelegateAgents, (agents) => {
  if (updateRoute.value !== 'specific_delegate') return
  if (!agents.length) {
    updateTargetAgent.value = ''
    return
  }
  if (!updateTargetAgent.value || !agents.includes(updateTargetAgent.value)) {
    updateTargetAgent.value = agents[0]!
  }
}, { immediate: true })

function nextRunId(): string {
  return `${Date.now()}-${Math.random().toString(36).slice(2, 9)}`
}

function queueRun(runId: string) {
  queuedRunIds.value = [...queuedRunIds.value, runId]
}

function promoteNextQueuedRun() {
  if (queuedRunIds.value.length === 0) return
  const next = queuedRunIds.value[0]!
  const rest = queuedRunIds.value.slice(1)
  queuedRunIds.value = rest
  currentRunId.value = next
  streaming.value = true
  prestreamThinking.value = true
  trivialAskPending.value = false
  startStreamingWatchdog()
  startElapsedTimer()
  pendingToolResults.value = []
  if (props.sessionId) {
    const msgs = getMessages(props.sessionId)
    msgs.push({
      id: `h-${Date.now()}`,
      role: 'assistant',
      content: '',
      streaming: true,
      agent: selectedAgentName.value || undefined,
      createdAt: new Date().toISOString(),
    })
  }
}

async function handleEditorSend(markdown: string) {
  const ws = wsRef.value
  if (!ws) return
  const runId = nextRunId()
  const sendingWhileStreaming = streaming.value
  // Idle send is just a hallway line. Mid-turn send defaults to a new
  // request (Slack-like queue) unless diagnose Send options picked interrupt.
  const intent = sendingWhileStreaming ? chatIntent.value : 'new_request'
  const payload: Record<string, unknown> = { intent }
  if (intent === 'update_active_work') {
    payload.update_route = updateRoute.value
    if (updateRoute.value === 'specific_delegate') {
      const target = updateTargetAgent.value || activeDelegateAgents.value[0] || ''
      if (target) payload.target_agent = target
    }
  }

  // ── Space mode ─────────────────────────────────────────────────────
  if (props.spaceId) {
    const tl = currentSpaceTimeline.value
    if (!tl) return
    let targetSessionId = tl.getState().activeSessionId

    // No session linked to this space yet — auto-create one on first send.
    if (!targetSessionId) {
      try {
        const newSession = await api.sessions.create(props.spaceId)
        targetSessionId = newSession.session_id
        tl.getState().activeSessionId = targetSessionId
        tl.getState().sessionToSpaceMap.set(targetSessionId, props.spaceId)
      } catch {
        return
      }
    }

    if (!sendingWhileStreaming) {
      currentRunId.value = runId
      streaming.value = true
      prestreamThinking.value = true
      armTrivialAskPending(markdown)
      startStreamingWatchdog()
      startElapsedTimer()
    } else {
      queueRun(runId)
    }

    // Optimistic user message into the space timeline.
    const sentAt = new Date().toISOString()
    tl.getState().messages.push({
      id: `u-${Date.now()}`,
      session_id: targetSessionId,
      seq: -1,
      ts: sentAt,
      created_at: sentAt,
      role: 'user',
      content: markdown,
      agent: '',
    })

    ws.send({
      type: 'chat',
      content: markdown,
      session_id: targetSessionId,
      run_id: runId,
      payload,
    })
    scrollToBottom()
    nextTick(() => chatEditorRef.value?.focus())
    return
  }

  // ── Session mode ────────────────────────────────────────────────────
  if (!props.sessionId || isAgentDMAlias(props.sessionId)) return

  // Auto-select default agent on first send if none chosen yet
  if (!selectedAgentName.value && agentsList.value.length > 0) {
    const def = agentsList.value.find(a => a.is_default) ?? agentsList.value[0]
    if (def) selectAgent(def.name)
  }

  const msgs = getMessages(props.sessionId)
  msgs.push({ id: `u-${Date.now()}`, role: 'user', content: markdown, createdAt: new Date().toISOString() })
  if (!sendingWhileStreaming) {
    currentRunId.value = runId
    streaming.value = true
    prestreamThinking.value = true
    armTrivialAskPending(markdown)
    startStreamingWatchdog()
    startElapsedTimer()
    pendingToolResults.value = [] // reset stale buffered prefetch results from prior response
    msgs.push({ id: `h-${Date.now()}`, role: 'assistant', content: '', streaming: true, agent: selectedAgentName.value || undefined, createdAt: new Date().toISOString() })
  } else {
    queueRun(runId)
  }

  if (props.sessionId) setLastSeenMessageId(props.sessionId, null)
  ws.send({
    type: 'chat',
    content: markdown,
    session_id: props.sessionId,
    run_id: runId,
    payload,
  })
  scrollToBottom()
  nextTick(() => chatEditorRef.value?.focus())
}


function handleRetry(content: string) {
  if (!props.sessionId || !wsRef.value) return
  const runId = `${Date.now()}-${Math.random().toString(36).slice(2, 9)}`
  currentRunId.value = runId
  streaming.value = true
  startStreamingWatchdog()
  startElapsedTimer()
  const msgs = getMessages(props.sessionId)
  msgs.push({ id: `u-${Date.now()}`, role: 'user', content, createdAt: new Date().toISOString() })
  msgs.push({ id: `h-${Date.now()}`, role: 'assistant', content: '', streaming: true,
    agent: selectedAgentName.value || undefined, createdAt: new Date().toISOString() })
  setLastSeenMessageId(props.sessionId, null)
  wsRef.value.send({ type: 'chat', content, session_id: props.sessionId, run_id: runId })
  scrollToBottom()
}


function cancelThread(threadId: string) {
  const ws = wsRef.value
  const sessionId = getThreadById(threadId)?.SessionID || resolvedThreadSessionId.value
  if (!ws || !sessionId) return
  ws.send({ type: 'thread_cancel', payload: { thread_id: threadId }, session_id: sessionId })
}

function injectThread(threadId: string, content: string) {
  const ws = wsRef.value
  if (!ws) return
  const threadSessionId = getThreadById(threadId)?.SessionID
  const sessionId = threadSessionId || resolvedThreadSessionId.value
  if (!sessionId) return
  ws.send({ type: 'thread_inject', payload: { thread_id: threadId, content }, session_id: sessionId })
}

function handleThreadDetailInject(threadId: string, content: string) {
  injectThread(threadId, content)
  // Ack/error will be handled by WS event handlers below
}

function handleThreadFollowUp(_threadId: string, draft?: string) {
  closeThreadDetail()
  nextTick(() => {
    if (draft && chatEditorRef.value?.setText) {
      chatEditorRef.value.setText(draft)
      return
    }
    chatEditorRef.value?.focus()
  })
}


function approvePermission(approved: boolean) {
  const ws = wsRef.value
  if (!ws || !pendingPermission.value) return
  ws.send({
    type: 'permission_response',
    payload: { id: (pendingPermission.value.payload as Record<string, string>)?.id, approved },
  })
  pendingPermission.value = null
  if (props.spaceId) pendingPermissionBySpace.delete(props.spaceId)
}

async function fetchStatus() {
  try {
    const s = await api.runtime.status()
    runtimeState.value = s.state
  } catch { /* ignore */ }
}

const { notify } = useBrowserNotifications()

// ── WS event handlers ────────────────────────────────────────────────
// Track registered handlers so we can remove them on unmount (prevents duplicate
// handlers accumulating across component remounts, e.g. when navigating away and back).
const wsCleanupFns: (() => void)[] = []

function registerWS(ws: HuginnWS, type: string, fn: (msg: WSMessage) => void) {
  ws.on(type, fn)
  wsCleanupFns.push(() => ws.off(type, fn))
}

watch(wsRef, (ws) => {
  if (!ws) return
  // Rebinding WS (route/session switches) must tear down all previous handlers
  // first to avoid duplicate event processing.
  wsCleanupFns.forEach(fn => fn())
  wsCleanupFns.length = 0

  registerWS(ws, 'token', (msg: WSMessage) => {
    // Route by the message's own session_id, not props.sessionId, so tokens
    // are appended to the correct session's message array even during session
    // switches (props.sessionId can change between WS registration and delivery).
    const sid = msg.session_id || props.sessionId
    if (!sid || sid !== props.sessionId) return // ignore tokens for other sessions
    // Set lastSeenMessageId to the last user message on first token (if not set)
    if (sid && !getLastSeenMessageId(sid)) {
      const msgs = getMessages(sid)
      const lastUser = [...msgs].reverse().find(m => m.role === 'user')
      if (lastUser) setLastSeenMessageId(sid, lastUser.id)
    }
    startStreamingWatchdog() // reset watchdog on each token to detect true inactivity
    const apply = () => {
      prestreamThinking.value = false
      // Flush buffered prefetch tool results now that the assistant message exists.
      flushPendingToolResults(sid)
      const msgs = getMessages(sid)
      const streamMsg = [...msgs].reverse().find(m => m.streaming)
      if (streamMsg?.streaming) {
        streamMsg.content += msg.content ?? ''
        streamMsg.content = visibleAssistantContent(streamMsg.content)
        scrollToBottom()
        return
      }
      // Resume replay of a finished hire must not mint a ghost bubble
      // ("They're here.") for an already-persisted assistant row.
      const lastPersisted = [...msgs].reverse().find(m => m.role === 'assistant' && !m.streaming)
      const incoming = (msg.content ?? '').trim()
      if (incoming && lastPersisted && typeof lastPersisted.content === 'string' && lastPersisted.content.includes(incoming)) {
        return
      }
      // If a queued run starts after the previous one completed, tokens can
      // arrive before any optimistic streaming bubble exists. Create one lazily.
      msgs.push({
        id: `h-${Date.now()}`,
        role: 'assistant',
        content: visibleAssistantContent(msg.content ?? ''),
        streaming: true,
        agent: selectedAgentName.value || undefined,
        createdAt: new Date().toISOString(),
      })
      scrollToBottom()
    }
    if (queueIfHydrating(sid, apply)) return
    apply()
  })

registerWS(ws, 'tool_call', (msg: WSMessage) => {
    if (!isForActiveSession(msg)) return
    rearmDelegationPlanFromLiveWork()
    const p = msg.payload as Record<string, unknown>
    activeToolCalls.value.push({
      id: (p?.id as string) ?? Date.now().toString(),
      name: (p?.tool as string) ?? '',
      args: (p?.args as Record<string, unknown>) ?? {},
    })
    scrollToBottom()
  })

registerWS(ws, 'tool_result', (msg: WSMessage) => {
    if (!isForActiveSession(msg)) return
    const p = msg.payload as Record<string, unknown>
    const id = p?.id as string
    // Find in activeToolCalls OR reconstruct from the payload itself (for late arrivals)
    const tc = activeToolCalls.value.find(t => t.id === id) ?? {
      id,
      name: (p?.tool as string) ?? '',
      args: (p?.args as Record<string, unknown>) ?? {},
    }
    if (props.sessionId) {
      const msgs = getMessages(props.sessionId)
      const last = [...msgs].reverse().find(m => m.role === 'assistant')
      if (last) {
        if (!last.toolCalls) last.toolCalls = []
        last.toolCalls.push({ id: tc.id, name: tc.name, args: tc.args, result: p?.result as string, done: true })
      } else {
        // No assistant message yet (prefetch tool fired before streaming started).
        // Buffer the result and flush it once the assistant message is created.
        pendingToolResults.value.push({ id: tc.id, name: tc.name, args: tc.args, result: p?.result as string ?? '' })
      }
    }
    activeToolCalls.value = activeToolCalls.value.filter(t => t.id !== id)
    scrollToBottom()
  })

registerWS(ws, 'permission_request', (msg: WSMessage) => {
    if (props.sessionId) {
      if (!isForActiveSession(msg)) return
      pendingPermission.value = msg
      scrollToBottom()
      return
    }
    if (!props.spaceId || !msg.session_id) return
    const ownerSpaceId = getSessionSpaceId(msg.session_id)
    if (!ownerSpaceId) return
    pendingPermissionBySpace.set(ownerSpaceId, msg)
    if (ownerSpaceId !== props.spaceId) return
    pendingPermission.value = msg
    scrollToBottom()
  })

registerWS(ws, 'done', (msg: WSMessage) => {
    if (!isForActiveSession(msg)) return
    // Ignore stale done events from previous chat runs (e.g. buffered in the WS connection).
    // run_id was introduced alongside this guard; old messages without run_id are also ignored.
    if (!msg.run_id || msg.run_id !== currentRunId.value) {
      if (msg.run_id && finishBackgroundOwnerRun(msg.run_id)) return
      console.debug('[done] ignoring stale done, run_id=', msg.run_id, 'expected=', currentRunId.value)
      return
    }
    clearStreamingWatchdog()
    stopElapsedTimer()
    streaming.value = false
    prestreamThinking.value = false
    trivialAskPending.value = false
    // Move any still-active tool calls to the last assistant message rather than
    // just discarding them. This preserves tool calls that completed during
    // streaming but whose results haven't been attached yet (e.g. timing edge cases).
    if (props.sessionId && activeToolCalls.value.length) {
      const msgs = getMessages(props.sessionId)
      const last = [...msgs].reverse().find(m => m.role === 'assistant')
      if (last) {
        if (!last.toolCalls) last.toolCalls = []
        for (const tc of activeToolCalls.value) {
          // Only add if not already present (tool_result may have already added it)
          if (!last.toolCalls.some(existing => existing.id === tc.id)) {
            last.toolCalls.push({ id: tc.id, name: tc.name, args: tc.args, result: '', done: true })
          }
        }
      }
    }
    activeToolCalls.value = []
    // Flush any buffered prefetch tool results now that the assistant message exists.
    if (props.sessionId) {
      flushPendingToolResults(props.sessionId)
      const msgs = getMessages(props.sessionId)
      const streamMsg = [...msgs].reverse().find(m => m.streaming)
      if (streamMsg) {
        streamMsg.streaming = false
        // Stamp the server-assigned message ID onto the streaming message so
        // thread_started events (with parent_message_id) can find the exact
        // assistant message. Without this, the client-generated "h-..." ID
        // wouldn't match the server's ID in the thread_started payload.
        const serverMsgId = (msg.payload as Record<string, string>)?.message_id
        if (serverMsgId) streamMsg.id = serverMsgId
      }
    }
    promoteNextQueuedRun()
    if (props.spaceId) persistOwnerRun(props.spaceId)
    scrollToBottom()
    fetchStatus()
    // Browser notification — only fires when tab is hidden (checked inside notify())
    if (props.sessionId || props.spaceId) {
      const msgs = props.sessionId ? getMessages(props.sessionId) : getSourceMessages()
      const last = msgs.at(-1)
      const agentName = last?.agent ?? 'Agent'
      const preview = visibleAssistantContent(last?.content ?? '').slice(0, 80)
      const dest = props.spaceId ? `/space/${props.spaceId}` : `/chat/${props.sessionId}`
      notify(
        agentName,
        preview || 'Finished responding',
        `session-done-${props.spaceId || props.sessionId}`,
        () => router.push(dest)
      )
    }
  })

registerWS(ws, 'error', (msg: WSMessage) => {
    if (!isForActiveSession(msg)) return
    // Allow errors without run_id (e.g. "orchestrator not initialized" sent before any run_id is
    // established). Errors that DO carry a run_id must match the current run to avoid stale errors.
    if (msg.run_id && msg.run_id !== currentRunId.value) {
      if (finishBackgroundOwnerRun(msg.run_id)) return
      return
    }
    clearStreamingWatchdog()
    stopElapsedTimer()
    streaming.value = false
    prestreamThinking.value = false
    trivialAskPending.value = false
    activeToolCalls.value = []
    if (props.sessionId) {
      const streamMsg = [...getMessages(props.sessionId)].reverse().find(m => m.streaming)
      if (streamMsg?.streaming) { streamMsg.content += `\n\nerror: ${msg.content}`; streamMsg.streaming = false }
    }
    promoteNextQueuedRun()
    if (props.spaceId) persistOwnerRun(props.spaceId)
    scrollToBottom()
  })

registerWS(ws, 'warning', (msg: WSMessage) => {
    if (isVaultMemoryWarning(msg.content ?? '')) {
      // Vault setup is a chip, not agent speech.
      return
    }
    if (props.sessionId) {
      if (!isForActiveSession(msg)) return
      const msgs = getMessages(props.sessionId)
      const warningForQueuedRun = !!msg.run_id && msg.run_id !== currentRunId.value
      const streamMsg = [...msgs].reverse().find(m => m.streaming)
      if (streamMsg?.role === 'assistant' && streamMsg.streaming && !warningForQueuedRun) {
        streamMsg.content = (msg.content ?? '') + '\n\n' + streamMsg.content
      } else {
        const id = `warn-${Date.now()}`
        msgs.push({ id, role: 'assistant', content: msg.content ?? '', createdAt: new Date().toISOString(), toolCalls: [] })
      }
      scrollToBottom()
      return
    }
    if (!props.spaceId) return
    const msgs = getOwnerMessages(msg.session_id)
    if (!msgs) return
    msgs.push({
      id: `warn-${Date.now()}`,
      session_id: msg.session_id as string,
      seq: -1,
      ts: new Date().toISOString(),
      role: 'assistant',
      content: msg.content ?? '',
      agent: '',
    } as ChatMessage)
    if (isOwnerView(msg.session_id)) scrollToBottom()
  })

registerWS(ws, 'primary_agent_changed', (msg: WSMessage) => {
    const name = (msg.payload as Record<string, string>)?.agent
    if (name && msg.session_id === props.sessionId) {
      selectedAgentName.value = name
    }
  })

  // Attach spawned threads to the exact parent message (Slack-style anchoring).
  // Do not guess when parent_message_id is missing; in busy sessions/channels
  // a "last assistant" fallback can misattach badges to the wrong message.
registerWS(ws, 'thread_started', (msg: WSMessage) => {
    const p = msg.payload as Record<string, string>
    if (!p.thread_id || (!props.sessionId && !props.spaceId)) return
    const parentMsgId = p.parent_message_id
    if (!parentMsgId) return
    const msgs = getSourceMessages()
    const target = msgs.find(m => m.id === parentMsgId)
    if (target) {
      if (!target.delegatedThreads) target.delegatedThreads = []
      const already = target.delegatedThreads.some((d: DelegatedThread) => d.threadId === p.thread_id)
      if (!already) {
        target.delegatedThreads.push({
          threadId: p.thread_id,
          agentId: p.agent_id || '',
          msgId: parentMsgId,
          task: (p.task as string) || undefined,
          replyCount: 0,
        })
      }
    }
  })

  // thread_help is only broadcast when AutoHelpResolver fails (fallback for human input).
  // The thread card's "Waiting for input" form handles this case via thread_inject.
registerWS(ws, 'thread_help', (_msg: WSMessage) => {
    if (!props.sessionId && !props.spaceId) return
    if (props.sessionId && _msg.session_id !== props.sessionId) return
    if (props.spaceId && !isOwnerView(_msg.session_id)) return
    const p = (_msg.payload as Record<string, unknown> | undefined) ?? {}
    const threadId = typeof p.thread_id === 'string' ? p.thread_id : ''
    const helpMessage = typeof p.message === 'string' ? p.message : ''
    const agentFromThread = threadId ? (getThreadById(threadId)?.AgentID ?? '') : ''
    const agentName = agentFromThread || (typeof p.agent_id === 'string' ? p.agent_id : 'Delegate')
    showBlockedThreadToast({
      threadId,
      agent: agentName,
      message: helpMessage,
    })
    if (props.sessionId || props.spaceId) {
      const dest = props.spaceId ? `/space/${props.spaceId}` : `/chat/${props.sessionId}`
      notify(
        `${agentName} needs input`,
        helpMessage || 'A delegated thread is blocked and waiting for guidance.',
        `thread-help-${threadId || Date.now().toString()}`,
        () => router.push(dest)
      )
    }
  })

  // Completion notification streaming (posted by CompletionNotifier after a sub-agent finishes)
registerWS(ws, 'notify_start', (msg: WSMessage) => {
    if (!props.sessionId || msg.session_id !== props.sessionId) return
    const p = msg.payload as Record<string, unknown>
    const agentName = p?.agent as string | undefined
    const msgs = getMessages(props.sessionId)
    msgs.push({ id: `n-${Date.now()}`, role: 'assistant', content: '', streaming: true, agent: agentName || selectedAgentName.value || undefined, createdAt: new Date().toISOString() })
    notifyStreaming.value = true
    scrollToBottom()
  })

registerWS(ws, 'notify_token', (msg: WSMessage) => {
    if (!props.sessionId || msg.session_id !== props.sessionId) return
    const msgs = getMessages(props.sessionId)
    const last = [...msgs].reverse().find(m => m.streaming)
    if (last) { last.content += (msg.payload as Record<string, string>)?.content ?? ''; scrollToBottom() }
  })

registerWS(ws, 'notify_done', (msg: WSMessage) => {
    if (!props.sessionId || msg.session_id !== props.sessionId) return
    const msgs = getMessages(props.sessionId)
    const last = [...msgs].reverse().find(m => m.streaming)
    if (last) last.streaming = false
    notifyStreaming.value = false
    scrollToBottom()
  })

// thread_result: sub-agent finished. Do NOT push content to main chat —
// Sam's output belongs in the thread panel only. The thread badge (already
// marked done by thread_done) and Tom's follow-up synthesis are the main-chat
// signals. Keeping the handler so future telemetry can be added here.
registerWS(ws, 'thread_result', (msg: WSMessage) => {
    if (!props.sessionId && !props.spaceId) return
    if (props.sessionId && msg.session_id !== props.sessionId) return
    // No-op for main chat display intentionally.
  })

// follow_up_start: lead agent is about to synthesize. Show a "thinking" bubble
// immediately so the user knows Tom is picking up where Sam left off.
registerWS(ws, 'follow_up_start', (msg: WSMessage) => {
    if (!props.sessionId && !props.spaceId) return
    if (props.sessionId && msg.session_id !== props.sessionId) return
    const p = msg.payload as Record<string, unknown>
    const agentName = p?.agent as string | undefined
    const msgs = getOwnerMessages(msg.session_id)
    if (!msgs) return
    // Only add if there isn't already a follow-up streaming bubble
    const alreadyExists = msgs.some(m => (m as any).followUpStreaming)
    if (!alreadyExists) {
      const fupStreamMsg: any = {
        id: `fup-stream-${Date.now()}`,
        role: 'assistant',
        content: '',
        agent: agentName || 'Agent',
        createdAt: new Date().toISOString(),
        followUpStreaming: true,
        followUpThinking: true,
        isFollowUp: true,
      }
      // In space mode, include SpaceMessage fields so adaptSpaceMessages works.
      if (props.spaceId && msg.session_id) {
        fupStreamMsg.session_id = msg.session_id
        fupStreamMsg.seq = -1
        fupStreamMsg.ts = new Date().toISOString()
      }
      msgs.push(fupStreamMsg)
      if (isOwnerView(msg.session_id)) scrollToBottom()
    }
  })

// follow_up_token: streaming token from the lead agent's follow-up synthesis.
// Builds a live streaming bubble in the main chat so the user sees Tom "typing".
registerWS(ws, 'follow_up_token', (msg: WSMessage) => {
    if (!props.sessionId && !props.spaceId) return
    if (props.sessionId && msg.session_id !== props.sessionId) return
    const p = msg.payload as Record<string, unknown>
    const agentName = p?.agent as string | undefined
    const token = p?.token as string | undefined
    if (!token) return
    const msgs = getOwnerMessages(msg.session_id)
    if (!msgs) return
    // Find the existing follow-up streaming bubble or create one
    const existing = [...msgs].reverse().find(m => (m as any).followUpStreaming)
    if (existing) {
      existing.content += token
      ;(existing as any).followUpThinking = false // first token: stop thinking dots
    } else {
      const fupFallback: any = {
        id: `fup-stream-${Date.now()}`,
        role: 'assistant',
        content: token,
        agent: agentName || 'Agent',
        followUpStreaming: true,
        isFollowUp: true,
      }
      if (props.spaceId && msg.session_id) {
        fupFallback.session_id = msg.session_id
        fupFallback.seq = -1
        fupFallback.ts = new Date().toISOString()
      }
      msgs.push(fupFallback)
    }
    if (isOwnerView(msg.session_id)) scrollToBottom()
  })

// agent_follow_up: final persisted follow-up reply from the lead agent.
// Always renders as a top-level message in the main channel (Slack-like UX:
// Tom synthesises Sam's thread result and posts it to the channel, not as
// a nested reply on the @mention message).
registerWS(ws, 'agent_follow_up', (msg: WSMessage) => {
    if (!props.sessionId && !props.spaceId) return
    if (props.sessionId && msg.session_id !== props.sessionId) return
    const p = msg.payload as Record<string, unknown>
    const agentName = p?.agent as string | undefined
    const content = p?.content as string | undefined
    if (!content) return
    const msgs = getOwnerMessages(msg.session_id)
    if (!msgs) return
    // Remove the streaming bubble if it exists
    const streamIdx = msgs.findIndex(m => (m as any).followUpStreaming)
    if (streamIdx >= 0) msgs.splice(streamIdx, 1)

    // Top-level message in the main channel — marked as follow-up so the
    // enrichedMessages computed always shows a header (prevents visual merge
    // with Tom's preceding @mention message).
    const fupMsg: any = {
      id: `fup-${Date.now()}`,
      role: 'assistant',
      content,
      agent: agentName || 'Agent',
      isFollowUp: true,
    }
    // In space mode, include SpaceMessage fields so adaptSpaceMessages works.
    if (props.spaceId && msg.session_id) {
      fupMsg.session_id = msg.session_id
      fupMsg.seq = -1
      fupMsg.ts = new Date().toISOString()
    }
    msgs.push(fupMsg)
    if (isOwnerView(msg.session_id)) scrollToBottom()
    if (props.sessionId || isOwnerView(msg.session_id)) {
      const dest = props.spaceId ? `/space/${props.spaceId}` : `/chat/${props.sessionId}`
      notify(
        agentName ?? 'Agent',
        'Has a follow-up for you',
        `follow-up-${props.spaceId || props.sessionId}`,
        () => router.push(dest)
      )
    }
  })

// follow_up_cancelled: lead agent failed to synthesize (session busy or error).
// Remove the thinking bubble so the UI doesn't hang indefinitely.
registerWS(ws, 'follow_up_cancelled', (msg: WSMessage) => {
    if (!props.sessionId && !props.spaceId) return
    if (props.sessionId && msg.session_id !== props.sessionId) return
    const msgs = getOwnerMessages(msg.session_id)
    if (!msgs) return
    // Remove the thinking bubble if it exists
    const idx = msgs.findIndex(m => (m as any).followUpStreaming)
    if (idx >= 0) msgs.splice(idx, 1)
  })

registerWS(ws, 'thread_inject_ack', (_msg: WSMessage) => {
    const payload = (_msg.payload as Record<string, unknown> | undefined) ?? {}
    const ackThreadID = payload.thread_id
    if (typeof ackThreadID === 'string' && openThreadLiveId.value && ackThreadID !== openThreadLiveId.value) {
      return
    }
    const deliveredTo = payload.delivered_to_agent
    const sharedWith = payload.shared_with_active
    threadDetailRef.value?.onInjectAck({
      delivered_to_agent: typeof deliveredTo === 'string' ? deliveredTo : undefined,
      shared_with_active: typeof sharedWith === 'number' ? sharedWith : undefined,
    })
  })

registerWS(ws, 'thread_inject_error', (_msg: WSMessage) => {
    const reason = (_msg.payload as Record<string, unknown> | undefined)?.reason
    threadDetailRef.value?.onInjectError(typeof reason === 'string' ? reason : undefined)
  })

  // Phase D: thread_done — update badge status on the parent message and add
  // a concise completion card to the main timeline so users don't need to hunt
  // in side panels for delegated outcomes.
registerWS(ws, 'thread_done', (msg: WSMessage) => {
    if (!props.sessionId && !props.spaceId) return
    // In session mode, verify session_id matches. In space mode, write the
    // completion card onto the owner space (getSessionSpaceId), not the
    // currently viewed room — the WS subscription is not per-space.
    if (props.sessionId && msg.session_id !== props.sessionId) return
    const p = msg.payload as Record<string, unknown>
    const threadId = p?.thread_id as string | undefined
    if (!threadId) return
    const summary = typeof p?.summary === 'string' ? p.summary : ''
    const agentId = p?.agent_id as string | undefined
    // Mark any delegatedThread entry for this thread as done so the badge
    // reflects the final status without requiring a page refresh.
    const replyCount = p?.reply_count as number | undefined
    const msgs = getOwnerMessages(msg.session_id)
    if (!msgs) return
    for (const m of msgs) {
      const dt = m.delegatedThreads
      if (dt) {
        const entry = dt.find(d => d.threadId === threadId)
        if (entry) {
          entry.done = true
          // Update reply count from thread_done payload when provided.
          if (replyCount != null && replyCount > 0) {
            entry.replyCount = replyCount
          } else if (entry.replyCount == null) {
            entry.replyCount = 0
          }
          // Show the thread summary inline under the badge (Slack-style thread preview)
          if (summary) {
            entry.inlineSummary = summary
          }
        }
      }
      if (m.permissionDenials?.length) {
        m.permissionDenials = m.permissionDenials.filter((pd: PermissionDenial) => pd.threadId !== threadId)
      }
    }
    // Remove any stale streaming tool calls scoped to the completed thread's
    // agent so they don't linger in the active tool call list.
    if (agentId) {
      activeToolCalls.value = activeToolCalls.value.filter(tc => (tc as any).agent !== agentId)
    }
    if (summary) {
      const alreadyPosted = msgs.some(m => m.threadSummaryThreadId === threadId)
      if (!alreadyPosted) {
        const completionMsg: ChatMessage = {
          id: `thread-summary-${threadId}-${Date.now()}`,
          role: 'assistant',
          content: `**${agentId || 'Delegate'}** completed delegated work: ${summary}`,
          agent: agentId || undefined,
          createdAt: new Date().toISOString(),
          threadSummary: true,
          threadSummaryThreadId: threadId,
          ...(props.spaceId && msg.session_id ? {
            session_id: msg.session_id as string,
            seq: -1,
            ts: new Date().toISOString(),
          } : {}),
        }
        msgs.push(completionMsg)
      }
    }
  })

registerWS(ws, 'delegation_preview_timeout', (msg: WSMessage) => {
    if (!props.sessionId && !props.spaceId) return
    if (props.sessionId && msg.session_id !== props.sessionId) return
    const p = (msg.payload as Record<string, unknown> | undefined) ?? {}
    const threadId = typeof p.thread_id === 'string' ? p.thread_id : ''
    if (!threadId) return
    const timeoutSeconds = typeof p.timeout_seconds === 'number'
      ? p.timeout_seconds
      : (typeof p.timeout_seconds === 'string' ? Number.parseInt(p.timeout_seconds, 10) : 30)
    const agentId = typeof p.agent_id === 'string'
      ? p.agent_id
      : (typeof p.agent === 'string' ? p.agent : 'Delegate')
    const msgs = getOwnerMessages(msg.session_id)
    if (!msgs) return
    if (isOwnerView(msg.session_id)) showAutoApproveNotice(agentId)
    const eventId = `preview-timeout-${threadId}`
    if (msgs.some(m => m.id === eventId)) return
    const timeoutLabel = Number.isFinite(timeoutSeconds) && timeoutSeconds > 0 ? timeoutSeconds : 30
    msgs.push({
      id: eventId,
      role: 'assistant',
      content: `Delegation to @${agentId} was auto-approved after ${timeoutLabel}s.`,
      createdAt: new Date().toISOString(),
      ...(props.spaceId && msg.session_id ? {
        session_id: msg.session_id as string,
        seq: -1,
        ts: new Date().toISOString(),
      } : {}),
    })
    if (isOwnerView(msg.session_id)) scrollToBottom()
  })

  // thread_reply_updated: parent message reply count changed in persistence.
  // This event is message-scoped, so only apply the count when there is a
  // single delegated thread anchored to that parent message.
registerWS(ws, 'space_reply', (msg: WSMessage) => {
    const p = (msg.payload ?? {}) as Record<string, unknown>
    const sid = typeof p.space_id === 'string' ? p.space_id : ''
    if (!props.spaceId || sid !== props.spaceId) return
    const parentId = typeof p.parent_id === 'string' ? p.parent_id : ''
    if (!parentId) return
    const count = typeof p.reply_count === 'number' ? p.reply_count : undefined
    const preview = typeof p.last_preview === 'string' ? p.last_preview : undefined
    applySpaceReplyMeta(parentId, { reply_count: count, last_preview: preview })
    const raw = p.message as any
    if (raw && raw.id && replyThreadParent.value?.id === parentId) {
      spaceReplyIncoming.value = raw
    }
    delete spaceReplyStream[parentId]
  })

registerWS(ws, 'space_reply_typing', (msg: WSMessage) => {
    const p = (msg.payload ?? {}) as Record<string, unknown>
    const sid = typeof p.space_id === 'string' ? p.space_id : ''
    if (!props.spaceId || sid !== props.spaceId) return
    const parentId = typeof p.parent_id === 'string' ? p.parent_id : ''
    const agent = typeof p.agent === 'string' ? p.agent : ''
    if (!parentId || !agent) return
    spaceReplyTyping[parentId] = agent
    delete spaceReplySnag[parentId]
  })

registerWS(ws, 'space_reply_token', (msg: WSMessage) => {
    const p = (msg.payload ?? {}) as Record<string, unknown>
    const sid = typeof p.space_id === 'string' ? p.space_id : ''
    if (!props.spaceId || sid !== props.spaceId) return
    const parentId = typeof p.parent_id === 'string' ? p.parent_id : ''
    const agent = typeof p.agent === 'string' ? p.agent : ''
    const token = typeof p.token === 'string' ? p.token : ''
    if (!parentId || !token) return
    const prev = spaceReplyStream[parentId]
    spaceReplyStream[parentId] = {
      agent: agent || prev?.agent || '',
      text: (prev?.text ?? '') + token,
    }
  })

registerWS(ws, 'space_reply_typing_done', (msg: WSMessage) => {
    const p = (msg.payload ?? {}) as Record<string, unknown>
    const sid = typeof p.space_id === 'string' ? p.space_id : ''
    if (!props.spaceId || sid !== props.spaceId) return
    const parentId = typeof p.parent_id === 'string' ? p.parent_id : ''
    if (!parentId) return
    delete spaceReplyTyping[parentId]
    delete spaceReplyStream[parentId]
    const agent = typeof p.agent === 'string' ? p.agent : ''
    const err = typeof p.error === 'string' ? p.error : ''
    if (err && agent) {
      spaceReplySnag[parentId] = { agent, reason: err }
    }
  })

registerWS(ws, 'space_reply_mention', (msg: WSMessage) => {
    const p = (msg.payload ?? {}) as Record<string, unknown>
    const sid = typeof p.space_id === 'string' ? p.space_id : ''
    if (!sid) return
    if (props.spaceId === sid && replyThreadParent.value) return
    const preview = typeof p.preview === 'string' && p.preview ? p.preview : 'You were mentioned in a thread'
    spaceMentionToast.value = preview
    setTimeout(() => { if (spaceMentionToast.value === preview) spaceMentionToast.value = null }, 6000)
  })

registerWS(ws, 'thread_reply_updated', (msg: WSMessage) => {
    if (!props.sessionId && !props.spaceId) return
    if (props.sessionId && msg.session_id !== props.sessionId) return
    const p = msg.payload as { message_id?: string; reply_count?: number }
    if (!p.message_id || typeof p.reply_count !== 'number') return
    const target = getSourceMessages().find((m) => m.id === p.message_id) as ChatMessage | undefined
    if (!target?.delegatedThreads?.length) return
    if (target.delegatedThreads.length === 1) {
      target.delegatedThreads[0]!.replyCount = p.reply_count
    }
  })

  // delegation_error: a thread could not be created for an @mentioned agent.
  // Mark the parent message with a failed-delegation badge so the user isn't
  // left waiting for a response that will never come.
registerWS(ws, 'delegation_error', (msg: WSMessage) => {
    if (!props.sessionId && !props.spaceId) return
    if (props.sessionId && msg.session_id !== props.sessionId) return
    const p = msg.payload as Record<string, unknown>
    const parentMsgId = p?.parent_msg_id as string | undefined
    const agent = p?.agent as string | undefined
    const reason = p?.error as string | undefined
    if (!parentMsgId || !agent) return
    const msgs = getSourceMessages()
    for (const m of msgs) {
      if (m.id === parentMsgId) {
        if (!(m as any).delegationErrors) (m as any).delegationErrors = []
        const errExists = (m as any).delegationErrors.some((e: any) => e.agent === agent)
        if (!errExists) (m as any).delegationErrors.push({ agent, reason: reason ?? 'unknown' })
        break
      }
    }
  })

  // delegation_warning: @mention was found but agent name was not recognised,
  // OR no @mention was found but the lead agent referenced an agent by name in
  // natural language (heuristic fallback). Surface to the user so they know
  // why delegation didn't fire.
registerWS(ws, 'delegation_warning', (msg: WSMessage) => {
    if (!props.sessionId && !props.spaceId) return
    if (props.sessionId && msg.session_id !== props.sessionId) return
    const p = msg.payload as Record<string, unknown>
    const parentMsgId = p?.parent_msg_id as string | undefined
    const unknown = p?.unknown as string[] | undefined
    const heuristic = p?.heuristic_agents as string[] | undefined
    if (!parentMsgId) return
    const msgs = getSourceMessages()
    for (const m of msgs) {
      if (m.id === parentMsgId) {
        if (unknown?.length) {
          if (!(m as any).delegationWarnings) (m as any).delegationWarnings = []
          for (const a of unknown) {
            const exists = (m as any).delegationWarnings.some((w: any) => w.agent === a && w.reason === 'unknown_agent')
            if (!exists) (m as any).delegationWarnings.push({ agent: a, reason: 'unknown_agent' })
          }
        }
        if (heuristic?.length) {
          if (!(m as any).delegationWarnings) (m as any).delegationWarnings = []
          for (const a of heuristic) {
            const exists = (m as any).delegationWarnings.some((w: any) => w.agent === a && w.reason === 'missing_mention_syntax')
            if (!exists) (m as any).delegationWarnings.push({ agent: a, reason: 'missing_mention_syntax' })
          }
        }
        break
      }
    }
  })

  // thread_permission_denied: a delegated agent was blocked from using a tool.
  // Surface a denial card on the parent message so the user can grant access.
registerWS(ws, 'thread_permission_denied', (msg: WSMessage) => {
    if (!props.sessionId && !props.spaceId) return
    if (props.sessionId && msg.session_id !== props.sessionId) return
    const p = msg.payload as Record<string, unknown>
    const threadId = p?.thread_id as string
    const agentId = p?.agent_id as string
    const tool = p?.tool as string
    if (!threadId || !agentId || !tool) return

    applyPermissionDenied(threadId, agentId, tool)
  })

  // Wire thread events via useThreads composable
  const cleanupThreadWS = wireThreadWS(ws, () => resolvedThreadSessionId.value)
  if (typeof cleanupThreadWS === 'function') {
    wsCleanupFns.push(cleanupThreadWS)
  }
}, { immediate: true })

// Clean up all WS event handlers when component unmounts to prevent
// duplicate handlers accumulating across route changes.
onUnmounted(() => {
  clearStreamingWatchdog()
  if (hydrationOverflowTimer) {
    clearTimeout(hydrationOverflowTimer)
    hydrationOverflowTimer = null
  }
  blockedThreadToastTimers.forEach(t => clearTimeout(t))
  blockedThreadToastTimers.clear()
  if (previewTicker) {
    clearInterval(previewTicker)
    previewTicker = null
  }
  if (intersectionObs) { intersectionObs.disconnect(); intersectionObs = null }
  wsCleanupFns.forEach(fn => fn())
  wsCleanupFns.length = 0
  document.removeEventListener('keydown', handleGlobalKeydown)
})

// Reset state and sync agent when switching sessions
watch(() => props.sessionId, async (newSessionId, oldSessionId) => {
  if (oldSessionId && oldSessionId !== newSessionId) {
    clearSessionPreviews(oldSessionId)
  }
  // Leaving session mode (e.g. /chat/:id → /space/:id) must not wipe
  // space-keyed run chrome that restoreOwnerRun just applied.
  if (!newSessionId) return
  resetStreaming()
  prestreamThinking.value = false
  trivialAskPending.value = false
  queuedRunIds.value = []
  dismissAllBlockedThreadToasts()
  pendingPermission.value = null
  agentDropdownOpen.value = false
  hydratingBadgesFor.clear()  // reset so new session can hydrate
  syncSessionAgent()
  fetchStatus()
  nextTick(() => chatEditorRef.value?.focus())
  // Load existing threads and message history for this session
  if (props.sessionId && !isAgentDMAlias(props.sessionId)) {
    loadThreads(props.sessionId)
    // Only show skeleton if the session has no cached messages yet
    const alreadyCached = getMessages(props.sessionId).length > 0
    if (!alreadyCached) sessionSwitching.value = true
    await fetchMessages(props.sessionId)
    sessionSwitching.value = false
    hydrateThreadBadges(props.sessionId)
    applyLiveThreadSummaries(props.sessionId)
    // Mark session as seen on switch (starts unread count from here)
    markCurrentSessionSeen()
    await scrollToBottom()
  }
})

// Close agent dropdown on outside click
function handleOutsideClick(e: MouseEvent) {
  if (agentDropdownOpen.value) {
    const target = e.target as HTMLElement
    if (!target.closest('.relative')) agentDropdownOpen.value = false
  }
}

function onHallwayTouchStart(e: TouchEvent) {
  hallwayTouchX = e.touches[0]?.clientX ?? 0
}
function onHallwayTouchMove(e: TouchEvent) {
  const x = e.touches[0]?.clientX ?? hallwayTouchX
  const dx = x - hallwayTouchX
  if (dx < -36) hallwayTimesRevealed.value = true
  if (dx > 36) hallwayTimesRevealed.value = false
}
function onHallwayTouchEnd() {
  // reveal persists until swipe-right
}

// Handle clicks on @agent-mention spans rendered inside v-html markdown.
// Uses event delegation so no per-message listener is needed.
// Shows a read-only agent profile modal instead of navigating away.
async function handleMessagesClick(e: MouseEvent) {
  const span = (e.target as HTMLElement).closest('.agent-mention')
  if (!span) return
  e.stopPropagation()
  const name = (span as HTMLElement).dataset.agent
  if (!name) return
  // Try local cache first for instant open, then fetch full config.
  const local = agentsList.value.find(a => a.name.toLowerCase() === name.toLowerCase())
  agentProfile.value = local ?? { name, color: '#58A6FF', icon: name[0]?.toUpperCase() ?? '?', model: '' }
  try {
    const full = await api.agents.get(name) as Agent
    agentProfile.value = full
  } catch { /* keep local fallback */ }
}

function handleGlobalKeydown(e: KeyboardEvent) {
  // Ctrl+F / Cmd+F — open in-chat search
  if ((e.ctrlKey || e.metaKey) && e.key === 'f' && (props.sessionId || props.spaceId)) {
    e.preventDefault()
    if (chatSearchOpen.value) {
      closeChatSearch()
    } else {
      openChatSearch()
    }
  }
}

onMounted(async () => {
  previewTicker = setInterval(() => {
    previewNowMs.value = Date.now()
  }, 1000)
  await loadAgents()
  syncSessionAgent()
  fetchStatus()
  void loadMuninnStatus()
  if (props.sessionId && !isAgentDMAlias(props.sessionId)) {
    await fetchMessages(props.sessionId)
    hydrateThreadBadges(props.sessionId)
    markCurrentSessionSeen()
    await scrollToBottom()
  }
  nextTick(() => chatEditorRef.value?.focus())
  document.addEventListener('click', handleOutsideClick)
  document.addEventListener('keydown', handleGlobalKeydown)
})
</script>

<style scoped>
.ws-banner-enter-active,
.ws-banner-leave-active {
  transition: max-height 0.2s ease, opacity 0.2s ease;
  overflow: hidden;
}
.ws-banner-enter-from,
.ws-banner-leave-to {
  max-height: 0;
  opacity: 0;
}
.ws-banner-enter-to,
.ws-banner-leave-from {
  max-height: 48px;
  opacity: 1;
}
@keyframes streaming-indeterminate {
  0%   { transform: translateX(-100%); }
  100% { transform: translateX(400%); }
}
.streaming-progress-bar {
  width: 30%;
  animation: streaming-indeterminate 1.6s ease-in-out infinite;
}
</style>
