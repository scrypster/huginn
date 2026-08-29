<template>
  <div
    class="rounded-2xl border transition-colors duration-200 overflow-hidden"
    style="background:rgba(22,27,34,0.8)"
    :style="{ borderColor: focused ? 'rgba(88,166,255,0.4)' : 'rgba(48,54,61,1)' }"
  >
    <div
      v-if="unknownMentionHint"
      class="px-3 pt-2 text-[11px]"
      data-testid="unknown-mention-hint"
      style="color:rgba(227,179,65,0.92)"
    >
      {{ unknownMentionHint }}
    </div>
    <div ref="editorEl" class="editor-content" />
    <ChatToolbar v-if="editorInstance" :editor="editorInstance" @send="handleSend" />
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, watch } from 'vue'
import { useEditor } from './useEditor'
import ChatToolbar from './ChatToolbar.vue'
import type { Editor } from '@tiptap/vue-3'
import { api } from '../../composables/useApi'
import { dropUnknownLeadMention } from './mentionSuggestions'

const props = defineProps<{
  disabled?: boolean
  placeholder?: string
  /** Active-space roster. Undefined = standalone (list every agent). */
  memberNames?: string[]
}>()

const emit = defineEmits<{
  (e: 'send', content: string): void
  (e: 'unknown-mention', name: string): void
}>()

const editorEl = ref<HTMLElement>()
const focused = ref(false)
const agents = ref<Array<Record<string, unknown>>>([])
const unknownMentionHint = ref('')
let unknownMentionTimer: ReturnType<typeof setTimeout> | null = null

// agentsReady resolves once the roster fetch below has settled (success or
// exhausted retries). The @ mention suggestion's items() awaits it before
// its first lookup — see useEditor.ts — so a user who types "@" the instant
// the composer mounts (autofocus) gets the real roster once it lands
// instead of a picker that opens empty and never updates (tiptap-suggestion
// only re-runs items() on the next keystroke, so a stale-empty open would
// otherwise stick until the user typed more).
const AGENTS_FETCH_MAX_ATTEMPTS = 3
const AGENTS_FETCH_RETRY_DELAY_MS = 400
let resolveAgentsReady!: () => void
const agentsReady = new Promise<void>(resolve => { resolveAgentsReady = resolve })

onMounted(async () => {
  for (let attempt = 1; attempt <= AGENTS_FETCH_MAX_ATTEMPTS; attempt++) {
    try {
      const data = await api.agents.list()
      agents.value = Array.isArray(data) ? data : []
      break
    } catch {
      // Transient failure (e.g. a 503 while the backend is warming up) would
      // otherwise leave agents.value permanently empty for this page load —
      // the mention picker would then never have anyone to suggest.
      if (attempt < AGENTS_FETCH_MAX_ATTEMPTS) {
        await new Promise(r => setTimeout(r, AGENTS_FETCH_RETRY_DELAY_MS))
      }
    }
  }
  resolveAgentsReady()
})

// Pass a reactive ref to useEditor so the Placeholder extension re-reads
// the value every render. Without this the placeholder set at editor init
// gets baked in and never updates when activeSpace switches.
const placeholderRef = computed(() => props.placeholder)
const memberNamesRef = computed(() => props.memberNames)
const { editor, init, getMarkdown, clear, focus, setText, isEmpty } = useEditor({
  agents,
  onSend: handleSend,
  placeholder: props.placeholder ?? 'Message huginn...',
  placeholderRef,
  memberNames: memberNamesRef,
  agentsReady,
})

const editorInstance = computed(() => editor.value as Editor | null)

onMounted(() => {
  if (editorEl.value) {
    init(editorEl.value)
    editor.value?.on('focus', () => { focused.value = true })
    editor.value?.on('blur', () => { focused.value = false })
  }
})

watch(() => props.disabled, (disabled, prevDisabled) => {
  editor.value?.setOptions({ editable: !disabled })
  // When transitioning from disabled → enabled, restore focus so the user can
  // type immediately without needing to click the editor again.
  // setTimeout defers until after all Vue/ProseMirror DOM updates have settled.
  if (prevDisabled && !disabled) {
    setTimeout(() => {
      const dom = editor.value?.view?.dom as HTMLElement | undefined
      dom?.focus()
    }, 0)
  }
})

