import type { On } from 'claude-code'
import { expect, mock, test } from 'claude-code/testing'
import type { Engine } from 'claude-code/testing'

import { runSpans } from '../hooks/model'
import { firstLine, parseLedger, visibleRuns, RUN_KEEP_MS } from '../hooks/runs'
import type { RunRow } from '../types'
import { FILES, ROOT, T0 } from './fixtures/runs'
import { memFs, under } from './memfs'

// The fixture is written by the Go run store, ledger and renderers
// (internal/delegate/render/modfixture_test.go): dlg-…01 one-shot claude-print
// ended 0, …02 one-shot codex live (explicit label), …03 interactive codex w1
// live, …04 one-shot ccx ended 2, …05 interactive claude w2 stopped (ledger only),
// …06 a live folder with no ledger row; …07 a ledger row whose folder was swept.
const NOW = (T0 + 230) * 1000
const RUNS = '.wipnote/delegate/runs'
const LEDGER = FILES['.wipnote/delegations-ledger.html'] ?? ''

const ABOVE = {
  plugin: 'wipnote-orchestra',
  component: 'AbovePrompt',
  props: { hasSurvey: false, isWorking: false, maxRows: 20, bodyColumns: 160, scroll: { offset: 0, bodyRows: 19 }, view: {} },
} as const

const text = (spans: { text: string }[]) => spans.map(span => span.text).join('')

test('the ledger gives each run its mode, harness and name; a swept or bad row is kept apart', () => {
  const rows = parseLedger(LEDGER)
  expect([...rows.keys()].sort()).toEqual(['dlg-0a0000000001', 'dlg-0a0000000002', 'dlg-0a0000000003', 'dlg-0a0000000004', 'dlg-0a0000000005', 'dlg-0a0000000007'])
  expect(rows.get('dlg-0a0000000003')).toMatchObject({ mode: 'interactive', harness: 'codex', name: 'w1', closed: false })
  expect(rows.get('dlg-0a0000000002')?.label).toBe('band "review" & <check>')
  expect(rows.get('dlg-0a0000000005')).toMatchObject({ closed: true, endReason: 'stop', exit: undefined })
  expect(rows.get('dlg-0a0000000004')).toMatchObject({ closed: true, exit: 2, endReason: 'exit' })
  expect(parseLedger('<tr id="x" data-run-id="dlg-12" data-mode="oneshot">').size).toBe(0)
  expect(parseLedger('').size).toBe(0)
})

test("a run's line is the first non-blank line of last.md, without its Markdown marks", () => {
  expect(firstLine(FILES[`${RUNS}/dlg-0a0000000005/last.md`] ?? '')).toBe('Summary')
  expect(firstLine(FILES[`${RUNS}/dlg-0a0000000004/last.md`] ?? '')).toBe('ccx: failed in 3s')
  expect(firstLine('Working on it.')).toBe('Working on it.')
  expect(firstLine('\n  \n')).toBe('')
})

const row = (over: Partial<RunRow>): RunRow => ({ id: 'dlg-000000000001', title: 't', harness: 'codex', mode: 'oneshot', state: 'live', answer: '', ...over })

test('live runs always show; an ended run shows for RUN_KEEP_MS after it ended', () => {
  const live = row({ id: 'a', startedAt: 0 })
  const fresh = row({ id: 'b', state: 'ended', exit: 0, endedAt: NOW - 60_000 })
  const old = row({ id: 'c', state: 'ended', exit: 0, endedAt: NOW - RUN_KEEP_MS - 1 })
  expect(visibleRuns([old, fresh, live], NOW).map(one => one.id)).toEqual(['a', 'b'])
})

test('a row says live or ended, and how an ended run ended', () => {
  expect(text(runSpans(row({ title: 'w1', mode: 'interactive' })))).toMatch(/⇄ w1 +codex · interactive +● live/)
  expect(text(runSpans(row({ state: 'ended', exit: 0 })))).toMatch(/✔ ended/)
  expect(text(runSpans(row({ state: 'ended', exit: 2 })))).toMatch(/✘ ended exit 2/)
  expect(text(runSpans(row({ state: 'ended', reason: 'stop' })))).toMatch(/■ ended \(stop\)/)
  expect(text(runSpans(row({ state: 'ended', exit: 0 })))).not.toMatch(/live/)
  expect(text(runSpans(row({})))).not.toMatch(/ended/)
})

