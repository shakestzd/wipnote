// The Claude Code pane: outside herdr (claudePane auto), a delegation runs headless into a run
// directory (wrap.ts) and this follows it in a pane of Claude Code's own, one pane per run (the
// panes are tabs). It reads the files the herdr viewer reads (clean.md, exit, usage, last.md),
// polling while a run is live, and draws Live (prose, and each tool result as a fold: a Button
// row that opens it) and Answer.

import { atom, read, update } from 'claude-code'
import type { EngineInterface as Api, On } from 'claude-code'
import type { RunView } from '../types'
import { parseRun, summarizeRun, type RunPart } from './livedoc.ts'
import type { ClosePolicy } from './wrap.ts'


export const PANE_PREFIX = 'codex-run-'
const POLL_MS = 700
const NO_HOST_MS = 60000 // a call that never ran (denied) leaves no host file
const MAX_CHARS = 80000 // what one drawing shows: the newest parts that fit (the engine cuts at 100000)

const runs = atom({ plugin: 'codex-herdr-pane', key: 'runs' } as const, {})
export const paneId = (id: string) => PANE_PREFIX + id

const clock = (ms: number) => {
  const s = Math.max(0, Math.floor(ms / 1000)), m = Math.floor(s / 60)
  return m ? `${m}m${String(s % 60).padStart(2, '0')}s` : `${s}s`
}

async function patch($: Api, id: string, fn: (r: RunView) => RunView | undefined) {
  await update($, runs, (all) => {
    const cur = all[id]
    if (!cur) return all
    const next = fn(cur)
    const { [id]: _gone, ...rest } = all
    return next ? { ...rest, [id]: next } : rest
  })
}

export type PaneOptions = { closePane: ClosePolicy; closeDelay: number }

let opts: PaneOptions = { closePane: 'always', closeDelay: 3 }
let timer: { cancel: () => void } | undefined

async function text($: Api, path: string) {
  try { return await $.fs.read(path) } catch { return undefined }
}

async function step($: Api, r: RunView, now: number) {
  if (r.host === 'pending') {
    const host = (await text($, `${r.dir}/host`))?.trim()
    if (host === 'claude') {
      const label = (await text($, `${r.dir}/label`))?.trim() || r.label
      await patch($, r.id, (c) => ({ ...c, host: 'claude', label }))
      // Opened unasked, a surface may hold it undrawn (a narrow one); /codex-runs opens it as asked
      const opened = await $.ui.open({ id: paneId(r.id), title: label.slice(0, 60) })
      if (!opened.isPlaced) $.ui.log(`codex-herdr-pane: the run's pane waits undrawn (${opened.reason}); /codex-runs shows it`)
    } else if (host === 'herdr' || now - r.registered > NO_HOST_MS) {
      await patch($, r.id, () => undefined)
    }
    return
  }
  if (r.exit !== undefined) return
  const md = (await text($, `${r.dir}/clean.md`)) ?? r.md
  const start = Number((await text($, `${r.dir}/start`))?.trim()) || 0
  const exit = (await text($, `${r.dir}/exit`))?.trim()
  if (!exit) {
    if (md !== r.md || (start && !r.started)) await patch($, r.id, (c) => ({ ...c, md, started: start * 1000 || c.started }))
    return
  }
  const usage = (await text($, `${r.dir}/usage`))?.trim() || undefined
  const answer = ((await text($, `${r.dir}/last.md`)) ?? '').trim()
  const end = Number((await text($, `${r.dir}/end`))?.trim()) || Math.floor(now / 1000)
  await patch($, r.id, (c) => ({
    ...c, md, exit, usage, answer, started: start * 1000 || c.started, ended: end * 1000,
    tab: exit === '0' && answer ? 'answer' : 'live',
  }))
  const close = opts.closePane === 'always' || (opts.closePane === 'success' && exit === '0')
  if (close) $.clock.after(Math.max(0, opts.closeDelay) * 1000, () => void $.ui.close({ id: paneId(r.id) }).catch(() => {}))
}

async function tick($: Api) {
  const all = await read($, runs)
  const live = Object.values(all).filter((r) => r.exit === undefined)
  if (live.length === 0) { timer?.cancel(); timer = undefined; return }
  const now = await $.clock.now()
  for (const r of live) await step($, r, now)
}

function poll($: Api) {
  timer ??= $.clock.every(POLL_MS, () => void tick($).catch((err) => $.ui.log(`codex-herdr-pane: ${String(err)}`, { to: 'debug' })))
}

// /codex-runs: the newest followed run in its pane, opened as the person asked (placed at any width)
async function showLatest($: Api) {
  const latest = Object.values(await read($, runs)).filter((r) => r.host === 'claude').sort((a, b) => b.registered - a.registered)[0]
  if (!latest) return { text: 'No delegation run is followed in this session yet.' }
  const opened = await $.ui.open({ id: paneId(latest.id), title: latest.label.slice(0, 60), focus: true })
  if (opened.isPlaced) return { text: `Showing ${latest.label}.` }
  // No surface draws the pane (a cloud session viewed in the app, a -p run): the run goes in the chat
  const now = await $.clock.now()
  return { text: summarizeRun(parseRun(latest.md, latest.exit !== undefined), `${latest.label}: ${runStatus(latest, now)}`, latest.answer, `${latest.dir}/clean.md`) }
}

