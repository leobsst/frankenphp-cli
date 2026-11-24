/**
 * Minimal Markdown reader for the GitHub release notes: headings, list items and paragraphs, with inline code, bold
 * and links. It yields plain data rendered by the template (never v-html): a release body cannot inject markup.
 */
export type Inline = { kind: 'text' | 'code' | 'bold'; text: string } | { kind: 'link'; text: string; href: string }
export type Block = { kind: 'heading' | 'item' | 'paragraph'; content: Inline[] }

const INLINE = /`([^`]+)`|\*\*([^*]+)\*\*|\[([^\]]+)\]\((https?:\/\/[^)\s]+)\)/g

export function inline(text: string): Inline[] {
  const out: Inline[] = []
  let last = 0
  for (const m of text.matchAll(INLINE)) {
    if (m.index > last) out.push({ kind: 'text', text: text.slice(last, m.index) })
    if (m[1] !== undefined) out.push({ kind: 'code', text: m[1] })
    else if (m[2] !== undefined) out.push({ kind: 'bold', text: m[2] })
    else out.push({ kind: 'link', text: m[3]!.replaceAll('`', ''), href: m[4]! })
    last = m.index + m[0].length
  }
  if (last < text.length) out.push({ kind: 'text', text: text.slice(last) })
  return out
}

/**
 * Blocks of a release body. The installation instructions every release repeats (from "## Installation" on) and the
 * "What's changed" title are left out: the dashboard shows the changes only.
 */
export function releaseBlocks(body: string): Block[] {
  const blocks: Block[] = []
  let para: string[] = []
  let fence = false
  const flush = () => {
    if (para.length) blocks.push({ kind: 'paragraph', content: inline(para.join(' ')) })
    para = []
  }
  for (const raw of body.replaceAll('\r\n', '\n').split('\n')) {
    const line = raw.trim()
    const heading = fence ? null : /^#{1,6}\s+(.*)$/.exec(line)
    if (heading && /^installation\b/i.test(heading[1]!)) break
    // Code blocks are left out
    if (line.startsWith('```')) { flush(); fence = !fence; continue }
    if (fence) continue
    if (!line) { flush(); continue }
    if (heading) {
      flush()
      if (!/^what.s changed$/i.test(heading[1]!)) blocks.push({ kind: 'heading', content: inline(heading[1]!) })
      continue
    }
    const item = /^[-*+]\s+(.*)$/.exec(line)
    if (item) { flush(); blocks.push({ kind: 'item', content: inline(item[1]!) }); continue }
    para.push(line)
  }
  flush()
  return blocks
}