/** Starts a session whose project is the fixture's, outside git and herdr, with the band above the prompt. */
async function start($: Engine, on: On, rel: Record<string, string>) {
  const clock = mock.clock(on, { now: NOW })
  const fs = memFs(on, ROOT, rel, NOW)
  const argv: string[][] = []
  on('session.start', (_$, e) => ({ cwd: e.cwd }))
  on('command.register', (_$, e) => ({ value: { command: e.name } }))
  on('session.cwd', async () => ({ value: `${ROOT}/sub/dir` }))
  on('env.get', async () => ({ value: undefined }))
  on('process.run', async (_$, e) => {
    argv.push([...e.argv])
    return { value: { exitCode: 128, stdout: '', stderr: 'not a git repository', isStdoutTruncated: false, isStderrTruncated: false } }
  })
  await $.session.start({ cwd: `${ROOT}/sub/dir`, surface: null, isInteractive: true })
  await $.command.run({ command: 'orchestra', args: 'above' } as never)
  return { clock, fs, argv }
}

test('Go-written run folders show one row per run, live and ended, outside git and herdr', { timeoutMs: 20_000 }, async ($, on) => {
  const { clock, argv } = await start($, on, FILES)
  await clock.advance(2000)
  for (const surface of ['terminal', 'desktop'] as const) {
    const ui = await $.ui.mount({ ...ABOVE, surface })
    const has = async (re: RegExp) => expect(await ui.find({ type: 'Text', text: re })).toBeDefined()
    await has(/⇄ w1 +codex · interactive +● live +Working on it\./)
    await has(/⇄ band "review" & <check> +codex · oneshot +● live/)
    await has(/⇄ Find the tsconfig base +claude-print · oneshot +✔ ended +`tsconfig\.json` extends/)
    await has(/⇄ Fix the tests +ccx · oneshot +✘ ended exit 2 +ccx: failed in 3s/)
    await has(/⇄ w2 +claude · interactive +■ ended \(stop\) +Summary/)
    await has(/⇄ orphan run +● live/)
    expect(await ui.findAll({ type: 'Text', text: /⇄ w1 / })).toHaveLength(1) // one row per run, not per pane or writer
    expect(await ui.find({ type: 'Text', text: /dlg-0a0000000007|not-a-run|noise/ })).toBeUndefined()
    await ui.unmount()
  }
  expect(argv.some(args => args.some(arg => /herdr/.test(arg)))).toBe(false)
})

test('the view never writes: it only lists and reads, and the files stay as Go wrote them', { timeoutMs: 20_000 }, async ($, on) => {
  const { clock, fs } = await start($, on, FILES)
  await clock.advance(5000)
  const ui = await $.ui.mount({ ...ABOVE, surface: 'terminal' })
  expect(await ui.find({ type: 'Text', text: /⇄ w1 / })).toBeDefined()
  await ui.unmount()
  const kinds = new Set(fs.ops.map(one => one.op))
  expect(kinds.has('fs.list') && kinds.has('fs.read')).toBe(true) // the log sees the reader's ops
  expect(fs.ops.some(one => one.path.endsWith('/dlg-0a0000000003/last.md'))).toBe(true)
  expect(fs.ops.filter(one => one.op === 'fs.write')).toEqual([])
  expect([...kinds].every(op => ['fs.list', 'fs.read', 'fs.exists', 'fs.stat'].includes(op))).toBe(true)
  expect(Object.fromEntries([...fs.files].map(([path, body]) => [path.slice(ROOT.length + 1), body]))).toEqual(FILES)
})

