import { expect, test } from 'claude-code/testing'

const msg = (text: string, surface: 'terminal' | 'desktop') =>
  ({ plugin: 'wipnote-visuals', component: 'AssistantMessage', surface, props: { text, isFirstOfReply: true } }) as never

const STEPS = 'Plan\n```wip:steps\nx Scaffold\n> Parse\nRender\n```'
const COMPARE = '```wip:compare\n# Redis*\n+ fast\n- extra service\n# SQLite\n+ simple\n```'
const CALLOUT = '```wip:callout\nwarning: this deletes data\n```'

for (const surface of ['terminal', 'desktop'] as const) {
  test(`steps draw numbered with markers on ${surface}`, async $ => {
    const ui = await $.ui.mount(msg(STEPS, surface))
    expect(await ui.find({ type: 'Text', text: /1\. / })).toBeDefined()
    expect(await ui.find({ type: 'Text', text: /Parse/ })).toBeDefined()
    expect(await ui.find({ type: 'Text', text: /→/ })).toBeDefined()
    await ui.unmount()
  })

  test(`compare draws a column per option on ${surface}`, async $ => {
    const ui = await $.ui.mount(msg(COMPARE, surface))
    expect(await ui.find({ type: 'Text', text: /Redis.*recommended/ })).toBeDefined()
    expect(await ui.find({ type: 'Text', text: /SQLite/ })).toBeDefined()
    expect(await ui.find({ type: 'Text', text: /extra service/ })).toBeDefined()
    await ui.unmount()
  })

  test(`callout draws its kind and text on ${surface}`, async $ => {
    const ui = await $.ui.mount(msg(CALLOUT, surface))
    expect(await ui.find({ type: 'Text', text: /Warning/ })).toBeDefined()
    expect(await ui.find({ type: 'Text', text: /deletes data/ })).toBeDefined()
    await ui.unmount()
  })
}

const OK = { result: { stdout: 'ok', stderr: '', interrupted: false } }
const done = (answer: string) => ({ turnId: 't1', answer, durationMs: 65_000, isAborted: false, reason: 'answer', usage: null }) as never

test('the receipt line replaces the turn text; subagent calls and turns are left out', async ($, on) => {
  on('tool.call', () => OK as never)
  on('turn.complete', (_$, e) => ({ text: e.answer }))
  await $.tool.call({ tool: 'Edit', file_path: 'a.go', old_string: 'x', new_string: 'y' } as never)
  await $.tool.call({ tool: 'Bash', command: 'go test ./...', description: 'test', agentId: 'sub1' } as never)
  await $.tool.call({ tool: 'Bash', command: 'go test ./...', description: 'test' } as never)
  const sub = await $.turn.complete({ ...(done('sub done') as object), agentId: 'sub1' } as never)
  expect(sub.text).toBe('sub done')
  const main = await $.turn.complete(done('Tests pass.'))
  expect(main.text).toBe('✎ 1 file · ✔ go test · 1m05s')
  // The turn was reset, so a tool-free turn gets no line
  const next = await $.turn.complete(done('A plain answer.'))
  expect(next.text).toBe('A plain answer.')
})

test('a claim without a test run is flagged; /visuals receipt off silences it', async ($, on) => {
  on('tool.call', () => OK as never)
  on('turn.complete', (_$, e) => ({ text: e.answer }))
  await $.tool.call({ tool: 'Write', file_path: 'b.go', content: '' } as never)
  const flagged = await $.turn.complete(done('All tests pass.'))
  expect(flagged.text).toBe('✎ 1 file · 1m05s\n⚠ reply says tests pass; no test command ran this turn')
  const off = await $.command.run({ command: 'visuals', args: 'receipt off' } as never)
  expect(off.text).toBe('Receipt: off.')
  await $.tool.call({ tool: 'Write', file_path: 'c.go', content: '' } as never)
  expect((await $.turn.complete(done('All tests pass.'))).text).toBe('All tests pass.')
  expect((await $.command.run({ command: 'visuals', args: 'receipt' } as never)).text).toBe('Receipt: on.')
})

test('/visuals off removes the rule; on restores it', async ($, on) => {
  on('prompt.compose', () => ({ sections: [] }) as never)
  on('session.start', (_$, e) => ({ cwd: e.cwd }))
  on('command.register', (_$, e) => ({ value: { command: e.name } }))
  await $.session.start({ cwd: '/tmp', surface: null, isInteractive: true })
  const has = async () => JSON.stringify(await $.prompt.compose({ promptModel: 'claude-sonnet-4-5', outputStyle: null, tools: [], traits: [], surfaces: [] } as never)).includes('wipnote-visuals:rule')
  expect(await has()).toBe(true)
  await $.command.run({ command: 'visuals', args: 'off' } as never)
  expect(await has()).toBe(false)
  await $.command.run({ command: 'visuals', args: 'on' } as never)
  expect(await has()).toBe(true)
})
