import type { Option, QuestionRow } from '../types'

export function listArgv(): string[] {
  return ['wipnote', 'question', 'list', '--open', '--format', 'json']
}

export function answerArgv(questionId: string, key: string): string[] {
  return ['wipnote', 'question', 'answer', questionId, key]
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

/** `item · age · blocking · recommends k`. */
export function rowLine(r: QuestionRow): string {
  return [r.itemId, r.age, r.blocking ? 'blocking' : '', r.recommended !== '' ? `recommends ${r.recommended}` : '']
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

/** The first non-empty line of a command's output, for a toast. */
export function firstLine(out: string): string {
  return out.split('\n').map(l => l.trim()).find(l => l !== '') ?? ''
}
