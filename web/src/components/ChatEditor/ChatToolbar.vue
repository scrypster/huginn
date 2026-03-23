<template>
  <div class="flex items-center gap-0.5 px-2 py-1.5 border-t border-huginn-border/50">
    <!-- Text formatting group -->
    <ToolbarBtn :active="editor.isActive('bold')" @click="editor.chain().focus().toggleBold().run()" title="Bold (⌘B)">
      <svg class="w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round">
        <path d="M6 4h7a4 4 0 0 1 0 8H6V4z"/>
        <path d="M6 12h8a4 4 0 0 1 0 8H6v-8z"/>
      </svg>
    </ToolbarBtn>
    <ToolbarBtn :active="editor.isActive('italic')" @click="editor.chain().focus().toggleItalic().run()" title="Italic (⌘I)">
      <svg class="w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round"><line x1="19" y1="4" x2="10" y2="4"/><line x1="14" y1="20" x2="5" y2="20"/><line x1="15" y1="4" x2="9" y2="20"/></svg>
    </ToolbarBtn>
    <ToolbarBtn :active="editor.isActive('code')" @click="editor.chain().focus().toggleCode().run()" title="Inline code (⌘E)">
      <svg class="w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><polyline points="16 18 22 12 16 6"/><polyline points="8 6 2 12 8 18"/></svg>
    </ToolbarBtn>

    <div class="w-px h-3.5 bg-huginn-border mx-1 flex-shrink-0" />

    <ToolbarBtn :active="editor.isActive('bulletList')" @click="editor.chain().focus().toggleBulletList().run()" title="Bullet list">
      <svg class="w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><line x1="9" y1="6" x2="20" y2="6"/><line x1="9" y1="12" x2="20" y2="12"/><line x1="9" y1="18" x2="20" y2="18"/><circle cx="4" cy="6" r="1" fill="currentColor"/><circle cx="4" cy="12" r="1" fill="currentColor"/><circle cx="4" cy="18" r="1" fill="currentColor"/></svg>
    </ToolbarBtn>
    <ToolbarBtn :active="editor.isActive('orderedList')" @click="editor.chain().focus().toggleOrderedList().run()" title="Numbered list">
      <svg class="w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><line x1="10" y1="6" x2="21" y2="6"/><line x1="10" y1="12" x2="21" y2="12"/><line x1="10" y1="18" x2="21" y2="18"/><path d="M4 6h1v4"/><path d="M4 10H6"/><path d="M6 18H4c0-1 2-2 2-3s-1-1.5-2-1"/></svg>
    </ToolbarBtn>
    <ToolbarBtn :active="editor.isActive('blockquote')" @click="editor.chain().focus().toggleBlockquote().run()" title="Blockquote">
      <svg class="w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><path d="M3 21c3 0 7-1 7-8V5c0-1.25-.756-2.017-2-2H4c-1.25 0-2 .75-2 1.972V11c0 1.25.75 2 2 2 1 0 1 0 1 1v1c0 1-1 2-2 2s-1 .008-1 1.031V20c0 1 0 1 1 1z"/><path d="M15 21c3 0 7-1 7-8V5c0-1.25-.757-2.017-2-2h-4c-1.25 0-2 .75-2 1.972V11c0 1.25.75 2 2 2h.75c0 2.25.25 4-2.75 4v3c0 1 0 1 1 1z"/></svg>
    </ToolbarBtn>

    <div class="w-px h-3.5 bg-huginn-border mx-1 flex-shrink-0" />

    <ToolbarBtn :active="editor.isActive('codeBlock')" @click="insertCodeBlock" title="Code block">
      <svg class="w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><polyline points="16 18 22 12 16 6"/><polyline points="8 6 2 12 8 18"/><line x1="12" y1="3" x2="12" y2="21"/></svg>
    </ToolbarBtn>

    <span class="ml-auto text-[11px] mr-1" style="color:rgba(139,148,158,0.4)">⏎ send &nbsp;·&nbsp; ⇧⏎ newline</span>

    <!-- Send button -->
    <button
      type="button"
      @mousedown.prevent="$emit('send')"
      class="w-7 h-7 rounded-xl flex items-center justify-center text-white transition-all duration-150 hover:opacity-80 active:scale-90 flex-shrink-0"
      style="background:rgba(88,166,255,0.9)"
      title="Send (⏎)"
    >
      <svg class="w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round">
        <line x1="22" y1="2" x2="11" y2="13" />
        <polygon points="22 2 15 22 11 13 2 9 22 2" />
      </svg>
    </button>
  </div>
</template>

<script setup lang="ts">
import { defineComponent, h } from 'vue'
import type { Editor } from '@tiptap/vue-3'

const props = defineProps<{ editor: Editor }>()
defineEmits<{ (e: 'send'): void }>()

function insertCodeBlock() {
  const { editor } = props
  if (editor.isActive('codeBlock')) {
    editor.chain().focus().toggleCodeBlock().run()
    return
  }
  const { $from } = editor.state.selection
  // If current block is empty, just convert it to a code block
  if ($from.parent.content.size === 0) {
    editor.chain().focus().setCodeBlock().run()
    return
  }
  // Otherwise insert a new empty code block after the current node
  const endOfNode = $from.after($from.depth)
  editor.chain().focus().insertContentAt(endOfNode, { type: 'codeBlock', content: [] }).run()
}

const ToolbarBtn = defineComponent({
  props: {
    active: { type: Boolean, default: false },
    title: { type: String, default: '' },
  },
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