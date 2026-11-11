import { mock, expect, test } from 'claude-code/testing'

const PANE = {
  plugin: 'wipnote-questions',
  component: 'Pane',
  requestId: 'wipnote-questions',
  props: {
    title: 'Questions',
    isFocused: false,
    bodyColumns: 60,
    placement: 'dock',
    scroll: { offset: 0, bodyRows: 20 },
    view: {},
  },
} as const

declare const setTimeout: (run: () => void, ms: number) => unknown
const settle = () => new Promise(resolve => setTimeout(() => resolve(undefined), 5))

const row = {
  item_id: 'spk-1', item_title: 'Pane', question_id: 'q-70cb39622fc549ec', text: 'Recommended first?',
  options: [{ key: 'yes', label: 'Yes' }, { key: 'no', label: 'No' }],
  recommended: 'yes', blocking: false, status: 'open', age: '1m',
}

const ok = (stdout: string) => ({ value: { exitCode: 0, stdout, stderr: '', isStdoutTruncated: false, isStderrTruncated: false } })

test('clicking an option answers the question with that key and drops the row', async ($, on) => {
  mock.clock(on)
  let open = true
  const answers: string[] = []
  on('command.register', (_$, e) => ({ value: { command: e.name } }))
  on('ui.open', () => ({ value: { isPlaced: true } }))
  on('session.start', (_, e) => ({ cwd: e.cwd }))
  on('ui.toast', () => ({ value: undefined }) as never)
  on('process.run', async (_$, e) => {
    const argv = e.argv.join(' ')
    if (argv.startsWith('wipnote question answer')) {
      answers.push(argv)
      open = false
      return ok('Answered q-70cb39622fc549ec on spk-1: no\n')
    }
    return ok(JSON.stringify(open ? [row] : []))
  })

  await $.session.start({ cwd: '/', surface: 'terminal', isInteractive: true })
  await $.command.run({ command: 'questions-pane', args: '' } as never)
  for (let i = 0; i < 10; i++) await settle()

  for (const surface of ['terminal', 'desktop', 'vscode', 'mobile'] as const) {
    const ui = await $.ui.mount({ ...PANE, surface })
    expect(await ui.find({ type: 'Button', key: 'q-70cb39622fc549ec:yes' })).toBeDefined()
    await ui.unmount()
  }

  const ui = await $.ui.mount({ ...PANE, surface: 'terminal' })
  await ui.press({ key: 'q-70cb39622fc549ec:no' })
  for (let i = 0; i < 10; i++) await settle()
  expect(answers).toEqual(['wipnote question answer q-70cb39622fc549ec no'])
  expect(await ui.find({ type: 'Text', text: /No open questions/ })).toBeDefined()
  await ui.unmount()
})