function runStatus(r: RunView, now: number): string {
  const took = clock((r.ended ?? now) - (r.started ?? r.registered))
  if (r.exit === undefined) return `running for ${took}`
  return `${r.exit === '0' ? 'finished' : `failed with exit ${r.exit}`} in ${took}${r.usage ? ` · ${r.usage}` : ''}`
}

// Called by the tool.call hook for each delegation it wraps while claudePane is on
export async function trackRun($: Api, run: { id: string; dir: string; label: string }) {
  const now = await $.clock.now()
  const view: RunView = { ...run, host: 'pending', registered: now, md: '', answer: '', tab: 'live', open: {} }
  await update($, runs, (all) => ({ ...all, [run.id]: view }))
  poll($)
}

export function runPane(on: On, o: PaneOptions) {
  opts = o
  // A reload keeps $.state but not the timer: follow the runs still live
  on('session.start', async ($, e, next) => {
    await $.command.register({ name: 'codex-runs', description: 'Show the newest Codex or ccx delegation run in a pane' })
    if (Object.values(await read($, runs)).some((r) => r.exit === undefined)) poll($)
    return next(e)
  })

  on('command.run', { command: 'codex-runs' }, async ($) => showLatest($))

  on('ui.close', async ($, e, next) => {
    if (e.id.startsWith(PANE_PREFIX)) await patch($, e.id.slice(PANE_PREFIX.length), () => undefined)
    return next(e)
  })

  on('ui.render', { component: 'Pane' }, async ($, e, next) => {
    if (!e.requestId.startsWith(PANE_PREFIX)) return next(e)
    const id = e.requestId.slice(PANE_PREFIX.length)
    const { Box, Text, Button, Markdown } = $.ui.resolve(e)
    const r = (await read($, runs))[id]
    if (!r) return <Text dimColor>This run is no longer followed.</Text>
    const now = await $.clock.now()
    const ok = r.exit === '0'
    const status = runStatus(r, now)
    const parts = parseRun(r.md, r.exit !== undefined)
    const isOpen = (i: number, p: Extract<RunPart, { kind: 'fold' }>) => r.open[String(i)] ?? p.failed
    const folds = parts.flatMap((p, i) => (p.kind === 'fold' ? [i] : []))
    const allOpen = folds.length > 0 && folds.every((i) => isOpen(i, parts[i] as Extract<RunPart, { kind: 'fold' }>))
    const setTab = (tab: RunView['tab']) => void patch($, id, (c) => ({ ...c, tab }))
    const toggle = (i: number, was: boolean) => void patch($, id, (c) => ({ ...c, open: { ...c.open, [String(i)]: !was } }))
    const setAll = (v: boolean) => void patch($, id, (c) => ({ ...c, open: Object.fromEntries(folds.map((i) => [String(i), v])) }))

    // The newest parts that fit in one drawing; the rest are named, with the file that has them
    let room = MAX_CHARS, first = parts.length
    while (first > 0) {
      const p = parts[first - 1]!
      const cost = p.kind === 'prose' ? p.text.length : p.title.length + (isOpen(first - 1, p) ? p.body.length : 0)
      if (cost > room && first < parts.length) break
      room -= cost; first -= 1
    }

    const live = parts.slice(first).map((p, k) => {
      const i = first + k
      if (p.kind === 'prose') return <Markdown key={`p${i}`} text={p.text} />
      const open = isOpen(i, p)
      return (
        <Box key={`f${i}`} flexDirection="column">
          <Button key={`fold-${i}`} plain onPress={() => toggle(i, open)}>
            {open ? '▼ ' : '▶ '}<Text color={p.failed ? 'red' : undefined}>{p.title}</Text>
          </Button>
          {open && <Box paddingLeft={2}><Markdown key={`b${i}`} text={p.body} /></Box>}
        </Box>
      )
    })

    return (
      <Box flexDirection="column" gap={1}>
        <Box flexDirection="column">
          <Text bold>{r.label}</Text>
          <Text color={r.exit === undefined ? undefined : ok ? 'green' : 'red'} dimColor={r.exit === undefined}>{status}</Text>
        </Box>
        <Box flexDirection="row" gap={1}>
          <Button key="tab-live" hotkey="l" variant={r.tab === 'live' ? 'primary' : undefined} onPress={() => setTab('live')}>Live</Button>
          <Button key="tab-answer" hotkey="a" variant={r.tab === 'answer' ? 'primary' : undefined} onPress={() => setTab('answer')}>Answer</Button>
          {r.tab === 'live' && folds.length > 0 && (
            <Button key="folds" hotkey="f" onPress={() => setAll(!allOpen)}>{allOpen ? 'Close all' : 'Open all'}</Button>
          )}
        </Box>
        {r.tab === 'answer'
          ? <Markdown key="answer" text={r.answer || (r.exit === undefined ? '*The answer appears here when the run finishes.*' : '*No answer was written; see Live.*')} />
          : (
            <Box flexDirection="column" gap={1}>
              {first > 0 && <Text dimColor>{first} earlier entries not shown: {r.dir}/clean.md</Text>}
              {parts.length === 0 && <Text dimColor>Waiting for the first event...</Text>}
              {live}
            </Box>
          )}
      </Box>
    )
  })

}
