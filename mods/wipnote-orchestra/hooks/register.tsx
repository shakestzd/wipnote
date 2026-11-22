import { atom, read, update } from 'claude-code'
import type { EngineInterface, Register } from 'claude-code'

import type { CardEntry, RunRow, WorkCard } from '../types'
import { callEnded, callStarted, reconcileRows, spawnedRows } from './agents'
import type { Hint } from './agents'
import type { BandData } from './band'
import { drawBand } from './band'
import { CLI_TIMEOUT_MS, cardOf, cardResult, changedIds, cliResult, drawAssistant, entryOf, openIds, questionsOf, replyIds, replyText, staleIds, transcriptIds } from './refcards'
import { Latest, parseWho, splitRefs } from './refs'
import type { CliResult } from './refs'
import { fresh, itemQuestionsArgv, openArgv, parseOpen, questionsText } from './questions'
import type { OpenQuestion } from './questions'
import { FOLDER_FILES, GIT_COMMON_ARGV, LEDGER_FILE, RUNS_DIR, commonCheckout, emptyLedger, folderFacts, ledgerKey, ledgerRead, newRunCache, parentDir, plainFiles, runIds, runRow } from './runs'
import type { LedgerCache, RunCache } from './runs'
import { aboveRoom, belowRoom, hintOf, isBusy, promptInfo, trackIds, wantedMode, withHint, withTracks } from './glue'
import type { SwapState } from './swap'
import { swapSeen, swapStarted } from './swap'
import { applyWipnote, modelLabel, describeCall, parseItemJson, parseTrackText, showArgv, pillIndent, zoneOf } from './model'
import { parseStatusCounts, viewOf } from './workview'
import type { WorkView } from './workview'

export type Elements = ReturnType<EngineInterface['ui']['resolve']>

const prompt = atom({ plugin: 'wipnote-orchestra', key: 'prompt' } as const, null)
const agents = atom({ plugin: 'wipnote-orchestra', key: 'agents' } as const, [])
const tally = atom({ plugin: 'wipnote-orchestra', key: 'tally' } as const, { done: 0, failed: 0 })
const items = atom({ plugin: 'wipnote-orchestra', key: 'items' } as const, [])
const cards = atom({ plugin: 'wipnote-orchestra', key: 'cards' } as const, [])
const mainTool = atom({ plugin: 'wipnote-orchestra', key: 'mainTool' } as const, null)
const now = atom({ plugin: 'wipnote-orchestra', key: 'now' } as const, 0)
const mode = atom({ plugin: 'wipnote-orchestra', key: 'mode' } as const, 'full')
const placement = atom({ plugin: 'wipnote-orchestra', key: 'placement' } as const, 'below')
const permissionMode = atom({ plugin: 'wipnote-orchestra', key: 'permissionMode' } as const, null)
const claim = atom({ plugin: 'wipnote-orchestra', key: 'claim' } as const, null)
const counts = atom({ plugin: 'wipnote-orchestra', key: 'counts' } as const, null)
const runs = atom({ plugin: 'wipnote-orchestra', key: 'runs' } as const, [])
const questions = atom({ plugin: 'wipnote-orchestra', key: 'questions' } as const, null)
const refs = atom({ plugin: 'wipnote-orchestra', key: 'refs' } as const, {})
// An item's open questions, as card lines; a failed read keeps the last ones.
const cardQuestions = atom({ plugin: 'wipnote-orchestra', key: 'cardQuestions' } as const, {})

const KEEP_DONE_MS = 90_000

async function refreshPrompt($: EngineInterface) {
  const [cwd, home, usage, git, top, model] = await Promise.all([
    $.session.cwd(),
    $.env.get('HOME'),
    $.session.usage(),
    $.process.run(['git', 'status', '--porcelain=v1', '--branch'], { timeoutMs: 5000 }).catch(() => undefined),
    $.process.run(['git', 'rev-parse', '--show-toplevel'], { timeoutMs: 5000 }).catch(() => undefined),
    $.session.model().catch(() => undefined),
  ])
  await update($, prompt, () => promptInfo({ cwd, home, usage, git, top, model }))
}

