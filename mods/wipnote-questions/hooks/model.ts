import type { Answer, Option, QuestionRow } from '../types'

export function listArgv(): string[] {
  return ['wipnote', 'question', 'list', '--open', '--format', 'json']
}

/** `question answer <id> <key>`, or `--text <reply>` for a free-text question. */
export function answerArgv(questionId: string, a: Answer): string[] {
  const base = ['wipnote', 'question', 'answer', questionId]
  return 'key' in a ? [...base, a.key] : [...base, '--text', a.text]
}

/** Reads `/answer-question <id> <key>` or `/answer-question <id> --text <reply...>`. */
export function parseAnswerArgs(args: string): { id: string; answer: Answer } | undefined {
  const m = /^\s*(\S+)\s+(.*?)\s*$/s.exec(args)
  if (m === null) return undefined
  const [, id, rest] = m as unknown as [string, string, string]
  if (rest.startsWith('--text')) {
    const text = rest.slice('--text'.length).trim()
    return text === '' ? undefined : { id, answer: { text } }
  }
  const key = rest.split(/\s+/)[0] ?? ''
  return key === '' ? undefined : { id, answer: { key } }
}

function text(v: unknown): string {
  return typeof v === 'string' ? v : ''
}

function optionOf(v: unknown): Option | undefined {
  if (typeof v !== 'object' || v === null) return undefined
  const o = v as Record<string, unknown>
  if (text(o.key) === '') return undefined
  const tradeoff = text(o.tradeoff)
  return { key: text(o.key), label: text(o.label), ...(tradeoff !== '' ? { tradeoff } : {}) }
}

function rowOf(v: unknown): QuestionRow | undefined {
  if (typeof v !== 'object' || v === null) return undefined
  const o = v as Record<string, unknown>
  if (text(o.question_id) === '' || text(o.item_id) === '') return undefined
  const options = Array.isArray(o.options) ? o.options.map(optionOf).filter((x): x is Option => x !== undefined) : []
  return {
    itemId: text(o.item_id),
    itemTitle: text(o.item_title),
    id: text(o.question_id),
    text: text(o.text),
    options,
    recommended: text(o.recommended),
    blocking: o.blocking === true,
    age: text(o.age),
    freeText: o.free_text === true,
  }
}

/** Reads `question list --format json`; undefined when it is not a list (null is an empty one). */
export function parseQuestions(stdout: string): QuestionRow[] | undefined {
  let raw: unknown
  try {
    raw = JSON.parse(stdout)
  } catch {
    return undefined
  }
  if (raw === null) return []
  if (!Array.isArray(raw)) return undefined
  return raw.map(rowOf).filter((r): r is QuestionRow => r !== undefined)
}

/** `item · age · blocking · recommends k`, or `· free text` for a question with no options. */
export function rowLine(r: QuestionRow): string {
  return [r.itemId, r.age, r.blocking ? 'blocking' : '', r.recommended !== '' ? `recommends ${r.recommended}` : '', r.freeText ? 'free text' : '']
    .filter(p => p !== '')
    .join(' · ')
}

export function countLine(rows: readonly QuestionRow[]): string {
  if (rows.length === 0) return 'No open questions'
  return `${rows.length} open · ${rows.filter(r => r.blocking).length} blocking`
}

/** The CLI's first error line, or its exit code. */
export function failReason(stderr: string, exitCode: number): string {
  const line = stderr.split('\n').map(l => l.trim()).find(l => l !== '')
  return line ?? `wipnote exited ${exitCode}`
}

/** The non-empty lines of a command's output, for the pane. */
export function fullOutput(out: string): string[] {
  return out.split('\n').map(l => l.trim()).filter(l => l !== '')
}

/** The line to toast: an overturn or reopen warning when the answer made one, else the first line. */
export function toastLine(out: string): string {
  const lines = fullOutput(out)
  return lines.find(l => l.startsWith('Assumption overturned') || l.includes('Reopened ')) ?? lines[0] ?? ''
}

/**
 * The id as the pane suggests typing it: "q-" and 6 hex digits. The CLI
 * resolves a unique prefix of at least 4 digits and refuses an ambiguous one,
 * so a short id never answers the wrong question.
 */
export function shortID(id: string): string {
  return id.slice(0, 8)
}
