// wipnote-ref cards, for few model tokens: the model ends a report with a
// ```wipnote-ref fence holding only an id, and this draws it as a card from
// `wipnote ... show --format json`. The stored message keeps the short fence,
// so later turns read ~10 tokens, and without the mod the fence still reads as
// an id.
//
// The one-line rule that asks for the fence lives in ../prompt.md. `wipnote
// claude` appends it to the system prompt when it loads this mod, so the model
// never writes fences nobody draws. It is not a prompt.compose hook: a managed
// policy plugin that sends that event past the user tier skips every mod's.
//
// A render hook never writes state, so cards are fetched from events: when a
// reply is stored (session.append), after each turn (turn.complete), and on
// start or resume (hydrate). The render hook only reads the refs atom.
//
// Pure: register.tsx owns the atoms, the hooks and every `$` call; this file turns data into
// argv, ids, entries and trees.

import type { RenderChildren } from 'claude-code'

import type { Card, CardEntry, FailKind } from '../types'
import type { Elements } from './register'
import { changedRefs, classifyExit, failureText, needsFetch, parseCard, pickRefresh, refsInMessages, showArgv, splitRefs, stepBar, stepCount } from './refs'
import type { CliResult, Segment } from './refs'
import { parseOpen, questionLines } from './questions'

export const CLI_TIMEOUT_MS = 15_000
export const MAX_REFRESH = 10
const SCAN_MESSAGES = 200

const STATUS_COLOR: Record<string, string> = {
  done: 'success', 'in-progress': 'warning', blocked: 'error', todo: 'subtle',
}

/** A read-only wipnote command's outcome: a spawn failure or timeout is 'unavailable'. */
export const cliResult = (r: { exitCode: number; stdout: string; stderr: string } | undefined): CliResult =>
  r === undefined ? { ok: false, kind: 'unavailable' } : classifyExit(r.exitCode, r.stdout, r.stderr)

/** The card a `show` outcome holds, or why there is none. */
export function cardResult(id: string, r: CliResult): { card: Card } | { fail: FailKind } {
  if (!r.ok) return { fail: r.kind }
  try {
    return { card: parseCard(id, r.stdout) }
  } catch {
    return { fail: 'error' }
  }
}

/** The card in `show` output the band already fetched, or undefined when it does not parse. */
export function cardOf(id: string, stdout: string): Card | undefined {
  try {
    return parseCard(id, stdout)
  } catch {
    return undefined
  }
}

/** An item's open questions as card lines, or undefined when they could not be read. */
export function questionsOf(r: CliResult): string[] | undefined {
  const rows = r.ok ? parseOpen(r.stdout) : undefined
  return rows === undefined ? undefined : questionLines(rows)
}

/** The entry a finished lookup stores. */
export const entryOf = (r: { card: Card } | { fail: FailKind }, at: number): CardEntry =>
  'card' in r ? { state: 'loaded', card: r.card, at } : { state: 'failed', reason: r.fail, at }

/** Ids that are missing or stale. */
export const staleIds = (all: Record<string, CardEntry | undefined>, ids: readonly string[], now: number): string[] =>
  ids.filter(id => needsFetch(all[id], now))

/**
 * The cards that can still change after a turn: any not done, and any that failed (the user may
 * have pulled). A done item rarely changes, so it waits for the TTL instead of costing a process.
 */
export const openIds = (all: Record<string, CardEntry>): string[] => pickRefresh(Object.entries(all), MAX_REFRESH)

/** The drawn cards a Bash command changed (a ticked step, a start or complete). The CLI takes a unique id prefix, so ids match by prefix. */
export function changedIds(all: Record<string, CardEntry>, command: string): string[] {
  const ids = changedRefs(command)
  if (ids.length === 0) return []
  return Object.keys(all).filter(id => ids.some(one => id.startsWith(one)))
}

/** The ids the transcript's last messages reference. */
export const transcriptIds = (messages: unknown): string[] =>
  Array.isArray(messages) ? refsInMessages(messages.slice(-SCAN_MESSAGES), MAX_REFRESH) : []

/** The ids a stored reply's text references. */
export const replyIds = (text: string): string[] => splitRefs(text).flatMap(s => (s.kind === 'refs' ? s.ids : []))

/** The text of a reply's blocks. */
export const replyText = (content: ReadonlyArray<{ type: string; text?: unknown }>): string =>
  content.map(b => (b.type === 'text' && typeof b.text === 'string' ? b.text : '')).join('\n')

/** One ref's card: its title, status, step bar and open questions; a dim line while loading or failed. */
function drawCard({ Box, Text }: Elements, id: string, entry: CardEntry | undefined, asked: readonly string[]) {
  if (!entry) return <Text key={id} dimColor>{id}</Text>
  if (entry.state === 'loading') return <Text key={id} dimColor>⧗ {id}</Text>
  if (entry.state === 'failed') {
    return <Text key={id} dimColor>{failureText(id, entry.reason)}</Text>
  }
  const c = entry.card
  return (
    <Box key={id} flexDirection="column" borderStyle="round" paddingX={1}>
      <Text>
        <Text bold>{c.title || c.id}</Text>
        <Text dimColor>  {c.id}</Text>
      </Text>
      <Text>
        <Text color={STATUS_COLOR[c.status] ?? 'subtle'}>● {c.status}</Text>
        {c.steps.length > 0 ? <Text dimColor>  {stepBar(c.steps)} {stepCount(c.steps)}</Text> : null}
      </Text>
      {asked.map((line, n) => (
        <Text key={`q${n}`} color="warning" wrap="truncate-end">{line}</Text>
      ))}
    </Box>
  )
}

/**
 * An assistant message with wipnote-ref fences: each ref as a card, and each
 * stretch of text between them drawn by `drawText`.
 *
 * `drawText` hands the text to the plugins beneath and then the engine, so a
 * fence another plugin draws (a lulawrite block) still reaches that plugin. A
 * plain Markdown element here would take the whole reply over and show the
 * other plugin's fence raw (feat-fd3637f9).
 */
export async function drawAssistant(
  els: Elements,
  segments: readonly Segment[],
  all: Record<string, CardEntry | undefined>,
  asked: Record<string, readonly string[] | undefined>,
  drawText: (text: string) => RenderChildren | Promise<RenderChildren>,
) {
  const { Box } = els
  const parts = await Promise.all(
    segments.map((s, i) => {
      if (s.kind === 'text') return s.text.trim() === '' ? null : drawText(s.text)
      return (
        <Box key={`r${i}`} flexDirection="column">
          {s.ids.map(id => drawCard(els, id, all[id], asked[id] ?? []))}
        </Box>
      )
    }),
  )
  return <Box flexDirection="column">{parts}</Box>
}