/** Runs a read-only wipnote command as this session; a spawn failure or timeout is 'unavailable'. */
async function wipnoteJSON($: EngineInterface, argv: string[]): Promise<CliResult> {
  try {
    const sessionId = await $.session.id()
    return cliResult(await $.process.run(argv, { timeoutMs: CLI_TIMEOUT_MS, env: { CLAUDE_CODE_SESSION_ID: sessionId } }))
  } catch {
    return cliResult(undefined)
  }
}

// Overlapping lookups finish in any order. Each one takes a ticket, and only
// the newest ticket may write, so an older, slower lookup never wins.
const latest = new Latest()

function setEntry($: EngineInterface, id: string, entry: CardEntry): Promise<unknown> {
  return update($, refs, all => ({ ...all, [id]: entry }))
}

/**
 * Stores a card from `show` output the band already fetched, so an active item costs one lookup,
 * not one for the band and one for its card. It takes a ticket like loadCard, so it never
 * overwrites a newer lookup.
 */
async function storeCard($: EngineInterface, id: string, stdout: string): Promise<void> {
  const ticket = latest.take(`card:${id}`)
  const card = cardOf(id, stdout)
  if (card === undefined) return
  if (latest.isNewest(`card:${id}`, ticket)) await setEntry($, id, { state: 'loaded', card, at: await $.clock.now() })
}

/** Reads the item's open questions into the atom, for its card; a failed read keeps the last ones. */
async function loadQuestions($: EngineInterface, id: string): Promise<void> {
  const lines = questionsOf(await wipnoteJSON($, itemQuestionsArgv(id)))
  if (lines !== undefined) await update($, cardQuestions, all => ({ ...all, [id]: lines }))
}

/** Loads one card into the atom; the write redraws every message that reads it. */
async function loadCard($: EngineInterface, id: string): Promise<void> {
  const ticket = latest.take(`card:${id}`)
  await setEntry($, id, { state: 'loading', at: await $.clock.now() })
  const r = cardResult(id, await wipnoteJSON($, showArgv(id)))
  if (!latest.isNewest(`card:${id}`, ticket)) return
  const at = await $.clock.now()
  if ('card' in r) await loadQuestions($, id).catch(() => undefined)
  await setEntry($, id, entryOf(r, at))
}

/** Starts a fetch for every id that is missing or stale. */
async function loadStale($: EngineInterface, ids: string[]): Promise<void> {
  const all = await read($, refs)
  for (const id of staleIds(all, ids, await $.clock.now())) void loadCard($, id)
}

/** After a Bash call ends, re-fetches the cards it changed, so a card moves mid-turn. Only drawn cards are fetched. */
async function refreshChanged($: EngineInterface, command: string): Promise<void> {
  for (const id of changedIds(await read($, refs), command)) void loadCard($, id)
}

/** Loads the cards the transcript already references: the refs atom starts empty in a new or resumed session. */
async function hydrate($: EngineInterface): Promise<void> {
  const ids = transcriptIds(await $.session.messages())
  if (ids.length > 0) await loadStale($, ids)
}

/** After a turn: the open cards (stalest first) and any the transcript references but the atom lacks. */
function refreshAfterTurn($: EngineInterface): void {
  void read($, refs).then(all => openIds(all).forEach(id => void loadCard($, id))).catch(() => undefined)
  void hydrate($).catch(() => undefined)
}

/** Syncs the agent rows with `$.agent.list()` and counts the ones that ended (reconcileRows). */
async function reconcileAgents($: EngineInterface, at: number, hints: ReadonlyMap<string, Hint>) {
  const listed = await $.agent.list()
  let ended = { done: 0, failed: 0 }
  await update($, agents, rows => {
    const next = reconcileRows(rows, listed, at, hints)
    ended = next.ended
    return next.rows
  })
  if (ended.done + ended.failed > 0) {
    await update($, tally, t => ({ done: t.done + ended.done, failed: t.failed + ended.failed }))
  }
}

