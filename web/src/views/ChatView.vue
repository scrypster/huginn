<template>
  <div class="flex flex-col h-full bg-huginn-bg">

    <!-- ── WebSocket connection state banner ───────────────────────── -->
    <Transition name="ws-banner">
      <div v-if="wsConnectionState === 'reconnecting'"
        class="flex-shrink-0 flex items-center justify-between gap-3 px-4 py-2 text-xs font-medium"
        style="background:rgba(227,179,65,0.12);border-bottom:1px solid rgba(227,179,65,0.25);color:rgba(227,179,65,0.92)">
        <div class="flex items-center gap-2">
          <svg class="w-3.5 h-3.5 animate-spin flex-shrink-0" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5"><path d="M21 12a9 9 0 1 1-6.219-8.56"/></svg>
          <span>Reconnecting… (attempt {{ wsReconnectAttempts }}/{{ wsMaxAttempts }})<span v-if="wsSecondsUntilRetry > 0"> — retrying in {{ wsSecondsUntilRetry }}s</span></span>
        </div>
        <button @click="wsReconnectNow()"
          class="px-2 py-0.5 rounded border border-huginn-amber/40 hover:bg-huginn-amber/10 transition-colors text-[11px]">
          Retry now
        </button>
      </div>
      <div v-else-if="wsConnectionState === 'disconnected'"
        class="flex-shrink-0 flex items-center justify-between gap-3 px-4 py-2 text-xs font-medium"
        style="background:rgba(248,81,73,0.12);border-bottom:1px solid rgba(248,81,73,0.25);color:rgba(248,81,73,0.92)">
        <div class="flex items-center gap-2 min-w-0">
          <svg class="w-3.5 h-3.5 flex-shrink-0" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round"><circle cx="12" cy="12" r="10"/><line x1="12" y1="8" x2="12" y2="12"/><line x1="12" y1="16" x2="12.01" y2="16"/></svg>
          <div class="min-w-0">
            <span>Connection lost — real-time updates unavailable</span>
            <span v-if="wsLastError" class="block text-[10px] opacity-70 truncate">{{ wsLastError }}</span>
          </div>
        </div>
        <button @click="reloadPage()"
          class="flex-shrink-0 px-2 py-0.5 rounded border border-huginn-red/40 hover:bg-huginn-red/10 transition-colors text-[11px]">
          Reload Page
        </button>
      </div>
    </Transition>

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

        <!-- Right side of header -->
        <div class="ml-auto flex items-center gap-2 flex-shrink-0">

          <!-- Agents chip (space context) -->
          <button v-if="activeSpace"
            @click="rosterOpen = true"
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

          <!-- Agent picker dropdown (standalone session context) -->
          <div v-else-if="agentsList.length" class="relative flex-shrink-0">
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
      <div ref="messagesEl" class="flex-1 overflow-y-auto relative" @click="handleMessagesClick" @scroll="onMessagesScroll">

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

        <!-- Empty chat -->
        <div v-else-if="messages.length === 0 && !streaming" class="flex flex-col items-center justify-center h-full gap-3 pb-16">
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
            <div v-if="msg.dateLabel" class="flex items-center gap-3 my-4">
              <div class="flex-1 h-px bg-huginn-border/40" />
              <span class="text-[11px] text-huginn-muted/50 font-medium select-none">{{ msg.dateLabel }}</span>
              <div class="flex-1 h-px bg-huginn-border/40" />
            </div>

            <!-- Thread completion summary card (subtle system message) -->
            <div v-if="(msg as any).threadSummary"
              class="flex items-start gap-2 px-3 py-2 rounded-xl border border-huginn-border/40 bg-huginn-surface/20 mx-2 mt-4">
              <svg class="w-3 h-3 text-huginn-green/60 flex-shrink-0 mt-0.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
                <polyline points="20 6 9 17 4 12" />
              </svg>
              <div class="md-content text-xs text-huginn-muted/70 flex-1 min-w-0 leading-relaxed"
                v-html="renderWithMentions(msg.content)" />
            </div>

            <!-- User message (right-aligned bubble) -->
            <div v-else-if="msg.role === 'user'" class="flex justify-end" :class="msg.showHeader ? 'mt-4' : 'mt-1'">
              <div class="md-content max-w-[75%] px-4 py-3 rounded-2xl rounded-tr-sm text-sm text-huginn-text leading-relaxed break-words"
                style="background:rgba(88,166,255,0.12);border:1px solid rgba(88,166,255,0.22)"
                v-html="renderWithMentions(msg.content)" />
            </div>

            <!-- Assistant message (left-aligned) -->
            <div v-else-if="msg.role === 'assistant'" class="flex gap-3" :class="msg.showHeader ? 'mt-4' : 'mt-1'">
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
                  v-if="msg.showHeader && msg.agent"
                  :agent-name="msg.agent"
                  :created-at="msg.createdAt"
                />
                <!-- Message text -->
                <div v-if="msg.content" class="md-content text-sm text-huginn-text leading-relaxed break-words"
                  v-html="renderWithMentions(msg.content)" />
                <span v-if="msg.streaming && !activeToolCalls.length"
                  class="inline-block w-1.5 h-3.5 bg-huginn-blue ml-0.5 align-middle rounded-sm animate-pulse" />
                <!-- Follow-up thinking indicator: lead agent is preparing their synthesis -->
                <div v-if="(msg as any).followUpThinking && !(msg as any).content"
                  class="flex items-center gap-1.5 py-1">
                  <span class="w-1.5 h-1.5 rounded-full bg-huginn-blue animate-bounce" style="animation-delay:0ms" />
                  <span class="w-1.5 h-1.5 rounded-full bg-huginn-blue animate-bounce" style="animation-delay:150ms" />
                  <span class="w-1.5 h-1.5 rounded-full bg-huginn-blue animate-bounce" style="animation-delay:300ms" />
                </div>

                <!-- Delegated thread reply strips (Slack-style compact) -->
                <div v-if="msg.delegatedThreads?.length" class="mt-1.5 space-y-0.5">
                  <button
                    v-for="d in msg.delegatedThreads"
                    :key="d.threadId"
                    @click="openThreadDetail(d)"
                    class="group flex items-center gap-2 py-1 px-2 -ml-1 rounded-lg transition-all duration-150 hover:bg-huginn-surface/60"
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
                    <!-- Reply count label or "working…" indicator -->
                    <span class="text-xs font-medium" :style="`color:${agentColorMap[d.agentId] ?? 'rgba(88,166,255,0.8)'}`">
                      <template v-if="['running','thinking','queued'].includes(getThreadById(d.threadId)?.Status ?? '')">
                        working…
                      </template>
                      <template v-else>
                        {{ (d.replyCount ?? 1) === 1 ? '1 reply' : `${d.replyCount} replies` }}
                      </template>
                    </span>
                    <!-- Separator · agent name · status when done/error -->
                    <span class="text-[11px] text-huginn-muted/50">
                      · {{ d.agentId }}
                      <template v-if="getThreadById(d.threadId) && !['running','thinking','queued'].includes(getThreadById(d.threadId)!.Status)">
                        · {{ formatThreadStatus(getThreadById(d.threadId)!.Status) }}
                      </template>
                    </span>
                    <!-- Chevron on hover -->
                    <svg class="w-3 h-3 text-huginn-muted/30 opacity-0 group-hover:opacity-100 transition-opacity flex-shrink-0"
                      viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
                      <polyline points="9 18 15 12 9 6" />
                    </svg>
                  </button>
                </div>

                <!-- Tool call chip (completed, attached to this message).
                     Hidden while streaming so the running chip is the sole indicator
                     and we don't show duplicate done+running chips mid-response. -->
                <div v-if="msg.toolCalls?.length && !msg.streaming" class="mt-2">
                  <!-- Collapsed chip -->
                  <button @click="toggleMsgToolCalls(msg.id)"
                    class="flex items-center gap-2 px-3 py-1.5 rounded-xl border border-huginn-border hover:bg-huginn-surface/80 transition-colors duration-100">
                    <svg class="w-3.5 h-3.5 text-huginn-yellow flex-shrink-0" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
                      <path d="M14.7 6.3a1 1 0 000 1.4l1.6 1.6a1 1 0 001.4 0l3.77-3.77a6 6 0 01-7.94 7.94l-6.91 6.91a2.12 2.12 0 01-3-3l6.91-6.91a6 6 0 017.94-7.94l-3.76 3.76z" />
                    </svg>
                    <span class="text-xs text-huginn-text">{{ msg.toolCalls.length }} tool call{{ msg.toolCalls.length === 1 ? '' : 's' }}</span>
                    <span class="text-[11px] text-huginn-green">· done</span>
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
                        <span class="text-xs font-medium text-huginn-text flex-1">{{ tc.name }}</span>
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
              </div>
            </div>
          </template>

          <!-- Active (in-flight) tool calls — single chip -->
          <div v-if="activeToolCalls.length" class="ml-10">
            <div class="inline-flex items-center gap-2 px-3 py-1.5 rounded-xl border border-huginn-border bg-huginn-surface/50">
              <div class="flex gap-0.5 flex-shrink-0">
                <span class="w-1 h-1 rounded-full bg-huginn-yellow animate-bounce" style="animation-delay:0ms" />
                <span class="w-1 h-1 rounded-full bg-huginn-yellow animate-bounce" style="animation-delay:120ms" />
                <span class="w-1 h-1 rounded-full bg-huginn-yellow animate-bounce" style="animation-delay:240ms" />
              </div>
              <svg class="w-3.5 h-3.5 text-huginn-yellow flex-shrink-0" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
                <path d="M14.7 6.3a1 1 0 000 1.4l1.6 1.6a1 1 0 001.4 0l3.77-3.77a6 6 0 01-7.94 7.94l-6.91 6.91a2.12 2.12 0 01-3-3l6.91-6.91a6 6 0 017.94-7.94l-3.76 3.76z" />
              </svg>
              <span class="text-xs text-huginn-text">{{ activeToolCalls.length }} tool call{{ activeToolCalls.length === 1 ? '' : 's' }}</span>
              <span class="text-[11px] text-huginn-muted animate-pulse flex-shrink-0">· running</span>
            </div>
          </div>

          <!-- Streaming thinking indicator (before first token) -->
          <div v-if="streaming && messages.at(-1)?.role !== 'assistant'" class="flex gap-3">
            <div class="w-7 h-7 rounded-lg flex items-center justify-center flex-shrink-0"
              style="background:rgba(88,166,255,0.12);border:1px solid rgba(88,166,255,0.2)">
              <span class="text-huginn-blue text-xs font-bold">H</span>
            </div>
            <div class="flex items-center gap-1 py-2">
              <span class="w-1.5 h-1.5 rounded-full bg-huginn-muted/60 animate-bounce" style="animation-delay:0ms" />
              <span class="w-1.5 h-1.5 rounded-full bg-huginn-muted/60 animate-bounce" style="animation-delay:150ms" />
              <span class="w-1.5 h-1.5 rounded-full bg-huginn-muted/60 animate-bounce" style="animation-delay:300ms" />
            </div>
          </div>
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
          </div>
          <div class="flex gap-1.5 flex-shrink-0">
            <button
              data-testid="delegation-preview-allow"
              @click="wsRef && ackPreview(wsRef, preview, true)"
              class="px-2 py-1 text-[10px] font-medium rounded border border-huginn-green/30 text-huginn-green hover:bg-huginn-green/15 transition-colors"
            >Allow</button>
            <button
              data-testid="delegation-preview-deny"
              @click="wsRef && ackPreview(wsRef, preview, false)"
              class="px-2 py-1 text-[10px] font-medium rounded border border-huginn-red/30 text-huginn-red hover:bg-huginn-red/15 transition-colors"
            >Deny</button>
          </div>
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

      <!-- ── Input area ──────────────────────────────────────────── -->
      <div class="px-4 pb-4 flex-shrink-0">
        <ChatEditor
          ref="chatEditorRef"
          :disabled="streaming"
          :placeholder="activeSpace ? `Message ${activeSpace.name}...` : undefined"
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
      />
      </div><!-- end flex row -->
    </template>

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
import { ref, shallowRef, computed, nextTick, inject, watch, onMounted, onUnmounted } from 'vue'
import type { Ref } from 'vue'
import { useSpaceTimeline, type SpaceMessage } from '../composables/useSpaceTimeline'
// import { useRouter } from 'vue-router'
import { marked, Renderer } from 'marked'
import hljs from 'highlight.js'
import { ChatEditor } from '../components/ChatEditor'
import { ThreadPanel } from '../components/ThreadPanel'
import SwarmStatus from '../components/SwarmStatus.vue'
import ThreadDetail from '../components/ThreadDetail.vue'
import AgentRosterModal from '../components/AgentRosterModal.vue'
import AgentMessageHeader from '../components/AgentMessageHeader.vue'
import type { HuginnWS, WSMessage } from '../composables/useHuginnWS'
import { api, apiFetch } from '../composables/useApi'
import { useSessions, hydrationQueueOverflowed, type ToolCallRecord } from '../composables/useSessions'
import { useThreads } from '../composables/useThreads'
import { useThreadDetail } from '../composables/useThreadDetail'
import { useSpaces } from '../composables/useSpaces'
import { useSwarmStatus } from '../composables/useSwarmStatus'

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

