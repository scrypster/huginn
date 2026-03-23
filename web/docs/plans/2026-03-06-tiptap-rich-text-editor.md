# Tiptap Rich Text Editor Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Replace the textarea + mirror overlay input in ChatView with a Tiptap v2 rich text editor that supports inline code blocks with syntax highlighting, lists, @mention autocomplete, and markdown serialization.

**Architecture:** Tiptap v2 (ProseMirror-based) with `@tiptap/vue-3`. A new `ChatEditor` component family lives in `src/components/ChatEditor/` and is dropped into `ChatView.vue` replacing the current textarea, mirror div, and all associated state. The editor serializes to markdown at send time via `tiptap-markdown`; the backend receives the same markdown strings it always has.

**Tech Stack:** `@tiptap/vue-3`, `@tiptap/starter-kit`, `@tiptap/extension-mention`, `@tiptap/extension-placeholder`, `@tiptap/extension-link`, `@tiptap/extension-code-block-lowlight`, `tiptap-markdown`, `lowlight`, `tippy.js`

---

## Context for the implementer

The project is a Vue 3 + TypeScript + Tailwind CSS frontend at `web/src/`. The current chat input (`web/src/views/ChatView.vue`) is a `<textarea>` with a transparent-text trick and a mirror `<div>` behind it to highlight `@mentions` and inline code. There is also a separate "code block" `<textarea>` section below it. All of this gets replaced.

The `ChatView.vue` sends messages over a WebSocket (`wsRef`) as `{ type: 'chat', content: markdownString, session_id }`. The new editor must produce the same markdown string at send time — nothing else in the backend changes.

The existing `@mention` feature queries `/api/v1/agents` and shows a dropdown. The Tiptap Mention extension replaces this cleanly.

There are no frontend unit tests in this project. "Testing" means: `npm run build` produces no TypeScript errors, and manual verification in the browser.

Build command: `cd web && npx vite build --outDir ../internal/server/dist`
Dev server: `cd web && npm run dev` (proxies API to localhost:8080)

---

## Task 1: Install packages

**Files:**
- Modify: `web/package.json`

**Step 1: Install Tiptap + dependencies**

```bash
cd /Users/mjbonanno/github.com/scrypster/huginn/web
npm install @tiptap/vue-3 @tiptap/pm @tiptap/starter-kit \
  @tiptap/extension-mention \
  @tiptap/extension-placeholder \
  @tiptap/extension-link \
  @tiptap/extension-code-block-lowlight \
  tiptap-markdown \
  lowlight \
  tippy.js
```

**Step 2: Remove packages that tiptap replaces**

```bash
npm uninstall highlight.js marked
```