/** A subagent's tool call starts or ends: its row counts the call. */
async function noteAgentCall($: EngineInterface, id: string, doing: string, pending: ReadonlyMap<string, Hint>) {
  const at = await $.clock.now()
  await update($, agents, rows => callStarted(rows, id, doing, pending, at))
}

async function endAgentCall($: EngineInterface, id: string) {
  try {
    const at = await $.clock.now()
    await update($, agents, rows => callEnded(rows, id, at))
  } catch {
    /* the next call or the list sets it again */
  }
}

/** The ledger's rows, read again only when its size or mtime changed. */
async function readLedger($: EngineInterface, path: string, last: LedgerCache): Promise<LedgerCache> {
  const key = ledgerKey(await $.fs.stat(path).catch(() => undefined))
  if (key === undefined) return emptyLedger()
  if (key === last.key) return last
  return ledgerRead(key, await $.fs.read(path).then(String).catch(() => undefined))
}

async function readFolder($: EngineInterface, dir: string) {
  const files = plainFiles(await $.fs.list(dir))
  const read = (name: string) => (files.has(name) ? $.fs.read(`${dir}/${name}`).then(String).catch(() => '') : Promise.resolve(''))
  return folderFacts(files, await Promise.all(FOLDER_FILES.map(read)))
}

/** The project whose .wipnote/ holds the runs, found as wipnote's CLI finds it: env, main checkout, then up from the cwd. */
async function findProject($: EngineInterface): Promise<string | null> {
  const has = (dir: string) => $.fs.exists(`${dir}/.wipnote`).catch(() => false)
  const env = await $.env.get('WIPNOTE_PROJECT_DIR').catch(() => undefined)
  if (env !== undefined && env !== '' && (await has(env))) return env
  const git = await $.process.run(GIT_COMMON_ARGV, { timeoutMs: 5000 }).catch(() => undefined)
  const main = commonCheckout(git?.exitCode, git?.stdout)
  if (main !== undefined && (await has(main))) return main
  let dir = (await $.session.cwd()).replace(/\/+$/, '')
  while (dir !== '') {
    if (await has(dir)) return dir
    dir = parentDir(dir)
  }
  return (await has('/')) ? '/' : null
}

async function readRuns($: EngineInterface, project: string, cache: RunCache): Promise<RunRow[]> {
  cache.ledger = await readLedger($, `${project}/${LEDGER_FILE}`, cache.ledger)
  const dir = `${project}/${RUNS_DIR}`
  const ids = runIds(await $.fs.list(dir).catch(() => []), cache)
  const rows: RunRow[] = []
  for (const id of ids) {
    const facts = cache.settled.get(id) ?? (await readFolder($, `${dir}/${id}`).catch(() => undefined))
    if (facts !== undefined) rows.push(runRow(id, facts, cache))
  }
  return rows
}

/** Titles, status and track progress for the work items in view: the session's own and the agents'. */
async function refreshCards($: EngineInterface) {
  const [work, rows] = await Promise.all([activeItems($), read($, agents)])
  const ids = [...new Set([...work, ...rows.map(row => row.workItem).filter((id): id is string => id !== undefined)])]
  const found: WorkCard[] = []
  for (const id of ids.slice(0, 12)) {
    const argv = showArgv(id)
    if (argv === undefined) continue
    const ran = await $.process.run(argv, { timeoutMs: 5000 }).catch(() => undefined)
    const item = ran?.exitCode === 0 ? parseItemJson(ran.stdout) : undefined
    if (item === undefined || ran === undefined) continue
    found.push({ id, ...item })
    await storeCard($, id, ran.stdout)
  }
  const tracks = new Map<string, ReturnType<typeof parseTrackText>>()
  for (const trackId of trackIds(found)) {
    const ran = await $.process.run(['wipnote', 'track', 'show', trackId], { timeoutMs: 5000 }).catch(() => undefined)
    tracks.set(trackId, ran?.exitCode === 0 ? parseTrackText(ran.stdout) : undefined)
  }
  await update($, cards, () => withTracks(found, tracks))
  await refreshCounts($)
}

