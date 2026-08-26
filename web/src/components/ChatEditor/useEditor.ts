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
  // Reactive placeholder source — when supplied, the placeholder shown in
  // the editor reads from this ref on every render, so changing the prop on
  // the wrapping component (e.g. when the active DM changes) updates the
  // placeholder without re-creating the editor.
  placeholderRef?: Ref<string | undefined>
}) {
  const editor = ref<Editor | null>(null)
  let suggestionOpen = false
  // After Escape, TipTap 3.20 rematches the same @ on the next transaction.
  // Refuse that range until the cursor leaves it.
  let dismissedFrom: number | null = null

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

        allow: ({ range, state }: { range: { from: number; to: number }; state: { selection: { from: number } } }) => {
          if (dismissedFrom == null) return true
          const pos = state.selection.from
          if (pos < range.from || pos > range.to) {
            dismissedFrom = null
            return true
          }
          return range.from !== dismissedFrom
        },

        render: () => {
          let component: VueRenderer | undefined
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
              component?.updateProps(props)
              if (!props.clientRect || !popup) return
              popup.setProps({
                getReferenceClientRect: props.clientRect as () => DOMRect,
              })
            },
            // Do not handle Escape here. TipTap 3.20 calls this first; a true
            // return skips onExit + dispatchExit, which is what popup.hide() did.
            onKeyDown(props: SuggestionKeyDownProps) {
              return (component?.ref as MentionListRef | null)
                ?.onKeyDown(props) ?? false
            },
            onExit(props: SuggestionProps) {
              const pos = props.editor.state.selection.from
              if (pos >= props.range.from && pos <= props.range.to) {
                dismissedFrom = props.range.from
              } else {
                dismissedFrom = null
              }
              suggestionOpen = false
              popup?.destroy()
              popup = null
              component?.destroy()
              component = undefined
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
          placeholder: () => options.placeholderRef?.value ?? options.placeholder ?? 'Message huginn...',
        }),
        // Markdown must come before Link so that when tiptap-markdown registers its
        // own internal link extension (also named 'link'), the explicit Link.configure
        // below wins the deduplication and preserves openOnClick: false + CSS class.
        Markdown.configure({
          html: false,
          tightLists: true,
          bulletListMarker: '-',
          transformPastedText: true,
          transformCopiedText: true,
        }),
        Link.configure({
          openOnClick: false,
          HTMLAttributes: { class: 'link' },
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
    const dom = editor.value?.view?.dom as HTMLElement | undefined
    dom?.focus()
  }

  function setText(content: string) {
    if (!editor.value) return
    editor.value.commands.setContent(content)
    focus()
  }

  function isEmpty(): boolean {
    return editor.value?.isEmpty ?? true
  }

  onBeforeUnmount(() => {
    editor.value?.destroy()
  })

  return { editor, init, getMarkdown, clear, focus, setText, isEmpty }
}
