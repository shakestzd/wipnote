import { mock, test, expect } from 'claude-code/testing'

const SHOW = JSON.stringify({ id: 'feat-1a2b3c4d', title: 'Cards', status: 'in-progress', steps: [] })

test('a resumed conversation reloads its cards and the status line', async ($, on) => {
  mock.clock(on)
  on('session.id', async () => ({ value: 'session-1' }))
  const runs: string[] = []
  const statuses: (string | undefined)[] = []
  on('process.run', async (_$, e) => {
    runs.push(e.argv.join(' '))
    if (e.argv[1] === 'who') return { value: { exitCode: 0, stdout: JSON.stringify({ work_item: 'feat-1a2b3c4d' }), stderr: '' } }
    return { value: { exitCode: 0, stdout: SHOW, stderr: '' } }
  })
  on('session.messages', async () => ({ value: [{ role: 'assistant', text: 'Done.\n```wipnote-ref\nfeat-1a2b3c4d\n```' }] }))
  on('ui.status', async (_$, e) => { statuses.push(e.text); return { value: undefined } })
  on('classic.SessionStart', async () => ({}))

  await $.classic.SessionStart({ source: 'resume' })
  await new Promise(r => setTimeout(r, 50))

  expect(runs).toContain('wipnote who --json')
  expect(runs.filter(r => r.includes('show feat-1a2b3c4d')).length).toBeGreaterThan(0)
  expect(statuses.at(-1)).toContain('feat-1a2b3c4d')
})
