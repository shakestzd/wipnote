// Follows each delegation's run directory (wrap.ts) from the moment the hook sees the call to the
// run's end, then writes run.html (runhtml.ts). The state holds what the pane needs to know, not
// the log: the pane reads clean.md when it draws (runview.tsx).
//
// One step per run at a time (stepRun): the poll timer and the tool.call hook's settleRun both go
// through it, so a run is finished, its page written and its pane closed once.

import { atom, read, update } from 'claude-code'
import type { EngineInterface as Api } from 'claude-code'
import type { RunView } from '../types'
import { parseRun } from './livedoc.ts'
import { runPage } from './runhtml.ts'
import type { ClosePolicy } from './wrap.ts'

export const PANE_PREFIX = 'codex-run-'
export const paneId = (id: string) => PANE_PREFIX + id
const POLL_MS = 700
const NO_HOST_MS = 30000 // after the call returned with no host file: it ran bare, or never ran
const STALL_MS = 30 * 60000 // a run whose log has not grown for this long is given up on
const KEEP_DONE = 20
const MAX_ANSWER = 20000

const runs = atom({ plugin: 'codex-herdr-pane', key: 'runs' } as const, {})

// Every followed run, by id (the state is read here only: the scan needs the atom in this module)
export async function allRuns($: Api): Promise<Record<string, RunView>> {
  return read($, runs)
}

export type PaneOptions = { closePane: ClosePolicy; closeDelay: number }
let opts: PaneOptions = { closePane: 'always', closeDelay: 3 }
export function setPaneOptions(o: PaneOptions) { opts = o }

let timer: { cancel: () => void } | undefined
let ticking = false
const inflight = new Map<string, Promise<void>>()

export const clock = (ms: number) => {
  const s = Math.max(0, Math.floor(ms / 1000)), m = Math.floor(s / 60)
  return m ? `${m}m${String(s % 60).padStart(2, '0')}s` : `${s}s`
}

export function runStatus(r: RunView, now: number): string {
  const took = clock((r.ended ?? now) - (r.started ?? r.registered))
  if (r.exit === undefined) return `running for ${took}`
  if (r.exit === 'gone') return `ended without an exit status after ${took}`
  return `${r.exit === '0' ? 'finished' : `failed with exit ${r.exit}`} in ${took}${r.usage ? ` · ${r.usage}` : ''}`
}

export async function patch($: Api, id: string, fn: (r: RunView) => RunView | undefined) {
  await update($, runs, (all) => {
    const cur = all[id]
    if (!cur) return all
    const next = fn(cur)
    const { [id]: _gone, ...rest } = all
    return next ? { ...rest, [id]: next } : rest
  })
}

export async function text($: Api, path: string) {
  try { return await $.fs.read(path) } catch { return undefined }
}

async function num($: Api, path: string) {
  return Number((await text($, path))?.trim()) || 0
}

// The host the wrapper wrote: claude opens this run's pane, herdr is only followed to its end
async function resolveHost($: Api, r: RunView, now: number) {
  const host = (await text($, `${r.dir}/host`))?.trim()
  if (host !== 'claude' && host !== 'herdr') {
    if (r.callEnded !== undefined && now - r.callEnded > NO_HOST_MS) await patch($, r.id, () => undefined)
    return
  }
  const label = (await text($, `${r.dir}/label`))?.trim() || r.label
  await patch($, r.id, (c) => ({ ...c, host, label }))
  if (host !== 'claude') return
  // Opened unasked, a surface may hold it undrawn (a narrow one); /codex-runs opens it as asked
  const opened = await $.ui.open({ id: paneId(r.id), title: label.slice(0, 60) })
  if (!opened.isPlaced) $.ui.log(`codex-herdr-pane: the run's pane waits undrawn (${opened.reason}); /codex-runs shows it`)
}

// The page, written unless run.html is already there as a link (only this user writes in the
// run directory, so that is never expected; it is refused all the same)
async function writePage($: Api, r: RunView, now: number): Promise<string | undefined> {
  const page = `${r.dir}/run.html`
  try {
    if (await $.fs.exists(page) && (await $.fs.stat(page)).isLink) return undefined
    const md = (await text($, `${r.dir}/clean.md`)) ?? ''
    await $.fs.write(page, runPage({
      label: r.label, status: runStatus(r, now), ok: r.exit === '0', parts: parseRun(md, true), answer: r.answer, logPath: `${r.dir}/clean.md`,
    }))
    return page
  } catch (err) {
    $.ui.log(`codex-herdr-pane: run page not written: ${String(err)}`, { to: 'debug' })
    return undefined
  }
}

