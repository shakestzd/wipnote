// Pure helpers for wipnote-ref cards: fence parsing, CLI argv and text formats.
// No `$` here, so the tests run them directly.

import type { Card, CardEntry, FailKind, Step } from '../types'

/** A work-item id: kind prefix, a dash, 6+ hex digits (feat-1a2b3c4d). */
const ID = /\b(feat|bug|spk|trk|plan)-[0-9a-f]{6,}\b/g

/** A closed ```wipnote-ref fence: ids on the info line or in the body. */
const FENCE = /```wipnote-ref([^\n`]*)\n?([\s\S]*?)```/g

export type Segment = { kind: 'text'; text: string } | { kind: 'refs'; ids: string[] }

/**
 * Splits assistant markdown into text runs and wipnote-ref fences. An open
 * (still streaming) fence stays text, so a half-written block draws as typed.
 */
export function splitRefs(markdown: string): Segment[] {
  const out: Segment[] = []
  let last = 0
  for (const m of markdown.matchAll(FENCE)) {
    const ids = [...new Set(`${m[1]} ${m[2]}`.match(ID) ?? [])]
    if (ids.length === 0) continue
    if (m.index > last) out.push({ kind: 'text', text: markdown.slice(last, m.index) })
    out.push({ kind: 'refs', ids })
    last = m.index + m[0].length
  }
  if (out.length === 0) return [{ kind: 'text', text: markdown }]
  if (last < markdown.length) out.push({ kind: 'text', text: markdown.slice(last) })
  return out
}

const KIND_COMMAND: Record<string, string> = {
  feat: 'feature', bug: 'bug', spk: 'spike', trk: 'track', plan: 'plan',
}

/** The read-only CLI call that reports one work item as JSON. */
export function showArgv(id: string): string[] {
  const kind = KIND_COMMAND[id.split('-')[0] ?? ''] ?? 'feature'
  return ['wipnote', kind, 'show', id, '--format', 'json']
}

/** Parses `wipnote <kind> show --format json` output into a Card. */
export function parseCard(id: string, stdout: string): Card {
  const raw = JSON.parse(stdout) as Record<string, unknown>
  const steps = Array.isArray(raw.steps)
    ? (raw.steps as Record<string, unknown>[]).map((s): Step => ({
        description: String(s.description ?? ''),
        completed: s.completed === true,
      }))
    : []
  return {
    id: String(raw.id ?? id),
    title: String(raw.title ?? ''),
    status: String(raw.status ?? 'unknown'),
    type: String(raw.type ?? ''),
    steps,
  }
}

/** "3/5 steps", or "" for an item with no steps. */
export function stepCount(steps: Step[]): string {
  if (steps.length === 0) return ''
  return `${steps.filter(s => s.completed).length}/${steps.length} steps`
}

/** A ten-cell text bar for step progress: ███████░░░. */
export function stepBar(steps: Step[]): string {
  if (steps.length === 0) return ''
  const done = Math.round((10 * steps.filter(s => s.completed).length) / steps.length)
  return '█'.repeat(done) + '░'.repeat(10 - done)
}

/** A shell call that changes a work item's steps or status. */
const CHANGE = /\bwipnote\s+(?:feature|bug|spike|track|plan)\s+(?:add-step|complete-step|complete|start)\s+((?:feat|bug|spk|trk|plan)-[0-9a-f]{6,})\b/g

/**
 * The ids a shell command changes with `wipnote <kind> add-step|complete-step|
 * complete|start <id>`, deduped, so their cards refresh as soon as it ran.
 */
export function changedRefs(command: string): string[] {
  return [...new Set([...command.matchAll(CHANGE)].map(m => m[1] as string))]
}

/** How long a loaded or failed card is shown before it is fetched again. */
export const CARD_TTL_MS = 60_000

/**
 * Whether a card needs a (re)fetch: missing, or loaded/failed longer than the
 * TTL ago. A loading entry is left alone unless it is stuck past the TTL.
 */
export function needsFetch(entry: { state: string; at: number } | undefined, now: number): boolean {
  if (!entry) return true
  return now - entry.at > CARD_TTL_MS
}

/** What a wipnote call returned: its output, or why it failed. */
export type CliResult = { ok: true; stdout: string } | { ok: false; kind: FailKind }

/**
 * Classifies a finished wipnote process. A nonzero exit that says "not found"
 * is a missing item (pulling may help); any other nonzero exit is an error.
 * A process that could not run or timed out is reported by the caller as
 * 'unavailable'.
 */
export function classifyExit(exitCode: number, stdout: string, stderr: string): CliResult {
  if (exitCode === 0) return { ok: true, stdout }
  return { ok: false, kind: /not found/i.test(`${stderr}\n${stdout}`) ? 'missing' : 'error' }
}

/** The one-line text drawn for a card whose lookup failed. */
export function failureText(id: string, kind: FailKind): string {
  if (kind === 'missing') return `${id} · not in this checkout (pull to update)`
  if (kind === 'unavailable') return `${id} · wipnote unavailable`
  return `${id} · details unavailable`
}

/**
 * The work item `wipnote who --json` reports: the id, '' for no claim, or
 * null when the output cannot be read (the caller keeps its last claim).
 */
export function parseWho(stdout: string): string | null {
  try {
    const raw = JSON.parse(stdout) as { work_item?: unknown }
    return typeof raw.work_item === 'string' ? raw.work_item : ''
  } catch {
    return null
  }
}

/**
 * The cards to refresh after a turn: open (not done) or failed ones, stalest
 * first, at most `max`. Ordering by age rotates the cap across every card, so
 * none is starved.
 */
export function pickRefresh(entries: [string, CardEntry][], max: number): string[] {
  return entries
    .filter(([, e]) => e.state === 'failed' || (e.state === 'loaded' && e.card.status !== 'done'))
    .sort(([, a], [, b]) => a.at - b.at)
    .slice(0, max)
    .map(([id]) => id)
}

/**
 * The ids referenced by wipnote-ref fences in the assistant messages, newest
 * last, deduped, at most `max` (the newest ones).
 */
export function refsInMessages(messages: { role: string; text: string }[], max: number): string[] {
  const ids: string[] = []
  for (const m of messages) {
    if (m.role !== 'assistant') continue
    for (const s of splitRefs(m.text)) {
      if (s.kind !== 'refs') continue
      for (const id of s.ids) {
        const at = ids.indexOf(id)
        if (at >= 0) ids.splice(at, 1)
        ids.push(id)
      }
    }
  }
  return ids.slice(-max)
}

/**
 * Hands out increasing tickets per key, so of several overlapping async calls
 * only the newest one acts on its result.
 */
export class Latest {
  private readonly last = new Map<string, number>()

  take(key: string): number {
    const ticket = (this.last.get(key) ?? 0) + 1
    this.last.set(key, ticket)
    return ticket
  }

  isNewest(key: string, ticket: number): boolean {
    return this.last.get(key) === ticket
  }
}