/** The project's in-progress and open counts for the band's summary line; the last ones stay on a failure. */
async function refreshCounts($: EngineInterface) {
  const ran = await $.process.run(['wipnote', 'status'], { timeoutMs: 5000 }).catch(() => undefined)
  const found = ran?.exitCode === 0 ? parseStatusCounts(ran.stdout) : undefined
  if (found !== undefined) await update($, counts, () => found)
}

/** Question ids already seen. The first read only marks them: old questions are not new. */
const toasted = new Set<string>()
let hasReadQuestions = false

let isReadingQuestions = false
/**
 * The open questions for the band; a new blocking one toasts. The last list stays on a failure.
 * Nothing is read while the band is off, and a tick that finds a call still running skips.
 */
async function refreshQuestions($: EngineInterface) {
  if (isReadingQuestions || (await read($, mode)) === 'off') return
  isReadingQuestions = true
  try {
    const ran = await $.process.run(openArgv(), { timeoutMs: 5000 }).catch(() => undefined)
    const rows: OpenQuestion[] | undefined = ran?.exitCode === 0 ? parseOpen(ran.stdout) : undefined
    if (rows === undefined) return
    const added = fresh(rows, toasted)
    if (hasReadQuestions) for (const q of added) $.ui.toast(`Blocking question on ${q.itemId}: ${q.text}`)
    hasReadQuestions = true
    await update($, questions, () => questionsText(rows))
  } finally {
    isReadingQuestions = false
  }
}

/**
 * The session's claimed work item (`wipnote who`). When `who` cannot be read
 * the last claim stays: an unknown claim is not "no claim".
 */
async function refreshClaim($: EngineInterface) {
  const who = await wipnoteJSON($, ['wipnote', 'who', '--json'])
  const id = who.ok ? parseWho(who.stdout) : null
  if (id !== null) await update($, claim, () => (id === '' ? null : id))
}

/** The work items in view: the session's claim first, then the ones shell commands started. */
async function activeItems($: EngineInterface): Promise<string[]> {
  const [claimed, started] = await Promise.all([read($, claim), read($, items)])
  return claimed === null ? started : [claimed, ...started.filter(id => id !== claimed)]
}

/**
 * Records the permission mode a main-loop `agent.spawn` carries (a subagent's spawn carries its
 * own loop's). It is the mod's one source for the mode pill: classic.* events, which carry it
 * on every prompt, may be continued past the mod by a managed plugin, so it hooks none.
 */
async function noteMode($: EngineInterface, mode: string | undefined, parentAgentId: string | undefined) {
  if (mode !== undefined && parentAgentId === undefined) await update($, permissionMode, () => mode)
}

/**
 * The project's delegate runs, followed read-only: every call reads the open runs again (a run
 * folder Go writes shows within one tick) and writes the atom only when a row changed. The
 * project is looked up again while none is found and after /resume, /branch or /clear.
 */
const runCache = newRunCache()
let runsProject: string | null | undefined
let isReadingRuns = false

async function refreshRuns($: EngineInterface, tick: number) {
  if (isReadingRuns) return
  isReadingRuns = true
  try {
    if (runsProject === undefined || (runsProject === null && tick % 10 === 0)) runsProject = await findProject($)
    const next = runsProject === null ? [] : await readRuns($, runsProject, runCache)
    const last = await read($, runs)
    if (JSON.stringify(last) !== JSON.stringify(next)) await update($, runs, () => next)
  } finally {
    isReadingRuns = false
  }
}

/** A swapped-in conversation: its project's runs, its claim, the cards its transcript references. */
function rehydrate($: EngineInterface) {
  runsProject = undefined
  void refreshClaim($).catch(() => undefined)
  void hydrate($).catch(() => undefined)
}

const sessionId = ($: EngineInterface): Promise<string | undefined> => $.session.id().catch(() => undefined)

/** Rehydrates once when the session id moved since it was last seen (swap.ts). */
async function checkSwap($: EngineInterface, swap: SwapState) {
  if (swapSeen(swap, await sessionId($))) rehydrate($)
}