// ── marked + highlight.js setup ──────────────────────────────────────
const renderer = new Renderer()
renderer.code = ({ text, lang }: { text: string; lang?: string }) => {
  const language = lang && hljs.getLanguage(lang) ? lang : 'plaintext'
  const highlighted = language === 'plaintext'
    ? hljs.highlightAuto(text).value
    : hljs.highlight(text, { language }).value
  const label = lang || ''
  return `<div class="code-block">
    <div class="code-header">
      <span class="code-lang">${label}</span>
      <button class="code-copy" onclick="navigator.clipboard.writeText(this.closest('.code-block').querySelector('code').innerText).then(()=>{this.textContent='copied';setTimeout(()=>this.textContent='copy',1500)})">copy</button>
    </div>
    <pre><code class="hljs language-${language}">${highlighted}</code></pre>
  </div>`
}
marked.use({ renderer, breaks: true, gfm: true })

function renderMarkdown(content: string): string {
  return marked.parse(content) as string
}

// renderWithMentions wraps @agent-name tokens in styled, hoverable spans.
// Agent names are resolved from agentsList so tooltip shows real model info.
//
// Safety: the lookbehind (?<![a-zA-Z0-9.]) ensures we don't match @domain.com
// inside email addresses like mailto:user@example.com (where @ is preceded by
// a word character). Only @mentions preceded by whitespace/punctuation match.
function renderWithMentions(content: string): string {
  const html = renderMarkdown(content)
  if (!html.includes('@')) return html
  return html.replace(/(?<![a-zA-Z0-9.])@([\w-]+)/g, (match, name: string) => {
    const agent = agentsList.value.find(a => a.name.toLowerCase() === name.toLowerCase())
    if (!agent) return match
    // Escape values used in HTML attributes to prevent injection.
    const safeName = name.replace(/[<>"'&]/g, '')
    const safeTooltip = `${agent.name} · ${agent.model}`
      .replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;')
    const safeColor = (agent.color ?? 'rgba(88,166,255,0.9)').replace(/[<>"']/g, '')
    return `<span class="agent-mention" data-agent="${safeName.toLowerCase()}" data-tooltip="${safeTooltip}" style="color:${safeColor}">@${safeName}</span>`
  })
}

const props = defineProps<{ sessionId?: string; spaceId?: string }>()

// const router  = useRouter()
const wsRef   = inject<Ref<HuginnWS | null>>('ws')!

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
  await scrollToBottom()

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

// ── WebSocket connection state (for the banner) ───────────────────────────────
const wsConnectionState = computed(() => wsRef.value?.connectionState?.value ?? 'connected')
const wsReconnectAttempts = computed(() => wsRef.value?.reconnectAttempts?.value ?? 0)
const wsMaxAttempts = computed(() => wsRef.value?.maxReconnectAttempts ?? 10)
const wsLastError = computed(() => wsRef.value?.lastError?.value ?? null)
const wsSecondsUntilRetry = computed(() => wsRef.value?.secondsUntilRetry?.value ?? 0)
function wsReconnectNow() { wsRef.value?.reconnectNow?.() }
function reloadPage() { window.location.reload() }

const { sessions, getMessages, fetchMessages, queueIfHydrating, formatSessionLabel, renameSession } = useSessions()
const { activeSpace } = useSpaces()

// ── Hydration overflow toast ──────────────────────────────────────────────────
// When the pre-hydration WS event queue overflows (> 500 events dropped while
// loading session history), we show a brief amber toast for 8 seconds so the
// user knows to refresh if data looks stale.
const hydrationOverflowToastVisible = ref(false)
let hydrationOverflowTimer: ReturnType<typeof setTimeout> | null = null

watch(hydrationQueueOverflowed, (overflowed) => {
  if (!overflowed) return
  hydrationOverflowToastVisible.value = true
  if (hydrationOverflowTimer) clearTimeout(hydrationOverflowTimer)
  hydrationOverflowTimer = setTimeout(() => {
    hydrationOverflowToastVisible.value = false
    hydrationOverflowTimer = null
  }, 8_000)
})

// ── Session-switch loading state ─────────────────────────────────────
const sessionSwitching = ref(false)

// ── In-chat search (Ctrl+F / Cmd+F) ─────────────────────────────────
const chatSearchOpen = ref(false)
const chatSearchQuery = ref('')
const chatSearchIndex = ref(0)
const chatSearchInputEl = ref<HTMLInputElement | null>(null)

const chatSearchMatches = computed((): string[] => {
  const q = chatSearchQuery.value.trim().toLowerCase()
  if (!q) return []
  return messages.value
    .filter(m => m.content?.toLowerCase().includes(q) && (m.role as string) !== 'tool_call' && (m.role as string) !== 'tool_result')
    .map(m => m.id)
})

function openChatSearch() {
  chatSearchOpen.value = true
  chatSearchIndex.value = 0
  nextTick(() => chatSearchInputEl.value?.focus())
}

function closeChatSearch() {
  chatSearchOpen.value = false
  chatSearchQuery.value = ''
  chatSearchIndex.value = 0
}

function nextChatSearchMatch() {
  if (!chatSearchMatches.value.length) return
  chatSearchIndex.value = (chatSearchIndex.value + 1) % chatSearchMatches.value.length
  scrollToSearchMatch()
}

function prevChatSearchMatch() {
  if (!chatSearchMatches.value.length) return
  chatSearchIndex.value = (chatSearchIndex.value - 1 + chatSearchMatches.value.length) % chatSearchMatches.value.length
  scrollToSearchMatch()
}

function scrollToSearchMatch() {
  const id = chatSearchMatches.value[chatSearchIndex.value]
  if (!id) return
  const el = messagesEl.value?.querySelector(`[data-msg-id="${id}"]`)
  el?.scrollIntoView({ behavior: 'smooth', block: 'center' })
}

// ── Unread jump pill ─────────────────────────────────────────────────
const lastSeenMessageCount = ref<Record<string, number>>({})
const atBottom = ref(true)

const unreadCount = computed(() => {
  if (!props.sessionId) return 0
  const seen = lastSeenMessageCount.value[props.sessionId] ?? 0
  const total = messages.value.filter(m => m.role === 'assistant' || m.role === 'user').length
  return Math.max(0, total - seen)
})

function onMessagesScroll() {
  const el = messagesEl.value
  if (!el) return
  const threshold = 80
  atBottom.value = el.scrollHeight - el.scrollTop - el.clientHeight < threshold
  if (atBottom.value && props.sessionId) {
    markCurrentSessionSeen()
  }
}

function markCurrentSessionSeen() {
  if (!props.sessionId) return
  const count = messages.value.filter(m => m.role === 'assistant' || m.role === 'user').length
  lastSeenMessageCount.value = { ...lastSeenMessageCount.value, [props.sessionId]: count }
}

function jumpToUnread() {
  if (!props.sessionId) return
  const seen = lastSeenMessageCount.value[props.sessionId] ?? 0
  const relevant = messages.value.filter(m => m.role === 'assistant' || m.role === 'user')
  const firstUnread = relevant[seen]
  if (firstUnread) {
    const el = messagesEl.value?.querySelector(`[data-msg-id="${firstUnread.id}"]`)
    el?.scrollIntoView({ behavior: 'smooth', block: 'start' })
  } else {
    messagesEl.value?.scrollTo({ top: messagesEl.value.scrollHeight, behavior: 'smooth' })
  }
  markCurrentSessionSeen()
}

// ── Header inline rename ─────────────────────────────────────────────
const headerEditing   = ref(false)
const headerEditValue = ref('')
const headerInputEl   = ref<HTMLInputElement | null>(null)

async function startHeaderEdit() {
  const s = sessions.value.find(s => s.id === props.sessionId)
  headerEditValue.value = s?.title ?? ''
  headerEditing.value   = true
  await nextTick()
  headerInputEl.value?.focus()
  headerInputEl.value?.select()
}

function commitHeaderEdit() {
  if (!headerEditing.value) return
  headerEditing.value = false
  if (props.sessionId) renameSession(props.sessionId, headerEditValue.value.trim())
}

function cancelHeaderEdit() {
  headerEditing.value = false
}

// ── Local UI state ───────────────────────────────────────────────────
interface ActiveToolCall { id: string; name: string; args: Record<string, unknown> }

const activeToolCalls   = ref<ActiveToolCall[]>([])
const expandedToolCalls = ref<Set<string>>(new Set())
const expandedMsgCalls  = ref<Set<string>>(new Set())
const streaming         = ref(false)
const currentRunId      = ref('')   // matches run_id echoed by server; guards against stale done events
const notifyStreaming    = ref(false)

// ── Streaming watchdog ────────────────────────────────────────────────────────
// If no token/done/error arrives within 60s of starting a run, reset streaming
// so the user is not permanently locked out of sending. Handles server crashes,
// network partitions, and any other scenario where done/error never arrives.
const STREAMING_WATCHDOG_MS = 60_000
let streamingWatchdog: ReturnType<typeof setTimeout> | null = null
function startStreamingWatchdog() {
  if (streamingWatchdog !== null) { clearTimeout(streamingWatchdog); streamingWatchdog = null }
  streamingWatchdog = setTimeout(() => {
    if (streaming.value) {
      console.warn('[chat] streaming watchdog: no activity for 60s — resetting streaming state')
      streaming.value = false
      activeToolCalls.value = []
    }
    streamingWatchdog = null
  }, STREAMING_WATCHDOG_MS)
}
function clearStreamingWatchdog() {
  if (streamingWatchdog !== null) { clearTimeout(streamingWatchdog); streamingWatchdog = null }
}
const messagesEl        = ref<HTMLElement>()
const pendingPermission = ref<WSMessage | null>(null)
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

// ── Agent state ──────────────────────────────────────────────────────
const agentsList        = ref<Agent[]>([])
const selectedAgentName = ref('')
const agentDropdownOpen = ref(false)
const rosterOpen        = ref(false)

// ── Swarm status panel ────────────────────────────────────────────────
const { swarmState } = useSwarmStatus()
const swarmPanelDismissed = ref(false)
// Reset dismiss when a new swarm session starts
watch(() => swarmState.value?.sessionId, (id) => {
  if (id) swarmPanelDismissed.value = false
})
const agentProfile      = ref<Agent | null>(null) // agent shown in read-only profile modal

// ── Thread panel state ────────────────────────────────────────────────
const { getSessionThreads, getActiveThreadCount, loadThreads, wireWS: wireThreadWS, getSessionPreviews, ackPreview, threadsError } = useThreads()
const threadPanelOpen   = ref(false)
const threadPanelPinned = ref(false) // true = don't auto-close when threads finish

// ── Thread detail (per-message thread slide-in) ───────────────────────
const threadDetail = useThreadDetail()
const threadDetailRef = ref<InstanceType<typeof ThreadDetail> | null>(null)
// Track live thread info for the currently-open ThreadDetail
const openThreadLiveId = ref<string>('')

function openThreadDetail(d: { threadId: string; agentId: string; msgId?: string }) {
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

function closeThreadDetail() {
  threadDetail.close()
  openThreadLiveId.value = ''
}

// hydrateThreadBadges restores thread reply badges from the DB after a page
// refresh. Calls GET /api/v1/containers/{id}/threads which returns root messages
// that have at least one reply (thread_reply_count > 0), then attaches
// delegatedThreads to the matching message in the UI.
let hydratingBadges = false
async function hydrateThreadBadges(sessionId: string) {
  if (!sessionId || hydratingBadges) return
  hydratingBadges = true
  try {
    type ContainerThreadRow = { id: string; agent: string; thread_reply_count: number }
    const rows = await apiFetch<ContainerThreadRow[]>(`/api/v1/containers/${sessionId}/threads`)
    if (!Array.isArray(rows) || rows.length === 0) return
    const msgs = getMessages(sessionId)
    for (const row of rows) {
      const msg = msgs.find(m => m.id === row.id)
      if (!msg) continue
      // Only hydrate if the badge isn't already present (WS may have set it live)
      if (!(msg as any).delegatedThreads?.length) {
        // threadId = row.id (message ID) is used for badge identity; msgId is
        // the parent message ID for fetching thread messages via the API.
        ;(msg as any).delegatedThreads = [{
          threadId: row.id,
          agentId: row.agent,
          msgId: row.id,
          done: true,
          replyCount: row.thread_reply_count || 1,
        }]
      } else {
        // Update reply count on existing badge in case it grew since last WS event
        const existing = (msg as any).delegatedThreads[0]
        if (existing && row.thread_reply_count > 0) {
          existing.replyCount = row.thread_reply_count
        }
      }
    }
  } catch {
    // Non-fatal: badges will be missing but session is still usable
  } finally {
    hydratingBadges = false
  }
}

// ── Computed ─────────────────────────────────────────────────────────

// Adapt SpaceMessage[] (which uses `ts` for timestamp) to the shape the
// existing enrichedMessages / template expect (which uses `createdAt`).
function adaptSpaceMessages(msgs: SpaceMessage[]) {
  return msgs.map(m => ({
    id: m.id,
    role: m.role as 'user' | 'assistant',
    content: m.content,
    agent: m.agent || undefined,
    createdAt: m.ts,
    streaming: false,
    toolCalls: [] as import('../composables/useSessions').ToolCallRecord[],
  }))
}

const messages = computed(() => {
  if (props.spaceId) {
    const spMsgs = currentSpaceTimeline.value?.getState().messages ?? []
    return adaptSpaceMessages(spMsgs) as ReturnType<typeof getMessages>
  }
  return props.sessionId ? getMessages(props.sessionId) : []
})

// enrichedMessages adds two display hints to each message:
//   showHeader  — false when this is a continuation from same agent (collapses avatar + name)
//   dateLabel   — set to "Today" / "Yesterday" / "Mon, Mar 15" when a date boundary is crossed
type EnrichedMessage = (typeof messages.value[number]) & {
  showHeader: boolean
  dateLabel?: string
}

function dateLabelFor(ts: string | undefined): string {
  if (!ts) return ''
  const d = new Date(ts)
  if (isNaN(d.getTime())) return ''
  const now = new Date()
  const today = new Date(now.getFullYear(), now.getMonth(), now.getDate())
  const msgDay = new Date(d.getFullYear(), d.getMonth(), d.getDate())
  const diffDays = Math.round((today.getTime() - msgDay.getTime()) / 86400000)
  if (diffDays === 0) return 'Today'
  if (diffDays === 1) return 'Yesterday'
  return d.toLocaleDateString(undefined, { weekday: 'short', month: 'short', day: 'numeric' })
}

function isSameDay(a: string | undefined, b: string | undefined): boolean {
  if (!a || !b) return false
  const da = new Date(a), db = new Date(b)
  return da.getFullYear() === db.getFullYear() &&
    da.getMonth() === db.getMonth() &&
    da.getDate() === db.getDate()
}

const enrichedMessages = computed((): EnrichedMessage[] => {
  const result: EnrichedMessage[] = []
  const msgs = messages.value
  for (let i = 0; i < msgs.length; i++) {
    const msg = msgs[i]!
    const prev = result[i - 1]

    // Date divider: show when this message is on a different day from the previous
    const ts = (msg as any).createdAt as string | undefined
    const prevTs = prev ? (prev as any).createdAt as string | undefined : undefined
    const dateLabel = (i === 0 || !isSameDay(ts, prevTs)) ? dateLabelFor(ts) : undefined

    // Header suppression: hide avatar+name for continuations from same agent
    // A message is a "continuation" when all of:
    //   1. Same role as previous
    //   2. Same agent name (assistant) or both user messages
    //   3. No date boundary between them
    //   4. Previous message is not a threadSummary separator
    let showHeader = true
    if (prev && !dateLabel) {
      const sameRole = msg.role === prev.role
      const prevIsThreadSummary = !!(prev as any).threadSummary
      const currIsThreadSummary = !!(msg as any).threadSummary
      if (sameRole && !prevIsThreadSummary && !currIsThreadSummary) {
        if (msg.role === 'user') {
          showHeader = false
        } else if (msg.role === 'assistant') {
          const sameAgent = (msg.agent || '') === (prev.agent || '')
          if (sameAgent) showHeader = false
        }
      }
    }

    result.push({ ...msg, showHeader, dateLabel } as EnrichedMessage)
  }
  return result
})

const sessionLabel = computed(() => {
  const s = sessions.value.find(s => s.id === props.sessionId)
  return s ? formatSessionLabel(s) : (props.sessionId?.slice(0, 8) ?? '')
})

const selectedAgent = computed(() =>
  agentsList.value.find(a => a.name === selectedAgentName.value) ?? null
)

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

// In a space context, the display agent is the space's lead agent (for avatar, icon, etc.)
// When not in a space, fall back to the picker's selectedAgent.
const displayAgent = computed(() =>
  (activeSpace.value ? spaceAgents.value[0] : null) ?? selectedAgent.value ?? null
)

const sessionThreads = computed(() =>
  props.sessionId ? getSessionThreads(props.sessionId) : []
)

const activeThreadCount = computed(() =>
  props.sessionId ? getActiveThreadCount(props.sessionId) : 0
)

// Pending delegation previews for this session (shown as approval banners).
const sessionPreviews = computed(() =>
  props.sessionId ? getSessionPreviews(props.sessionId) : []
)

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

const spaceAgents = computed(() => {
  if (!activeSpace.value) return []
  const names = [activeSpace.value.leadAgent, ...activeSpace.value.memberAgents.filter(m => m !== activeSpace.value!.leadAgent)]
  return names.map(n => agentsList.value.find(a => a.name === n)).filter((a): a is Agent => !!a)
})

const spaceAgentPreviews = computed(() => spaceAgents.value.slice(0, 3))

// Auto-show panel when threads appear; auto-hide 4s after all finish (unless pinned)
watch(activeThreadCount, (count) => {
  if (count > 0) {
    threadPanelOpen.value = true
  } else if (!threadPanelPinned.value && sessionThreads.value.length > 0) {
    setTimeout(() => {
      if (getActiveThreadCount(props.sessionId ?? '') === 0 && !threadPanelPinned.value) {
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

function toggleToolCall(tc: ToolCallRecord) {
  if (expandedToolCalls.value.has(tc.id)) expandedToolCalls.value.delete(tc.id)
  else expandedToolCalls.value.add(tc.id)
}

function toggleMsgToolCalls(msgId: string) {
  if (expandedMsgCalls.value.has(msgId)) expandedMsgCalls.value.delete(msgId)
  else expandedMsgCalls.value.add(msgId)
}

// ── Thread helpers ────────────────────────────────────────────────────
function getThreadById(threadId: string) {
  return sessionThreads.value.find(t => t.ID === threadId)
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
const chatEditorRef = ref<{ focus: () => void } | null>(null)

async function handleEditorSend(markdown: string) {
  const ws = wsRef.value
  if (!ws || streaming.value) return

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
      } catch {
        return
      }
    }

    const runId = `${Date.now()}-${Math.random().toString(36).slice(2, 9)}`
    currentRunId.value = runId
    streaming.value = true
    startStreamingWatchdog()

    // Optimistic user message into the space timeline.
    tl.getState().messages.push({
      id: `u-${Date.now()}`,
      session_id: targetSessionId,
      seq: -1,
      ts: new Date().toISOString(),
      role: 'user',
      content: markdown,
      agent: '',
    })

    ws.send({ type: 'chat', content: markdown, session_id: targetSessionId, run_id: runId })
    scrollToBottom()
    return
  }

  // ── Session mode ────────────────────────────────────────────────────
  if (!props.sessionId) return

  // Auto-select default agent on first send if none chosen yet
  if (!selectedAgentName.value && agentsList.value.length > 0) {
    const def = agentsList.value.find(a => a.is_default) ?? agentsList.value[0]
    if (def) selectAgent(def.name)
  }

  const runId = `${Date.now()}-${Math.random().toString(36).slice(2, 9)}`
  currentRunId.value = runId
  streaming.value = true
  startStreamingWatchdog()
  pendingToolResults.value = [] // reset stale buffered prefetch results from prior response
  const msgs = getMessages(props.sessionId)
  msgs.push({ id: `u-${Date.now()}`, role: 'user', content: markdown })
  msgs.push({ id: `h-${Date.now()}`, role: 'assistant', content: '', streaming: true, agent: selectedAgentName.value || undefined, createdAt: new Date().toISOString() })

  ws.send({ type: 'chat', content: markdown, session_id: props.sessionId, run_id: runId })
  scrollToBottom()
}


function cancelThread(threadId: string) {
  const ws = wsRef.value
  if (!ws || !props.sessionId) return
  ws.send({ type: 'thread_cancel', payload: { thread_id: threadId }, session_id: props.sessionId })
}

function injectThread(threadId: string, content: string) {
  const ws = wsRef.value
  if (!ws || !props.sessionId) return
  ws.send({ type: 'thread_inject', payload: { thread_id: threadId, content }, session_id: props.sessionId })
}

function handleThreadDetailInject(threadId: string, content: string) {
  injectThread(threadId, content)
  // Ack/error will be handled by WS event handlers below
}


function approvePermission(approved: boolean) {
  const ws = wsRef.value
  if (!ws || !pendingPermission.value) return
  ws.send({
    type: 'permission_response',
    payload: { id: (pendingPermission.value.payload as Record<string, string>)?.id, approved },
  })
  pendingPermission.value = null
}

async function fetchStatus() {
  try {
    const s = await api.runtime.status()
    runtimeState.value = s.state
  } catch { /* ignore */ }
}

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

  registerWS(ws, 'token', (msg: WSMessage) => {
    // Route by the message's own session_id, not props.sessionId, so tokens
    // are appended to the correct session's message array even during session
    // switches (props.sessionId can change between WS registration and delivery).
    const sid = msg.session_id || props.sessionId
    if (!sid || sid !== props.sessionId) return // ignore tokens for other sessions
    startStreamingWatchdog() // reset watchdog on each token to detect true inactivity
    const apply = () => {
      // Flush buffered prefetch tool results now that the assistant message exists.
      flushPendingToolResults(sid)
      const last = getMessages(sid).at(-1)
      if (last?.streaming) { last.content += msg.content ?? ''; scrollToBottom() }
    }
    if (queueIfHydrating(sid, apply)) return
    apply()
  })

registerWS(ws, 'tool_call', (msg: WSMessage) => {
    const p = msg.payload as Record<string, unknown>
    activeToolCalls.value.push({
      id: (p?.id as string) ?? Date.now().toString(),
      name: (p?.tool as string) ?? '',
      args: (p?.args as Record<string, unknown>) ?? {},
    })
    scrollToBottom()
  })

registerWS(ws, 'tool_result', (msg: WSMessage) => {
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
    pendingPermission.value = msg
    scrollToBottom()
  })

registerWS(ws, 'done', (msg: WSMessage) => {
    // Ignore stale done events from previous chat runs (e.g. buffered in the WS connection).
    // run_id was introduced alongside this guard; old messages without run_id are also ignored.
    if (!msg.run_id || msg.run_id !== currentRunId.value) {
      console.debug('[done] ignoring stale done, run_id=', msg.run_id, 'expected=', currentRunId.value)
      return
    }
    clearStreamingWatchdog()
    streaming.value = false
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
      const last = getMessages(props.sessionId).at(-1)
      if (last) last.streaming = false
    }
    scrollToBottom()
    fetchStatus()
  })

registerWS(ws, 'error', (msg: WSMessage) => {
    // Allow errors without run_id (e.g. "orchestrator not initialized" sent before any run_id is
    // established). Errors that DO carry a run_id must match the current run to avoid stale errors.
    if (msg.run_id && msg.run_id !== currentRunId.value) return
    clearStreamingWatchdog()
    streaming.value = false
    activeToolCalls.value = []
    if (props.sessionId) {
      const last = getMessages(props.sessionId).at(-1)
      if (last?.streaming) { last.content += `\n\nerror: ${msg.content}`; last.streaming = false }
    }
    scrollToBottom()
  })

registerWS(ws, 'primary_agent_changed', (msg: WSMessage) => {
    const name = (msg.payload as Record<string, string>)?.agent
    if (name && msg.session_id === props.sessionId) {
      selectedAgentName.value = name
    }
  })

  // Attach spawned threads to the last assistant message (Slack-style thread anchoring)
registerWS(ws, 'thread_started', (msg: WSMessage) => {
    const p = msg.payload as Record<string, string>
    if (!p.thread_id || !props.sessionId) return
    const msgs = getMessages(props.sessionId)
    const lastAssistant = [...msgs].reverse().find(m => m.role === 'assistant')
    if (lastAssistant) {
      if (!lastAssistant.delegatedThreads) lastAssistant.delegatedThreads = []
      const already = lastAssistant.delegatedThreads.some(d => d.threadId === p.thread_id)
      if (!already) {
        lastAssistant.delegatedThreads.push({
          threadId: p.thread_id,
          agentId: p.agent_id || '',
          // Use parent_message_id from WS payload; fall back to lastAssistant.id
          // for @mention-initiated threads where parent_message_id may be absent.
          msgId: p.parent_message_id || (lastAssistant as any).id || '',
          replyCount: 0,
        })
      }
    }
  })

  // thread_help is only broadcast when AutoHelpResolver fails (fallback for human input).
  // The thread card's "Waiting for input" form handles this case via thread_inject.
registerWS(ws, 'thread_help', (_msg: WSMessage) => {
    // Thread status is updated by useThreads wireWS handler (sets to 'blocked').
    // No relay needed — AutoHelpResolver handles this automatically; this is the fallback.
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
    if (!props.sessionId || msg.session_id !== props.sessionId) return
    // No-op for main chat display intentionally.
  })

// follow_up_start: lead agent is about to synthesize. Show a "thinking" bubble
// immediately so the user knows Tom is picking up where Sam left off.
registerWS(ws, 'follow_up_start', (msg: WSMessage) => {
    if (!props.sessionId || msg.session_id !== props.sessionId) return
    const p = msg.payload as Record<string, unknown>
    const agentName = p?.agent as string | undefined
    const msgs = getMessages(props.sessionId)
    // Only add if there isn't already a follow-up streaming bubble
    const alreadyExists = msgs.some(m => (m as any).followUpStreaming)
    if (!alreadyExists) {
      msgs.push({
        id: `fup-stream-${Date.now()}`,
        role: 'assistant',
        content: '',
        agent: agentName || 'Agent',
        createdAt: new Date().toISOString(),
        followUpStreaming: true,
        followUpThinking: true,
      } as any)
      scrollToBottom()
    }
  })

// follow_up_token: streaming token from the lead agent's follow-up synthesis.
// Builds a live streaming bubble in the main chat so the user sees Tom "typing".
registerWS(ws, 'follow_up_token', (msg: WSMessage) => {
    if (!props.sessionId || msg.session_id !== props.sessionId) return
    const p = msg.payload as Record<string, unknown>
    const agentName = p?.agent as string | undefined
    const token = p?.token as string | undefined
    if (!token) return
    const msgs = getMessages(props.sessionId)
    // Find the existing follow-up streaming bubble or create one
    const existing = [...msgs].reverse().find(m => (m as any).followUpStreaming)
    if (existing) {
      existing.content += token
      ;(existing as any).followUpThinking = false // first token: stop thinking dots
    } else {
      msgs.push({
        id: `fup-stream-${Date.now()}`,
        role: 'assistant',
        content: token,
        agent: agentName || 'Agent',
        followUpStreaming: true,
      } as any)
    }
    scrollToBottom()
  })

// agent_follow_up: final persisted follow-up reply from the lead agent.
// Replaces the streaming bubble with the complete content.
registerWS(ws, 'agent_follow_up', (msg: WSMessage) => {
    if (!props.sessionId || msg.session_id !== props.sessionId) return
    const p = msg.payload as Record<string, unknown>
    const agentName = p?.agent as string | undefined
    const content = p?.content as string | undefined
    if (!content) return
    const msgs = getMessages(props.sessionId)
    // Remove the streaming bubble if it exists
    const streamIdx = msgs.findIndex(m => (m as any).followUpStreaming)
    if (streamIdx >= 0) msgs.splice(streamIdx, 1)
    // Add the final message
    msgs.push({
      id: `fup-${Date.now()}`,
      role: 'assistant',
      content,
      agent: agentName || 'Agent',
    } as any)
    scrollToBottom()
  })

// follow_up_cancelled: lead agent failed to synthesize (session busy or error).
// Remove the thinking bubble so the UI doesn't hang indefinitely.
registerWS(ws, 'follow_up_cancelled', (msg: WSMessage) => {
    if (!props.sessionId || msg.session_id !== props.sessionId) return
    const msgs = getMessages(props.sessionId)
    // Remove the thinking bubble if it exists
    const idx = msgs.findIndex(m => (m as any).followUpStreaming)
    if (idx >= 0) msgs.splice(idx, 1)
  })

registerWS(ws, 'thread_inject_ack', (_msg: WSMessage) => {
    threadDetailRef.value?.onInjectAck()
  })

registerWS(ws, 'thread_inject_error', (_msg: WSMessage) => {
    threadDetailRef.value?.onInjectError()
  })

  // Phase D: thread_done — update badge status on the parent message.
  // Do NOT add a system message to the main channel; the thread panel is the
  // correct place for completion details.
registerWS(ws, 'thread_done', (msg: WSMessage) => {
    if (!props.sessionId || msg.session_id !== props.sessionId) return
    const p = msg.payload as Record<string, unknown>
    const threadId = p?.thread_id as string | undefined
    if (!threadId) return
    // Mark any delegatedThread entry for this thread as done so the badge
    // reflects the final status without requiring a page refresh.
    const replyCount = p?.reply_count as number | undefined
    const msgs = getMessages(props.sessionId)
    for (const m of msgs) {
      const dt = (m as any).delegatedThreads as Array<{ threadId: string; agentId: string; msgId?: string; done?: boolean; replyCount?: number }> | undefined
      if (dt) {
        const entry = dt.find(d => d.threadId === threadId)
        if (entry) {
          entry.done = true
          // Update reply count from thread_done payload if provided, otherwise use at least 1
          if (replyCount != null && replyCount > 0) {
            entry.replyCount = replyCount
          } else if (!entry.replyCount) {
            entry.replyCount = 1
          }
        }
      }
    }
    // Remove any stale streaming tool calls scoped to the completed thread's
    // agent so they don't linger in the active tool call list.
    const agentId = p?.agent_id as string | undefined
    if (agentId) {
      activeToolCalls.value = activeToolCalls.value.filter(tc => (tc as any).agent !== agentId)
    }
  })

  // Wire thread events via useThreads composable
  wireThreadWS(ws, () => props.sessionId ?? '')
}, { immediate: true })

// Clean up all WS event handlers when component unmounts to prevent
// duplicate handlers accumulating across route changes.
onUnmounted(() => {
  clearStreamingWatchdog()
  if (intersectionObs) { intersectionObs.disconnect(); intersectionObs = null }
  wsCleanupFns.forEach(fn => fn())
  wsCleanupFns.length = 0
  document.removeEventListener('keydown', handleGlobalKeydown)
})

// Reset state and sync agent when switching sessions
watch(() => props.sessionId, async () => {
  clearStreamingWatchdog()
  streaming.value = false
  currentRunId.value = ''  // prevent late done from old session matching new session's run
  notifyStreaming.value = false
  activeToolCalls.value = []
  pendingPermission.value = null
  agentDropdownOpen.value = false
  hydratingBadges = false  // reset so new session can hydrate
  syncSessionAgent()
  fetchStatus()
  nextTick(() => chatEditorRef.value?.focus())
  // Load existing threads and message history for this session
  if (props.sessionId) {
    loadThreads(props.sessionId)
    // Only show skeleton if the session has no cached messages yet
    const alreadyCached = getMessages(props.sessionId).length > 0
    if (!alreadyCached) sessionSwitching.value = true
    await fetchMessages(props.sessionId)
    sessionSwitching.value = false
    hydrateThreadBadges(props.sessionId)
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
  if ((e.ctrlKey || e.metaKey) && e.key === 'f' && props.sessionId) {
    e.preventDefault()
    if (chatSearchOpen.value) {
      closeChatSearch()
    } else {
      openChatSearch()
    }
  }
}

onMounted(async () => {
  await loadAgents()
  syncSessionAgent()
  fetchStatus()
  if (props.sessionId) {
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
</style>
