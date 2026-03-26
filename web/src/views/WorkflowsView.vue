<template>
  <div class="flex flex-col h-full">

    <!-- ── Grid view (no workflow selected) ── -->
    <Transition name="page" mode="out-in">
      <div v-if="!selectedId" key="grid" class="flex flex-col h-full">

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
            <p class="text-huginn-muted text-sm">{{ search ? 'No workflows match your search' : 'No workflows yet' }}</p>
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

          <button v-if="selectedWorkflow" @click="confirmDelete"
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
                <label class="block text-[11px] font-medium text-huginn-muted mb-1.5 uppercase tracking-wider">Schedule (cron)</label>
                <input v-model="editForm.schedule" placeholder="0 8 * * 1-5"
                  class="w-full bg-huginn-bg border border-huginn-border rounded-lg px-3 py-2 text-sm font-mono text-huginn-text focus:outline-none focus:border-huginn-blue/60 transition-colors"/>
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

            <!-- Steps section -->
            <div>
              <div class="flex items-center justify-between mb-3">
                <h2 class="text-xs font-semibold text-huginn-muted uppercase tracking-wider">Steps</h2>
                <button @click="addStep"
                  data-testid="add-step-btn"
                  class="flex items-center gap-1 text-xs text-huginn-blue hover:text-huginn-blue/80 transition-colors">
                  <svg class="w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5">
                    <line x1="12" y1="5" x2="12" y2="19"/><line x1="5" y1="12" x2="19" y2="12"/>
                  </svg>
                  Add Step
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
                        {{ step.agent ? `@${step.agent}` : 'No agent' }}{{ step.prompt ? ' · ' + step.prompt.slice(0, 60) + (step.prompt.length > 60 ? '…' : '') : '' }}
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
                        <div>
                          <label class="block text-[10px] font-semibold text-huginn-muted/70 uppercase tracking-wider mb-1">Agent</label>
                          <AgentPicker v-model="step.agent!" data-testid="step-agent-input" @select:agent="onAgentSelected(idx, $event)" />
                        </div>
                        <div class="col-span-2">
                          <label class="block text-[10px] font-medium text-huginn-muted mb-1 uppercase tracking-wider">On Failure</label>
                          <select v-model="step.on_failure"
                            class="w-full bg-huginn-bg border border-huginn-border rounded-lg px-3 py-1.5 text-xs text-huginn-text focus:outline-none focus:border-huginn-blue/60 transition-colors">
                            <option value="stop">Stop pipeline</option>
                            <option value="continue">Continue anyway</option>
                          </select>
                        </div>
                      </div>

                      <!-- Agent detail card -->
                      <div v-if="stepAgentDetails[idx]" class="col-span-2 flex flex-wrap gap-1.5 items-center p-2 rounded-lg bg-huginn-surface/50 border border-huginn-border/50">
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
                  Add your first step
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
                <div v-for="(ev, i) in currentRunEvents" :key="i"
                  class="text-[11px] rounded-lg px-3 py-2 font-mono"
                  :class="{
                    'bg-huginn-blue/10 text-huginn-blue': ev.type === 'workflow_started',
                    'bg-huginn-surface text-huginn-text': ev.type === 'workflow_step_complete' && ev.status === 'success',
                    'bg-amber-500/10 text-amber-400': (ev.type === 'workflow_step_complete' && ev.status === 'failed' && isPlaceholderError(ev.error)) || ev.type === 'workflow_cancelled',
                    'bg-red-500/10 text-red-400': ev.type === 'workflow_step_complete' && ev.status === 'failed' && !isPlaceholderError(ev.error),
                    'bg-huginn-green/10 text-huginn-green': ev.type === 'workflow_complete',
                    'bg-red-500/15 text-red-400': ev.type === 'workflow_failed',
                  }">
                  <div class="flex items-center gap-1.5">
                    <span class="opacity-60">{{ eventIcon(ev) }}</span>
                    <span class="truncate">{{ eventLabel(ev) }}</span>
                  </div>
                  <div v-if="ev.error && !isPlaceholderError(ev.error)" class="mt-1 opacity-70 text-[10px] break-words">{{ ev.error }}</div>
                  <div v-if="ev.error && isPlaceholderError(ev.error)" class="mt-1 text-[10px] break-words text-amber-400/80">
                    ⚠ Template placeholder not resolved — check from_step references
                  </div>
                </div>
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
          <div class="absolute inset-0 bg-black/40 backdrop-blur-sm" @click="showHistory = false"/>
          <div class="relative w-96 bg-huginn-bg border-l border-huginn-border flex flex-col shadow-2xl">
            <div class="flex items-center justify-between px-5 py-4 border-b border-huginn-border">
              <h2 class="text-sm font-semibold text-huginn-text">Run History</h2>
              <button @click="showHistory = false" class="text-huginn-muted hover:text-huginn-text transition-colors">
                <svg class="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                  <line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/>
                </svg>
              </button>
            </div>
            <div class="flex-1 overflow-y-auto p-4 space-y-3">
              <div v-if="loadingHistory" class="flex justify-center py-10">
                <div class="w-5 h-5 border border-huginn-border border-t-huginn-blue rounded-full animate-spin"/>
              </div>
              <div v-else-if="!runs.length" class="text-center py-10 text-huginn-muted text-xs">No runs yet</div>
              <div v-else v-for="run in runs" :key="run.id"
                class="bg-huginn-surface border rounded-xl overflow-hidden transition-colors duration-150 cursor-pointer"
                :class="expandedRunId === run.id ? 'border-huginn-blue/30' : 'border-huginn-border'"
                @click="expandedRunId = expandedRunId === run.id ? null : run.id">
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
                    :title="isPlaceholderError(s.error) ? '⚠ Template placeholder not resolved — check from_step references' : s.error">
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
                      <a v-if="s.session_id"
                        :href="`/sessions/${s.session_id}`"
                        @click.stop
                        class="text-[10px] text-huginn-blue/70 hover:text-huginn-blue flex-shrink-0 flex items-center gap-1 transition-colors"
                        title="Open session">
                        <svg class="w-3 h-3" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                          <path d="M18 13v6a2 2 0 01-2 2H5a2 2 0 01-2-2V8a2 2 0 012-2h6M15 3h6v6M10 14L21 3"/>
                        </svg>
                        session
                      </a>
                    </div>
                    <div v-if="s.error" class="mt-1 text-[10px] font-mono break-words"
                      :class="isPlaceholderError(s.error) ? 'text-amber-400/80' : 'text-red-400/80'">
                      {{ isPlaceholderError(s.error) ? '⚠ unresolved template placeholder' : s.error }}
                    </div>
                    <div v-if="s.output" class="mt-1 text-[10px] font-mono text-huginn-muted/70 line-clamp-3 break-words">
                      {{ s.output }}
                    </div>
                  </div>
                  <div v-if="run.error" class="px-4 py-2.5 text-[10px] font-mono text-red-400/80 break-words">
                    {{ run.error }}
                  </div>
                </div>
              </div>
            </div>
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
import { ref, computed, watch, nextTick, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { useWorkflows, type Workflow, type WorkflowStep, type WorkflowTemplate, type WorkflowRun, type WorkflowEvent } from '../composables/useWorkflows'
import { getToken } from '../composables/useApi'
import { useAgents } from '../composables/useAgents'
import AgentPicker from '../components/AgentPicker.vue'
import { remapIndex } from '../utils/remapIndex'

const props = defineProps<{ id?: string }>()
const router = useRouter()

const { workflows, loading, liveEvents, fetchWorkflows, fetchTemplates, createWorkflow, updateWorkflow, deleteWorkflow, triggerWorkflow, cancelWorkflow, fetchWorkflowRuns } = useWorkflows()
const { agents: agentList } = useAgents()

const search = ref('')
const selectedId = ref<string | null>(props.id || null)
const selectedWorkflow = ref<Workflow | null>(null)
const showCreate = ref(false)
const showHistory = ref(false)
const saving = ref(false)
const saveMsg = ref('')
const saveError = ref(false)
const running = ref(false)
const cancelling = ref(false) // optimistic: set immediately on cancel click, cleared on workflow_cancelled WS event
const expandedSteps = ref<Set<number>>(new Set())
const dragFrom = ref<number | null>(null)
const dragOver = ref<number | null>(null)
const runs = ref<WorkflowRun[]>([])
const loadingHistory = ref(false)
const expandedRunId = ref<string | null>(null)
const templates = ref<WorkflowTemplate[]>([])
const loadingTemplates = ref(false)
const eventsRef = ref<HTMLElement | null>(null)

const stepAgentDetails = ref<Record<number, Record<string, unknown>>>({})

const availableSpaces = ref<Array<{id: string, name: string, kind: string}>>([])

const editForm = ref<{
  name: string
  description: string
  enabled: boolean
  schedule: string
  timeout_minutes: number
  tags: string[]
  steps: WorkflowStep[]
  notification: {
    on_success?: boolean
    on_failure?: boolean
    severity?: string
    deliver_to?: Array<{ type: string; space_id?: string }>
  }
}>({
  name: '',
  description: '',
  enabled: false,
  schedule: '',
  timeout_minutes: 0,
  tags: [],
  steps: [],
  notification: { on_success: false, on_failure: true, severity: 'info' },
})

const filteredWorkflows = computed(() => {
  if (!search.value) return workflows.value
  const q = search.value.toLowerCase()
  return workflows.value.filter(w =>
    w.name.toLowerCase().includes(q) ||
    (w.description || '').toLowerCase().includes(q) ||
    (w.tags || []).some(t => t.toLowerCase().includes(q))
  )
})

const currentRunEvents = computed(() => {
  if (!selectedId.value) return []
  return liveEvents.value[selectedId.value] || []
})

onMounted(async () => {
  await fetchWorkflows()
  fetchSpaces()
  if (props.id) openById(props.id)
})

watch(() => props.id, (id) => {
  if (id) openById(id)
  else closeWorkflow()
})

// When a running workflow finishes (any terminal state), clear the cancelling flag.
watch(running, (isRunning) => {
  if (!isRunning) cancelling.value = false
})

watch(showHistory, async (open) => {
  if (open && selectedId.value) {
    expandedRunId.value = null
    loadingHistory.value = true
    runs.value = await fetchWorkflowRuns(selectedId.value)
    loadingHistory.value = false
  } else {
    expandedRunId.value = null
  }
})

watch(showCreate, async (open) => {
  if (open && !templates.value.length) {
    loadingTemplates.value = true
    templates.value = await fetchTemplates()
    loadingTemplates.value = false
  }
})

watch(currentRunEvents, async () => {
  await nextTick()
  if (eventsRef.value) {
    eventsRef.value.scrollTop = eventsRef.value.scrollHeight
  }
})

async function fetchSpaces() {
  try {
    const token = getToken()
    const data = await fetch('/api/v1/spaces', {
      headers: { Authorization: `Bearer ${token}` }
    }).then(r => r.json())
    availableSpaces.value = Array.isArray(data) ? data : []
  } catch { /* ignore */ }
}

function onAgentSelected(stepIdx: number, agent: Record<string, unknown>) {
  stepAgentDetails.value[stepIdx] = agent
}

// Clear stale agent detail card when the agent field is manually cleared
watch(
  () => editForm.value.steps.map(s => s.agent),
  (agents) => {
    agents.forEach((agent, idx) => {
      if (!agent && stepAgentDetails.value[idx]) {
        const next = { ...stepAgentDetails.value }
        delete next[idx]
        stepAgentDetails.value = next
      }
    })
  },
  { deep: false }
)

function addStepInput(step: WorkflowStep) {
  if (!step.inputs) step.inputs = []
  step.inputs.push({ from_step: '', as: '' })
}

function removeStepInput(step: WorkflowStep, idx: number) {
  step.inputs?.splice(idx, 1)
}

function addWorkflowDeliveryTarget() {
  if (!editForm.value.notification) editForm.value.notification = {}
  if (!editForm.value.notification.deliver_to) editForm.value.notification.deliver_to = []
  editForm.value.notification.deliver_to.push({ type: 'inbox' })
}

function removeWorkflowDeliveryTarget(idx: number) {
  editForm.value.notification?.deliver_to?.splice(idx, 1)
}

function addStepDeliveryTarget(step: WorkflowStep) {
  if (!step.notify) step.notify = {}
  if (!step.notify.deliver_to) step.notify.deliver_to = []
  step.notify.deliver_to.push({ type: 'inbox' })
}

function toggleStepNotify(step: WorkflowStep, enabled: boolean) {
  if (enabled) {
    step.notify = { on_failure: true }
  } else {
    step.notify = undefined
  }
}

function openWorkflow(wf: Workflow) {
  selectedId.value = wf.id
  selectedWorkflow.value = wf
  editForm.value = {
    name: wf.name,
    description: wf.description || '',
    enabled: wf.enabled,
    schedule: wf.schedule || '',
    timeout_minutes: wf.timeout_minutes ?? 0,
    tags: [...(wf.tags || [])],
    steps: wf.steps.map(s => ({
      ...s,
      inputs: s.inputs ? s.inputs.map(inp => ({ ...inp })) : [],
      notify: s.notify
        ? {
            ...s.notify,
            deliver_to: s.notify.deliver_to ? s.notify.deliver_to.map(d => ({ ...d })) : undefined,
          }
        : undefined,
    })),
    notification: wf.notification
      ? {
          ...wf.notification,
          deliver_to: wf.notification.deliver_to ? wf.notification.deliver_to.map(d => ({ ...d })) : [],
        }
      : { on_success: false, on_failure: true, severity: 'info', deliver_to: [] },
  }
  // Pre-populate agent detail cards for steps that already have an agent set.
  // AgentPicker only fires select:agent on user interaction, so we seed details here
  // using the shared agents list so the detail card is visible without re-picking.
  const details: Record<number, Record<string, unknown>> = {}
  editForm.value.steps.forEach((s, idx) => {
    if (s.agent) {
      const found = agentList.value.find(a => a.name === s.agent)
      if (found) details[idx] = found as Record<string, unknown>
    }
  })
  stepAgentDetails.value = details
  expandedSteps.value = new Set()
  router.push(`/workflows/${wf.id}`)
}

function openById(id: string) {
  const wf = workflows.value.find(w => w.id === id)
  if (wf) openWorkflow(wf)
  else selectedId.value = id
}

function closeWorkflow() {
  selectedId.value = null
  selectedWorkflow.value = null
  router.push('/workflows')
}

function addStep() {
  const pos = editForm.value.steps.length
  editForm.value.steps.push({
    name: '',
    agent: '',
    prompt: '',
    connections: {},
    vars: {},
    position: pos,
    on_failure: 'stop',
    inputs: [],
  })
  expandedSteps.value = new Set([...expandedSteps.value, pos])
}

function removeStep(idx: number) {
  editForm.value.steps.splice(idx, 1)
  editForm.value.steps.forEach((s, i) => { s.position = i })
  const newExpanded = new Set<number>()
  for (const n of expandedSteps.value) {
    if (n < idx) newExpanded.add(n)
    else if (n > idx) newExpanded.add(n - 1)
  }
  expandedSteps.value = newExpanded
  // Clean up agent details for removed step
  const newDetails: Record<number, Record<string, unknown>> = {}
  for (const key in stepAgentDetails.value) {
    const k = Number(key)
    if (k < idx) newDetails[k] = stepAgentDetails.value[k]!
    else if (k > idx) newDetails[k - 1] = stepAgentDetails.value[k]!
  }
  stepAgentDetails.value = newDetails
}

function toggleStep(idx: number) {
  const next = new Set(expandedSteps.value)
  if (next.has(idx)) next.delete(idx)
  else next.add(idx)
  expandedSteps.value = next
}

function onDragStart(idx: number, e: DragEvent) {
  dragFrom.value = idx
  if (e.dataTransfer) e.dataTransfer.effectAllowed = 'move'
}

function onDragOver(idx: number) {
  dragOver.value = idx
}

function onDrop(toIdx: number) {
  if (dragFrom.value === null || dragFrom.value === toIdx) {
    dragFrom.value = null
    dragOver.value = null
    return
  }
  const fromIdx = dragFrom.value
  const steps = [...editForm.value.steps]
  const [moved] = steps.splice(fromIdx, 1)
  if (!moved) return
  steps.splice(toIdx, 0, moved)
  steps.forEach((s, i) => { s.position = i })
  editForm.value.steps = steps

  // Remap expandedSteps indices to match new order
  const newExpanded = new Set<number>()
  for (const n of expandedSteps.value) {
    const remapped = remapIndex(n, fromIdx, toIdx)
    if (remapped !== null) newExpanded.add(remapped)
  }
  expandedSteps.value = newExpanded

  // Remap stepAgentDetails indices to match new order
  const newDetails: Record<number, Record<string, unknown>> = {}
  for (const key in stepAgentDetails.value) {
    const k = Number(key)
    const remapped = remapIndex(k, fromIdx, toIdx)
    if (remapped !== null) newDetails[remapped] = stepAgentDetails.value[k]!
  }
  stepAgentDetails.value = newDetails

  dragFrom.value = null
  dragOver.value = null
}


async function saveWorkflow() {
  if (!selectedId.value || !selectedWorkflow.value) return
  saving.value = true
  saveError.value = false
  saveMsg.value = ''
  try {
    const wf: Workflow = {
      ...selectedWorkflow.value,
      ...editForm.value,
      steps: editForm.value.steps.map((s, i) => ({ ...s, position: i })),
    }
    const updated = await updateWorkflow(selectedId.value, wf)
    selectedWorkflow.value = updated
  } catch (e) {
    saveError.value = true
    saveMsg.value = e instanceof Error ? e.message : 'Failed to save workflow. Please try again.'
  } finally {
    saving.value = false
  }
}

async function triggerRun() {
  if (!selectedId.value || running.value) return
  running.value = true
  cancelling.value = false
  try {
    await triggerWorkflow(selectedId.value)
  } catch {
    // error handled by composable
  } finally {
    setTimeout(() => { running.value = false }, 1000)
  }
}

async function cancelRun() {
  if (!selectedId.value || cancelling.value) return
  cancelling.value = true
  try {
    await cancelWorkflow(selectedId.value)
    // cancelling stays true until the workflow_cancelled WS event arrives and
    // fetchWorkflows() updates the run list, at which point running becomes false.
  } catch {
    cancelling.value = false
  }
}

const pendingDelete = ref<{ id: string; name: string } | null>(null)

function confirmDelete() {
  if (!selectedWorkflow.value) return
  pendingDelete.value = selectedWorkflow.value
}

async function doDeleteWorkflow() {
  if (!pendingDelete.value) return
  await deleteWorkflow(pendingDelete.value.id)
  pendingDelete.value = null
  closeWorkflow()
}

function clearRunEvents() {
  if (selectedId.value) {
    delete liveEvents.value[selectedId.value]
  }
}

async function createBlank() {
  showCreate.value = false
  const wf = await createWorkflow({
    name: 'New Workflow',
    enabled: false,
    schedule: '',
    steps: [],
  })
  openWorkflow(wf)
}

async function createFromTemplate(tpl: WorkflowTemplate) {
  showCreate.value = false
  const wf = await createWorkflow({
    name: tpl.workflow.name,
    description: tpl.workflow.description,
    enabled: false,
    schedule: tpl.workflow.schedule,
    steps: tpl.workflow.steps,
    notification: tpl.workflow.notification,
  })
  openWorkflow(wf)
}

// isPlaceholderError returns true when an error string indicates that the
// step failed due to unresolved template placeholders (a config error, not a
// runtime error). These are shown in amber rather than red.
function isPlaceholderError(error?: string): boolean {
  return !!error && error.includes('unresolved template placeholders')
}

function eventIcon(ev: WorkflowEvent): string {
  if (ev.type === 'workflow_step_complete' && ev.status === 'failed' && isPlaceholderError(ev.error)) {
    return '⚠'
  }
  switch (ev.type) {
    case 'workflow_started': return '▶'
    case 'workflow_step_complete': return ev.status === 'success' ? '✓' : '✗'
    case 'workflow_complete': return '✓'
    case 'workflow_failed': return '✗'
    case 'workflow_cancelled': return '⊘'
    default: return '·'
  }
}

function eventLabel(ev: WorkflowEvent): string {
  switch (ev.type) {
    case 'workflow_started': return `Started: ${ev.workflow_name || 'workflow'}`
    case 'workflow_step_complete': return `Step ${ev.position}: ${ev.slug || 'done'} [${ev.status}]`
    case 'workflow_complete': return 'Workflow completed'
    case 'workflow_failed': return 'Workflow failed'
    case 'workflow_cancelled': return 'Workflow cancelled by user'
    default: return ev.type
  }
}
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
