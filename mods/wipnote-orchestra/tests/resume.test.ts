import { mock, test, expect } from 'claude-code/testing'

const ID = 'feat-1a2b3c4d'
const SHOW = JSON.stringify({
  id: ID,
  title: 'Cards in the band',
  status: 'blocked',
  steps: [{ completed: true }, { completed: false }],
})

const BAND = {
  plugin: 'wipnote-orchestra',
  component: 'PromptHint',
  props: { isDraft: false, isWorking: false, hint: '? for shortcuts' },
} as const

// The kit has timers but the mod's lib has no DOM or Node typings.
declare const setTimeout: (run: () => void, ms: number) => unknown
const settle = () => new Promise(resolve => setTimeout(() => resolve(undefined), 5))

test('a resumed conversation reads the claim, shows it in the band and reloads its cards', async ($, on) => {
  mock.clock(on)
  // /resume moves the process to another session id; no session.start and no classic event.
  let sid = 'session-1'
  let resumed = false
  on('session.id', async () => ({ value: sid }))
  on('session.start', (_$, e) => ({ cwd: e.cwd }))
  on('command.register', (_$, e) => ({ value: { command: e.name } }))
  on('session.end', (_$, e) => {
    sid = 'session-2'
    resumed = true
    return { sessionId: e.sessionId }
  })
  const runs: string[] = []
  const ok = (stdout: string) => ({ value: { exitCode: 0, stdout, stderr: '', isStdoutTruncated: false, isStderrTruncated: false } })
  on('process.run', async (_$, e) => {
    runs.push(e.argv.join(' '))
    if (e.argv[1] === 'who') return ok(JSON.stringify({ work_item: resumed ? ID : '' }))
    if (e.argv[1] === 'feature') return ok(SHOW)
    return { value: { exitCode: 1, stdout: '', stderr: '', isStdoutTruncated: false, isStderrTruncated: false } }
  })
  const fence = `Done.\n\`\`\`wipnote-ref\n${ID}\n\`\`\``
  on('session.messages', async () => ({ value: resumed ? [{ role: 'assistant', text: fence }] : [] }) as never)

  await $.session.start({ cwd: '/tmp', surface: 'terminal', isInteractive: true })
  for (let i = 0; i < 20; i++) await settle()
  runs.length = 0
  await $.session.end({ reason: 'resume', sessionId: 'session-1', resume: { id: 'session-2' } })
  for (let i = 0; i < 20; i++) await settle()

  expect(runs).toContain('wipnote who --json')
  expect(runs.filter(r => r.includes(`show ${ID}`)).length).toBeGreaterThan(0)

  // The claimed item is in the band without a shell `start`; its title and steps
  // arrive with the band's next card refresh.
  for (const surface of ['terminal', 'desktop'] as const) {
    const ui = await $.ui.mount({ ...BAND, surface })
    expect(await ui.find({ type: 'Text', text: new RegExp(ID) })).toBeDefined()
    await ui.unmount()
  }
})
