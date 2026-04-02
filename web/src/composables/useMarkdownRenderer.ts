import { type Ref } from 'vue'
import { marked, Renderer } from 'marked'
import hljs from 'highlight.js'

// ── marked + highlight.js setup ──────────────────────────────────────────

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

/** Render raw markdown to HTML. */
export function renderMarkdown(content: string): string {
  return marked.parse(content) as string
}

interface Agent {
  name: string
  color: string
  icon: string
  model: string
  [key: string]: unknown
}

/**
 * renderWithMentions wraps @agent-name tokens in styled, hoverable spans.
 * Agent names are resolved from agentsList so tooltip shows real model info.
 *
 * Safety: the lookbehind (?<![a-zA-Z0-9.]) ensures we don't match @domain.com
 * inside email addresses like mailto:user@example.com (where @ is preceded by
 * a word character). Only @mentions preceded by whitespace/punctuation match.
 */
export function renderWithMentions(content: string, agentsList: Ref<Agent[]>): string {
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

/**
 * Composable that returns bound rendering functions.
 * The returned `renderWithMentions` automatically uses the provided agents list.
 */
export function useMarkdownRenderer(agentsList: Ref<Agent[]>) {
  function render(content: string): string {
    return renderWithMentions(content, agentsList)
  }

  return {
    renderMarkdown,
    renderWithMentions: render,
  }
}