// Force a no-op transaction when the prop changes so ProseMirror re-runs
// the Placeholder extension's decorations (which now reads from a function
// closure over placeholderRef).
watch(() => props.placeholder, () => {
  const ed = editor.value
  if (!ed) return
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  ;(ed.view as any).dispatch(ed.state.tr)
})

function showUnknownMentionHint(name: string) {
  unknownMentionHint.value = `${name} is not in this channel`
  emit('unknown-mention', name)
  if (unknownMentionTimer) clearTimeout(unknownMentionTimer)
  unknownMentionTimer = setTimeout(() => {
    unknownMentionHint.value = ''
    unknownMentionTimer = null
  }, 6000)
}

function handleSend() {
  if (isEmpty() || props.disabled) return
  const markdown = getMarkdown()
  const { content, dropped } = dropUnknownLeadMention(markdown, props.memberNames)
  if (dropped) showUnknownMentionHint(dropped)
  if (!content.trim()) return
  emit('send', content)
  clear()
  focus()
}

// `editor` (the raw Tiptap instance) is exposed alongside the public API
// mainly so component tests can drive real ProseMirror transactions — jsdom
// doesn't support the DOM input pipeline tiptap relies on for real typing.
defineExpose({ focus, setText, clear, editor: editorInstance })
</script>

<style>
.editor-content .ProseMirror {
  padding: 14px 16px;
  min-height: 42px;
  max-height: 300px;
  overflow-y: auto;
  outline: none;
  font-size: 0.875rem;
  line-height: 1.625;
  color: rgb(230, 237, 243);
  font-family: inherit;
  word-break: break-word;
}

.editor-content .ProseMirror p.is-editor-empty:first-child::before {
  content: attr(data-placeholder);
  color: rgba(139, 148, 158, 0.6);
  pointer-events: none;
  float: left;
  height: 0;
}

.editor-content .ProseMirror .mention {
  color: rgb(88, 166, 255);
  font-weight: 500;
  background: rgba(88, 166, 255, 0.12);
  border-radius: 3px;
  padding: 0 3px;
}

.editor-content .ProseMirror code {
  color: rgb(121, 192, 255);
  background: rgba(110, 118, 129, 0.2);
  border-radius: 3px;
  padding: 1px 4px;
  font-size: 0.75rem;
  font-family: ui-monospace, monospace;
}

.editor-content .ProseMirror pre {
  background: #0d1117;
  border: 1px solid rgba(48, 54, 61, 1);
  border-radius: 10px;
  padding: 12px 16px;
  margin: 8px 0;
  overflow-x: auto;
}

.editor-content .ProseMirror pre code {
  background: transparent;
  color: #e6edf3;
  padding: 0;
  font-size: 0.75rem;
  line-height: 1.625;
}

.editor-content .ProseMirror ul {
  list-style-type: disc;
  padding-left: 1.25rem;
  margin: 4px 0;
}

.editor-content .ProseMirror ol {
  list-style-type: decimal;
  padding-left: 1.25rem;
  margin: 4px 0;
}

.editor-content .ProseMirror li { font-size: 0.875rem; }
.editor-content .ProseMirror li p { margin: 0; }

.editor-content .ProseMirror blockquote {
  border-left: 2px solid rgba(48, 54, 61, 1);
  padding-left: 12px;
  color: rgba(139, 148, 158, 1);
  font-style: italic;
  margin: 4px 0;
}

.editor-content .ProseMirror a.link {
  color: rgb(88, 166, 255);
  text-decoration: underline;
  text-underline-offset: 2px;
}

.editor-content .ProseMirror p { margin: 0; }
.editor-content .ProseMirror p + p { margin-top: 4px; }

.editor-content .ProseMirror::-webkit-scrollbar { width: 4px; }
.editor-content .ProseMirror::-webkit-scrollbar-track { background: transparent; }
.editor-content .ProseMirror::-webkit-scrollbar-thumb {
  background: rgba(48, 54, 61, 1);
  border-radius: 2px;
}
</style>
