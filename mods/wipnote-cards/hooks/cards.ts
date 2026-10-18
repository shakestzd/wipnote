// Pure helpers for wipnote-cards: fence parsing, CLI argv and text formats.
// No `$` here, so the tests run them directly.

import type { Card, Step } from '../types'

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

/** The status line: "wipnote · feat-x · in-progress · 3/5 steps". */
export function statusLine(card: Card | null): string {
  if (!card) return 'wipnote · no active work item'
  return ['wipnote', card.id, card.status, stepCount(card.steps)].filter(Boolean).join(' · ')
}

/** The model-facing rule, sent only while this mod is loaded. */
export const REF_RULE =
  'When you report on a wipnote work item, end the reply with a fenced code block whose ' +
  'info string is `wipnote-ref` and whose only line is the item id (for example feat-1a2b3c4d). ' +
  'The UI draws that block as a card with the title, status and steps, so do not repeat them in prose.'