Note: `highlight.js` and `marked` are currently used in ChatView.vue for rendering received messages AND for the input mirror. Tiptap replaces the input side. The received message rendering also needs updating (Task 7 handles this — we'll use a simple marked replacement or keep a minimal highlight.js just for rendering).

Actually: Keep `highlight.js` and `marked` for now. We will remove them in Task 7 after the rendered markdown side is also migrated. Don't remove them in this task.

Revised Step 2: No uninstall needed yet.

**Step 3: Verify install**

```bash
cd /Users/mjbonanno/github.com/scrypster/huginn/web
npx vite build --outDir ../internal/server/dist 2>&1 | tail -5
```

Expected: build still succeeds (nothing imported yet).

**Step 4: Commit**

```bash
cd /Users/mjbonanno/github.com/scrypster/huginn/web
git add package.json package-lock.json
git commit -m "chore: install tiptap + tiptap-markdown + lowlight + tippy"
```

---

## Task 2: Create MentionList.vue

This is the dropdown popup that appears when typing `@`. It receives a list of matching agents and a `command` callback from Tiptap's suggestion plugin.

**Files:**
- Create: `web/src/components/ChatEditor/MentionList.vue`

**Step 1: Create the component directory**

```bash
mkdir -p /Users/mjbonanno/github.com/scrypster/huginn/web/src/components/ChatEditor
```

**Step 2: Create MentionList.vue**

```vue
<!-- web/src/components/ChatEditor/MentionList.vue -->
<template>
  <div
    v-if="items.length > 0"
    class="rounded-xl border border-huginn-border overflow-hidden"
    style="background:rgba(22,27,34,0.97);min-width:180px"
  >
    <button
      v-for="(item, i) in items"
      :key="String(item.name)"
      @mousedown.prevent="selectItem(i)"
      class="w-full flex items-center gap-2.5 px-3 py-2 text-left text-xs transition-colors duration-100"
      :class="i === selectedIndex ? 'bg-huginn-blue/20 text-huginn-text' : 'text-huginn-muted hover:bg-huginn-surface'"
    >
      <div
        class="w-5 h-5 rounded-md flex items-center justify-center text-[10px] font-bold text-white flex-shrink-0"
        :style="{ background: (item.color as string) || '#58a6ff' }"
      >
        {{ (item.icon as string) || String(item.name)?.[0]?.toUpperCase() }}
      </div>
      <span>{{ item.name }}</span>
      <span v-if="i === 0" class="ml-auto text-[10px] text-huginn-muted/50">Tab</span>
    </button>
  </div>
</template>

<script setup lang="ts">
import { ref, watch } from 'vue'

const props = defineProps<{
  items: Array<Record<string, unknown>>
  command: (item: { id: string; label: string }) => void
}>()

const selectedIndex = ref(0)

watch(() => props.items, () => { selectedIndex.value = 0 })

function selectItem(index: number) {
  const item = props.items[index]
  if (item) {
    props.command({ id: String(item.name), label: String(item.name) })
  }
}

// Called by the parent (via template ref) for keyboard nav
function onKeyDown({ event }: { event: KeyboardEvent }) {
  if (event.key === 'ArrowUp') {
    selectedIndex.value = (selectedIndex.value - 1 + props.items.length) % props.items.length
    return true
  }
  if (event.key === 'ArrowDown' || event.key === 'Tab') {
    selectedIndex.value = (selectedIndex.value + 1) % props.items.length
    return true
  }
  if (event.key === 'Enter') {
    selectItem(selectedIndex.value)
    return true
  }
  return false
}

defineExpose({ onKeyDown })
</script>
```

**Step 3: Verify no TS errors**

```bash
cd /Users/mjbonanno/github.com/scrypster/huginn/web
npx vue-tsc --noEmit 2>&1 | head -20
```

Expected: no errors for this file (it may show errors in ChatView.vue for the refs we haven't wired yet — that's fine).

**Step 4: Commit**

```bash
git add web/src/components/ChatEditor/MentionList.vue
git commit -m "feat: add MentionList dropdown component for @mention autocomplete"
```

---

## Task 3: Create useEditor.ts composable

This composable creates and configures the Tiptap editor instance. It is the heart of the implementation.

**Files:**
- Create: `web/src/components/ChatEditor/useEditor.ts`

**Step 1: Create useEditor.ts**

```typescript
// web/src/components/ChatEditor/useEditor.ts
import { ref, onBeforeUnmount, type Ref } from 'vue'
import { Editor, VueRenderer } from '@tiptap/vue-3'
import StarterKit from '@tiptap/starter-kit'
import Placeholder from '@tiptap/extension-placeholder'
import Link from '@tiptap/extension-link'
import Mention from '@tiptap/extension-mention'
import CodeBlockLowlight from '@tiptap/extension-code-block-lowlight'
import { Markdown } from 'tiptap-markdown'
import { common, createLowlight } from 'lowlight'
import tippy from 'tippy.js'
import type { Instance as TippyInstance } from 'tippy.js'
import MentionList from './MentionList.vue'

const lowlight = createLowlight(common)

export function useEditor(options: {
  agents: Ref<Array<Record<string, unknown>>>
  onSend: () => void
  placeholder?: string
}) {
  const editor = ref<Editor | null>(null)

  function createMentionExtension() {
    return Mention.configure({
      HTMLAttributes: { class: 'mention' },
      suggestion: {
        items: ({ query }: { query: string }) =>
          options.agents.value
            .filter(a => String(a.name).toLowerCase().startsWith(query.toLowerCase()))
            .slice(0, 6),

        render: () => {
          let component: VueRenderer
          let popup: TippyInstance[]

          return {
            onStart(props: Record<string, unknown>) {
              component = new VueRenderer(MentionList, {
                props,
                editor: props.editor as Editor,
              })

              if (!props.clientRect) return

              popup = tippy('body', {
                getReferenceClientRect: props.clientRect as () => DOMRect,
                appendTo: () => document.body,
                content: component.element,
                showOnCreate: true,
                interactive: true,
                trigger: 'manual',
                placement: 'top-start',
              })
            },
            onUpdate(props: Record<string, unknown>) {
              component.updateProps(props)
              if (!props.clientRect) return
              popup[0].setProps({
                getReferenceClientRect: props.clientRect as () => DOMRect,
              })
            },
            onKeyDown(props: Record<string, unknown>) {
              if ((props.event as KeyboardEvent).key === 'Escape') {
                popup[0].hide()
                return true
              }
              return (component.ref as { onKeyDown: (p: unknown) => boolean } | null)
                ?.onKeyDown(props) ?? false
            },
            onExit() {
              popup[0].destroy()
              component.destroy()
            },
          }
        },
      },
    })
  }

  function init(element: HTMLElement) {
    editor.value = new Editor({
      element,
      extensions: [
        StarterKit.configure({
          // Disable the default code block in favor of CodeBlockLowlight
          codeBlock: false,
          // We want hard breaks on Shift+Enter, not paragraph breaks
          hardBreak: false,
        }),
        CodeBlockLowlight.configure({
          lowlight,
          defaultLanguage: 'plaintext',
        }),
        Placeholder.configure({
          placeholder: options.placeholder ?? 'Message huginn...',
        }),
        Link.configure({
          openOnClick: false,
          HTMLAttributes: { class: 'link' },
        }),
        Markdown.configure({
          html: false,
          tightLists: true,
          bulletListMarker: '-',
          transformPastedText: true,
          transformCopiedText: true,
        }),
        createMentionExtension(),
      ],
      editorProps: {
        handleKeyDown(view, event) {
          // Enter = send (unless inside code block or shift is held)
          if (event.key === 'Enter' && !event.shiftKey) {
            const { state } = view
            const { $from } = state.selection
            // If inside a code block, let default behavior (newline) happen
            if ($from.parent.type.name === 'codeBlock') return false
            // Otherwise send
            event.preventDefault()
            options.onSend()
            return true
          }
          // Shift+Enter = hard break (new line in paragraph)
          if (event.key === 'Enter' && event.shiftKey) {
            view.dispatch(
              view.state.tr.replaceSelectionWith(
                view.state.schema.nodes.hardBreak.create()
              ).scrollIntoView()
            )
            return true
          }
          return false
        },
      },
      autofocus: true,
    })
  }

  function getMarkdown(): string {
    if (!editor.value) return ''
    return (editor.value.storage as { markdown: { getMarkdown: () => string } })
      .markdown.getMarkdown()
  }

  function clear() {
    editor.value?.commands.clearContent(true)
  }

  function focus() {
    editor.value?.commands.focus()
  }

  function isEmpty(): boolean {
    return editor.value?.isEmpty ?? true
  }

  onBeforeUnmount(() => {
    editor.value?.destroy()
  })

  return { editor, init, getMarkdown, clear, focus, isEmpty }
}
```

**Step 2: Verify**

```bash
cd /Users/mjbonanno/github.com/scrypster/huginn/web
npx vue-tsc --noEmit 2>&1 | head -30
```

Expected: may show errors in ChatView.vue (old refs), but no errors in useEditor.ts itself.

**Step 3: Commit**

```bash
git add web/src/components/ChatEditor/useEditor.ts
git commit -m "feat: add useEditor composable with tiptap + mention + code-block-lowlight + markdown"
```

---

## Task 4: Create ChatToolbar.vue

The formatting toolbar. Minimal, developer-focused.

**Files:**
- Create: `web/src/components/ChatEditor/ChatToolbar.vue`

**Step 1: Create ChatToolbar.vue**

```vue
<!-- web/src/components/ChatEditor/ChatToolbar.vue -->
<template>
  <div class="flex items-center gap-0.5 px-2 py-1.5 border-t border-huginn-border/50">
    <!-- Text formatting group -->
    <ToolbarBtn :active="editor.isActive('bold')" @click="editor.chain().focus().toggleBold().run()" title="Bold (⌘B)">
      <svg class="w-3.5 h-3.5" viewBox="0 0 24 24" fill="currentColor"><path d="M6 4h8a4 4 0 0 1 4 4 4 4 0 0 1-4 4H6z"/><path d="M6 12h9a4 4 0 0 1 4 4 4 4 0 0 1-4 4H6z"/></svg>
    </ToolbarBtn>
    <ToolbarBtn :active="editor.isActive('italic')" @click="editor.chain().focus().toggleItalic().run()" title="Italic (⌘I)">
      <svg class="w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round"><line x1="19" y1="4" x2="10" y2="4"/><line x1="14" y1="20" x2="5" y2="20"/><line x1="15" y1="4" x2="9" y2="20"/></svg>
    </ToolbarBtn>
    <ToolbarBtn :active="editor.isActive('code')" @click="editor.chain().focus().toggleCode().run()" title="Inline code (⌘E)">
      <svg class="w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><polyline points="16 18 22 12 16 6"/><polyline points="8 6 2 12 8 18"/></svg>
    </ToolbarBtn>

    <!-- Separator -->
    <div class="w-px h-3.5 bg-huginn-border mx-1 flex-shrink-0" />

    <!-- Lists -->
    <ToolbarBtn :active="editor.isActive('bulletList')" @click="editor.chain().focus().toggleBulletList().run()" title="Bullet list">
      <svg class="w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><line x1="9" y1="6" x2="20" y2="6"/><line x1="9" y1="12" x2="20" y2="12"/><line x1="9" y1="18" x2="20" y2="18"/><circle cx="4" cy="6" r="1" fill="currentColor"/><circle cx="4" cy="12" r="1" fill="currentColor"/><circle cx="4" cy="18" r="1" fill="currentColor"/></svg>
    </ToolbarBtn>
    <ToolbarBtn :active="editor.isActive('orderedList')" @click="editor.chain().focus().toggleOrderedList().run()" title="Numbered list">
      <svg class="w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><line x1="10" y1="6" x2="21" y2="6"/><line x1="10" y1="12" x2="21" y2="12"/><line x1="10" y1="18" x2="21" y2="18"/><path d="M4 6h1v4"/><path d="M4 10H6"/><path d="M6 18H4c0-1 2-2 2-3s-1-1.5-2-1"/></svg>
    </ToolbarBtn>
    <ToolbarBtn :active="editor.isActive('blockquote')" @click="editor.chain().focus().toggleBlockquote().run()" title="Blockquote">
      <svg class="w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><path d="M3 21c3 0 7-1 7-8V5c0-1.25-.756-2.017-2-2H4c-1.25 0-2 .75-2 1.972V11c0 1.25.75 2 2 2 1 0 1 0 1 1v1c0 1-1 2-2 2s-1 .008-1 1.031V20c0 1 0 1 1 1z"/><path d="M15 21c3 0 7-1 7-8V5c0-1.25-.757-2.017-2-2h-4c-1.25 0-2 .75-2 1.972V11c0 1.25.75 2 2 2h.75c0 2.25.25 4-2.75 4v3c0 1 0 1 1 1z"/></svg>
    </ToolbarBtn>

    <!-- Separator -->
    <div class="w-px h-3.5 bg-huginn-border mx-1 flex-shrink-0" />

    <!-- Code block -->
    <ToolbarBtn :active="editor.isActive('codeBlock')" @click="editor.chain().focus().toggleCodeBlock().run()" title="Code block (⌘⇧E)">
      <svg class="w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><polyline points="16 18 22 12 16 6"/><polyline points="8 6 2 12 8 18"/><line x1="12" y1="3" x2="12" y2="21"/></svg>
    </ToolbarBtn>

    <!-- Right side hint -->
    <span class="ml-auto text-[11px]" style="color:rgba(139,148,158,0.4)">⏎ send &nbsp;·&nbsp; ⇧⏎ newline</span>
  </div>
</template>

<script setup lang="ts">
import { defineComponent, h } from 'vue'
import type { Editor } from '@tiptap/vue-3'

const props = defineProps<{ editor: Editor }>()

// Local helper component for toolbar buttons
const ToolbarBtn = defineComponent({
  props: { active: Boolean, title: String },
  emits: ['click'],
  setup(p, { slots, emit }) {
    return () => h('button', {
      type: 'button',
      title: p.title,
      onMousedown: (e: MouseEvent) => { e.preventDefault(); emit('click') },
      class: [
        'p-1.5 rounded transition-all duration-100 flex items-center justify-center',
        p.active
          ? 'bg-huginn-blue/20 text-huginn-blue'
          : 'text-huginn-muted hover:text-huginn-text hover:bg-huginn-surface',
      ],
    }, slots.default?.())
  },
})
</script>
```

**Step 2: Verify**

```bash
cd /Users/mjbonanno/github.com/scrypster/huginn/web
npx vue-tsc --noEmit 2>&1 | head -20
```

**Step 3: Commit**

```bash
git add web/src/components/ChatEditor/ChatToolbar.vue
git commit -m "feat: add ChatToolbar with bold/italic/code/lists/blockquote/code-block buttons"
```

---

## Task 5: Create ChatEditor.vue

The main editor component. Replaces the textarea + mirror + code block section in ChatView.

**Files:**
- Create: `web/src/components/ChatEditor/ChatEditor.vue`
- Create: `web/src/components/ChatEditor/index.ts` (barrel export)

**Step 1: Create ChatEditor.vue**

```vue
<!-- web/src/components/ChatEditor/ChatEditor.vue -->
<template>
  <div
    class="rounded-2xl border transition-colors duration-200 overflow-hidden"
    style="background:rgba(22,27,34,0.8)"
    :style="{ borderColor: focused ? 'rgba(88,166,255,0.4)' : 'rgba(48,54,61,1)' }"
  >
    <!-- Editor content area -->
    <div ref="editorEl" class="editor-content" />

    <!-- Toolbar -->
    <ChatToolbar v-if="editorInstance" :editor="editorInstance" />
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
}>()

const emit = defineEmits<{
  (e: 'send', content: string): void
}>()

const editorEl = ref<HTMLElement>()
const focused = ref(false)
const agents = ref<Array<Record<string, unknown>>>([])

// Load agents for @mention autocomplete
onMounted(async () => {
  try { agents.value = await api.agents.list() } catch { /* ignore */ }
})

const { editor, init, getMarkdown, clear, focus, isEmpty } = useEditor({
  agents,
  onSend: handleSend,
  placeholder: 'Message huginn...',
})

// The editor instance for the toolbar (typed)
const editorInstance = computed(() => editor.value as Editor | null)

onMounted(() => {
  if (editorEl.value) {
    init(editorEl.value)

    // Track focus state for border color
    editor.value?.on('focus', () => { focused.value = true })
    editor.value?.on('blur', () => { focused.value = false })
  }
})

// Disable/enable editor when streaming
watch(() => props.disabled, (disabled) => {
  editor.value?.setOptions({ editable: !disabled })
})

function handleSend() {
  if (isEmpty() || props.disabled) return
  const markdown = getMarkdown()
  if (!markdown.trim()) return
  emit('send', markdown)
  clear()
}

// Expose focus for parent to call
defineExpose({ focus })
</script>

<style>
/* ProseMirror editor content area */
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

/* Mention node */
.editor-content .ProseMirror .mention {
  color: rgb(88, 166, 255);
  font-weight: 500;
  background: rgba(88, 166, 255, 0.12);
  border-radius: 3px;
  padding: 0 3px;
}

/* Inline code */
.editor-content .ProseMirror code {
  color: rgb(121, 192, 255);
  background: rgba(110, 118, 129, 0.2);
  border-radius: 3px;
  padding: 1px 4px;
  font-size: 0.75rem;
  font-family: ui-monospace, monospace;
}

/* Code block */
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

/* Lists */
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

.editor-content .ProseMirror li {
  font-size: 0.875rem;
}

.editor-content .ProseMirror li p {
  margin: 0;
}

/* Blockquote */
.editor-content .ProseMirror blockquote {
  border-left: 2px solid rgba(48, 54, 61, 1);
  padding-left: 12px;
  color: rgba(139, 148, 158, 1);
  font-style: italic;
  margin: 4px 0;
}

/* Links */
.editor-content .ProseMirror a.link {
  color: rgb(88, 166, 255);
  text-decoration: underline;
  text-underline-offset: 2px;
}

/* Paragraph spacing */
.editor-content .ProseMirror p {
  margin: 0;
}

.editor-content .ProseMirror p + p {
  margin-top: 4px;
}

/* Scrollbar */
.editor-content .ProseMirror::-webkit-scrollbar {
  width: 4px;
}
.editor-content .ProseMirror::-webkit-scrollbar-track {
  background: transparent;
}
.editor-content .ProseMirror::-webkit-scrollbar-thumb {
  background: rgba(48, 54, 61, 1);
  border-radius: 2px;
}
</style>
```

**Step 2: Create barrel export**

```typescript
// web/src/components/ChatEditor/index.ts
export { default as ChatEditor } from './ChatEditor.vue'
```

**Step 3: Verify build**

```bash
cd /Users/mjbonanno/github.com/scrypster/huginn/web
npx vite build --outDir ../internal/server/dist 2>&1 | tail -10
```

Expected: builds successfully. May warn about chunk size (that's fine).

**Step 4: Commit**

```bash
git add web/src/components/ChatEditor/
git commit -m "feat: add ChatEditor component with tiptap rich text editor"
```

---

## Task 6: Update ChatView.vue — replace textarea with ChatEditor

This is the migration task. We remove the textarea, mirror overlay, code block section, and all their associated state/functions, and replace with ChatEditor.

**Files:**
- Modify: `web/src/views/ChatView.vue`

**Step 1: Read the current ChatView.vue before editing**

(The implementer must read the file first to understand what is being replaced.)

**Step 2: Replace the input area template section**

Find this entire block in the template (the `<!-- ── Input area ──` section):

```html
      <!-- ── Input area ──────────────────────────────────────────── -->
      <div class="px-4 pb-4 flex-shrink-0">
        <!-- @ mention dropdown -->
        ...all the mention dropdown HTML...

        <!-- Main input wrapper -->
        <div class="rounded-2xl border ...">
          <!-- Text row -->
          ...textarea + mirror + send button...

          <!-- Code block section -->
          ...

          <!-- Text after code block -->
          ...

          <!-- Toolbar -->
          ...
        </div>
      </div>
```

Replace it with:

```html
      <!-- ── Input area ──────────────────────────────────────────── -->
      <div class="px-4 pb-4 flex-shrink-0 flex gap-2 items-end">
        <div class="flex-1 min-w-0">
          <ChatEditor
            ref="chatEditorRef"
            :disabled="streaming"
            @send="handleEditorSend"
          />
        </div>
        <button
          @click="triggerSend"
          :disabled="streaming"
          class="mb-0 w-9 h-9 rounded-xl flex items-center justify-center flex-shrink-0 transition-all duration-150"
          :class="!streaming ? 'text-white hover:opacity-80 active:scale-90' : 'text-huginn-muted cursor-not-allowed'"
          :style="!streaming ? 'background:rgba(88,166,255,0.9)' : 'background:rgba(48,54,61,0.5)'"
        >
          <svg class="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round">
            <line x1="22" y1="2" x2="11" y2="13" />
            <polygon points="22 2 15 22 11 13 2 9 22 2" />
          </svg>
        </button>
      </div>
```

**Step 3: Update the script imports**

Add ChatEditor import. Remove: `marked`, `Renderer`, `hljs` imports. Keep `renderMarkdown` for received messages (we'll handle this separately — for now, keep the existing marked/hljs for the message display side).

Add to imports:
```typescript
import { ChatEditor } from '../components/ChatEditor'
```

Keep these imports (still used for rendering received messages):
```typescript
import { marked, Renderer } from 'marked'
import hljs from 'highlight.js'
```

**Step 4: Replace the script input state section**

Remove these refs/state (no longer needed):
- `input`
- `inputFocused`
- `codeBlock`
- `showCodeBlock`
- `codeBlockFocused`
- `inputEl`
- `codeBlockEl`
- `textAfterEl` (if added)
- `mirrorEl`
- `allAgents`
- `mentionOpen`
- `mentionIndex`
- `mentionQuery`
- `mentionStart`
- `canSend` computed
- `highlightedInput` computed
- `mentionMatches` computed
- `agentNames` computed

Remove these functions:
- `detectMention`
- `insertMention`
- `handleInput`
- `handleKeydown`
- `handlePaste`
- `toggleCodeBlock`
- `removeCodeBlock`
- `handleCodeBlockKeydown`
- `autoResize`
- The `onMounted` that loaded agents (now inside ChatEditor)

Add these instead:

```typescript
// ── Chat editor ref ───────────────────────────────────────────────
const chatEditorRef = ref<{ focus: () => void } | null>(null)

function handleEditorSend(markdown: string) {
  const ws = wsRef.value
  if (!ws || streaming.value || !props.sessionId) return
  streaming.value = true

  const msgs = getMessages(props.sessionId)
  msgs.push({ id: `u-${Date.now()}`, role: 'user', content: markdown })
  msgs.push({ id: `h-${Date.now()}`, role: 'assistant', content: '', streaming: true })

  ws.send({ type: 'chat', content: markdown, session_id: props.sessionId })
  scrollToBottom()
}

function triggerSend() {
  // The send button triggers a send event on the editor
  // The editor handles its own isEmpty check; we just need to dispatch
  // Since the editor exposes no "trigger send" method, clicking the
  // external button focuses the editor and the user must press Enter.
  // Alternative: move send button inside ChatEditor (preferred if this feels wrong).
  chatEditorRef.value?.focus()
}
```

Note: The send button outside the editor is awkward. **Recommended alternative:** Move the send button inside ChatEditor.vue (add it to the toolbar area, right-aligned). If you choose this, remove the external button from ChatView.vue and the `triggerSend` function, and instead have ChatEditor emit `send` only when the content is non-empty (which `handleSend` already enforces). Update ChatEditor to show the send button internally in the toolbar:

In `ChatToolbar.vue`, add after the hint span:
```html
<button
  @mousedown.prevent="$emit('send')"
  class="ml-2 w-7 h-7 rounded-xl flex items-center justify-center text-white transition-all duration-150 hover:opacity-80 active:scale-90 flex-shrink-0"
  style="background:rgba(88,166,255,0.9)"
  title="Send (⏎)"
>
  <svg class="w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round">
    <line x1="22" y1="2" x2="11" y2="13" />
    <polygon points="22 2 15 22 11 13 2 9 22 2" />
  </svg>
</button>
```

Add `emits: ['send']` to ChatToolbar and wire it: in ChatEditor.vue, `@send="handleSend"` on the toolbar. Then the input area in ChatView becomes simply:

```html
      <!-- ── Input area ──────────────────────────────────────────── -->
      <div class="px-4 pb-4 flex-shrink-0">
        <ChatEditor
          ref="chatEditorRef"
          :disabled="streaming"
          @send="handleEditorSend"
        />
      </div>
```

**Step 5: Update the session switch watcher**

Change:
```typescript
watch(() => props.sessionId, () => {
  streaming.value = false
  activeToolCalls.value = []
  pendingPermission.value = null
  fetchStatus()
  nextTick(() => inputEl.value?.focus())
})
```

To:
```typescript
watch(() => props.sessionId, () => {
  streaming.value = false
  activeToolCalls.value = []
  pendingPermission.value = null
  fetchStatus()
  nextTick(() => chatEditorRef.value?.focus())
})
```

**Step 6: Update onMounted**

Change:
```typescript
onMounted(() => {
  fetchStatus()
  nextTick(() => inputEl.value?.focus())
})
```

To:
```typescript
onMounted(() => {
  fetchStatus()
  nextTick(() => chatEditorRef.value?.focus())
})
```

**Step 7: Build and verify**

```bash
cd /Users/mjbonanno/github.com/scrypster/huginn/web
npx vite build --outDir ../internal/server/dist 2>&1
```

Expected: builds with no TypeScript errors. May have chunk size warning — acceptable.

**Step 8: Manual verification**

Restart the server binary (`go run ./cmd/huginn serve` or equivalent), open `http://localhost:8080`, and verify:

1. The input area shows the Tiptap editor (single-line that expands, with toolbar below)
2. Typing `@` shows the agent dropdown with keyboard navigation
3. Clicking Bold/Italic/Code/List buttons formats text
4. Typing `` ` `` + text + `` ` `` converts to inline code
5. Typing ` ``` ` on a new line creates a code block with syntax highlighting
6. Pasting a markdown code block (``` ... ```) creates a code block node
7. Enter sends the message; Shift+Enter creates a new line
8. Inside a code block, Enter creates a new line (not send)
9. The sent message shows formatted markdown in the chat bubbles

**Step 9: Commit**

```bash
git add web/src/views/ChatView.vue web/src/components/ChatEditor/
git commit -m "feat: replace textarea with tiptap rich text editor in ChatView"
```

---

## Task 7: Migrate received message rendering from marked+hljs to tiptap-markdown (optional cleanup)

This task cleans up the old `marked` + `highlight.js` dependencies. The `renderMarkdown` function in ChatView.vue currently uses them to render received AI messages. Since we now have `tiptap-markdown` and `lowlight`, we can replace this.

However, **the simplest path is to keep the existing `renderMarkdown` function as-is** for received messages. The `marked` renderer with custom code blocks and copy buttons is working well. The only downside is we ship both `marked`+`highlight.js` AND `tiptap`+`lowlight`.

**Decision: Skip this task for now.** The bundle duplication is ~70KB (gzipped) which is acceptable. This can be done as a separate cleanup task when/if bundle size becomes a concern.

If you choose to do it anyway, the approach is:
- Replace the `renderMarkdown` function with a function that parses markdown using `marked` but highlights code blocks using `lowlight` grammars (they share the same highlight.js grammar format).
- Or: use a standalone `marked` with a `lowlight`-based renderer.
- Since both approaches are complex and the current code works, skip this task.

---

## Task 8: Final build, verify, and clean commit

**Step 1: Full clean build**

```bash
cd /Users/mjbonanno/github.com/scrypster/huginn/web
npx vite build --outDir ../internal/server/dist 2>&1
```

**Step 2: TypeScript check**

```bash
cd /Users/mjbonanno/github.com/scrypster/huginn/web
npx vue-tsc --noEmit 2>&1
```

Expected: 0 errors.

**Step 3: Restart server and smoke test**

Open the app and test all scenarios from Task 6 Step 8.

**Step 4: Final commit**

```bash
cd /Users/mjbonanno/github.com/scrypster/huginn/web
git add -A
git commit -m "feat: tiptap rich text editor — code blocks, lists, @mentions, markdown-native input"
```

---

## Summary of what changes

| File | Action |
|------|--------|
| `web/package.json` | Add 9 tiptap/lowlight/tippy packages |
| `web/src/components/ChatEditor/MentionList.vue` | Create |
| `web/src/components/ChatEditor/useEditor.ts` | Create |
| `web/src/components/ChatEditor/ChatToolbar.vue` | Create |
| `web/src/components/ChatEditor/ChatEditor.vue` | Create |
| `web/src/components/ChatEditor/index.ts` | Create |
| `web/src/views/ChatView.vue` | Replace input section; keep renderMarkdown |

**Lines of code removed from ChatView.vue:** ~120 (textarea, mirror, code block, mention autocomplete state/functions)

**Lines added to ChatView.vue:** ~20 (ChatEditor import, chatEditorRef, handleEditorSend)
