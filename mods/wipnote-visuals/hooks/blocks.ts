// Pure parsers for the wip:* fenced blocks. No engine imports. Every parser is
// total: malformed input gives a best-effort parse or null, never a throw.
// Each format reads fine as plain text without the mod.

export type StepState = 'done' | 'next' | 'todo'
export type Step = { state: StepState; text: string }
export type Option = { name: string; recommended: boolean; pros: string[]; cons: string[] }
export type CalloutKind = 'decision' | 'warning' | 'note'
export type Callout = { kind: CalloutKind; text: string }

export type Block =
  | { kind: 'steps'; steps: Step[] }
  | { kind: 'compare'; options: Option[] }
  | { kind: 'callout'; callout: Callout }

export type Segment = { kind: 'text'; text: string } | { kind: 'block'; block: Block }

const lines = (body: string): string[] =>
  String(body ?? '').split(/\r\n|\r|\n/).map(l => l.trim()).filter(l => l !== '')

/** `x text` = done, `> text` = next, otherwise todo. Null when no step has text. */
export function parseSteps(body: string): Step[] | null {
  const steps: Step[] = []
  for (const l of lines(body)) {
    const m = /^([x>])(?:\s+(.*))?$/.exec(l)
    const state: StepState = m ? (m[1] === 'x' ? 'done' : 'next') : 'todo'
    const text = (m ? m[2] : l) ?? ''
    if (text.trim() !== '') steps.push({ state, text: text.trim() })
  }
  return steps.length > 0 ? steps : null
}

/** `# Name` (trailing `*` = recommended) opens an option; `+ pro` / `- con` lines follow. */
export function parseCompare(body: string): Option[] | null {
  const options: Option[] = []
  let cur: Option | undefined
  for (const l of lines(body)) {
    const head = /^#+\s*(.*)$/.exec(l)
    if (head) {
      const raw = (head[1] ?? '').trim()
      const recommended = raw.endsWith('*')
      const name = (recommended ? raw.slice(0, -1) : raw).trim()
      cur = { name: name || `Option ${options.length + 1}`, recommended, pros: [], cons: [] }
      options.push(cur)
      continue
    }
    const item = /^([+-])\s*(.*)$/.exec(l)
    if (!item || (item[2] ?? '') === '') continue
    if (!cur) {
      cur = { name: 'Option 1', recommended: false, pros: [], cons: [] }
      options.push(cur)
    }
    ;(item[1] === '+' ? cur.pros : cur.cons).push(item[2] as string)
  }
  return options.length > 0 ? options : null
}

/** First line `kind: text` (decision, warning, note); later lines continue the text. */
export function parseCallout(body: string): Callout | null {
  const ls = lines(body)
  if (ls.length === 0) return null
  const m = /^(decision|warning|note)\s*:\s*(.*)$/i.exec(ls[0] as string)
  const kind = (m ? (m[1] as string).toLowerCase() : 'note') as CalloutKind
  const first = m ? (m[2] as string) : (ls[0] as string)
  const text = [first, ...ls.slice(1)].filter(s => s !== '').join('\n')
  return text === '' ? null : { kind, text }
}

export function parseBlock(name: string, body: string): Block | null {
  try {
    if (name === 'steps') {
      const steps = parseSteps(body)
      return steps && { kind: 'steps', steps }
    }
    if (name === 'compare') {
      const options = parseCompare(body)
      return options && { kind: 'compare', options }
    }
    if (name === 'callout') {
      const callout = parseCallout(body)
      return callout && { kind: 'callout', callout }
    }
  } catch {
    // total by contract
  }
  return null
}

const FENCE = /^[ \t]*```wip:(steps|compare|callout)[ \t]*\r?\n([\s\S]*?)\r?\n[ \t]*```[ \t]*$/gm

/**
 * Splits markdown into text runs and parsed blocks. An open (streaming) fence
 * or one that does not parse stays text, so it draws as typed.
 */
export function splitBlocks(markdown: string): Segment[] {
  const src = String(markdown ?? '')
  const out: Segment[] = []
  let last = 0
  for (const m of src.matchAll(FENCE)) {
    const block = parseBlock(m[1] as string, m[2] as string)
    if (!block) continue
    if (m.index > last) out.push({ kind: 'text', text: src.slice(last, m.index) })
    out.push({ kind: 'block', block })
    last = m.index + m[0].length
  }
  if (out.length === 0) return [{ kind: 'text', text: src }]
  if (last < src.length) out.push({ kind: 'text', text: src.slice(last) })
  return out
}

/** The model-facing rule, sent only while the mod is on. */
export const RULE =
  'For steps use ```wip:steps instead of a numbered list (a step per line; `x ` done, `> ` current). ' +
  'For options use ```wip:compare (`# Name*` marks the pick; `+ pro`, `- con`). ' +
  'For a warning or decision use ```wip:callout (`warning:` or `decision:`, then text). ' +
  'Say each thing once: never repeat a block in prose.'
