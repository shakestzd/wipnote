// wipnote-cards: wipnote at a glance, for few model tokens.
//
// 1. Status line (no tokens): the session's claimed work item, its status and
//    step progress, refreshed when the session starts and after each turn.
// 2. Cards (~10 tokens): the model ends a report with a ```wipnote-ref fence
//    holding only an id; this mod draws it as a card from `wipnote ... show
//    --format json`. The stored message keeps the short fence, so later turns
//    read ~10 tokens, and without the mod the fence still reads as an id.
//
// The one-line rule that asks for the fence is sent only while this mod is
// loaded (prompt.compose), so the model never writes fences nobody draws.
//
// A render hook never writes state, so cards are fetched from events: when a
// reply is stored (session.append) and after each turn (turn.complete). The
// render hook only reads the cards atom and draws.

import { atom, read, update } from 'claude-code'
import type { EngineInterface, Register } from 'claude-code'

import type { Card, CardEntry, FailKind } from '../types'
import { Latest, REF_RULE, classifyExit, failureText, needsFetch, parseCard, parseWho, pickRefresh, refsInMessages, showArgv, splitRefs, statusLine, stepBar, stepCount } from './cards'
import type { CliResult } from './cards'

const cards = atom({ plugin: 'wipnote-cards', key: 'cards' } as const, {})
const CLI_TIMEOUT_MS = 15_000

/** Runs a read-only wipnote command; a spawn failure or timeout is 'unavailable'. */
async function wipnoteJSON($: EngineInterface, argv: string[]): Promise<CliResult> {
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
  return update($, cards, all => ({ ...all, [id]: entry }))
}

// Overlapping lookups finish in any order. Each one takes a ticket, and only
// the newest ticket may write, so an older, slower lookup never wins.
const latest = new Latest()

/** Loads one card into the atom; the write redraws every message that reads it. */
async function loadCard($: EngineInterface, id: string): Promise<void> {
  const ticket = latest.take(`card:${id}`)
  await setEntry($, id, { state: 'loading', at: await $.clock.now() })
  const r = await fetchCard($, id)
  if (!latest.isNewest(`card:${id}`, ticket)) return
  await setEntry($, id, entryFrom(r, await $.clock.now()))
}

function entryFrom(r: { card: Card } | { fail: FailKind }, at: number): CardEntry {
  return 'card' in r ? { state: 'loaded', card: r.card, at } : { state: 'failed', reason: r.fail, at }
}

/** Starts a fetch for every id that is missing or stale. */
async function loadStale($: EngineInterface, ids: string[]): Promise<void> {
  const all = await read($, cards)
  const now = await $.clock.now()
  for (const id of ids) if (needsFetch(all[id], now)) void loadCard($, id)
}

/**
 * Sets the status line from the session's claimed work item. When `who`
 * itself fails, the last status stays: an unknown claim is not "no claim".
 * The item's card lookup takes a card ticket too, so it shares one order with
 * loadCard and an older lookup of either kind never overwrites a newer one.
 */
async function refreshStatus($: EngineInterface): Promise<void> {
  const ticket = latest.take('status')
  const who = await wipnoteJSON($, ['wipnote', 'who', '--json'])
  const workItem = who.ok ? parseWho(who.stdout) : null
  if (workItem === null) return
  if (!workItem) {
    if (latest.isNewest('status', ticket)) $.ui.status(statusLine(null, ''))
    return
  }
  const cardKey = `card:${workItem}`
  const cardTicket = latest.take(cardKey)
  const r = await fetchCard($, workItem)
  if (latest.isNewest(cardKey, cardTicket)) await setEntry($, workItem, entryFrom(r, await $.clock.now()))
  if (latest.isNewest('status', ticket)) $.ui.status(statusLine('card' in r ? r.card : null, workItem))
}

/**
 * After a turn, re-fetches the cards that can still change: any not done, and
 * any that failed (the user may have pulled). A done item rarely changes, so
 * it waits for the TTL instead of costing a process every turn.
 */
async function refreshOpenCards($: EngineInterface): Promise<void> {
  const all = await read($, cards)
  for (const id of pickRefresh(Object.entries(all), MAX_REFRESH)) void loadCard($, id)
}

/**
 * Loads the cards the transcript already references. The cards atom starts
 * empty in a new or resumed session, and session.append sees only new
 * replies, so saved references are read back from the transcript.
 */
async function hydrate($: EngineInterface): Promise<void> {
  const messages = await $.session.messages()
  if (!Array.isArray(messages)) return
  const ids = refsInMessages(messages.slice(-SCAN_MESSAGES), MAX_REFRESH)
  if (ids.length > 0) await loadStale($, ids)
}

const MAX_REFRESH = 10
const SCAN_MESSAGES = 200

const STATUS_COLOR: Record<string, string> = {
  done: 'success', 'in-progress': 'warning', blocked: 'error', todo: 'subtle',
}

export const register: Register = on => {
  on('session.start', async ($, e, next) => {
    void refreshStatus($)
    void hydrate($)
    return next(e)
  })

  // /resume, /branch and /clear swap the conversation and reset the cards atom,
  // but session.start does not fire again; the settings SessionStart does.
  on('classic.SessionStart', async ($, e, next) => {
    if (e.source !== 'startup') {
      void refreshStatus($)
      void hydrate($)
    }
    return next(e)
  })

  // After each turn: the status line, the open cards (stalest first), and any
  // card the transcript references but the atom lacks (a resumed conversation).
  on('turn.complete', async ($, e, next) => {
    void refreshStatus($)
    void refreshOpenCards($)
    void hydrate($)
    return next(e)
  })

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

  on('prompt.compose', async ($, e, next) => {
    const r = await next(e)
    return { sections: [...r.sections, { id: 'wipnote-cards:ref-rule', text: REF_RULE, scope: 'session' }] }
  })

  on('ui.render', { component: 'AssistantMessage' }, async ($, e, next) => {
    const segments = splitRefs(e.props.text)
    if (!segments.some(s => s.kind === 'refs')) return next(e)

    const { Box, Text, Markdown } = $.ui.resolve(e)
    const all = await read($, cards)

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