async function bandData($: EngineInterface, zone: string, view: WorkView): Promise<BandData> {
  const [info, rows, ended, work, known, doing, delegated, at, total, asked] = await Promise.all([
    read($, prompt),
    read($, agents),
    read($, tally),
    activeItems($),
    read($, cards),
    read($, mainTool),
    read($, runs),
    read($, now),
    read($, counts),
    read($, questions),
  ])
  return { info, rows, tally: ended, keepMs: KEEP_DONE_MS, work, cards: known, counts: total ?? undefined, questions: asked ?? undefined, view, doing, runs: delegated, at, zone }
}

type On = Parameters<Register>[0]

/** One second of the band's clock: the swap check, the agent sync and the staggered refreshes. */
async function tick($: EngineInterface, n: number, pending: ReadonlyMap<string, Hint>) {
  const at = await $.clock.now()
  const rows = await read($, agents)
  if (isBusy(rows, at, KEEP_DONE_MS) || n % 30 === 0) await update($, now, () => at)
  if (n % 2 === 0) await reconcileAgents($, at, pending)
  if (n % 5 === 0) await refreshPrompt($)
  if (n % 5 === 1) await refreshCards($)
  if (n % 5 === 3) await refreshQuestions($)
}

/** session.start: the /orchestra command, the first reads and the one-second clock. */
function onSessionStart(on: On, swap: SwapState, pending: ReadonlyMap<string, Hint>) {
  on('session.start', async ($, e, next) => {
    await $.command.register({
      name: 'orchestra',
      description: 'Orchestra band: full (agents by work item, stalled or failed ones), compact (one line), off; above or below the prompt',
      argumentHint: 'full|compact|off|above|below',
    })
    await refreshPrompt($).catch(() => undefined)
    swapStarted(swap, await sessionId($))
    void refreshClaim($).catch(() => undefined)
    void hydrate($).catch(() => undefined)
    const started = await $.clock.now()
    await update($, now, () => started)
    runsProject = undefined
    void refreshRuns($, 0).catch(() => undefined)
    let n = 0
    $.clock.every(1000, () => {
      n += 1
      void checkSwap($, swap).catch(() => undefined) // a swap session.end did not see
      void tick($, n, pending).catch(() => undefined)
      void refreshRuns($, n).catch(() => undefined)
    })
    return next(e)
  })
}

/** /orchestra full|compact|off|above|below; no argument toggles off and full. */
function onCommand(on: On) {
  on('command.run', { command: 'orchestra' }, async ($, e) => {
    const arg = e.args.trim()
    if (arg === 'above' || arg === 'below') {
      await update($, placement, () => arg)
      return { text: `Orchestra band: ${arg} the prompt.` }
    }
    const current = await read($, mode)
    const wanted = wantedMode(arg, current)
    await update($, mode, () => wanted)
    return { text: `Orchestra band: ${wanted}.` }
  })
}

/** An agent spawn: its work item and model hint, then its row once the engine names it. */
function onAgentSpawn(on: On, pending: Map<string, Hint>) {
  on('agent.spawn', async ($, e, next) => {
    const hint = hintOf(e.prompt, e.model)
    pending.set(e.description, hint)
    // A main-loop spawn carries the main loop's mode: a source no classic.* skip can hide.
    await noteMode($, e.permissionMode, e.parentAgentId)
    const at = await $.clock.now()
    const ran = await next(e)
    const id = ran.agentId
    if (id !== undefined) {
      const model = modelLabel(ran.model)
      const fresh = { id, type: e.subagentType, description: e.description }
      await update($, agents, rows => spawnedRows(rows, fresh, hint, model, at))
      pending.delete(e.description)
    }
    return ran
  }).catch(($, e, next) => next(e))
}

