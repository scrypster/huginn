<template>
  <div
    class="rounded-2xl border transition-colors duration-200 overflow-hidden"
    style="background:rgba(22,27,34,0.8)"
    :style="{ borderColor: focused ? 'rgba(88,166,255,0.4)' : 'rgba(48,54,61,1)' }"
  >
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

const props = defineProps<{
  disabled?: boolean
  placeholder?: string
}>()

const emit = defineEmits<{
  (e: 'send', content: string): void
}>()

const editorEl = ref<HTMLElement>()
const focused = ref(false)
const agents = ref<Array<Record<string, unknown>>>([])

onMounted(async () => {
  try { agents.value = await api.agents.list() } catch { /* ignore */ }
})

const { editor, init, getMarkdown, clear, focus, isEmpty } = useEditor({
  agents,
  onSend: handleSend,
  placeholder: props.placeholder ?? 'Message huginn...',
})

const editorInstance = computed(() => editor.value as Editor | null)

onMounted(() => {
  if (editorEl.value) {
    init(editorEl.value)
    editor.value?.on('focus', () => { focused.value = true })
    editor.value?.on('blur', () => { focused.value = false })
  }
})

watch(() => props.disabled, (disabled) => {
  editor.value?.setOptions({ editable: !disabled })
})

// Update the TipTap placeholder when the prop changes (e.g. switching spaces).
watch(() => props.placeholder, (newPlaceholder) => {
  const ed = editor.value
  if (!ed) return
  const ext = ed.extensionManager.extensions.find(e => e.name === 'placeholder')
  if (ext) {
    ext.options.placeholder = newPlaceholder ?? 'Message huginn...'
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    ;(ed.view as any).dispatch(ed.state.tr) // trigger a no-op transaction to re-render decorations
  }
})

function handleSend() {
  if (isEmpty() || props.disabled) return
  const markdown = getMarkdown()
  if (!markdown.trim()) return
  emit('send', markdown)
  clear()
  focus()
}

defineExpose({ focus })
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
