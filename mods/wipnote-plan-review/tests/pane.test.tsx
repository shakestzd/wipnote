import { mock, expect, test } from 'claude-code/testing'

const PANE = {
  plugin: 'wipnote-plan-review',
  component: 'Pane',
  requestId: 'wipnote-plan-review',
  props: {
    title: 'Plan review',
    isFocused: false,
    bodyColumns: 60,
    placement: 'dock',
    scroll: { offset: 0, bodyRows: 20 },
    view: {},
  },
} as const

declare const setTimeout: (run: () => void, ms: number) => unknown
const settle = () => new Promise(resolve => setTimeout(() => resolve(undefined), 5))

const slices = (second: string) => JSON.stringify({
  plan_id: 'plan-abc123', title: 'Review pane', status: 'review',
  slices: [
    { num: 1, title: 'Slices command', complexity: 'trivial', approval_status: 'approved' },
    { num: 2, title: 'The mod', complexity: 'standard', approval_status: second },
  ],
})

test('the pane opens on a review command and follows each decision', async ($, on) => {
  mock.clock(on)
  let second = 'pending'
  let opened = 0
  on('command.register', () => ({ value: { command: 'plan-review-pane' } }))
  on('ui.open', () => {
    opened += 1
    return { value: { isPlaced: true } }
  })
  on('session.start', (_, e) => ({ cwd: e.cwd }))
  on('process.run', async (_$, e) => {
    if (e.argv.join(' ') === 'wipnote plan slices plan-abc123') {
      return { value: { exitCode: 0, stdout: slices(second), stderr: '', isStdoutTruncated: false, isStderrTruncated: false } }
    }
    return { value: { exitCode: 1, stdout: '', stderr: 'Error: load plan: not found', isStdoutTruncated: false, isStderrTruncated: false } }
  })
  on('tool.call', () => ({ result: { stdout: '', stderr: '', interrupted: false } }) as never)

  await $.session.start({ cwd: '/', surface: 'desktop', isInteractive: true })

  const empty = await $.ui.mount({ ...PANE, surface: 'desktop' })
  expect((await empty.find({ type: 'Text', text: /No plan in view/ }))).toBeDefined()
  await empty.unmount()

  await $.tool.call({ tool: 'Bash', command: 'wipnote plan slices plan-abc123', description: 'slices' } as never)
  for (let i = 0; i < 10; i++) await settle()
  expect(opened).toBe(1)

  for (const surface of ['terminal', 'desktop', 'vscode', 'mobile'] as const) {
    const ui = await $.ui.mount({ ...PANE, surface })
    expect(await ui.find({ type: 'Text', text: /1 approved · 1 pending/ })).toBeDefined()
    await ui.unmount()
  }

  second = 'changes_requested'
  await $.tool.call({ tool: 'Bash', command: 'wipnote plan reject-slice plan-abc123 2 --changes-requested', description: 'reject' } as never)
  for (let i = 0; i < 10; i++) await settle()
  const after = await $.ui.mount({ ...PANE, surface: 'desktop' })
  expect(await after.find({ type: 'Text', text: /1 approved · 1 changes/ })).toBeDefined()
  await after.unmount()
})

test('the command shows why a plan cannot load', async ($, on) => {
  mock.clock(on)
  on('command.register', () => ({ value: { command: 'plan-review-pane' } }))
  on('ui.open', () => ({ value: { isPlaced: true } }))
  on('session.start', (_, e) => ({ cwd: e.cwd }))
  on('process.run', async () => ({ value: { exitCode: 1, stdout: '', stderr: 'Error: load plan: not found', isStdoutTruncated: false, isStderrTruncated: false } }))

  await $.session.start({ cwd: '/', surface: 'desktop', isInteractive: true })
  const ran = await $.command.run({ command: 'plan-review-pane', args: 'plan-missing' } as never)
  expect((ran as { text?: string }).text).toBe('Plan review pane: plan-missing.')

  const ui = await $.ui.mount({ ...PANE, surface: 'desktop' })
  expect((await ui.find({ type: 'Text', text: /load plan: not found/ }))).toBeDefined()
  await ui.unmount()
})

test('a value an earlier version stored is declined', async ($, on) => {
  on('state.get', () => ({ value: { value: { planId: 'plan-x', slices: 3 }, version: 1 } }))
  const ui = await $.ui.mount({ ...PANE, surface: 'desktop' })
  expect((await ui.find({ type: 'Text' }))?.text).toBe('No plan in view.')
  await ui.unmount()
})
