import { mock, test, expect } from 'claude-code/testing'

// The band polls `question list --open` only while it is shown, and never
// stacks a call on one still running.

declare const setTimeout: (run: () => void, ms: number) => unknown
const settle = () => new Promise(resolve => setTimeout(() => resolve(undefined), 5))

type Ran = { value: { exitCode: number; stdout: string; stderr: string; isStdoutTruncated: boolean; isStderrTruncated: boolean } }
const ok = (stdout: string): Ran => ({ value: { exitCode: 0, stdout, stderr: '', isStdoutTruncated: false, isStderrTruncated: false } })
const none: Ran = { value: { exitCode: 1, stdout: '', stderr: '', isStdoutTruncated: false, isStderrTruncated: false } }

/** Wires the engine; listCalls counts `question list --open` calls (not the per-item ones). */
function wire(on: Parameters<Parameters<typeof test>[1]>[1], answer: () => Promise<Ran>) {
  const clock = mock.clock(on)
  const seen = { listCalls: 0 }
  on('session.id', async () => ({ value: 'session-1' }))
  on('session.messages', async () => ({ value: [] }) as never)
  on('process.run', async (_$, e) => {
    if (e.argv[1] === 'who') return ok(JSON.stringify({ work_item: '' }))
    if (e.argv[1] === 'question' && !e.argv.includes('--item')) {
      seen.listCalls += 1
      return answer()
    }
    return none
  })
  on('session.start', (_$, e) => ({ cwd: e.cwd }))
  on('command.register', (_$, e) => ({ value: { command: e.name } }))
  return { clock, seen }
}

test('with the band off, question list is not polled', { timeoutMs: 20_000 }, async ($, on) => {
  const { clock, seen } = wire(on, async () => ok('[]'))
  await $.session.start({ cwd: '/', surface: 'terminal', isInteractive: true })
  await $.command.run({ command: 'orchestra', args: 'off' } as never)
  await clock.advance(30_000)
  for (let i = 0; i < 10; i++) await settle()
  expect(seen.listCalls).toBe(0)

  await $.command.run({ command: 'orchestra', args: 'full' } as never)
  await clock.advance(10_000)
  for (let i = 0; i < 10; i++) await settle()
  expect(seen.listCalls).toBeGreaterThan(0)
})

test('a tick skips question list while the previous call is in flight', { timeoutMs: 20_000 }, async ($, on) => {
  let release: (r: Ran) => void = () => undefined
  const slow = new Promise<Ran>(resolve => {
    release = resolve
  })
  const { clock, seen } = wire(on, () => slow)
  await $.session.start({ cwd: '/', surface: 'terminal', isInteractive: true })
  await clock.advance(30_000)
  for (let i = 0; i < 10; i++) await settle()
  expect(seen.listCalls).toBe(1)
  release(ok('[]'))
  for (let i = 0; i < 10; i++) await settle()
  await clock.advance(10_000)
  for (let i = 0; i < 10; i++) await settle()
  expect(seen.listCalls).toBeGreaterThan(1)
})
