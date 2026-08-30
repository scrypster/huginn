<template>
  <div class="flex flex-col h-full">

    <!-- ── Grid view (no workflow selected) ── -->
    <Transition name="page" mode="out-in">
      <div v-if="!selectedId" key="grid" class="flex flex-col h-full relative"
          data-testid="workflow-drop-surface"
          @dragenter.prevent="onFileDragEnter"
          @dragover.prevent="onFileDragOver"
          @dragleave="onFileDragLeave"
          @drop.prevent="onFileDrop">
        <div v-if="fileDropActive" data-testid="workflow-drop-overlay"
          class="absolute inset-0 z-20 flex items-center justify-center pointer-events-none"
          style="background:rgba(88,166,255,0.08);border:2px dashed rgba(88,166,255,0.45)">
          <p class="text-sm font-medium text-huginn-blue">Drop a .yaml or .json workflow</p>
        </div>
        <div v-if="dropMsg" class="px-6 pt-3">
          <div class="px-4 py-2.5 rounded-xl border text-xs"
            :class="dropError ? 'border-huginn-red/40 text-huginn-red bg-huginn-red/8' : 'border-huginn-green/40 text-huginn-green bg-huginn-green/8'">
            {{ dropMsg }}
          </div>
        </div>

        <!-- Top bar -->
        <div class="flex items-center gap-3 px-6 h-14 border-b border-huginn-border flex-shrink-0">
          <h1 class="text-sm font-semibold text-huginn-text">Workflows</h1>
          <div class="relative flex-1 max-w-xs">
            <svg class="absolute left-2.5 top-1/2 -translate-y-1/2 w-3.5 h-3.5 text-huginn-muted pointer-events-none"
              viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
              <circle cx="11" cy="11" r="8"/><path d="m21 21-4.35-4.35"/>
            </svg>
            <input v-model="search" placeholder="Search workflows…"
              class="w-full bg-huginn-bg border border-huginn-border rounded-lg pl-8 pr-3 py-1.5 text-xs text-huginn-text placeholder-huginn-muted/60 focus:outline-none focus:border-huginn-blue/60 transition-colors"/>
          </div>
          <div class="flex-1" />
          <button @click="showCreate = true"
            data-testid="new-workflow-btn"
            class="flex items-center gap-1.5 px-3 py-1.5 bg-huginn-blue text-white text-xs font-medium rounded-lg hover:bg-huginn-blue/90 active:scale-95 transition-all duration-150">
            <svg class="w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5">
              <line x1="12" y1="5" x2="12" y2="19"/><line x1="5" y1="12" x2="19" y2="12"/>
            </svg>
            New Workflow
          </button>
        </div>

        <!-- Card grid -->
        <div class="flex-1 overflow-y-auto p-6" data-testid="workflow-list">
          <div v-if="loading" class="flex items-center justify-center py-20">
            <div class="w-5 h-5 border border-huginn-border border-t-huginn-blue rounded-full animate-spin" />
          </div>
          <div v-else-if="!filteredWorkflows.length" class="flex flex-col items-center justify-center py-24 gap-4">
            <svg class="w-12 h-12 text-huginn-muted/30" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1">
              <path d="M13 2L3 14h9l-1 8 10-12h-9l1-8z"/>
            </svg>
            <p class="text-huginn-muted text-sm">{{ search ? 'No workflows match your search' : 'No workflows yet — drop a .yaml or .json file here' }}</p>
            <button v-if="!search" @click="showCreate = true"
              class="text-xs text-huginn-blue hover:text-huginn-blue/80 transition-colors">
              Create your first workflow →
            </button>
          </div>
          <div v-else class="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-4">
            <div v-for="wf in filteredWorkflows" :key="wf.id"
              @click="openWorkflow(wf)"
              data-testid="workflow-item"
              class="group relative flex flex-col bg-huginn-surface border border-huginn-border rounded-xl p-4 cursor-pointer transition-all duration-200 hover:border-huginn-blue/40 hover:shadow-lg hover:shadow-huginn-blue/5 hover:-translate-y-0.5">
              <div class="absolute top-4 right-4">
                <div class="w-2 h-2 rounded-full"
                  :class="wf.enabled ? 'bg-huginn-green' : 'bg-huginn-muted/40'"
                  :style="wf.enabled ? 'box-shadow:0 0 6px rgba(63,185,80,0.5)' : ''"/>
              </div>
              <h3 class="text-sm font-semibold text-huginn-text pr-6 mb-1 truncate group-hover:text-huginn-blue transition-colors duration-200">
                {{ wf.name }}
              </h3>
              <p v-if="wf.description" class="text-xs text-huginn-muted mb-3 line-clamp-2 leading-relaxed">
                {{ wf.description }}
              </p>
              <div class="flex items-center gap-2 mt-auto flex-wrap">
                <span class="text-[10px] font-mono text-huginn-muted/70 bg-huginn-bg px-1.5 py-0.5 rounded">
                  {{ wf.schedule || 'manual' }}
                </span>
                <span class="text-[10px] text-huginn-muted/60">
                  {{ wf.steps?.length ?? 0 }} step{{ (wf.steps?.length ?? 0) !== 1 ? 's' : '' }}
                </span>
                <span v-for="tag in (wf.tags || []).slice(0,2)" :key="tag"
                  class="text-[10px] px-1.5 py-0.5 rounded bg-huginn-blue/10 text-huginn-blue/80">
                  {{ tag }}
                </span>
              </div>
            </div>
          </div>
        </div>
      </div>

      <!-- ── Editor view (workflow selected) ── -->
      <div v-else key="editor" class="flex flex-col h-full">

        <!-- Top bar -->
        <div class="flex items-center gap-3 px-4 h-14 border-b border-huginn-border flex-shrink-0">
          <button @click="closeWorkflow"
            class="flex items-center gap-1.5 text-xs text-huginn-muted hover:text-huginn-text transition-colors">
            <svg class="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
              <path d="M19 12H5M12 5l-7 7 7 7"/>
            </svg>
            Workflows
          </button>
          <span class="text-huginn-border/80">›</span>
          <span class="text-sm font-medium text-huginn-text truncate flex-1">{{ editForm.name || 'Untitled' }}</span>

          <label class="flex items-center gap-2 cursor-pointer">
            <span class="text-xs text-huginn-muted">{{ editForm.enabled ? 'Enabled' : 'Disabled' }}</span>
            <button @click="editForm.enabled = !editForm.enabled"
              class="relative w-8 h-4 rounded-full transition-colors duration-200"
              :class="editForm.enabled ? 'bg-huginn-green' : 'bg-huginn-muted/30'">
              <div class="absolute top-0.5 w-3 h-3 rounded-full bg-white shadow transition-transform duration-200"
                :class="editForm.enabled ? 'translate-x-4' : 'translate-x-0.5'"/>
            </button>
          </label>

          <button @click="triggerRun" :disabled="running"
            class="flex items-center gap-1.5 px-3 py-1.5 text-xs font-medium rounded-lg transition-all duration-150 border"
            :class="running ? 'border-huginn-border text-huginn-muted cursor-not-allowed' : 'border-huginn-blue/60 text-huginn-blue hover:bg-huginn-blue/10 active:scale-95'">
            <svg class="w-3.5 h-3.5" :class="running ? 'animate-spin' : ''" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
              <polygon v-if="!running" points="5 3 19 12 5 21 5 3"/>
              <path v-else d="M12 2v4M12 18v4M4.93 4.93l2.83 2.83M16.24 16.24l2.83 2.83M2 12h4M18 12h4M4.93 19.07l2.83-2.83M16.24 7.76l2.83-2.83"/>
            </svg>
            {{ running ? 'Running…' : 'Run Now' }}
          </button>

          <!-- Cancel button — only shown while a run is in progress -->
          <button v-if="running" @click="cancelRun" :disabled="cancelling"
            class="flex items-center gap-1.5 px-3 py-1.5 text-xs font-medium rounded-lg transition-all duration-150 border"
            :class="cancelling ? 'border-huginn-border text-huginn-muted cursor-not-allowed' : 'border-red-500/60 text-red-400 hover:bg-red-500/10 active:scale-95'">
            <svg class="w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
              <rect x="6" y="6" width="12" height="12" rx="1"/>
            </svg>
            {{ cancelling ? 'Cancelling…' : 'Cancel' }}
          </button>

          <button @click="showHistory = true"
            class="flex items-center gap-1.5 px-3 py-1.5 text-xs text-huginn-muted hover:text-huginn-text border border-huginn-border rounded-lg hover:border-huginn-border/80 transition-all duration-150">
            <svg class="w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
              <circle cx="12" cy="12" r="10"/><path d="M12 6v6l4 2"/>
            </svg>
            History
          </button>

          <template v-if="pendingDelete">
            <span class="text-xs text-huginn-muted">Delete "{{ pendingDelete.name }}"?</span>
            <button @click="doDeleteWorkflow" class="text-xs text-red-400 hover:text-red-300 transition-colors">Confirm</button>
            <button @click="pendingDelete = null" class="text-xs text-huginn-muted hover:text-huginn-text transition-colors">Cancel</button>
          </template>
          <button v-else-if="selectedWorkflow" @click="confirmDelete"
            data-testid="delete-workflow-btn"
            class="p-1.5 text-huginn-muted hover:text-red-400 transition-colors rounded">
            <svg class="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
              <path d="M3 6h18M19 6l-1 14H6L5 6M8 6V4h8v2"/>
            </svg>
          </button>

          <button @click="saveWorkflow" :disabled="saving"
            data-testid="save-workflow-btn"
            class="px-3 py-1.5 bg-huginn-blue text-white text-xs font-medium rounded-lg hover:bg-huginn-blue/90 active:scale-95 transition-all duration-150 disabled:opacity-60">
            {{ saving ? 'Saving…' : 'Save' }}
          </button>
        </div>

        <div v-if="saveMsg" class="px-6 pt-3">
          <div class="px-4 py-2.5 rounded-xl border text-xs"
            :class="saveError ? 'border-huginn-red/40 text-huginn-red bg-huginn-red/8' : 'border-huginn-green/40 text-huginn-green bg-huginn-green/8'">
            {{ saveMsg }}
          </div>
        </div>

        <div class="flex flex-1 overflow-hidden">

          <!-- Left: workflow form -->
          <div class="flex-1 overflow-y-auto p-6 space-y-6">
            <div class="grid grid-cols-2 gap-4">
              <div>
                <label class="block text-[11px] font-medium text-huginn-muted mb-1.5 uppercase tracking-wider">Name</label>
                <input v-model="editForm.name"
                  data-testid="workflow-name-input"
                  class="w-full bg-huginn-bg border border-huginn-border rounded-lg px-3 py-2 text-sm text-huginn-text focus:outline-none focus:border-huginn-blue/60 transition-colors"/>
              </div>
              <div>
                <label class="block text-[11px] font-medium text-huginn-muted mb-1.5 uppercase tracking-wider">When to run</label>
                <div class="flex gap-2 mb-2">
                  <button type="button" @click="scheduleMode = 'once'"
                    data-testid="schedule-once-btn"
                    class="flex-1 px-2 py-1.5 text-xs rounded-lg border transition-colors"
                    :class="scheduleMode === 'once' ? 'border-huginn-blue/60 bg-huginn-blue/10 text-huginn-blue' : 'border-huginn-border text-huginn-muted hover:text-huginn-text'">
                    Run once
                  </button>
                  <button type="button" @click="scheduleMode = 'repeat'"
                    data-testid="schedule-repeat-btn"
                    class="flex-1 px-2 py-1.5 text-xs rounded-lg border transition-colors"
                    :class="scheduleMode === 'repeat' ? 'border-huginn-blue/60 bg-huginn-blue/10 text-huginn-blue' : 'border-huginn-border text-huginn-muted hover:text-huginn-text'">
                    Repeat
                  </button>
                </div>
                <p v-if="scheduleMode === 'once'" class="text-[11px] text-huginn-muted/70">
                  No cron. Hit <span class="text-huginn-text">Run Now</span> whenever you want this pipeline.
                </p>
                <CronPicker v-else v-model="editForm.schedule" />
              </div>
              <div>
                <label class="block text-[11px] font-medium text-huginn-muted mb-1.5 uppercase tracking-wider">Company</label>
                <select v-model="editForm.company_id" data-testid="workflow-company-select"
                  class="w-full bg-huginn-bg border border-huginn-border rounded-lg px-3 py-2 text-sm text-huginn-text focus:outline-none focus:border-huginn-blue/60">
                  <option value="">Desk — any teammate</option>
                  <option v-for="c in availableCompanies" :key="c.id" :value="c.id">{{ c.name }}</option>
                </select>
                <p class="text-[10px] text-huginn-muted/50 mt-1">A company pipeline can only wake teammates seated there.</p>
              </div>
              <div>
                <label class="block text-[11px] font-medium text-huginn-muted mb-1.5 uppercase tracking-wider">
                  Timeout (minutes)
                  <span class="text-huginn-muted/50 normal-case ml-1">0 = 30 min default, max 1440</span>
                </label>
                <input v-model.number="editForm.timeout_minutes" type="number" min="0" max="1440" placeholder="30"
                  class="w-full bg-huginn-bg border border-huginn-border rounded-lg px-3 py-2 text-sm text-huginn-text focus:outline-none focus:border-huginn-blue/60 transition-colors"/>
              </div>
              <div class="col-span-2">
                <label class="block text-[11px] font-medium text-huginn-muted mb-1.5 uppercase tracking-wider">Description</label>
                <input v-model="editForm.description"
                  class="w-full bg-huginn-bg border border-huginn-border rounded-lg px-3 py-2 text-sm text-huginn-text focus:outline-none focus:border-huginn-blue/60 transition-colors"/>
              </div>
            </div>

            <!-- Workflow advanced: retry defaults + downstream chain -->
            <div class="border border-huginn-border/60 rounded-xl overflow-hidden">
              <button type="button" @click="showWorkflowAdvanced = !showWorkflowAdvanced"
                data-testid="workflow-advanced-toggle"
                class="w-full flex items-center justify-between px-4 py-2.5 text-left text-xs font-medium text-huginn-muted hover:bg-huginn-bg/40 transition-colors">
                <span>Workflow options (retry defaults, chain)</span>
                <span class="text-[10px] font-mono">{{ showWorkflowAdvanced ? '▼' : '▶' }}</span>
              </button>
              <div v-show="showWorkflowAdvanced" class="px-4 pb-4 pt-2 space-y-4 border-t border-huginn-border/40">
                <div class="grid grid-cols-2 gap-3">
                  <div>
                    <label class="block text-[10px] font-medium text-huginn-muted mb-1 uppercase tracking-wider">Default max retries</label>
                    <input v-model.number="editForm.retry.max_retries" type="number" min="0" max="10" placeholder="0"
                      data-testid="workflow-retry-max-input"
                      class="w-full bg-huginn-bg border border-huginn-border rounded-lg px-3 py-1.5 text-xs text-huginn-text focus:outline-none focus:border-huginn-blue/60"/>
                    <p class="text-[10px] text-huginn-muted/50 mt-1">Inherited by steps with max retries 0.</p>
                  </div>
                  <div>
                    <label class="block text-[10px] font-medium text-huginn-muted mb-1 uppercase tracking-wider">Default retry delay</label>
                    <input v-model="editForm.retry.delay" placeholder="e.g. 30s, 2m"
                      data-testid="workflow-retry-delay-input"
                      class="w-full bg-huginn-bg border border-huginn-border rounded-lg px-3 py-1.5 text-xs font-mono text-huginn-text focus:outline-none focus:border-huginn-blue/60"/>
                  </div>
                </div>
                <div class="space-y-2">
                  <label class="block text-[10px] font-medium text-huginn-muted uppercase tracking-wider">Chain — trigger another workflow when this one finishes</label>
                  <select v-model="editForm.chain.next"
                    data-testid="workflow-chain-next-input"
                    class="w-full bg-huginn-bg border border-huginn-border rounded-lg px-3 py-1.5 text-xs text-huginn-text focus:outline-none focus:border-huginn-blue/60">
                    <option value="">(none)</option>
                    <option v-for="w in chainCandidateWorkflows" :key="w.id" :value="w.id">{{ w.name }} — {{ w.id }}</option>
                  </select>
                  <div class="flex flex-wrap gap-4 pt-1">
                    <label class="flex items-center gap-2 cursor-pointer">
                      <input type="checkbox" v-model="editForm.chain.on_success" class="rounded border-huginn-border text-huginn-blue"/>
                      <span class="text-xs text-huginn-text">On success</span>
                    </label>
                    <label class="flex items-center gap-2 cursor-pointer">
                      <input type="checkbox" v-model="editForm.chain.on_failure" class="rounded border-huginn-border text-huginn-blue"/>
                      <span class="text-xs text-huginn-text">On failure</span>
                    </label>
                  </div>
                </div>
              </div>
            </div>

            <!-- Steps section -->
            <div>
              <div class="flex items-center justify-between mb-3">
                <div>
                  <h2 class="text-xs font-semibold text-huginn-muted uppercase tracking-wider">Teammates</h2>
                  <p v-if="pipelinePreview.length" class="text-[11px] text-huginn-text/80 mt-1 font-medium" data-testid="pipeline-preview">
                    <template v-for="(who, i) in pipelinePreview" :key="i">
                      <span v-if="i"> <span class="text-huginn-muted/50">→</span> </span>
                      <span>{{ who }}</span>
                    </template>
                  </p>
                  <p v-else class="text-[11px] text-huginn-muted/50 mt-1">Stack teammates. Each one sees the last output.</p>
                </div>
                <button @click="addStep"
                  data-testid="add-step-btn"
                  class="flex items-center gap-1 text-xs text-huginn-blue hover:text-huginn-blue/80 transition-colors">
                  <svg class="w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5">
                    <line x1="12" y1="5" x2="12" y2="19"/><line x1="5" y1="12" x2="19" y2="12"/>
                  </svg>
                  Add teammate
                </button>
              </div>

              <div class="space-y-2">
                <div v-for="(step, idx) in editForm.steps" :key="idx"
                  data-testid="workflow-step"
                  class="border rounded-xl bg-huginn-surface overflow-hidden transition-all duration-200"
                  :class="[
                    expandedSteps.has(idx) ? 'border-huginn-blue/30' : 'border-huginn-border',
                    dragOver === idx ? 'border-huginn-blue/60 ring-1 ring-huginn-blue/30' : ''
                  ]"
                  @dragover.prevent="onDragOver(idx)"
                  @drop="onDrop(idx)">

                  <!-- Step header -->
                  <div class="flex items-center gap-3 px-4 py-3 cursor-pointer hover:bg-huginn-bg/40 transition-colors"
                    @click="toggleStep(idx)">
                    <div class="flex flex-col gap-0.5 cursor-grab opacity-40 hover:opacity-70 transition-opacity flex-shrink-0"
                      draggable="true"
                      @dragstart="onDragStart(idx, $event)"
                      @dragend="dragFrom = null; dragOver = null"
                      @click.stop>
                      <div class="w-3 h-0.5 bg-huginn-muted rounded"/>
                      <div class="w-3 h-0.5 bg-huginn-muted rounded"/>
                      <div class="w-3 h-0.5 bg-huginn-muted rounded"/>
                    </div>
                    <span class="flex-shrink-0 w-5 h-5 rounded-full bg-huginn-bg border border-huginn-border text-[10px] font-mono text-huginn-muted flex items-center justify-center">
                      {{ idx + 1 }}
                    </span>
                    <div class="flex-1 min-w-0">
                      <span class="text-sm text-huginn-text truncate block">{{ step.name || `Step ${idx + 1}` }}</span>
                      <span v-if="!expandedSteps.has(idx)" class="text-xs text-huginn-muted truncate block">
                        <template v-if="isSubWorkflowStep(step)">sub:{{ step.sub_workflow }}</template>
                        <template v-else>{{ step.agent ? `@${step.agent}` : 'No agent' }}{{ step.prompt ? ' · ' + step.prompt.slice(0, 60) + (step.prompt.length > 60 ? '…' : '') : '' }}</template>
                      </span>
                    </div>
                    <span class="text-[10px] px-1.5 py-0.5 rounded font-mono flex-shrink-0"
                      :class="step.on_failure === 'continue' ? 'bg-huginn-green/10 text-huginn-green/80' : 'bg-red-500/10 text-red-400/80'">
                      {{ step.on_failure === 'continue' ? 'continue' : 'stop' }}
                    </span>
                    <svg class="w-4 h-4 text-huginn-muted transition-transform duration-200 flex-shrink-0"
                      :class="expandedSteps.has(idx) ? 'rotate-180' : ''"
                      viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                      <path d="M6 9l6 6 6-6"/>
                    </svg>
                  </div>

                  <!-- Step body -->
                  <Transition name="step-expand">
                    <div v-if="expandedSteps.has(idx)" class="border-t border-huginn-border px-4 py-4 space-y-3">
                      <div class="grid grid-cols-2 gap-3">
                        <div>
                          <label class="block text-[10px] font-medium text-huginn-muted mb-1 uppercase tracking-wider">Step Name</label>
                          <input v-model="step.name" placeholder="e.g. Morning Standup"
                            class="w-full bg-huginn-bg border border-huginn-border rounded-lg px-3 py-1.5 text-xs text-huginn-text focus:outline-none focus:border-huginn-blue/60 transition-colors"/>
                        </div>
                        <div :class="{ 'opacity-40 pointer-events-none': isSubWorkflowStep(step) }">
                          <label class="block text-[10px] font-semibold text-huginn-muted/70 uppercase tracking-wider mb-1">Agent</label>
                          <AgentPicker v-model="step.agent!" data-testid="step-agent-input" @select:agent="onAgentSelected(idx, $event)" />
                          <p v-if="companyAgentsForPicker()" class="text-[10px] text-huginn-muted/50 mt-1">
                            This company can wake: {{ companyAgentsForPicker()!.join(', ') || 'nobody seated yet' }}
                          </p>
                        </div>
                        <div class="col-span-2">
                          <label class="block text-[10px] font-medium text-huginn-muted mb-1 uppercase tracking-wider">On Failure</label>
                          <select v-model="step.on_failure"
                            class="w-full bg-huginn-bg border border-huginn-border rounded-lg px-3 py-1.5 text-xs text-huginn-text focus:outline-none focus:border-huginn-blue/60 transition-colors">
                            <option value="stop">Stop pipeline</option>
                            <option value="continue">Continue anyway</option>
                          </select>
                        </div>
                        <div class="col-span-2">
                          <label class="block text-[10px] font-medium text-huginn-muted mb-1 uppercase tracking-wider">Model override (optional)</label>
                          <input v-model="step.model_override" :disabled="isSubWorkflowStep(step)" placeholder="e.g. claude-haiku-4"
                            data-testid="step-model-override-input"
                            class="w-full bg-huginn-bg border border-huginn-border rounded-lg px-3 py-1.5 text-xs font-mono text-huginn-text placeholder-huginn-muted/40 focus:outline-none focus:border-huginn-blue/60 disabled:opacity-40"/>
                          <p class="text-[10px] text-huginn-muted/45 mt-1">Ignored when this step calls a sub-workflow.</p>
                        </div>
                        <div class="col-span-2">
                          <label class="block text-[10px] font-medium text-huginn-muted mb-1 uppercase tracking-wider">When (optional)</label>
                          <textarea v-model="step.when" rows="2" placeholder="e.g. true or run.scratch.flag via placeholders"
                            data-testid="step-when-input"
                            class="w-full bg-huginn-bg border border-huginn-border rounded-lg px-3 py-1.5 text-xs font-mono text-huginn-text placeholder-huginn-muted/40 focus:outline-none focus:border-huginn-blue/60 resize-y"/>
                          <p class="text-[10px] text-huginn-muted/45 mt-1">After <code class="bg-huginn-surface px-0.5 rounded">&#123;&#123;…&#125;&#125;</code> resolve: empty, false, 0, no, off → skip this step.</p>
                        </div>
                        <div class="col-span-2">
                          <label class="block text-[10px] font-medium text-huginn-muted mb-1 uppercase tracking-wider">Sub-workflow (optional)</label>
                          <div class="flex gap-2">
                            <input v-model="step.sub_workflow" placeholder="workflow id"
                              data-testid="step-sub-workflow-input"
                              class="flex-1 min-w-0 bg-huginn-bg border border-huginn-border rounded-lg px-3 py-1.5 text-xs font-mono text-huginn-text placeholder-huginn-muted/40 focus:outline-none focus:border-huginn-blue/60"/>
                            <select
                              class="w-40 flex-shrink-0 bg-huginn-surface border border-huginn-border rounded-lg px-2 py-1.5 text-[10px] text-huginn-text focus:outline-none focus:border-huginn-blue/50"
                              :value="''"
                              @change="pickSubWorkflowStepId(step, $event)">
                              <option value="">Set from…</option>
                              <option v-for="w in chainCandidateWorkflows" :key="'sub-'+w.id" :value="w.id">{{ w.id }}</option>
                            </select>
                          </div>
                          <p class="text-[10px] text-huginn-muted/45 mt-1">Runs another workflow by id; agent and prompt below are ignored.</p>
                        </div>
                      </div>

                      <!-- Agent detail card -->
                      <div v-if="!isSubWorkflowStep(step) && stepAgentDetails[idx]" class="col-span-2 flex flex-wrap gap-1.5 items-center p-2 rounded-lg bg-huginn-surface/50 border border-huginn-border/50">
                        <span class="text-[10px] font-mono px-2 py-0.5 rounded bg-huginn-blue/10 text-huginn-blue/80 border border-huginn-blue/20">
                          {{ (stepAgentDetails[idx] as any).model || 'no model' }}
                        </span>
                        <template v-if="(stepAgentDetails[idx] as any).toolbelt?.length">
                          <span class="text-[10px] text-huginn-muted/50 mx-1">connections:</span>
                          <span v-for="t in (stepAgentDetails[idx] as any).toolbelt" :key="t.provider"
                            class="text-[10px] px-1.5 py-0.5 rounded bg-huginn-surface border border-huginn-border text-huginn-muted/70">
                            {{ t.provider }}
                          </span>
                        </template>
                        <template v-if="(stepAgentDetails[idx] as any).skills?.length">
                          <span class="text-[10px] text-huginn-muted/50 mx-1">skills:</span>
                          <span v-for="s in (stepAgentDetails[idx] as any).skills" :key="s"
                            class="text-[10px] px-1.5 py-0.5 rounded bg-huginn-surface border border-huginn-border text-huginn-muted/70">
                            {{ s }}
                          </span>
                        </template>
                        <span class="text-[10px] text-huginn-muted/50 mx-1">memory:</span>
                        <span class="text-[10px] px-1.5 py-0.5 rounded bg-huginn-surface border border-huginn-border text-huginn-muted/70">
                          {{ (stepAgentDetails[idx] as any).memory_enabled === false ? 'off' : 'on' }}
                        </span>
                      </div>

                      <template v-if="!isSubWorkflowStep(step)">
                      <div>
                        <label class="block text-[10px] font-medium text-huginn-muted mb-1 uppercase tracking-wider">Prompt</label>
                        <textarea v-model="step.prompt" rows="5" placeholder="What should this agent do?"
                          data-testid="step-prompt-input"
                          class="w-full bg-huginn-bg border border-huginn-border rounded-lg px-3 py-2 text-xs text-huginn-text font-mono resize-y focus:outline-none focus:border-huginn-blue/60 transition-colors leading-relaxed"/>
                      </div>

                      <!-- Inputs section -->
                      <div class="col-span-2">
                        <div class="flex items-center justify-between mb-2">
                          <label class="text-[10px] font-semibold text-huginn-muted/70 uppercase tracking-wider">Inputs from prior steps</label>
                          <button @click="addStepInput(step)" type="button"
                            class="text-[10px] text-huginn-blue hover:text-huginn-blue/80 transition-colors">
                            + Add Input
                          </button>
                        </div>
                        <div v-if="step.inputs?.length" class="space-y-1.5">
                          <div v-for="(inp, iIdx) in step.inputs" :key="iIdx" class="flex gap-2 items-center">
                            <select v-model="inp.from_step"
                              class="flex-1 bg-huginn-surface border border-huginn-border rounded-lg px-2 py-1.5 text-xs text-huginn-text focus:outline-none focus:border-huginn-blue/50">
                              <option value="">From step...</option>
                              <option v-for="(s, sIdx) in editForm.steps.filter((_, si) => si < idx)" :key="sIdx"
                                :value="s.name || `step-${s.position}`">
                                {{ s.name || `Step ${sIdx + 1}` }}
                              </option>
                            </select>
                            <span class="text-[10px] text-huginn-muted/50">as</span>
                            <input v-model="inp.as" placeholder="alias, e.g. result"
                              class="flex-1 bg-huginn-surface border border-huginn-border rounded-lg px-2 py-1.5 text-xs text-huginn-text placeholder-huginn-muted/40 focus:outline-none focus:border-huginn-blue/50" />
                            <button @click="removeStepInput(step, iIdx)" type="button"
                              class="text-[10px] text-red-400/70 hover:text-red-400 transition-colors px-1">×</button>
                          </div>
                          <p class="text-[10px] text-huginn-muted/40 mt-1">
                            Use <code class="bg-huginn-surface px-1 rounded">&#123;&#123;inputs.alias&#125;&#125;</code> or <code class="bg-huginn-surface px-1 rounded">&#123;&#123;prev.output&#125;&#125;</code> in your prompt.
                          </p>
                        </div>
                        <p v-else class="text-[10px] text-huginn-muted/30 italic">
                          No inputs. Use <code class="bg-huginn-surface px-1 rounded">&#123;&#123;prev.output&#125;&#125;</code> in your prompt to reference the previous step's output automatically.
                        </p>
                      </div>
                      </template>
                      <div v-else class="rounded-lg border border-amber-500/25 bg-amber-500/5 px-3 py-2.5 text-xs text-amber-200/90">
                        This step runs workflow <code class="font-mono text-amber-100">{{ step.sub_workflow }}</code> synchronously. Prompt and inputs are not used.
                      </div>

                      <!-- Step notification (opt-in) -->
                      <div class="col-span-2 border-t border-huginn-border/50 pt-3">
                        <label class="flex items-center gap-2 cursor-pointer">
                          <input type="checkbox" :checked="!!step.notify"
                            @change="toggleStepNotify(step, ($event.target as HTMLInputElement).checked)"
                            class="rounded border-huginn-border text-huginn-blue" />
                          <span class="text-xs text-huginn-text">Notify on this step</span>
                        </label>
                        <div v-if="step.notify" class="mt-2 space-y-2 pl-5">
                          <div class="flex items-center gap-4">
                            <label class="flex items-center gap-2 cursor-pointer">
                              <input type="checkbox" v-model="step.notify!.on_success"
                                class="rounded border-huginn-border text-huginn-blue" />
                              <span class="text-xs text-huginn-text">On success</span>
                            </label>
                            <label class="flex items-center gap-2 cursor-pointer">
                              <input type="checkbox" v-model="step.notify!.on_failure"
                                class="rounded border-huginn-border text-huginn-blue" />
                              <span class="text-xs text-huginn-text">On failure</span>
                            </label>
                          </div>
                          <div>
                            <div class="flex items-center justify-between mb-1">
                              <label class="text-[10px] text-huginn-muted/60">Deliver To</label>
                              <button @click="addStepDeliveryTarget(step)" type="button"
                                class="text-[10px] text-huginn-blue hover:text-huginn-blue/80 transition-colors">+ Add</button>
                            </div>
                            <div v-for="(d, dIdx) in (step.notify?.deliver_to || [])" :key="dIdx" class="flex gap-2 items-start mb-1">
                              <div class="flex flex-col gap-1 flex-1">
                              <select v-model="d.type"
                                class="bg-huginn-surface border border-huginn-border rounded-lg px-2 py-1 text-xs text-huginn-text focus:outline-none focus:border-huginn-blue/50">
                                <option value="inbox">Inbox only</option>
                                <option value="space">Space / Channel</option>
                                <option value="webhook">Webhook (HTTP POST)</option>
                                <option value="email">Email</option>
                              </select>
                              <select v-if="d.type === 'space'" v-model="d.space_id"
                                class="bg-huginn-surface border border-huginn-border rounded-lg px-2 py-1 text-xs text-huginn-text focus:outline-none focus:border-huginn-blue/50">
                                <option value="">{{ availableSpaces.length ? 'Select space...' : 'No spaces configured — create one first' }}</option>
                                <option v-for="sp in availableSpaces" :key="sp.id" :value="sp.id">
                                  {{ sp.kind === 'dm' ? '⊙' : '#' }} {{ sp.name }}
                                </option>
                              </select>
                              <input v-if="d.type === 'webhook'" v-model="(d as any).url"
                                placeholder="https://hooks.example.com/..."
                                class="bg-huginn-surface border border-huginn-border rounded-lg px-2 py-1 text-xs font-mono text-huginn-text focus:outline-none focus:border-huginn-blue/50"/>
                              <input v-if="d.type === 'email'" v-model="(d as any).to"
                                placeholder="recipient@example.com"
                                class="bg-huginn-surface border border-huginn-border rounded-lg px-2 py-1 text-xs text-huginn-text focus:outline-none focus:border-huginn-blue/50"/>
                              </div>
                              <button @click="step.notify?.deliver_to?.splice(dIdx, 1)" type="button"
                                class="text-[10px] text-red-400/70 hover:text-red-400 transition-colors px-1 mt-1.5">×</button>
                            </div>
                          </div>
                        </div>
                      </div>

                      <div class="flex justify-end">
                        <button @click="removeStep(idx)" class="text-xs text-red-400/70 hover:text-red-400 transition-colors">
                          Remove step
                        </button>
                      </div>
                    </div>
                  </Transition>
                </div>

                <button v-if="editForm.steps.length === 0" @click="addStep"
                  class="w-full py-8 border border-dashed border-huginn-border rounded-xl text-xs text-huginn-muted hover:text-huginn-text hover:border-huginn-blue/40 transition-all duration-200 flex flex-col items-center gap-2">
                  <svg class="w-6 h-6 opacity-40" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5">
                    <line x1="12" y1="5" x2="12" y2="19"/><line x1="5" y1="12" x2="19" y2="12"/>
                  </svg>
                  Add your first teammate
                </button>
              </div>
            </div>

            <!-- Notifications -->
            <div class="mt-6">
              <h2 class="text-xs font-semibold text-huginn-muted/70 uppercase tracking-wider mb-3">Notifications</h2>
              <div class="bg-huginn-surface border border-huginn-border rounded-xl p-4 space-y-3">
                <div class="flex items-center gap-4">
                  <label class="flex items-center gap-2 cursor-pointer">
                    <input type="checkbox" v-model="editForm.notification!.on_success"
                      class="rounded border-huginn-border text-huginn-blue" />
                    <span class="text-xs text-huginn-text">On success</span>
                  </label>
                  <label class="flex items-center gap-2 cursor-pointer">
                    <input type="checkbox" v-model="editForm.notification!.on_failure"
                      class="rounded border-huginn-border text-huginn-blue" />
                    <span class="text-xs text-huginn-text">On failure</span>
                  </label>
                  <select v-model="editForm.notification!.severity"
                    class="ml-auto bg-huginn-surface border border-huginn-border rounded-lg px-2 py-1 text-xs text-huginn-text focus:outline-none focus:border-huginn-blue/50">
                    <option value="info">Info</option>
                    <option value="warning">Warning</option>
                    <option value="urgent">Urgent</option>
                  </select>
                </div>
                <!-- Delivery targets -->
                <div>
                  <div class="flex items-center justify-between mb-2">
                    <label class="text-[10px] font-semibold text-huginn-muted/70 uppercase tracking-wider">Deliver To</label>
                    <button @click="addWorkflowDeliveryTarget()" type="button"
                      class="text-[10px] text-huginn-blue hover:text-huginn-blue/80 transition-colors">+ Add target</button>
                  </div>
                  <div class="text-[10px] text-huginn-muted/40 italic mb-2">Inbox is always included.</div>
                  <div v-for="(d, dIdx) in (editForm.notification?.deliver_to || [])" :key="dIdx"
                    class="flex gap-2 items-start mb-1.5">
                    <div class="flex flex-col gap-1 flex-1">
                      <select v-model="d.type"
                        class="bg-huginn-surface border border-huginn-border rounded-lg px-2 py-1.5 text-xs text-huginn-text focus:outline-none focus:border-huginn-blue/50">
                        <option value="inbox">Inbox only</option>
                        <option value="space">Space / Channel</option>
                        <option value="webhook">Webhook (HTTP POST)</option>
                        <option value="email">Email</option>
                      </select>
                      <select v-if="d.type === 'space'" v-model="d.space_id"
                        class="bg-huginn-surface border border-huginn-border rounded-lg px-2 py-1.5 text-xs text-huginn-text focus:outline-none focus:border-huginn-blue/50">
                        <option value="">{{ availableSpaces.length ? 'Select space...' : 'No spaces configured — create one first' }}</option>
                        <option v-for="sp in availableSpaces" :key="sp.id" :value="sp.id">
                          {{ sp.kind === 'dm' ? '⊙' : '#' }} {{ sp.name }}
                        </option>
                      </select>
                      <input v-if="d.type === 'webhook'" v-model="(d as any).url"
                        placeholder="https://hooks.example.com/..."
                        class="bg-huginn-surface border border-huginn-border rounded-lg px-2 py-1.5 text-xs font-mono text-huginn-text focus:outline-none focus:border-huginn-blue/50"/>
                      <input v-if="d.type === 'email'" v-model="(d as any).to"
                        placeholder="recipient@example.com"
                        class="bg-huginn-surface border border-huginn-border rounded-lg px-2 py-1.5 text-xs text-huginn-text focus:outline-none focus:border-huginn-blue/50"/>
                    </div>
                    <button @click="removeWorkflowDeliveryTarget(dIdx)" type="button"
                      class="text-[10px] text-red-400/70 hover:text-red-400 transition-colors px-1 mt-2">×</button>
                  </div>
                </div>
              </div>
            </div>

          </div>

          <!-- Right: live execution panel -->
          <Transition name="slide-in">
            <div v-if="currentRunEvents.length > 0"
              class="w-72 flex-shrink-0 border-l border-huginn-border flex flex-col">
              <div class="flex items-center justify-between px-4 py-3 border-b border-huginn-border">
                <span class="text-xs font-semibold text-huginn-muted uppercase tracking-wider">Live Execution</span>
                <button @click="clearRunEvents" class="text-huginn-muted hover:text-huginn-text transition-colors">
                  <svg class="w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                    <line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/>
                  </svg>
                </button>
              </div>
              <div class="flex-1 overflow-y-auto p-3 space-y-2" ref="eventsRef">
                <template v-for="(row, i) in displayedLiveEvents" :key="i">
                  <button v-if="isTokenBatchRow(row)" type="button"
                    class="w-full text-left text-[11px] rounded-lg px-3 py-2 font-mono bg-slate-600/15 text-slate-300 border border-slate-500/20 cursor-pointer select-none"
                    @click="toggleTokenBatchExpand(i)">
                    <div class="flex items-center gap-1.5">
                      <span class="opacity-60">⋯</span>
                      <span class="truncate">Model tokens · {{ row.count }} chunk(s) · {{ row.text.length }} chars</span>
                    </div>
                    <div v-if="expandedTokenBatchIndex === i"
                      class="mt-1 max-h-36 overflow-y-auto text-[10px] text-slate-400/90 whitespace-pre-wrap break-words">
                      {{ row.text }}
                    </div>
                  </button>
                  <div v-else
                    class="text-[11px] rounded-lg px-3 py-2 font-mono"
                    :class="{
                      'bg-huginn-blue/10 text-huginn-blue': row.type === 'workflow_started',
                      'bg-indigo-500/10 text-indigo-300': row.type === 'workflow_step_started',
                      'bg-huginn-surface text-huginn-text': row.type === 'workflow_step_complete' && row.status === 'success',
                      'bg-amber-500/10 text-amber-400': (row.type === 'workflow_step_complete' && row.status === 'failed' && isPlaceholderError(row.error)) || row.type === 'workflow_cancelled',
                      'bg-red-500/10 text-red-400': row.type === 'workflow_step_complete' && row.status === 'failed' && !isPlaceholderError(row.error),
                      'bg-huginn-green/10 text-huginn-green': row.type === 'workflow_complete',
                      'bg-red-500/15 text-red-400': row.type === 'workflow_failed',
                      'bg-amber-500/12 text-amber-300': row.type === 'workflow_partial',
                      'bg-teal-500/10 text-teal-300': row.type === 'workflow_skipped',
                    }">
                    <div class="flex items-center gap-1.5">
                      <span class="opacity-60">{{ eventIcon(row) }}</span>
                      <span class="truncate">{{ eventLabel(row) }}</span>
                    </div>
                    <div v-if="row.error && !isPlaceholderError(row.error)" class="mt-1 opacity-70 text-[10px] break-words">{{ row.error }}</div>
                    <div v-if="row.error && isPlaceholderError(row.error)" class="mt-1 text-[10px] break-words text-amber-400/80">
                      ⚠ Template placeholder not resolved — check from_step references
                    </div>
                    <div v-if="row.type === 'workflow_skipped' && row.when_resolved" class="mt-1 text-[10px] text-teal-200/70 break-words">
                      when: {{ row.when_resolved }}
                    </div>
                  </div>
                </template>
              </div>
            </div>
          </Transition>
        </div>
      </div>
    </Transition>

    <!-- ── Run History slide-over ── -->
    <Teleport to="body">
      <Transition name="overlay">
        <div v-if="showHistory" class="fixed inset-0 z-50 flex justify-end">
          <div class="absolute inset-0 bg-black/40 backdrop-blur-sm" @click="closeHistory()"/>
          <div class="relative w-96 bg-huginn-bg border-l border-huginn-border flex flex-col shadow-2xl">
            <div class="flex items-center justify-between px-5 py-4 border-b border-huginn-border">
              <h2 class="text-sm font-semibold text-huginn-text">Run History</h2>
              <button @click="closeHistory()" class="text-huginn-muted hover:text-huginn-text transition-colors">
                <svg class="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                  <line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/>
                </svg>
              </button>
            </div>
            <div v-if="historyFeedback" class="px-4 py-2 text-[11px] border-b border-huginn-border/60"
              :class="historyFeedback.err ? 'text-red-400 bg-red-500/8' : 'text-huginn-green bg-huginn-green/8'">
              {{ historyFeedback.text }}
            </div>
            <div class="flex-1 overflow-y-auto p-4 space-y-3">
              <div v-if="loadingHistory" class="flex justify-center py-10">
                <div class="w-5 h-5 border border-huginn-border border-t-huginn-blue rounded-full animate-spin"/>
              </div>
              <div v-else-if="!runs.length" class="text-center py-10 text-huginn-muted text-xs">No runs yet</div>
              <div v-else v-for="run in runs" :key="run.id"
                class="bg-huginn-surface border rounded-xl overflow-hidden transition-colors duration-150 cursor-pointer"
                :class="expandedRunId === run.id ? 'border-huginn-blue/30' : 'border-huginn-border'"
                @click="toggleRun(run.id)">
                <!-- Run header -->
                <div class="flex items-center justify-between p-3">
                  <div class="flex items-center gap-2 min-w-0">
                    <svg class="w-3 h-3 text-huginn-muted flex-shrink-0 transition-transform duration-200"
                      :class="expandedRunId === run.id ? 'rotate-90' : ''"
                      viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                      <path d="M9 18l6-6-6-6"/>
                    </svg>
                    <span class="text-[10px] font-mono text-huginn-muted truncate">{{ run.id.slice(-12) }}</span>
                  </div>
                  <span class="text-[10px] px-1.5 py-0.5 rounded font-medium flex-shrink-0"
                    :class="{
                      'bg-huginn-green/10 text-huginn-green': run.status === 'complete',
                      'bg-red-500/10 text-red-400': run.status === 'failed',
                      'bg-huginn-blue/10 text-huginn-blue animate-pulse': run.status === 'running',
                      'bg-amber-500/10 text-amber-400': run.status === 'cancelled' || run.status === 'partial',
                    }">
                    {{ run.status }}
                  </span>
                </div>
                <!-- Step pills summary -->
                <div class="flex flex-wrap gap-1 px-3 pb-2">
                  <div v-for="s in run.steps" :key="s.position"
                    class="text-[10px] px-1.5 py-0.5 rounded font-mono"
                    :class="{
                      'bg-huginn-green/10 text-huginn-green': s.status === 'success',
                      'bg-amber-500/10 text-amber-400': s.status === 'failed' && isPlaceholderError(s.error),
                      'bg-red-500/10 text-red-400': s.status === 'failed' && !isPlaceholderError(s.error),
                      'bg-huginn-muted/10 text-huginn-muted': s.status === 'skipped',
                    }"
                    :title="stepPillTitle(s)">
                    {{ s.slug || `step ${s.position}` }}
                  </div>
                </div>
                <div class="text-[10px] text-huginn-muted/60 px-3 pb-3">
                  {{ new Date(run.started_at).toLocaleString() }}
                  <span v-if="run.completed_at" class="ml-2 opacity-70">
                    · {{ Math.round((new Date(run.completed_at).getTime() - new Date(run.started_at).getTime()) / 1000) }}s
                  </span>
                </div>
                <!-- Expanded step detail -->
                <div v-if="expandedRunId === run.id" class="border-t border-huginn-border/60 bg-huginn-bg/50">
                  <div class="px-4 py-2 flex flex-wrap gap-2 border-b border-huginn-border/40">
                    <button type="button"
                      data-testid="run-replay-btn"
                      class="text-[10px] px-2 py-1 rounded border border-huginn-border text-huginn-text hover:border-huginn-blue/40 transition-colors"
                      @click.stop="startReplay(run)">Replay</button>
                    <button type="button"
                      data-testid="run-fork-btn"
                      class="text-[10px] px-2 py-1 rounded border border-huginn-border text-huginn-text hover:border-huginn-blue/40 transition-colors"
                      @click.stop="openForkModal(run)">Fork…</button>
                    <button type="button"
                      data-testid="run-diff-btn"
                      class="text-[10px] px-2 py-1 rounded border border-huginn-border text-huginn-text hover:border-huginn-blue/40 transition-colors"
                      @click.stop="openDiffModal(run)">Diff vs…</button>
                  </div>

                  <!-- Tab bar -->
                  <div class="flex gap-4 border-b border-huginn-border px-4 mb-0 text-xs">
                    <button
                      @click.stop="runDetailTab = 'steps'"
                      :class="runDetailTab === 'steps'
                        ? 'text-huginn-text border-b-2 border-huginn-blue pb-1 pt-1'
                        : 'text-huginn-muted pb-1 pt-1'"
                    >Steps</button>
                    <button
                      @click.stop="runDetailTab = 'deliveries'"
                      :class="runDetailTab === 'deliveries'
                        ? 'text-huginn-text border-b-2 border-huginn-blue pb-1 pt-1'
                        : 'text-huginn-muted pb-1 pt-1'"
                    >
                      Deliveries
                      <span v-if="runDeliveries.length > 0"
                        class="ml-1 bg-red-500 text-white text-[8px] font-bold rounded-full px-1">
                        {{ runDeliveries.length }}
                      </span>
                    </button>
                  </div>

                  <!-- Steps panel -->
                  <template v-if="runDetailTab === 'steps'">
                    <div v-for="s in run.steps" :key="s.position"
                      class="px-4 py-2.5 border-b border-huginn-border/40 last:border-b-0">
                      <div class="flex items-start justify-between gap-2">
                        <div class="flex items-center gap-2 min-w-0">
                          <span class="text-[10px] font-mono flex-shrink-0"
                            :class="{
                              'text-huginn-green': s.status === 'success',
                              'text-red-400': s.status === 'failed',
                              'text-huginn-muted': s.status === 'skipped',
                            }">
                            {{ s.status === 'success' ? '✓' : s.status === 'failed' ? '✗' : '–' }}
                          </span>
                          <span class="text-xs text-huginn-text truncate">{{ s.slug || `Step ${s.position}` }}</span>
                        </div>
                        <div v-if="s.session_id" class="flex items-start gap-1 flex-shrink-0 relative">
                          <a :href="`/sessions/${s.session_id}`"
                            @click.stop
                            class="text-[10px] text-huginn-blue/70 hover:text-huginn-blue flex items-center gap-1 transition-colors"
                            title="Open session">
                            <svg class="w-3 h-3" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                              <path d="M18 13v6a2 2 0 01-2 2H5a2 2 0 01-2-2V8a2 2 0 012-2h6M15 3h6v6M10 14L21 3"/>
                            </svg>
                            session
                          </a>
                          <div class="relative">
                            <button type="button"
                              data-testid="step-session-artifacts-btn"
                              class="text-[10px] text-huginn-muted hover:text-huginn-text px-1 py-0.5 rounded border border-huginn-border/60"
                              title="Artifacts produced in this session"
                              @click.stop="toggleArtifactPopover(s.session_id!)">
                              Artifacts
                            </button>
                            <div v-if="artifactPopoverSessionId === s.session_id"
                              class="absolute right-0 z-20 mt-1 w-60 max-h-52 overflow-y-auto rounded-lg border border-huginn-border bg-huginn-surface shadow-xl p-2 text-left">
                              <div v-if="sessionArtifactsLoading[s.session_id]" class="text-[10px] text-huginn-muted py-1">Loading…</div>
                              <template v-else>
                                <p v-if="!(sessionArtifactsById[s.session_id]?.length)" class="text-[10px] text-huginn-muted">No artifacts in this session.</p>
                                <ul v-else class="space-y-1.5">
                                  <li v-for="a in sessionArtifactsById[s.session_id]" :key="a.id" class="text-[10px] leading-snug">
                                    <span class="text-huginn-text font-medium">{{ a.title || a.id }}</span>
                                    <span class="text-huginn-muted/70"> · {{ a.kind }} · {{ a.status }}</span>
                                  </li>
                                </ul>
                              </template>
                            </div>
                          </div>
                        </div>
                      </div>
                      <div v-if="stepMetricsLine(s)" class="mt-0.5 text-[10px] font-mono text-huginn-muted/55">
                        {{ stepMetricsLine(s) }}
                      </div>
                      <div v-if="s.status === 'skipped'" class="mt-1 text-[10px] text-huginn-muted/80 break-words">
                        {{ skipStepTooltip(s) }}
                      </div>
                      <div v-if="s.error" class="mt-1 text-[10px] font-mono break-words"
                        :class="isPlaceholderError(s.error) ? 'text-amber-400/80' : 'text-red-400/80'">
                        {{ isPlaceholderError(s.error) ? '⚠ unresolved template placeholder' : s.error }}
                      </div>
                      <div v-if="s.output" class="mt-1 space-y-1">
                        <div class="text-[10px] font-mono text-huginn-muted/70 line-clamp-3 break-words">{{ s.output }}</div>
                        <div class="flex flex-wrap gap-2">
                          <button type="button"
                            class="text-[10px] text-huginn-blue hover:text-huginn-blue/80 transition-colors"
                            @click.stop="stepOutputModal = { title: `${run.id.slice(-12)} · ${s.slug || 'step ' + s.position}`, body: s.output || '' }">
                            Expand output
                          </button>
                          <button type="button"
                            class="text-[10px] text-huginn-muted hover:text-huginn-text transition-colors"
                            @click.stop="copyStepOutput(s.output || '')">
                            Copy full output
                          </button>
                        </div>
                      </div>
                    </div>
                    <div v-if="run.error" class="px-4 py-2.5 text-[10px] font-mono text-red-400/80 break-words">
                      {{ run.error }}
                    </div>
                  </template>

                  <!-- Deliveries panel -->
                  <div v-else class="flex flex-col gap-2 px-4 py-3">
                    <div v-if="runDeliveries.length === 0" class="text-huginn-muted text-xs py-4 text-center">
                      All deliveries successful
                    </div>
                    <div v-for="entry in runDeliveries" :key="entry.id"
                      class="bg-huginn-surface rounded-lg p-3 border border-huginn-border text-xs">
                      <div class="flex items-center justify-between gap-2">
                        <div class="flex items-center gap-2 min-w-0">
                          <span :class="entry.channel === 'webhook' ? 'text-huginn-blue' : 'text-purple-400'">
                            {{ entry.channel }}
                          </span>
                          <span class="text-huginn-muted truncate">{{ entry.endpoint }}</span>
                        </div>
                        <div class="flex items-center gap-2 flex-shrink-0">
                          <span class="text-red-400">failed after {{ entry.attempt_count }} attempts</span>
                          <button @click="retryEntry(entry.id)"
                            class="px-2 py-0.5 bg-huginn-blue/20 text-huginn-blue rounded hover:bg-huginn-blue/30 text-xs">
                            Retry
                          </button>
                          <button @click="dismissEntry(entry.id)"
                            class="px-2 py-0.5 bg-huginn-surface text-huginn-muted rounded hover:text-huginn-text text-xs">
                            Dismiss
                          </button>
                        </div>
                      </div>
                      <div v-if="entry.last_error" class="text-huginn-muted mt-1 truncate">
                        {{ entry.last_error }}
                      </div>
                    </div>
                  </div>

                </div>
              </div>
            </div>
          </div>
        </div>
      </Transition>
    </Teleport>

    <Teleport to="body">
      <Transition name="overlay">
        <div v-if="stepOutputModal" class="fixed inset-0 z-[60] flex items-center justify-center p-4">
          <div class="absolute inset-0 bg-black/50 backdrop-blur-sm" @click="stepOutputModal = null"/>
          <div class="relative w-full max-w-3xl max-h-[85vh] flex flex-col bg-huginn-bg border border-huginn-border rounded-xl shadow-2xl">
            <div class="flex items-center justify-between px-4 py-3 border-b border-huginn-border flex-shrink-0">
              <h3 class="text-xs font-medium text-huginn-text truncate pr-2">{{ stepOutputModal.title }}</h3>
              <div class="flex items-center gap-2 flex-shrink-0">
                <button type="button"
                  class="text-[10px] text-huginn-blue hover:text-huginn-blue/80"
                  @click="copyStepOutput(stepOutputModal.body)">Copy</button>
                <button type="button" class="text-huginn-muted hover:text-huginn-text p-1" @click="stepOutputModal = null" aria-label="Close">
                  <svg class="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                    <line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/>
                  </svg>
                </button>
              </div>
            </div>
            <pre class="flex-1 overflow-y-auto p-4 text-[11px] font-mono text-huginn-text whitespace-pre-wrap break-words">{{ stepOutputModal.body }}</pre>
          </div>
        </div>
      </Transition>
    </Teleport>

    <Teleport to="body">
      <Transition name="overlay">
        <div v-if="showForkModal && forkTargetRun" class="fixed inset-0 z-[60] flex items-center justify-center p-4">
          <div class="absolute inset-0 bg-black/50 backdrop-blur-sm" @click="showForkModal = false"/>
          <div class="relative w-full max-w-md bg-huginn-bg border border-huginn-border rounded-xl shadow-2xl p-5 space-y-3">
            <h3 class="text-sm font-semibold text-huginn-text">Fork run</h3>
            <p class="text-[11px] text-huginn-muted">Optional JSON object of input overrides (merged with the prior run's trigger inputs).</p>
            <textarea v-model="forkInputsJson" rows="4" placeholder='{ "key": "value" }'
              data-testid="fork-inputs-json"
              class="w-full bg-huginn-surface border border-huginn-border rounded-lg px-3 py-2 text-[11px] font-mono text-huginn-text focus:outline-none focus:border-huginn-blue/50"/>
            <label class="flex items-center gap-2 cursor-pointer text-xs text-huginn-text">
              <input type="checkbox" v-model="forkUseLive" data-testid="fork-use-live-checkbox" class="rounded border-huginn-border text-huginn-blue"/>
              Use live workflow definition (not snapshot)
            </label>
            <div class="flex justify-end gap-2 pt-2">
              <button type="button" class="text-xs text-huginn-muted hover:text-huginn-text px-3 py-1.5" @click="showForkModal = false">Cancel</button>
              <button type="button" data-testid="fork-submit-btn"
                class="text-xs px-3 py-1.5 rounded-lg bg-huginn-blue text-white hover:bg-huginn-blue/90 disabled:opacity-50"
                :disabled="forkSubmitting" @click="submitFork">{{ forkSubmitting ? 'Starting…' : 'Fork run' }}</button>
            </div>
          </div>
        </div>
      </Transition>
    </Teleport>

    <Teleport to="body">
      <Transition name="overlay">
        <div v-if="showDiffModal && diffBaseRun" class="fixed inset-0 z-[60] flex items-center justify-center p-4">
          <div class="absolute inset-0 bg-black/50 backdrop-blur-sm" @click="showDiffModal = false"/>
          <div class="relative w-full max-w-2xl max-h-[90vh] flex flex-col bg-huginn-bg border border-huginn-border rounded-xl shadow-2xl p-5 gap-3">
            <h3 class="text-sm font-semibold text-huginn-text">Compare runs</h3>
            <p class="text-[11px] text-huginn-muted truncate">Base: {{ diffBaseRun.id.slice(-12) }} ({{ diffBaseRun.status }})</p>
            <div class="flex flex-wrap items-end gap-2">
              <label class="flex-1 min-w-[12rem] text-[10px] text-huginn-muted uppercase tracking-wider">Other run</label>
              <select v-model="diffOtherRunId" data-testid="diff-other-run-select"
                class="flex-1 min-w-[12rem] bg-huginn-surface border border-huginn-border rounded-lg px-2 py-1.5 text-xs font-mono text-huginn-text">
                <option v-for="r in runs.filter(x => x.id !== diffBaseRun!.id)" :key="r.id" :value="r.id">
                  {{ r.id.slice(-12) }} — {{ r.status }}
                </option>
              </select>
              <button type="button" data-testid="diff-compare-btn"
                class="text-xs px-3 py-1.5 rounded-lg bg-huginn-blue text-white hover:bg-huginn-blue/90 disabled:opacity-50"
                :disabled="diffLoading || !diffOtherRunId" @click="runDiffCompare">
                {{ diffLoading ? 'Loading…' : 'Compare' }}
              </button>
            </div>
            <pre v-if="diffResultJson" class="flex-1 overflow-y-auto max-h-[55vh] p-3 text-[10px] font-mono bg-huginn-surface/50 rounded-lg border border-huginn-border/60 whitespace-pre-wrap break-words">{{ diffResultJson }}</pre>
            <button type="button" class="text-xs text-huginn-muted self-end" @click="showDiffModal = false">Close</button>
          </div>
        </div>
      </Transition>
    </Teleport>

    <!-- ── Create / Template gallery modal ── -->
    <Teleport to="body">
      <Transition name="overlay">
        <div v-if="showCreate" class="fixed inset-0 z-50 flex items-center justify-center p-6">
          <div class="absolute inset-0 bg-black/50 backdrop-blur-sm" @click="showCreate = false"/>
          <div class="relative w-full max-w-2xl bg-huginn-bg border border-huginn-border rounded-2xl shadow-2xl flex flex-col max-h-[80vh]">
            <div class="flex items-center justify-between px-6 py-4 border-b border-huginn-border">
              <div>
                <h2 class="text-sm font-semibold text-huginn-text">New Workflow</h2>
                <p class="text-xs text-huginn-muted mt-0.5">Start from a template or create blank</p>
              </div>
              <button @click="showCreate = false" class="text-huginn-muted hover:text-huginn-text transition-colors">
                <svg class="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                  <line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/>
                </svg>
              </button>
            </div>
            <div class="overflow-y-auto p-6">
              <button @click="createBlank"
                class="w-full text-left px-4 py-3 border border-dashed border-huginn-border rounded-xl hover:border-huginn-blue/50 hover:bg-huginn-blue/5 transition-all duration-200 mb-4">
                <div class="flex items-center gap-3">
                  <div class="w-8 h-8 rounded-lg bg-huginn-surface border border-huginn-border flex items-center justify-center flex-shrink-0">
                    <svg class="w-4 h-4 text-huginn-muted" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                      <line x1="12" y1="5" x2="12" y2="19"/><line x1="5" y1="12" x2="19" y2="12"/>
                    </svg>
                  </div>
                  <div>
                    <div class="text-sm font-medium text-huginn-text">Blank Workflow</div>
                    <div class="text-xs text-huginn-muted">Start from scratch</div>
                  </div>
                </div>
              </button>
              <div class="text-[11px] font-semibold text-huginn-muted uppercase tracking-wider mb-3">Templates</div>
              <div v-if="loadingTemplates" class="flex justify-center py-6">
                <div class="w-4 h-4 border border-huginn-border border-t-huginn-blue rounded-full animate-spin"/>
              </div>
              <div v-else class="grid grid-cols-1 sm:grid-cols-2 gap-3">
                <button v-for="tpl in templates" :key="tpl.id"
                  @click="createFromTemplate(tpl)"
                  class="text-left px-4 py-3 border border-huginn-border rounded-xl hover:border-huginn-blue/50 hover:bg-huginn-blue/5 transition-all duration-200">
                  <div class="text-sm font-medium text-huginn-text mb-0.5">{{ tpl.name }}</div>
                  <div class="text-xs text-huginn-muted leading-relaxed">{{ tpl.description }}</div>
                </button>
              </div>
            </div>
          </div>
        </div>
      </Transition>
    </Teleport>

  </div>
</template>

<script setup lang="ts">
import { toRef } from 'vue'
import { useRouter } from 'vue-router'
import AgentPicker from '../components/AgentPicker.vue'
import CronPicker from '../components/CronPicker.vue'
import { useWorkflowsViewState } from './workflows/useWorkflowsViewState'

const props = defineProps<{ id?: string; runId?: string }>()
const router = useRouter()

const {
  loading,
  liveEvents,
  search,
  selectedId,
  selectedWorkflow,
  showCreate,
  showHistory,
  saving,
  saveMsg,
  saveError,
  running,
  cancelling,
  expandedSteps,
  dragFrom,
  dragOver,
  runs,
  loadingHistory,
  expandedRunId,
  runDetailTab,
  runDeliveries,
  historyFeedback,
  showForkModal,
  forkTargetRun,
  forkInputsJson,
  forkUseLive,
  forkSubmitting,
  showDiffModal,
  diffBaseRun,
  diffOtherRunId,
  diffLoading,
  diffResultJson,
  sessionArtifactsById,
  sessionArtifactsLoading,
  artifactPopoverSessionId,
  templates,
  loadingTemplates,
  stepAgentDetails,
  availableSpaces,
  availableCompanies,
  pipelinePreview,
  scheduleMode,
  companyAgentsForPicker,
  showWorkflowAdvanced,
  stepOutputModal,
  expandedTokenBatchIndex,
  chainCandidateWorkflows,
  editForm,
  filteredWorkflows,
  currentRunEvents,
  displayedLiveEvents,
  isSubWorkflowStep,
  pickSubWorkflowStepId,
  isTokenBatchRow,
  onAgentSelected,
  addStepInput,
  removeStepInput,
  addWorkflowDeliveryTarget,
  removeWorkflowDeliveryTarget,
  addStepDeliveryTarget,
  toggleStepNotify,
  openWorkflow,
  closeWorkflow,
  toggleRun,
  closeHistory,
  toggleArtifactPopover,
  startReplay,
  openForkModal,
  submitFork,
  openDiffModal,
  runDiffCompare,
  stepMetricsLine,
  skipStepTooltip,
  stepPillTitle,
  copyStepOutput,
  toggleTokenBatchExpand,
  addStep,
  removeStep,
  toggleStep,
  onDragStart,
  onDragOver,
  onDrop,
  saveWorkflow,
  triggerRun,
  cancelRun,
  pendingDelete,
  confirmDelete,
  doDeleteWorkflow,
  clearRunEvents,
  createBlank,
  createFromTemplate,
  fileDropActive,
  dropMsg,
  dropError,
  onFileDragEnter,
  onFileDragOver,
  onFileDragLeave,
  onFileDrop,
  isPlaceholderError,
  eventIcon,
  eventLabel,
  retryEntry,
  dismissEntry,
} = useWorkflowsViewState(
  { id: toRef(props, 'id'), runId: toRef(props, 'runId') },
  router,
)

defineExpose({
  liveEvents,
})
</script>

<style scoped>
.page-enter-active, .page-leave-active { transition: opacity 0.15s ease, transform 0.15s ease; }
.page-enter-from { opacity: 0; transform: translateX(10px); }
.page-leave-to { opacity: 0; transform: translateX(-10px); }

.step-expand-enter-active, .step-expand-leave-active { transition: all 0.2s ease; overflow: hidden; }
.step-expand-enter-from, .step-expand-leave-to { max-height: 0; opacity: 0; }
.step-expand-enter-to, .step-expand-leave-from { max-height: 600px; opacity: 1; }

.slide-in-enter-active, .slide-in-leave-active { transition: all 0.25s ease; }
.slide-in-enter-from, .slide-in-leave-to { transform: translateX(100%); opacity: 0; }

.overlay-enter-active, .overlay-leave-active { transition: opacity 0.2s ease; }
.overlay-enter-from, .overlay-leave-to { opacity: 0; }
</style>
