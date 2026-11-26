import { mock, test, expect } from 'claude-code/testing'

// The band refreshes its cards on a slow cadence and never stacks a refresh on
// one still running (bug-0672aef0). `wipnote status` is the last call of a
// refresh, so its count is the number of refreshes that reached the end.

declare const setTimeout: (run: () => void, ms: number) => unknown
const settle = () => new Promise(resolve => setTimeout(() => resolve(undefined), 5))

type Ran = { value: { exitCode: number; stdout: string; stderr: string; isStdoutTruncated: boolean; isStderrTruncated: boolean } }
const ok = (stdout: string): Ran => ({ value: { exitCode: 0, stdout, stderr: '', isStdoutTruncated: false, isStderrTruncated: false } })
const none: Ran = { value: { exitCode: 1, stdout: '', stderr: '', isStdoutTruncated: false, isStderrTruncated: false } }

function wire(on: Parameters<Parameters<typeof test>[1]>[1], status: () => Promise<Ran>) {
  const clock = mock.clock(on)
  const seen = { statusCalls: 0 }
  on('session.id', async () => ({ value: 'session-1' }))
  on('session.messages', async () => ({ value: [] }) as never)
  on('process.run', async (_$, e) => {
    if (e.argv[1] === 'who') return ok(JSON.stringify({ work_item: '' }))
    if (e.argv[0] === 'wipnote' && e.argv[1] === 'status') {
      seen.statusCalls += 1
      return status()
    }
    return none
  })
  on('session.start', (_$, e) => ({ cwd: e.cwd }))
  on('command.register', (_$, e) => ({ value: { command: e.name } }))
  return { clock, seen }
}

test('cards refresh once per thirty ticks', { timeoutMs: 20_000 }, async ($, on) => {
  const { clock, seen } = wire(on, async () => none)
  await $.session.start({ cwd: '/', surface: 'terminal', isInteractive: true })
  await clock.advance(29_000)
  for (let i = 0; i < 10; i++) await settle()
  expect(seen.statusCalls).toBe(1)
  await clock.advance(30_000)
  for (let i = 0; i < 10; i++) await settle()
  expect(seen.statusCalls).toBe(2)
})

test('a tick skips the card refresh while the previous one is in flight', { timeoutMs: 20_000 }, async ($, on) => {
  let release: (r: Ran) => void = () => undefined
  const slow = new Promise<Ran>(resolve => {
    release = resolve
  })
  const { clock, seen } = wire(on, () => slow)
  await $.session.start({ cwd: '/', surface: 'terminal', isInteractive: true })
  await clock.advance(95_000)
  for (let i = 0; i < 10; i++) await settle()
  expect(seen.statusCalls).toBe(1)
  release(none)
  for (let i = 0; i < 10; i++) await settle()
  await clock.advance(30_000)
  for (let i = 0; i < 10; i++) await settle()
  expect(seen.statusCalls).toBe(2)
})
