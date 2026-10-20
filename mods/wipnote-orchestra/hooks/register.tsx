import { atom, read, update } from 'claude-code'
import type { EngineInterface, Register } from 'claude-code'

import type { AgentRow, Mode, PromptInfo, Tally, WorkCard } from '../types'
import type { BandData } from './band'
import { drawBand } from './band'
import { hydrate, refreshAfterTurn, registerRefs, storeCard, wipnoteJSON } from './refcards'
import { parseWho } from './refs'
import {
  FAILED_KEEP_MS,
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

const prompt = atom({ plugin: 'wipnote-orchestra', key: 'prompt' } as const, null)
const agents = atom({ plugin: 'wipnote-orchestra', key: 'agents' } as const, [])
const items = atom({ plugin: 'wipnote-orchestra', key: 'items' } as const, [])
const cards = atom({ plugin: 'wipnote-orchestra', key: 'cards' } as const, [])
const mainTool = atom({ plugin: 'wipnote-orchestra', key: 'mainTool' } as const, null)
const now = atom({ plugin: 'wipnote-orchestra', key: 'now' } as const, 0)
const mode = atom({ plugin: 'wipnote-orchestra', key: 'mode' } as const, 'full')
const placement = atom({ plugin: 'wipnote-orchestra', key: 'placement' } as const, 'below')
const permissionMode = atom({ plugin: 'wipnote-orchestra', key: 'permissionMode' } as const, null)
const claim = atom({ plugin: 'wipnote-orchestra', key: 'claim' } as const, null)
const tally = atom({ plugin: 'wipnote-orchestra', key: 'tally' } as const, { done: 0, failed: 0 })

const KEEP_DONE_MS = 90_000

type Hint = { workItem?: string; model?: string }

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

async function reconcileAgents($: EngineInterface, at: number, hints: ReadonlyMap<string, Hint>) {
  const listed = await $.agent.list()
  const ended: Tally = { done: 0, failed: 0 }
  const count = (status: string) => {
    if (status === 'failed' || status === 'killed') ended.failed += 1
    else ended.done += 1
  }
  await update($, agents, rows => {
    ended.done = 0
    ended.failed = 0
    const byId = new Map(rows.map(row => [row.id, row]))
    for (const info of listed) {
      const row = byId.get(info.id)
      const wasLive = row === undefined || isLive(row.status)
      if (wasLive && !isLive(info.status)) count(info.status)
      byId.set(info.id, {
        tools: 0,
        startedAt: at,
        ...row,
        id: info.id,
        type: info.name ?? info.type,
        description: info.description,
        workItem: row?.workItem ?? hints.get(info.description)?.workItem,
        model: row?.model ?? hints.get(info.description)?.model,
        status: info.status,
        endedAt: wasLive && !isLive(info.status) ? at : row?.endedAt,
      })
    }
    const seen = new Set(listed.map(info => info.id))
    for (const row of byId.values()) {
      if (!seen.has(row.id) && isLive(row.status)) {
        byId.set(row.id, { ...row, status: 'completed', endedAt: at })
        count('completed')
      }
    }
    return [...byId.values()].slice(-100)
  })
  if (ended.done + ended.failed > 0) {
    await update($, tally, t => ({ done: t.done + ended.done, failed: t.failed + ended.failed }))
  }
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

async function bandData($: EngineInterface, zone: string): Promise<BandData> {
  const [info, rows, ended, work, known, doing, at] = await Promise.all([
    read($, prompt),
    read($, agents),
    read($, tally),
    activeItems($),
    read($, cards),
    read($, mainTool),
    read($, now),
  ])
  return { info, rows, tally: ended, keepMs: KEEP_DONE_MS, work, cards: known, doing, at, zone }
}

export const register: Register = (on, options) => {
  const zone = zoneOf(options.timeZone)
  const pending = new Map<string, Hint>()

  on('session.start', async ($, e, next) => {
    await $.command.register({
      name: 'orchestra',
      description: 'Orchestra band: full (agents by work item, stalled or failed ones), compact (one line), off; above or below the prompt',
      argumentHint: 'full|compact|off|above|below',
    })
    await refreshPrompt($).catch(() => undefined)
    void refreshClaim($).catch(() => undefined)
    void hydrate($).catch(() => undefined)
    const started = await $.clock.now()
    await update($, now, () => started)
    let tick = 0
    $.clock.every(1000, () => {
      tick += 1
      void (async () => {
        const at = await $.clock.now()
        const rows = await read($, agents)
        const isBusy = rows.some(row => isLive(row.status) || at - (row.endedAt ?? 0) < FAILED_KEEP_MS)
        if (isBusy || tick % 30 === 0) {
          await update($, now, () => at)
        }
        if (tick % 2 === 0) await reconcileAgents($, at, pending)
        if (tick % 5 === 0) await refreshPrompt($)
        if (tick % 5 === 1) await refreshCards($)
      })().catch(() => undefined)
    })
    return next(e)
  })

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

  on('agent.spawn', async ($, e, next) => {
    const hint: Hint = {
      workItem: workItemIn(e.prompt),
      model: e.model !== undefined ? modelLabel(e.model) : undefined,
    }
    pending.set(e.description, hint)
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

  on('tool.call', async ($, e, next) => {
    const input = e as unknown as Record<string, unknown>
    const doing = describeCall(String(e.tool), input)
    if (e.agentId === undefined) {
      await update($, mainTool, () => doing)
    } else {
      const id = e.agentId
      const at = await $.clock.now()
      await update($, agents, rows => {
        const row = rows.find(one => one.id === id)
        const base: AgentRow = row ?? {
          id,
          type: 'agent',
          description: '',
          status: 'running',
          startedAt: at,
          tools: 0,
        }
        const hint = pending.get(base.description)
        const workItem = base.workItem ?? hint?.workItem
        const model = base.model ?? hint?.model
        return [
          ...rows.filter(one => one.id !== id),
          { ...base, workItem, model, tools: base.tools + 1, lastTool: doing, lastAt: at, inFlight: (base.inFlight ?? 0) + 1 },
        ]
      })
    }
    if (e.tool === 'Bash') {
      const command = e.command
      await update($, items, list => applyWipnote(list, command))
    }
    const ran = await next(e)
    // Never throws: the .catch below would run the call a second time
    const id = e.agentId
    if (id !== undefined) {
      try {
        const at = await $.clock.now()
        await update($, agents, rows =>
          rows.map(row => (row.id === id ? { ...row, lastAt: at, inFlight: Math.max(0, (row.inFlight ?? 1) - 1) } : row)),
        )
      } catch {
        /* the next call or the list sets it again */
      }
    }
    return ran
  }).catch(($, e, next) => next(e))

  on('classic.UserPromptSubmit', async ($, e, next) => {
    const mode = e.permission_mode
    if (mode !== undefined) await update($, permissionMode, () => mode)
    return next(e)
  })

  on('classic.PostToolUse', async ($, e, next) => {
    const mode = e.permission_mode
    if (mode !== undefined && e.agent_id === undefined) await update($, permissionMode, () => mode)
    return next(e)
  })

  on('turn.complete', async ($, e, next) => {
    await update($, mainTool, () => null)
    await refreshPrompt($).catch(() => undefined)
    void refreshClaim($).catch(() => undefined)
    refreshAfterTurn($)
    return next(e)
  })

  // /resume, /branch and /clear swap the conversation, but session.start does
  // not fire again; the settings SessionStart does.
  on('classic.SessionStart', async ($, e, next) => {
    if (e.source !== 'startup') {
      void refreshClaim($).catch(() => undefined)
      void hydrate($).catch(() => undefined)
    }
    return next(e)
  })

  registerRefs(on)

  on('ui.render', { component: 'AbovePrompt' }, async ($, e, next) => {
    const [shown, where] = await Promise.all([read($, mode), read($, placement)])
    if (e.props.hasSurvey || shown === 'off' || where !== 'above') return next(e)
    const tree = drawBand($.ui.resolve(e), await bandData($, zone), {
      width: e.props.bodyColumns,
      maxRows: e.props.maxRows,
      isWorking: e.props.isWorking,
      isCompact: shown === 'compact',
    })
    return tree ?? next(e)
  })

  on('ui.render', { component: 'PromptHint' }, async ($, e, next) => {
    const [shown, where] = await Promise.all([read($, mode), read($, placement)])
    if (shown === 'off' || where !== 'below') return next(e)
    const els = $.ui.resolve(e)
    const tree = drawBand(els, await bandData($, zone), {
      width: Math.max(20, (e.viewport?.columns ?? 100) - 2),
      maxRows: Math.max(2, Math.floor((e.viewport?.rows ?? 30) / 4) - 1),
      isWorking: e.props.isWorking,
      isCompact: shown === 'compact',
    })
    if (tree === null) return next(e)
    // Row one is the engine's own hint line, as it draws it; the band takes the rows under it.
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