test('a run folder Go writes while the band is up shows within 2 s', { timeoutMs: 20_000 }, async ($, on) => {
  const later = under(FILES, `${RUNS}/dlg-0a0000000006`)
  const first = Object.fromEntries(Object.entries(FILES).filter(([path]) => !later.includes(path)))
  const { clock, fs } = await start($, on, first)
  await clock.advance(2000)
  const before = await $.ui.mount({ ...ABOVE, surface: 'terminal' })
  expect(await before.find({ type: 'Text', text: /⇄ w1 / })).toBeDefined()
  expect(await before.find({ type: 'Text', text: /orphan run/ })).toBeUndefined()
  await before.unmount()
  for (const path of later) fs.files.set(`${ROOT}/${path}`, FILES[path] ?? '')
  await clock.advance(2000)
  const after = await $.ui.mount({ ...ABOVE, surface: 'terminal' })
  expect(await after.find({ type: 'Text', text: /⇄ orphan run +● live/ })).toBeDefined()
  await after.unmount()
})

test('a live run that ends shows ended at the next refresh', { timeoutMs: 20_000 }, async ($, on) => {
  const { clock, fs } = await start($, on, FILES)
  await clock.advance(2000)
  fs.files.set(`${ROOT}/${RUNS}/dlg-0a0000000006/end`, `${T0 + 231}\n`)
  fs.files.set(`${ROOT}/${RUNS}/dlg-0a0000000006/exit`, '0\n')
  await clock.advance(2000)
  const ui = await $.ui.mount({ ...ABOVE, surface: 'terminal' })
  expect(await ui.find({ type: 'Text', text: /⇄ orphan run +✔ ended/ })).toBeDefined()
  expect(await ui.find({ type: 'Text', text: /⇄ orphan run +● live/ })).toBeUndefined()
  await ui.unmount()
})

test("from a linked worktree the runs are the main checkout's, as wipnote's CLI finds them", { timeoutMs: 20_000 }, async ($, on) => {
  const clock = mock.clock(on, { now: NOW })
  memFs(on, ROOT, FILES, NOW)
  on('session.start', (_$, e) => ({ cwd: e.cwd }))
  on('command.register', (_$, e) => ({ value: { command: e.name } }))
  on('session.cwd', async () => ({ value: '/worktrees/wt-1' }))
  on('env.get', async () => ({ value: undefined }))
  on('process.run', async (_$, e) => {
    const isCommon = e.argv.includes('--git-common-dir')
    return { value: { exitCode: isCommon ? 0 : 128, stdout: isCommon ? `${ROOT}/.git\n` : '', stderr: '', isStdoutTruncated: false, isStderrTruncated: false } }
  })
  await $.session.start({ cwd: '/worktrees/wt-1', surface: null, isInteractive: true })
  await $.command.run({ command: 'orchestra', args: 'above' } as never)
  await clock.advance(2000)
  const ui = await $.ui.mount({ ...ABOVE, surface: 'terminal' })
  expect(await ui.find({ type: 'Text', text: /⇄ w1 +codex · interactive +● live/ })).toBeDefined()
  await ui.unmount()
})

test('with no .wipnote/ anywhere up from the session, the band shows no run rows', { timeoutMs: 20_000 }, async ($, on) => {
  const clock = mock.clock(on, { now: NOW })
  memFs(on, '/other', FILES, NOW)
  on('session.start', (_$, e) => ({ cwd: e.cwd }))
  on('command.register', (_$, e) => ({ value: { command: e.name } }))
  on('session.cwd', async () => ({ value: '/elsewhere' }))
  on('env.get', async () => ({ value: undefined }))
  on('process.run', async () => ({ value: { exitCode: 128, stdout: '', stderr: '', isStdoutTruncated: false, isStderrTruncated: false } }))
  await $.session.start({ cwd: '/elsewhere', surface: null, isInteractive: true })
  await $.command.run({ command: 'orchestra', args: 'above' } as never)
  await clock.advance(2000)
  const ui = await $.ui.mount({ ...ABOVE, surface: 'terminal' })
  expect(await ui.find({ type: 'Text', text: /⇄/ })).toBeUndefined()
  await ui.unmount()
})