/** Every tool call: the main loop's current call, a subagent's row, and the items a Bash call names. */
function onToolCall(on: On, pending: ReadonlyMap<string, Hint>) {
  on('tool.call', async ($, e, next) => {
    const input = e as unknown as Record<string, unknown>
    const doing = describeCall(String(e.tool), input)
    if (e.agentId === undefined) await update($, mainTool, () => doing)
    else await noteAgentCall($, e.agentId, doing, pending)
    if (e.tool === 'Bash') {
      const command = e.command
      await update($, items, list => applyWipnote(list, command))
    }
    try {
      const r = await next(e)
      if (e.tool === 'Bash') void refreshChanged($, e.command).catch(() => undefined)
      return r
    } finally {
      if (e.agentId !== undefined) await endAgentCall($, e.agentId)
    }
  }).catch(($, e, next) => next(e))
}

/** turn.complete refreshes; session.end catches a /resume, /branch or /clear swap. */
function onTurnAndSwap(on: On, swap: SwapState) {
  on('turn.complete', async ($, e, next) => {
    await update($, mainTool, () => null)
    await refreshPrompt($).catch(() => undefined)
    void refreshClaim($).catch(() => undefined)
    refreshAfterTurn($)
    return next(e)
  })

  // /resume, /branch and /clear swap the conversation, but session.start does not fire again.
  // The swap is caught by the changed session id: here, at session.end, and on the tick.
  on('session.end', async ($, e, next) => {
    const r = await next(e)
    if (e.reason === 'clear' || e.reason === 'resume') await checkSwap($, swap)
    return r
  })
}

/** The band above the prompt, when placed there. */
function onRenderAbove(on: On, zone: string, view: WorkView) {
  on('ui.render', { component: 'AbovePrompt' }, async ($, e, next) => {
    const [shown, where] = await Promise.all([read($, mode), read($, placement)])
    if (e.props.hasSurvey || shown === 'off' || where !== 'above') return next(e)
    const tree = drawBand($.ui.resolve(e), await bandData($, zone, view), aboveRoom(e.props, shown))
    return tree ?? next(e)
  })
}

/** The band below the prompt, under the engine's own hint line. */
function onRenderBelow(on: On, zone: string, view: WorkView) {
  on('ui.render', { component: 'PromptHint' }, async ($, e, next) => {
    const [shown, where] = await Promise.all([read($, mode), read($, placement)])
    if (shown === 'off' || where !== 'below') return next(e)
    const els = $.ui.resolve(e)
    const tree = drawBand(els, await bandData($, zone, view), belowRoom(e.viewport, e.props.isWorking, shown))
    if (tree === null) return next(e)
    // Row one is the engine's own hint line, as it draws it; the band takes the rows under it,
    // shifted left past the mode pill the engine draws beside the tree (noteMode, pillIndent).
    return withHint(els, tree, e.props.hint, pillIndent(await read($, permissionMode), e.props.hint))
  })
}

/** A stored reply with a wipnote-ref fence starts the card fetches; the render hook only reads the result. */
function onRefs(on: On) {
  on('session.append', { door: 'response' }, async ($, e, next) => {
    const r = await next(e)
    const ids = replyIds(replyText(e.message.content))
    if (ids.length > 0) void loadStale($, ids)
    return r
  })
  on('ui.render', { component: 'AssistantMessage' }, async ($, e, next) => {
    const segments = splitRefs(e.props.text)
    if (!segments.some(s => s.kind === 'refs')) return next(e)
    const els = $.ui.resolve(e)
    const [all, asked] = await Promise.all([read($, refs), read($, cardQuestions)])
    // Text between the cards goes to the plugins beneath, so their fences still draw.
    return drawAssistant(els, segments, all, asked, text => next({ ...e, props: { ...e.props, text } }))
  })
}

export const register: Register = (on, options) => {
  const zone = zoneOf(options.timeZone)
  const view = viewOf(options)
  const pending = new Map<string, Hint>()
  const swap: SwapState = {}
  onSessionStart(on, swap, pending)
  onCommand(on)
  onAgentSpawn(on, pending)
  onToolCall(on, pending)
  onTurnAndSwap(on, swap)
  onRefs(on)
  onRenderAbove(on, zone, view)
  onRenderBelow(on, zone, view)
}
