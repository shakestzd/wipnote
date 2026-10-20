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
  on('session.id', async () => ({ value: 'session-1' }))
  const runs: string[] = []
  const ok = (stdout: string) => ({ value: { exitCode: 0, stdout, stderr: '', isStdoutTruncated: false, isStderrTruncated: false } })
  on('process.run', async (_$, e) => {
    runs.push(e.argv.join(' '))
    if (e.argv[1] === 'who') return ok(JSON.stringify({ work_item: ID }))
    if (e.argv[1] === 'feature') return ok(SHOW)
    return { value: { exitCode: 1, stdout: '', stderr: '', isStdoutTruncated: false, isStderrTruncated: false } }
  })
  on('session.messages', async () => ({ value: [{ role: 'assistant', text: `Done.\n\`\`\`wipnote-ref\n${ID}\n\`\`\`` }] }) as never)
  on('classic.SessionStart', async () => ({}))

  await $.classic.SessionStart({ source: 'resume' })
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
