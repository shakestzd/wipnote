import type { On } from 'claude-code'
import { mock, expect, test } from 'claude-code/testing'
import type { Engine } from 'claude-code/testing'

import type { Snapshot } from '../types'
import { COMPUTING, FAILED, FULL, GREEN, MERGED, NOW, NO_CHECKS, RUNNING, UNAVAILABLE } from './fixtures/snapshots'

const PANE = {
  plugin: 'wipnote-ci',
  component: 'Pane',
  requestId: 'wipnote-ci',
  props: {
    title: 'CI',
    isFocused: false,
    bodyColumns: 100,
    placement: 'dock',
    scroll: { offset: 0, bodyRows: 40 },
    view: {},
  },
} as const

type World = { snap: Snapshot | string; runs: string[]; seen: string[]; names: string[] }

// Everything beneath the plugin is the engine: each hook answers for it.
function engine(on: On, first: Snapshot | string): World {
  const w: World = { snap: first, runs: [], seen: [], names: [] }
  on('command.register', (_, e) => { w.names.push(e.name); return { value: { command: e.name } } })
  on('ui.open', () => ({ value: { isPlaced: true } }))
  on('ui.status', (_, e) => { w.seen.push(`status:${JSON.stringify(e)}`); return { value: undefined } })
  on('ui.toast', (_, e) => { w.seen.push(`toast:${e.text}`); return { value: undefined } })
  on('ui.close', () => ({ value: undefined }))
  on('session.start', (_, e) => ({ cwd: e.cwd }))
  on('process.run', (_, e) => {
    w.runs.push(e.argv.join(' '))
    const stdout = typeof w.snap === 'string' ? w.snap : JSON.stringify(w.snap)
    return { value: { exitCode: 0, stdout, stderr: '', isStdoutTruncated: false, isStderrTruncated: false } }
  })
  return w
}

async function start($: Engine, args: string) {
  await $.session.start({ cwd: '/', surface: 'terminal', isInteractive: true })
  return (await $.command.run({ command: 'ci-pane', args } as never)) as { text?: string }
}

async function texts($: Engine) {
  const ui = await $.ui.mount({ ...PANE, surface: 'terminal' })
  const all = await ui.findAll({ type: 'Text' })
  await ui.unmount()
  return all
}