// Ends a run: its exit (or 'gone'), usage, answer and times, then its page; a claude pane closes
// per closePane
async function finish($: Api, r: RunView, exit: string, now: number) {
  const usage = (await text($, `${r.dir}/usage`))?.trim() || undefined
  const answer = ((await text($, `${r.dir}/last.md`)) ?? '').trim().slice(0, MAX_ANSWER)
  const start = await num($, `${r.dir}/start`)
  const end = await num($, `${r.dir}/end`) || Math.floor(now / 1000)
  const done: RunView = { ...r, exit, usage, answer, started: start * 1000 || r.started, ended: end * 1000, tab: exit === '0' && answer ? 'answer' : 'live' }
  const page = await writePage($, done, now)
  await patch($, r.id, (c) => ({ ...c, exit, usage, answer, started: done.started, ended: done.ended, page, tab: c.tab === 'live' && done.tab === 'answer' ? 'answer' : c.tab }))
  if (r.host !== 'claude') return
  const close = opts.closePane === 'always' || (opts.closePane === 'success' && exit === '0')
  if (close) $.clock.after(Math.max(0, opts.closeDelay) * 1000, () => void $.ui.close({ id: paneId(r.id) }).catch(() => {}))
}

async function step($: Api, r: RunView, now: number) {
  if (r.exit !== undefined) return
  if (r.host === 'pending') return resolveHost($, r, now)
  const exit = (await text($, `${r.dir}/exit`))?.trim()
  if (exit) return finish($, r, exit, now)
  let size = r.size
  try { size = (await $.fs.stat(`${r.dir}/clean.md`)).size } catch { /* not there yet */ }
  if (size !== r.size) {
    const start = await num($, `${r.dir}/start`)
    await patch($, r.id, (c) => ({ ...c, size, lastChange: now, started: start * 1000 || c.started }))
  } else if (now - r.lastChange > STALL_MS) {
    await finish($, r, 'gone', now)
  }
}

// One piece of work at a time per key: a second caller waits for the one in flight, then runs its
// own (which reads the run as it is by then)
export function serial(key: string, work: () => Promise<void>): Promise<void> {
  const before = inflight.get(key) ?? Promise.resolve()
  const p: Promise<void> = before.catch(() => {}).then(work).finally(() => { if (inflight.get(key) === p) inflight.delete(key) })
  inflight.set(key, p)
  return p
}

async function exclusive($: Api, id: string, work: (r: RunView, now: number) => Promise<void>): Promise<void> {
  return serial(id, async () => {
    const r = (await read($, runs))[id]
    if (r) await work(r, await $.clock.now())
  })
}

export function stepRun($: Api, id: string): Promise<void> {
  return exclusive($, id, (r, now) => step($, r, now))
}

async function tick($: Api) {
  if (ticking) return
  ticking = true
  try {
    const live = Object.values(await read($, runs)).filter((r) => r.exit === undefined)
    if (live.length === 0) {
      timer?.cancel(); timer = undefined
      // a run registered while the state was read found the old timer and started none: start one
      if (Object.values(await read($, runs)).some((r) => r.exit === undefined)) poll($)
      return
    }
    for (const r of live) await stepRun($, r.id)
  } finally { ticking = false }
}

export function poll($: Api) {
  timer ??= $.clock.every(POLL_MS, () => void tick($).catch((err) => $.ui.log(`codex-herdr-pane: ${String(err)}`, { to: 'debug' })))
}

// Called by the tool.call hook for each delegation it wraps, before the call (and its approval)
export async function trackRun($: Api, run: { id: string; dir: string; label: string }) {
  const now = await $.clock.now()
  const view: RunView = { ...run, host: 'pending', registered: now, size: 0, lastChange: now, answer: '', tab: 'live', open: {} }
  // Every delegation is followed, so only the newest KEEP_DONE finished runs are kept
  await update($, runs, (all) => {
    const done = Object.values(all).filter((r) => r.exit !== undefined).sort((a, b) => b.registered - a.registered)
    const drop = new Set(done.slice(KEEP_DONE).map((r) => r.id))
    return { ...Object.fromEntries(Object.entries(all).filter(([id]) => !drop.has(id))), [run.id]: view }
  })
  poll($)
}

// Called once the Bash call returned: a refused call is dropped; otherwise the no-host clock starts
export async function callEnded($: Api, id: string, refused: boolean) {
  const now = await $.clock.now()
  await patch($, id, (c) => (refused ? undefined : { ...c, callEnded: now }))
}

// For a foreground call that returned: its wrapper has ended, so the run has ended. Reads its end
// now (or gives it up as 'gone' when it left none), so the page exists before the model reads the
// result. The page's path, or undefined (a bare run, or one whose page could not be written).
export async function settleRun($: Api, id: string): Promise<string | undefined> {
  for (let i = 0; i < 3; i++) {
    const r = (await read($, runs))[id]
    if (!r) return undefined
    if (r.exit !== undefined) return r.page
    if (i < 2) await stepRun($, id)
    else await exclusive($, id, (c, now) => (c.exit === undefined && c.host !== 'pending' ? finish($, c, 'gone', now) : Promise.resolve()))
  }
  return (await read($, runs))[id]?.page
}
