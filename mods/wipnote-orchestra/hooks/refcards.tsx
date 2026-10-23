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

import { atom, read, update } from 'claude-code'
import type { EngineInterface, Register } from 'claude-code'

import type { Card, CardEntry, FailKind } from '../types'
import { Latest, changedRefs, classifyExit, failureText, needsFetch, parseCard, pickRefresh, refsInMessages, showArgv, splitRefs, stepBar, stepCount } from './refs'
import type { CliResult } from './refs'

type On = Parameters<Register>[0]

const refs = atom({ plugin: 'wipnote-orchestra', key: 'refs' } as const, {})
const CLI_TIMEOUT_MS = 15_000
const MAX_REFRESH = 10
const SCAN_MESSAGES = 200

const STATUS_COLOR: Record<string, string> = {
  done: 'success', 'in-progress': 'warning', blocked: 'error', todo: 'subtle',
}

/** Runs a read-only wipnote command as this session; a spawn failure or timeout is 'unavailable'. */
export async function wipnoteJSON($: EngineInterface, argv: string[]): Promise<CliResult> {
  try {
    const sessionId = await $.session.id()
    const r = await $.process.run(argv, {
      timeoutMs: CLI_TIMEOUT_MS,
      env: { CLAUDE_CODE_SESSION_ID: sessionId },
    })
    return classifyExit(r.exitCode, r.stdout, r.stderr)
  } catch {
    return { ok: false, kind: 'unavailable' }
  }
}

async function fetchCard($: EngineInterface, id: string): Promise<{ card: Card } | { fail: FailKind }> {
  const r = await wipnoteJSON($, showArgv(id))
  if (!r.ok) return { fail: r.kind }
  try {
    return { card: parseCard(id, r.stdout) }
  } catch {
    return { fail: 'error' }
  }
}

function setEntry($: EngineInterface, id: string, entry: CardEntry): Promise<unknown> {
  return update($, refs, all => ({ ...all, [id]: entry }))
}

// Overlapping lookups finish in any order. Each one takes a ticket, and only
// the newest ticket may write, so an older, slower lookup never wins.
const latest = new Latest()

/**
 * Stores a card from `show` output the band already fetched, so an active
 * item costs one lookup, not one for the band and one for its card. It takes
 * a ticket like loadCard, so it never overwrites a newer lookup.
 */
export async function storeCard($: EngineInterface, id: string, stdout: string): Promise<void> {
  const ticket = latest.take(`card:${id}`)
  let card: Card
  try {
    card = parseCard(id, stdout)
  } catch {
    return
  }
  if (latest.isNewest(`card:${id}`, ticket)) await setEntry($, id, { state: 'loaded', card, at: await $.clock.now() })
}

/** Loads one card into the atom; the write redraws every message that reads it. */
async function loadCard($: EngineInterface, id: string): Promise<void> {
  const ticket = latest.take(`card:${id}`)
  await setEntry($, id, { state: 'loading', at: await $.clock.now() })
  const r = await fetchCard($, id)
  if (!latest.isNewest(`card:${id}`, ticket)) return
  const at = await $.clock.now()
  await setEntry($, id, 'card' in r ? { state: 'loaded', card: r.card, at } : { state: 'failed', reason: r.fail, at })
}

/** Starts a fetch for every id that is missing or stale. */
async function loadStale($: EngineInterface, ids: string[]): Promise<void> {
  const all = await read($, refs)
  const now = await $.clock.now()
  for (const id of ids) if (needsFetch(all[id], now)) void loadCard($, id)
}

/**
 * After a turn, re-fetches the cards that can still change: any not done, and
 * any that failed (the user may have pulled). A done item rarely changes, so
 * it waits for the TTL instead of costing a process every turn.
 */
async function refreshOpenCards($: EngineInterface): Promise<void> {
  const all = await read($, refs)
  for (const id of pickRefresh(Object.entries(all), MAX_REFRESH)) void loadCard($, id)
}

/**
 * After a Bash call ends, re-fetches the cards it changed (a ticked step, a
 * start or complete), so a card moves mid-turn instead of at the turn's end.
 * Only cards already drawn are fetched; others load when a reply names them.
 */
export async function refreshChanged($: EngineInterface, command: string): Promise<void> {
  const ids = changedRefs(command)
  if (ids.length === 0) return
  const all = await read($, refs)
  for (const id of ids) if (all[id]) void loadCard($, id)
}

/**
 * Loads the cards the transcript already references. The refs atom starts
 * empty in a new or resumed session, and session.append sees only new
 * replies, so saved references are read back from the transcript.
 */
export async function hydrate($: EngineInterface): Promise<void> {
  const messages = await $.session.messages()
  if (!Array.isArray(messages)) return
  const ids = refsInMessages(messages.slice(-SCAN_MESSAGES), MAX_REFRESH)
  if (ids.length > 0) await loadStale($, ids)
}

/** After a turn: the open cards (stalest first) and any the transcript references but the atom lacks. */
export function refreshAfterTurn($: EngineInterface): void {
  void refreshOpenCards($).catch(() => undefined)
  void hydrate($).catch(() => undefined)
}

/**
 * The wipnote-ref hooks: fetch on reply, the model rule, and the card drawing.
 * register.tsx owns session.start, classic.SessionStart and turn.complete (an
 * event takes one unmatched hook per mod) and calls hydrate and refreshAfterTurn.
 */
export function registerRefs(on: On): void {
  // A stored reply with a wipnote-ref fence starts the card fetches; the
  // render hook below only reads the result.
  on('session.append', { door: 'response' }, async ($, e, next) => {
    const r = await next(e)
    const text = e.message.content
      .map(b => (b.type === 'text' && typeof b.text === 'string' ? b.text : ''))
      .join('\n')
    const ids = splitRefs(text).flatMap(s => (s.kind === 'refs' ? s.ids : []))
    if (ids.length > 0) void loadStale($, ids)
    return r
  })

  on('ui.render', { component: 'AssistantMessage' }, async ($, e, next) => {
    const segments = splitRefs(e.props.text)
    if (!segments.some(s => s.kind === 'refs')) return next(e)

    const { Box, Text, Markdown } = $.ui.resolve(e)
    const all = await read($, refs)

    return (
      <Box flexDirection="column">
        {segments.map((s, i) =>
          s.kind === 'text' ? (
            <Markdown key={`t${i}`} text={s.text} />
          ) : (
            <Box key={`r${i}`} flexDirection="column">
              {s.ids.map(id => {
                const entry = all[id]
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
                  </Box>
                )
              })}
            </Box>
          ),
        )}
      </Box>
    )
  })
}
