import { mock, test, expect } from 'claude-code/testing'

const ID = 'feat-1a2b3c4d'
const show = (done: number) => JSON.stringify({
  id: ID,
  title: 'Linked deck',
  status: 'in-progress',
  steps: [0, 1, 2].map(i => ({ description: `s${i}`, completed: i < done })),
})
const REPLY = `Started.\n\`\`\`wipnote-ref\n${ID}\n\`\`\``

// The kit has timers but the mod's lib has no DOM or Node typings.
declare const setTimeout: (run: () => void, ms: number) => unknown
const settle = () => new Promise(resolve => setTimeout(() => resolve(undefined), 5))

test('a card moves as soon as a Bash call ticks one of its steps, mid-turn', async ($, on) => {
  mock.clock(on)
  on('session.id', async () => ({ value: 'session-1' }))
  let done = 0
  const shows: string[] = []
  const ok = (stdout: string) => ({ value: { exitCode: 0, stdout, stderr: '', isStdoutTruncated: false, isStderrTruncated: false } })
  on('process.run', async (_$, e) => {
    if (e.argv[1] === 'who') return ok(JSON.stringify({ work_item: '' }))
    if (e.argv[2] === 'show') {
      shows.push(e.argv[3] ?? '')
      return ok(show(done))
    }
    return { value: { exitCode: 1, stdout: '', stderr: '', isStdoutTruncated: false, isStderrTruncated: false } }
  })
  on('session.messages', async () => ({ value: [{ role: 'assistant', text: REPLY }] }) as never)
  on('classic.SessionStart', async () => ({}))
  on('tool.call', () => ({ result: { stdout: 'ok', stderr: '', interrupted: false } }) as never)

  await $.classic.SessionStart({ source: 'resume' })
  for (let i = 0; i < 20; i++) await settle()

  const message = { plugin: 'wipnote-orchestra', component: 'AssistantMessage', props: { text: REPLY }, surface: 'terminal' } as const
  const before = await $.ui.mount(message as never)
  expect(await before.find({ type: 'Text', text: /0\/3 steps/ })).toBeDefined()
  await before.unmount()

  // An unrelated command fetches nothing; a step change refetches only its card.
  const fetched = shows.length
  await $.tool.call({ tool: 'Bash', command: 'go test ./...', description: 'test' } as never)
  for (let i = 0; i < 10; i++) await settle()
  expect(shows.length).toBe(fetched)

  done = 1
  await $.tool.call({ tool: 'Bash', command: `wipnote feature complete-step ${ID} 1`, description: 'tick' } as never)
  for (let i = 0; i < 20; i++) await settle()
  expect(shows.length).toBe(fetched + 1)

  const after = await $.ui.mount(message as never)
  expect(await after.find({ type: 'Text', text: /1\/3 steps/ })).toBeDefined()
  await after.unmount()
})
