import { atom, read, update } from 'claude-code'
import type { EngineInterface, Register } from 'claude-code'

import type { AgentRow, Mode, PromptInfo, WorkCard } from '../types'
import { endAgentCall, noteAgentCall, reconcileAgents } from './agents'
import type { Hint } from './agents'
import type { BandData } from './band'
import { drawBand } from './band'
import { hydrate, refreshAfterTurn, refreshChanged, registerRefs, storeCard, wipnoteJSON } from './refcards'
import { parseWho } from './refs'
import { fresh, openArgv, parseOpen, questionsText } from './questions'
import type { OpenQuestion } from './questions'
import { findProject, newRunCache, readRuns } from './runs'
import type { SwapState } from './swap'
import { swapSeen, swapStarted } from './swap'
import {
  keepFor,
  applyWipnote,
  baseName,
  modelLabel,
  describeCall,
  isLive,
  parseGitStatus,
  parseItemJson,
  parseTrackText,
  showArgv,
  pillIndent,
  shortDir,
  workItemIn,
  zoneOf,
} from './model'
import { parseStatusCounts, viewOf } from './workview'
import type { WorkView } from './workview'

const prompt = atom({ plugin: 'wipnote-orchestra', key: 'prompt' } as const, null)
// agents and tally are written in agents.ts, which declares them too.
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

const KEEP_DONE_MS = 90_000

async function refreshPrompt($: EngineInterface) {
  const [cwd, home, usage, git, top, model] = await Promise.all([
    $.session.cwd(),
    $.env.get('HOME'),
    $.session.usage(),
    $.process
      .run(['git', 'status', '--porcelain=v1', '--branch'], { timeoutMs: 5000 })
      .catch(() => undefined),
    $.process.run(['git', 'rev-parse', '--show-toplevel'], { timeoutMs: 5000 }).catch(() => undefined),
    $.session.model().catch(() => undefined),
  ])
  const root = top?.exitCode === 0 ? top.stdout.trim() : ''
  const info: PromptInfo = {
    dir: root !== '' ? baseName(root) : shortDir(cwd, home),
    dirty: 0,
    ahead: 0,
    behind: 0,
    ...(git?.exitCode === 0 ? parseGitStatus(git.stdout) : {}),
    model: model !== undefined ? modelLabel(model) : undefined,
    ctxPercent: usage.context.percent,
    limits: usage.rateLimits.map(({ kind, percentUsed, resetsAt }) => ({ kind, percentUsed, resetsAt })),
  }
  await update($, prompt, () => info)
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
  for (const trackId of new Set(found.map(card => card.trackId).filter((id): id is string => id !== undefined))) {
    const ran = await $.process.run(['wipnote', 'track', 'show', trackId], { timeoutMs: 5000 }).catch(() => undefined)
    tracks.set(trackId, ran?.exitCode === 0 ? parseTrackText(ran.stdout) : undefined)
  }
  const next = found.map(card => {
    const track = card.trackId === undefined ? undefined : tracks.get(card.trackId)
    return track === undefined ? card : { ...card, trackTitle: track.title, done: track.done, total: track.total }
  })
  await update($, cards, () => next)
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

async function sessionId($: EngineInterface): Promise<string | undefined> {
  return $.session.id().catch(() => undefined)
}

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
  const isBusy = rows.some(row => isLive(row.status) || at - (row.endedAt ?? 0) < keepFor(row.status, KEEP_DONE_MS))
  if (isBusy || n % 30 === 0) {
    await update($, now, () => at)
  }
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
    const wanted: Mode =
      arg === 'full' || arg === 'compact' || arg === 'off'
        ? arg
        : current === 'off'
          ? 'full'
          : 'off'
    await update($, mode, () => wanted)
    return { text: `Orchestra band: ${wanted}.` }
  })
}

/** An agent spawn: its work item and model hint, then its row once the engine names it. */
function onAgentSpawn(on: On, pending: Map<string, Hint>) {
  on('agent.spawn', async ($, e, next) => {
    const hint: Hint = {
      workItem: workItemIn(e.prompt),
      model: e.model !== undefined ? modelLabel(e.model) : undefined,
    }
    pending.set(e.description, hint)
    // A main-loop spawn carries the main loop's mode: a source no classic.* skip can hide.
    await noteMode($, e.permissionMode, e.parentAgentId)
    const at = await $.clock.now()
    const ran = await next(e)
    const id = ran.agentId
    if (id !== undefined) {
      const model = modelLabel(ran.model)
      await update($, agents, rows => {
        const row = rows.find(one => one.id === id)
        const base: AgentRow = row ?? {
          id,
          type: e.subagentType,
          description: e.description,
          status: 'running',
          startedAt: at,
          tools: 0,
        }
        return [
          ...rows.filter(one => one.id !== id),
          { ...base, workItem: base.workItem ?? hint.workItem, model },
        ]
      })
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
    const tree = drawBand($.ui.resolve(e), await bandData($, zone, view), {
      width: e.props.bodyColumns,
      maxRows: e.props.maxRows,
      isWorking: e.props.isWorking,
      isCompact: shown === 'compact',
    })
    return tree ?? next(e)
  })
}

/** The band below the prompt, under the engine's own hint line. */
function onRenderBelow(on: On, zone: string, view: WorkView) {
  on('ui.render', { component: 'PromptHint' }, async ($, e, next) => {
    const [shown, where] = await Promise.all([read($, mode), read($, placement)])
    if (shown === 'off' || where !== 'below') return next(e)
    const els = $.ui.resolve(e)
    const tree = drawBand(els, await bandData($, zone, view), {
      width: Math.max(20, (e.viewport?.columns ?? 100) - 2),
      maxRows: Math.max(2, Math.floor((e.viewport?.rows ?? 30) / 4) - 1),
      isWorking: e.props.isWorking,
      isCompact: shown === 'compact',
    })
    if (tree === null) return next(e)
    // Row one is the engine's own hint line, as it draws it; the band takes the rows under it,
    // shifted left past the mode pill the engine draws beside the tree (noteMode, pillIndent).
    const { Box, Text } = els
    const indent = pillIndent(await read($, permissionMode), e.props.hint)
    return (
      <Box flexDirection="column">
        <Text key="hint" dimColor wrap="truncate-end">
          {e.props.hint}
        </Text>
        <Box key="band" marginLeft={-indent}>
          {tree}
        </Box>
      </Box>
    )
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
  registerRefs(on)
  onRenderAbove(on, zone, view)
  onRenderBelow(on, zone, view)
}
