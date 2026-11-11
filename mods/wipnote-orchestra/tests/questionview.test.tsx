import { mock, test, expect } from 'claude-code/testing'

const ID = 'feat-1a2b3c4d'
const REPLY = `Asked.\n\`\`\`wipnote-ref\n${ID}\n\`\`\``
const ROWS = JSON.stringify([
  { item_id: ID, question_id: 'q-1', text: 'Which store?', blocking: true, recommended: 'a' },
  { item_id: 'feat-2', question_id: 'q-2', text: 'Color?', blocking: false, recommended: '' },
])

declare const setTimeout: (run: () => void, ms: number) => unknown
const settle = () => new Promise(resolve => setTimeout(() => resolve(undefined), 5))

const ok = (stdout: string) => ({ value: { exitCode: 0, stdout, stderr: '', isStdoutTruncated: false, isStderrTruncated: false } })
const none = { value: { exitCode: 1, stdout: '', stderr: '', isStdoutTruncated: false, isStderrTruncated: false } }

function wire(on: Parameters<Parameters<typeof test>[1]>[1]) {
  const clock = mock.clock(on)
  on('session.id', async () => ({ value: 'session-1' }))
  on('process.run', async (_$, e) => {
    if (e.argv[1] === 'who') return ok(JSON.stringify({ work_item: '' }))
    if (e.argv[2] === 'show') return ok(JSON.stringify({ id: ID, title: 'Deck', status: 'todo', steps: [] }))
    if (e.argv[1] === 'question') return ok(e.argv.includes('--item') ? JSON.stringify(JSON.parse(ROWS).slice(0, 1)) : ROWS)
    return none
  })
  on('session.messages', async () => ({ value: [{ role: 'assistant', text: REPLY }] }) as never)
  on('session.start', (_$, e) => ({ cwd: e.cwd }))
  on('command.register', (_$, e) => ({ value: { command: e.name } }))
  return clock
}

test('a ref card lists its item open questions', async ($, on) => {
  wire(on)
  await $.session.start({ cwd: '/', surface: 'terminal', isInteractive: true })
  for (let i = 0; i < 20; i++) await settle()
  const ui = await $.ui.mount({ plugin: 'wipnote-orchestra', component: 'AssistantMessage', props: { text: REPLY }, surface: 'terminal' } as never)
  expect(await ui.find({ type: 'Text', text: /\? Which store\? · blocking · recommends a/ })).toBeDefined()
  expect(await ui.find({ type: 'Text', text: /Color\?/ })).toBeUndefined()
  await ui.unmount()
})

test('the band shows the open and blocking count', async ($, on) => {
  const clock = wire(on)
  await $.session.start({ cwd: '/', surface: 'terminal', isInteractive: true })
  await clock.advance(20_000)
  for (let i = 0; i < 20; i++) await settle()
  const ui = await $.ui.mount({
    plugin: 'wipnote-orchestra', component: 'PromptHint', surface: 'terminal',
    props: { isDraft: false, isWorking: false, hint: '? for shortcuts' },
  } as never)
  expect(await ui.find({ type: 'Text', text: /2 open questions · 1 blocking/ })).toBeDefined()
  await ui.unmount()
})
