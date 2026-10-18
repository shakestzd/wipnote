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
import { REF_RULE, classifyExit, failureText, needsFetch, parseCard, parseWho, showArgv, splitRefs, statusLine, stepBar, stepCount } from './cards'
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

/** Loads one card into the atom; the write redraws every message that reads it. */
async function loadCard($: EngineInterface, id: string): Promise<void> {
  await setEntry($, id, { state: 'loading', at: await $.clock.now() })
  const r = await fetchCard($, id)
  const at = await $.clock.now()
  await setEntry($, id, 'card' in r ? { state: 'loaded', card: r.card, at } : { state: 'failed', reason: r.fail, at })
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
 */
async function refreshStatus($: EngineInterface): Promise<void> {
  const who = await wipnoteJSON($, ['wipnote', 'who', '--json'])
  const workItem = who.ok ? parseWho(who.stdout) : null
  if (workItem === null) return
  const r = workItem ? await fetchCard($, workItem) : null
  const card = r && 'card' in r ? r.card : null
  $.ui.status(statusLine(card, workItem))
  if (card) await setEntry($, card.id, { state: 'loaded', card, at: await $.clock.now() })
}

/**
 * After a turn, re-fetches the cards that can still change: any not done, and
 * any that failed (the user may have pulled). A done item rarely changes, so
 * it waits for the TTL instead of costing a process every turn.
 */
async function refreshOpenCards($: EngineInterface): Promise<void> {
  const all = await read($, cards)
  const ids = Object.entries(all)
    .filter(([, e]) => e.state === 'failed' || (e.state === 'loaded' && e.card.status !== 'done'))
    .map(([id]) => id)
    .slice(-MAX_REFRESH)
  for (const id of ids) void loadCard($, id)
}

const MAX_REFRESH = 10

const STATUS_COLOR: Record<string, string> = {
  done: 'success', 'in-progress': 'warning', blocked: 'error', todo: 'subtle',
}

export const register: Register = on => {
  on('session.start', async ($, e, next) => {
    void refreshStatus($)
    return next(e)
  })

  on('turn.complete', async ($, e, next) => {
    void refreshStatus($)
    void refreshOpenCards($)
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