test('/ci-pane 85 reads wipnote ci status, never gh, and the pane lists failures first', async ($, on) => {
  mock.clock(on, { now: NOW })
  const w = engine(on, FULL)
  const ran = await start($, '85')
  expect(ran.text).toBe('Following CI of PR #85 in the CI pane.')
  expect(w.runs).toEqual(['wipnote ci status 85 --json'])
  expect(w.runs.some(r => r.startsWith('gh'))).toBe(false)
  expect(w.seen.some(l => l.startsWith('status:') && l.includes('CI #85: 1/4 passed · 1 failed · 1 running'))).toBe(true)

  const ui = await $.ui.mount({ ...PANE, surface: 'terminal' })
  expect((await ui.find({ type: 'Text', text: /PR #85/ }))?.text).toContain('CI pane over the CLI')
  const checks = (await ui.findAll({ type: 'Text', text: /^[✗●✓–] / })).map(t => t.text)
  expect(checks).toEqual(['✗ go-test 3:00', '● lint 5:00', '✓ build 4:10', '– docs'])
  expect(await ui.find({ type: 'Text', text: 'https://example.test/run/2' })).toBeDefined()
  expect(await ui.find({ type: 'Text', text: 'https://example.test/run/3' })).toBeUndefined()
  await ui.unmount()
})

test('/ci-pane with no argument and /ci-pane <branch> pass the argument through', async ($, on) => {
  mock.clock(on, { now: NOW })
  const w = engine(on, RUNNING)
  await start($, '')
  await $.command.run({ command: 'ci-pane', args: 'feat/ci-pane' } as never)
  expect(w.runs).toEqual(['wipnote ci status --json', 'wipnote ci status feat/ci-pane --json'])
})

test('before any /ci-pane the pane says how to start', async ($, on) => {
  engine(on, FULL)
  const all = await texts($)
  expect(all[0]?.text).toBe('No pull request is followed.')
})

test('every section renders from a full snapshot, with the work item', async ($, on) => {
  mock.clock(on, { now: NOW })
  engine(on, FULL)
  await start($, '85')
  const shown = (await texts($)).map(t => t.text)
  for (const line of [
    'feat/ci-pane → main · open · feat-8354cf43',
    '1 passed · 1 failed · 1 running · 1 skipped',
    'review: approved · 2 open threads · merge blocked · requested core-team',
    'branch feat/ci-pane: 2 ahead, 1 behind origin/main · 3 dirty · 1 unpushed',
    'stale worktree old-pane (feat/old-pane): merged, idle 9d',
    'base main: red',
    '  ✗ Release (failure)',
    '    https://example.test/run/9',
    'release v0.82.1 (2026-10-20), 14 commits since · release: green',
    'note: release: gh release view: HTTP 502',
  ]) expect(shown).toContain(line)
})

test('a review still computing mergeability says so; empty sections draw nothing', async ($, on) => {
  mock.clock(on, { now: NOW })
  engine(on, COMPUTING)
  await start($, '85')
  const shown = (await texts($)).map(t => t.text)
  expect(shown).toContain('review: none · 0 open threads · merge computing')
  expect(shown).toContain('base main: green')
  expect(shown).toContain('release: none')
  expect(shown.some(l => l.startsWith('note:') || l.startsWith('stale worktree'))).toBe(false)
})

test('available:false shows the reason dimmed and keeps polling every 60 s', async ($, on) => {
  const clock = mock.clock(on, { now: NOW })
  const w = engine(on, UNAVAILABLE)
  await start($, '')
  const reason = (await texts($)).find(t => t.text.includes('gh is not installed'))
  expect(reason?.props.dimColor).toBe(true)
  await clock.advance(59_000)
  expect(w.runs).toHaveLength(1)
  await clock.advance(1_000)
  expect(w.runs).toHaveLength(2)
})

test('a pending check polls every 15 s and the elapsed time ticks every second', async ($, on) => {
  const clock = mock.clock(on, { now: NOW })
  const w = engine(on, RUNNING)
  await start($, '85')
  expect((await texts($)).map(t => t.text)).toContain('● lint 5:00')
  await clock.advance(1_000)
  expect((await texts($)).map(t => t.text)).toContain('● lint 5:01')
  expect(w.runs).toHaveLength(1)
  await clock.advance(14_000)
  expect(w.runs).toHaveLength(2)
})

for (const [name, snap, toast] of [
  ['a failure', FAILED, 'CI #85: 1 failed'],
  ['a merge', MERGED, 'PR #85 merged'],
  ['all green', GREEN, 'CI #85: all 2 checks finished'],
] as const) {
  test(`settling on ${name} toasts once and stops polling`, async ($, on) => {
    const clock = mock.clock(on, { now: NOW })
    const w = engine(on, RUNNING)
    await start($, '85')
    w.snap = snap
    await clock.advance(15_000)
    expect(w.runs).toHaveLength(2)
    expect(w.seen.filter(l => l.startsWith('toast:'))).toEqual([`toast:${toast}`])
    await clock.advance(300_000)
    expect(w.runs).toHaveLength(2)
    expect((await texts($)).some(t => t.text.startsWith('finished'))).toBe(true)
  })
}

test('/ci-pane stop cancels both timers and clears the status line', async ($, on) => {
  const clock = mock.clock(on, { now: NOW })
  const w = engine(on, RUNNING)
  await start($, '85')
  await clock.advance(1_000)
  expect((await texts($)).map(t => t.text)).toContain('● lint 5:01')

  const stopped = (await $.command.run({ command: 'ci-pane', args: 'stop' } as never)) as { text?: string }
  expect(stopped.text).toBe('CI pane stopped. /ci-pane starts it again.')
  expect(w.seen[w.seen.length - 1]).toBe('status:{}')
  await clock.advance(300_000)
  expect(w.runs).toHaveLength(1)
  const shown = (await texts($)).map(t => t.text)
  expect(shown).toContain('● lint 5:01')
  expect(shown.some(l => l.startsWith('stopped'))).toBe(true)
})

test('unreadable output keeps the last snapshot and shows the problem', async ($, on) => {
  const clock = mock.clock(on, { now: NOW })
  const w = engine(on, RUNNING)
  await start($, '85')
  w.snap = 'Error: unknown command "ci"'
  await clock.advance(15_000)
  const shown = (await texts($)).map(t => t.text)
  expect(shown).toContain('● lint 5:15')
  expect(shown).toContain('last read failed: unreadable output from wipnote ci status')
})

test('an open PR still without checks after the 2 m grace settles, noted and toasted', async ($, on) => {
  const clock = mock.clock(on, { now: NOW })
  const w = engine(on, NO_CHECKS)
  await start($, '85')
  await clock.advance(119_000)
  expect(w.runs).toHaveLength(2)
  expect(w.seen.some(l => l.startsWith('toast:'))).toBe(false)
  await clock.advance(1_000)
  expect(w.runs).toHaveLength(3)
  expect(w.seen.filter(l => l.startsWith('toast:'))).toEqual(['toast:CI #85: no checks reported'])
  await clock.advance(600_000)
  expect(w.runs).toHaveLength(3)
  const shown = (await texts($)).map(t => t.text)
  expect(shown).toContain('no checks reported')
  expect(shown.some(l => l.startsWith('finished'))).toBe(true)
})

test('checks that appear inside the grace are followed as normal', async ($, on) => {
  const clock = mock.clock(on, { now: NOW })
  const w = engine(on, NO_CHECKS)
  await start($, '85')
  w.snap = RUNNING
  await clock.advance(60_000)
  expect(w.runs).toHaveLength(2)
  await clock.advance(120_000)
  expect(w.runs).toHaveLength(10)
  expect(w.seen.some(l => l.startsWith('toast:'))).toBe(false)
  expect((await texts($)).map(t => t.text)).toContain('● lint 8:00')
})

test('the slash command is /ci-pane and the empty pane names it', async ($, on) => {
  const w = engine(on, FULL)
  await $.session.start({ cwd: '/', surface: 'terminal', isInteractive: true })
  expect(w.names).toEqual(['ci-pane'])
  const shown = (await texts($)).map(t => t.text)
  expect(shown).toContain('/ci-pane follows the pull request of this branch.')
  expect(shown.some(l => /\/ci\b(?!-pane)/.test(l))).toBe(false)
})

// Another plugin that closes a pane by id, as the person's close mark does:
// every close raises ui.close for the plugins, whoever closed it.
const CLOSER = {
  plugins: [{
    name: 'closer',
    register(on: On) {
      on('command.run', { command: 'close' }, async ($, e) => {
        await $.ui.close({ id: e.args })
        return { text: 'closed' }
      })
    },
  }],
}
const close = ($: Engine, id: string) => $.command.run({ command: 'close', args: id } as never)

test('closing the pane cancels both timers and clears the status line; /ci-pane starts again', CLOSER, async ($, on) => {
  const clock = mock.clock(on, { now: NOW })
  const w = engine(on, RUNNING)
  await start($, '85')
  await clock.advance(1_000)
  await close($, 'wipnote-ci')
  expect(w.seen[w.seen.length - 1]).toBe('status:{}')
  await clock.advance(300_000)
  expect(w.runs).toHaveLength(1)
  const shown = (await texts($)).map(t => t.text)
  expect(shown).toContain('● lint 5:01')
  expect(shown.some(l => l.startsWith('stopped'))).toBe(true)

  await $.command.run({ command: 'ci-pane', args: '85' } as never)
  expect(w.runs).toHaveLength(2)
  await clock.advance(15_000)
  expect(w.runs).toHaveLength(3)
})

test('closing another pane leaves the CI pane polling', CLOSER, async ($, on) => {
  const clock = mock.clock(on, { now: NOW })
  const w = engine(on, RUNNING)
  await start($, '85')
  await close($, 'some-other-pane')
  expect(w.seen[w.seen.length - 1]).not.toBe('status:{}')
  await clock.advance(15_000)
  expect(w.runs).toHaveLength(2)
})
