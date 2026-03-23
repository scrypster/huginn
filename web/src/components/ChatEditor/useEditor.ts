import { ref, onBeforeUnmount, type Ref } from 'vue'
import { Editor, VueRenderer } from '@tiptap/vue-3'
import { textblockTypeInputRule } from '@tiptap/core'
import StarterKit from '@tiptap/starter-kit'
import Placeholder from '@tiptap/extension-placeholder'
import Link from '@tiptap/extension-link'
import Mention from '@tiptap/extension-mention'
import CodeBlockLowlight from '@tiptap/extension-code-block-lowlight'
import { Markdown } from 'tiptap-markdown'
import { common, createLowlight } from 'lowlight'
import tippy from 'tippy.js'
import type { Instance as TippyInstance } from 'tippy.js'
import type { SuggestionProps, SuggestionKeyDownProps } from '@tiptap/suggestion'
import MentionList from './MentionList.vue'

const lowlight = createLowlight(common)

// Override the default code block input rule to trigger immediately on ```
// (no trailing space/enter required, matching Slack behavior)
const CodeBlockImmediate = CodeBlockLowlight.extend({
  addInputRules() {
    return [
      textblockTypeInputRule({
        find: /^```([a-z]*)$/,
        type: this.type,
        getAttributes: match => ({ language: match[1] || null }),
      }),
    ]
  },
})

interface MentionListRef {
  onKeyDown: (p: unknown) => boolean
}

export function useEditor(options: {
  agents: Ref<Array<Record<string, unknown>>>
  onSend: () => void
  placeholder?: string
}) {
  const editor = ref<Editor | null>(null)
  let suggestionOpen = false

  function createMentionExtension() {
    // Extend Mention with a tiptap-markdown serializer so @Name renders as
    // "@Name" in the outgoing markdown string instead of the default "[mention]".
    const MentionWithMarkdown = Mention.extend({
      addStorage() {
        return {
          markdown: {
            serialize(state: { write: (s: string) => void }, node: { attrs: Record<string, string> }) {
              state.write(`@${node.attrs.id || node.attrs.label || ''}`)
            },
          },
        }
      },
    })
    return MentionWithMarkdown.configure({
      HTMLAttributes: { class: 'mention' },
      suggestion: {
        items: ({ query }: { query: string }) =>
          options.agents.value
            .filter(a => String(a.name).toLowerCase().startsWith(query.toLowerCase()))
            .slice(0, 6),

        render: () => {
          let component: VueRenderer
          let popup: TippyInstance | null = null

          return {
            onStart(props: SuggestionProps) {
              suggestionOpen = true
              component = new VueRenderer(MentionList, {
                props,
                editor: props.editor,
              })

              if (!props.clientRect || !component.element) return

              popup = tippy(document.body, {
                getReferenceClientRect: props.clientRect as () => DOMRect,
                appendTo: () => document.body,
                content: component.element,
                showOnCreate: true,
                interactive: true,
                trigger: 'manual',
                placement: 'top-start',
              })
            },
            onUpdate(props: SuggestionProps) {
              component.updateProps(props)
              if (!props.clientRect || !popup) return
              popup.setProps({
                getReferenceClientRect: props.clientRect as () => DOMRect,
              })
            },
            onKeyDown(props: SuggestionKeyDownProps) {
              if (props.event.key === 'Escape') {
                popup?.hide()
                return true
              }
              return (component.ref as MentionListRef | null)
                ?.onKeyDown(props) ?? false
            },
            onExit() {
              suggestionOpen = false
              popup?.destroy()
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
          codeBlock: false,
        }),
        CodeBlockImmediate.configure({
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
        handleTextInput(view, _from, _to, text) {
          // Triple backtick → code block (Slack-style, immediate on 3rd backtick)
          if (text === '`') {
            const { $from } = view.state.selection
            if ($from.parent.type.name !== 'paragraph') return false
            const textBefore = $from.parent.textContent.slice(0, $from.parentOffset)
            if (textBefore === '``') {
              const start = $from.start()
              const tr = view.state.tr
                .delete(start, start + 2)
                .setBlockType(start, start, view.state.schema.nodes.codeBlock!, { language: null })
              view.dispatch(tr)
              return true
            }
          }
          return false
        },
        handleKeyDown(view, event) {
          if (event.key === 'Enter' && !event.shiftKey) {
            // Let suggestion plugin handle Enter when dropdown is open
            if (suggestionOpen) return false
            const { $from } = view.state.selection
            if ($from.parent.type.name === 'codeBlock') return false
            event.preventDefault()
            options.onSend()
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
    return (editor.value.storage as unknown as { markdown: { getMarkdown: () => string } })
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
