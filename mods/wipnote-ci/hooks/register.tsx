import { atom, read, update } from 'claude-code'
import type { EngineInterface, Register, Timer } from 'claude-code'

import type { CiView, Snapshot } from '../types'
import { failReason, interval, parseSnapshot, settled, summary, toastText } from './format'
import { paneTree } from './pane'

const PANE = 'wipnote-ci'
const EMPTY: CiView = { arg: null, snap: null, fetchedAt: 0, error: '', done: false }

// Change a tag when the fields of its value change: it declines what an
// earlier version stored, which outlives a reload.
const view = atom({ plugin: 'wipnote-ci', key: 'view' } as const, EMPTY, { shape: 'view-1' })
const clock = atom({ plugin: 'wipnote-ci', key: 'now' } as const, 0, { shape: 'now-1' })

// The next read (rescheduled after each one, at the watch cadence) and the
// one-second tick of the elapsed times. `run` numbers each /ci, so a read
// still in flight when /ci stop or a new /ci lands schedules nothing.
let poll: Timer | undefined
let tick: Timer | undefined
let run = 0

function stop() {
  run += 1
  poll?.cancel()
  tick?.cancel()
  poll = undefined
  tick = undefined
}

/** One `wipnote ci status --json`: the snapshot, or why there is none. */
async function readSnapshot($: EngineInterface, arg: string): Promise<Snapshot | string> {
  const argv = arg === '' ? ['wipnote', 'ci', 'status', '--json'] : ['wipnote', 'ci', 'status', arg, '--json']
  const ran = await $.process.run(argv, { timeoutMs: 60_000 }).catch(() => undefined)
  if (ran === undefined) return 'wipnote unavailable'
  const snap = parseSnapshot(ran.stdout)
  if (snap !== undefined) return snap
  return ran.exitCode !== 0 ? failReason(ran.stderr, ran.exitCode) : 'unreadable output from wipnote ci status'
}

/** Reads once into the view; a failed read keeps the last snapshot. */
async function refresh($: EngineInterface, arg: string): Promise<CiView> {
  const got = await readSnapshot($, arg)
  const now = await $.clock.now()
  const before = await read($, view)
  if (typeof got === 'string') {
    return update($, view, old => ({ ...old, fetchedAt: now, error: got.slice(0, 200) }))
  }
  const done = settled(got)
  const next = await update($, view, () => ({ arg, snap: got, fetchedAt: now, error: '', done }))
  await update($, clock, () => now)
  if (got.pr !== undefined) await $.ui.status(summary(got))
  const toast = toastText(got)
  if (done && !before.done && toast !== undefined) await $.ui.toast(toast)
  return next
}

/** Reads, then waits the cadence the read calls for, until settled or stopped. */
async function cycle($: EngineInterface, arg: string, mine: number) {
  const v = await refresh($, arg)
  if (mine !== run) return
  if (v.done) {
    stop()
    return
  }
  poll = $.clock.after(interval(v.snap), () => void cycle($, arg, mine).catch(() => undefined))
}

async function follow($: EngineInterface, arg: string): Promise<string> {
  stop()
  const mine = run
  await update($, view, () => ({ ...EMPTY, arg }))
  await $.ui.open({ id: PANE, title: 'CI' })
  await cycle($, arg, mine)
  const v = await read($, view)
  if (mine === run && !v.done) {
    tick = $.clock.every(1000, () => void $.clock.now().then(now => update($, clock, () => now)).catch(() => undefined))
  }
  const s = v.snap
  if (s === null) return `CI pane: ${v.error}. It tries again in a minute.`
  if (!s.available) return `CI status not available: ${s.reason ?? 'unknown'}. The pane keeps trying.`
  if (s.pr === undefined) return `No pull request found: ${s.reason ?? 'none matched'}.`
  return `Following CI of PR #${s.pr.number} in the CI pane.`
}

export const register: Register = on => {
  on('session.start', async ($, e, next) => {
    await $.command.register({
      name: 'ci',
      description: 'Follow a pull request\'s CI live in a pane: /ci, /ci 85, /ci <branch>, /ci stop',
      argumentHint: '[pr|branch|stop]',
    })
    return next(e)
  })

  on('session.end', async ($, e, next) => {
    stop()
    return next(e)
  })

  on('command.run', { command: 'ci' }, async ($, e) => {
    const arg = e.args.trim()
    if (arg !== 'stop') return { text: await follow($, arg) }
    stop()
    await update($, view, old => ({ ...old, arg: null }))
    await $.ui.status(undefined)
    return { text: 'CI pane stopped. /ci starts it again.' }
  })

  on('ui.render', { component: 'Pane', requestId: PANE }, async ($, e) => {
    const v = await read($, view)
    const now = (await read($, clock)) || v.fetchedAt
    return paneTree($.ui.resolve(e), v, now, e.props.bodyColumns)
  })
}
