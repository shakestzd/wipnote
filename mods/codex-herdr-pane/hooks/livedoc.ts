// Splits a run's clean.md into prose and folds for the Claude Code pane (runpane.tsx): the same
// rules as viewer/codex_view.py's LiveDoc. A fold is a tool result between the markers md.jq
// writes; a marker counts only outside a fenced block (CommonMark fences, as md.jq's
// fence_open / fence_step), so tool output and prose cannot make one.

export type RunPart =
  | { kind: 'prose'; text: string }
  | { kind: 'fold'; title: string; body: string; failed: boolean }

type Fence = { c: string; n: number } | null

const FOLD_OPEN = /^<!-- fold (open|closed): (.*) -->$/
const FOLD_END = '<!-- /fold -->'
const FENCE_OPEN = /^ {0,3}(`{3,}|~{3,})(.*)$/
// The call's label (md.jq's toolline), the first line of a fold body
const LABEL = /^\*\*[A-Za-z0-9_.:-]+\*\*( .*)?$/

export function fenceStep(state: Fence, line: string): Fence {
  if (state === null) {
    const m = FENCE_OPEN.exec(line)
    if (m && (m[1]![0] === '~' || !m[2]!.includes('`'))) return { c: m[1]![0]!, n: m[1]!.length }
    return null
  }
  const close = new RegExp(`^ {0,3}${state.c === '`' ? '`' : '~'}{${state.n},}\\s*$`)
  return close.test(line) ? null : state
}

// The fold's Markdown without the label that repeats its title; a cut title ("...") keeps it,
// the only place the whole command is shown.
function foldBody(title: string, lines: string[]): string {
  const rest = lines.length > 0 && !title.includes('...') && LABEL.test(lines[0]!) ? lines.slice(1) : lines
  return rest.join('\n').replace(/^\n+|\n+$/g, '') || '*(empty)*'
}

// Only complete lines are read (a trailing partial line waits for its newline). A fold whose end
// marker has not arrived is held back, unless `final` (the run ended): then it is shown as is.
export function parseRun(text: string, final = false): RunPart[] {
  const end = text.lastIndexOf('\n')
  const lines = (final ? text : text.slice(0, end + 1)).split('\n')
  if (lines[lines.length - 1] === '') lines.pop()
  const parts: RunPart[] = []
  let prose: string[] = []
  let fence: Fence = null
  let fold: { title: string; failed: boolean; lines: string[]; fence: Fence } | null = null
  const flush = () => {
    if (prose.join('').trim()) parts.push({ kind: 'prose', text: prose.join('\n').replace(/\n+$/, '') })
    prose = []
  }
  for (const line of lines) {
    if (fold) {
      if (fold.fence === null && line === FOLD_END) {
        parts.push({ kind: 'fold', title: fold.title, failed: fold.failed, body: foldBody(fold.title, fold.lines) })
        fold = null
      } else {
        fold.lines.push(line)
        fold.fence = fenceStep(fold.fence, line)
      }
      continue
    }
    const m = fence === null ? FOLD_OPEN.exec(line) : null
    if (m) {
      flush()
      fold = { title: m[2]!, failed: m[1] === 'open', lines: [], fence: null }
      continue
    }
    prose.push(line)
    fence = fenceStep(fence, line)
  }
  flush()
  if (fold && final) parts.push({ kind: 'fold', title: fold.title, failed: fold.failed, body: foldBody(fold.title, fold.lines) })
  return parts
}

// The run as plain text, for /codex-runs where no surface draws panes (the reply lands in the
// chat): the assistant's text, one line per tool result (a failed one with the first lines of its
// output), then the answer. Headings and fences are dropped so it reads as text; the newest part
// is kept when it is long.
const FAILED_LINES = 6
const MAX_SUMMARY = 6000

function plainLines(md: string): string[] {
  // headings, fences, the thinking label and codex's token footer (the head already says it)
  return md.split('\n').filter((l) => !/^#{1,6} /.test(l) && !/^ {0,3}(`{3,}|~{3,})/.test(l) && l !== '**thinking**' && !/^\*input [0-9,]+ .*\*$/.test(l))
}

export function summarizeRun(parts: RunPart[], head: string, answer: string, logPath: string): string {
  const body: string[] = []
  for (const p of parts) {
    if (p.kind === 'prose') {
      const text = plainLines(p.text).join('\n').replace(/\n{3,}/g, '\n\n').trim()
      if (text) body.push(...(body.length ? [''] : []), text, '')
      continue
    }
    body.push(`${p.failed ? '✗' : '✓'} ${p.title}`)
    if (p.failed) {
      const out = plainLines(p.body).filter((l) => l.trim() && !l.startsWith('> **error**') && !/^\*\*[A-Za-z0-9_.:-]+\*\*/.test(l))
      body.push(...out.slice(0, FAILED_LINES).map((l) => `    ${l}`))
      if (out.length > FAILED_LINES) body.push(`    ... ${out.length - FAILED_LINES} more lines`)
    }
  }
  let text = body.join('\n').replace(/\n{3,}/g, '\n\n').trim()
  if (text.length > MAX_SUMMARY) text = '... (earlier steps cut)\n' + text.slice(-MAX_SUMMARY)
  const tail = answer.trim() ? `\n\n--- answer ---\n${answer.trim()}` : ''
  return `${head}\n\n${text || '(no events yet)'}${tail}\n\nFull log: ${logPath}`
}
