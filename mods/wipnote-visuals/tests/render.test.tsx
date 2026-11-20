import { expect, test } from 'claude-code/testing'

const OK = { result: { stdout: 'ok', stderr: '', interrupted: false } }
const done = (answer: string, isAborted = false) =>
  ({ turnId: 't1', answer, durationMs: 65_000, isAborted, reason: isAborted ? 'aborted' : 'answer', usage: null }) as never

test('the receipt line replaces the turn text; subagent calls and turns are left out; no score while plainspeak is off', async ($, on) => {
  on('tool.call', () => OK as never)
  on('turn.complete', (_$, e) => ({ text: e.answer }))
  await $.tool.call({ tool: 'Edit', file_path: 'a.go', old_string: 'x', new_string: 'y' } as never)
  await $.tool.call({ tool: 'Bash', command: 'go test ./...', description: 'test', agentId: 'sub1' } as never)
  await $.tool.call({ tool: 'Bash', command: 'go test ./...', description: 'test' } as never)
  const sub = await $.turn.complete({ ...(done('sub done') as object), agentId: 'sub1' } as never)
  expect(sub.text).toBe('sub done')
  const main = await $.turn.complete(done('Tests pass.'))
  expect(main.text).toBe('✎ 1 file · ✔ go test · 1m05s')
  // The turn was reset, so a tool-free turn gets no line, with or without a tell (plainspeak is off)
  expect((await $.turn.complete(done('A plain answer.'))).text).toBe('A plain answer.')
  expect((await $.turn.complete(done('This is a crucial step.'))).text).toBe('This is a crucial step.')
})

test('an aborted turn resets the collected calls, so nothing leaks into the next receipt (finding 1)', async ($, on) => {
  on('tool.call', () => OK as never)
  on('turn.complete', (_$, e) => ({ text: e.answer }))
  await $.tool.call({ tool: 'Edit', file_path: 'a.go', old_string: 'x', new_string: 'y' } as never)
  await $.tool.call({ tool: 'Bash', command: 'go test ./...', description: 'test' } as never)
  expect((await $.turn.complete(done('', true))).text).toBe('')
  // The next turn ran nothing, so its claim is flagged and no stale file or test shows
  expect((await $.turn.complete(done('Tests pass.'))).text).toBe('⚠ reply says tests pass; no test command ran this turn')
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
  expect((await $.command.run({ command: 'visuals', args: 'receipt on' } as never)).text).toBe('Receipt: on.')
})

test('/plainspeak drives the score: off none, 80 fewer rules, strict all (finding 5)', async ($, on) => {
  on('tool.call', () => OK as never)
  on('turn.complete', (_$, e) => ({ text: e.answer }))
  const curly = 'This is a crucial step. “Quoted” too.'
  await $.command.run({ command: 'plainspeak', args: '80' } as never)
  await $.tool.call({ tool: 'Edit', file_path: 'a.go', old_string: 'x', new_string: 'y' } as never)
  expect((await $.turn.complete(done('Edited it.'))).text).toBe('✎ 1 file · 1m05s · tells 0 · long 0 · em— 0')
  expect((await $.turn.complete(done(curly))).text).toBe('tells 1 · long 0 · em— 0')
  await $.command.run({ command: 'plainspeak', args: 'strict' } as never)
  expect((await $.turn.complete(done(curly))).text).toBe('tells 3 · long 0 · em— 0')
  await $.command.run({ command: 'plainspeak', args: 'off' } as never)
  expect((await $.turn.complete(done(curly))).text).toBe(curly)
})

test('/plainspeak: a tell adds one context line to the next prompt, once; clean, off or an abort adds none (finding 6)', async ($, on) => {
  on('turn.complete', (_$, e) => ({ text: e.answer }))
  on('prompt.submit', (_$, e) => ({ text: e.text, context: e.context }) as never)
  const submit = async () => ((await $.prompt.submit({ text: 'next' } as never)) as { context?: readonly string[] }).context ?? []
  // Default off: nothing even after a tell
  await $.turn.complete(done('This is a crucial step.'))
  expect(await submit()).toEqual([])
  expect((await $.command.run({ command: 'plainspeak', args: 'strict' } as never)).text).toBe('Plainspeak: strict.')
  await $.turn.complete(done('This is a crucial step. “Quoted” too.'))
  expect(await submit()).toEqual(['Last reply: "crucial", curly quotes or en dash.'])
  // Consumed once: the same note never rides a second prompt
  expect(await submit()).toEqual([])
  await $.turn.complete(done('The cache is stale.'))
  expect(await submit()).toEqual([])
  // An aborted turn clears a pending note
  await $.turn.complete(done('This is a crucial step.'))
  await $.turn.complete(done('', true))
  expect(await submit()).toEqual([])
  // 80 drops the punctuation rule
  await $.command.run({ command: 'plainspeak', args: '80' } as never)
  await $.turn.complete(done('This is a crucial step. “Quoted” too.'))
  expect(await submit()).toEqual(['Last reply: "crucial".'])
  expect((await $.command.run({ command: 'plainspeak', args: '' } as never)).text).toBe('Plainspeak: 80 (strict|80|off).')
  await $.command.run({ command: 'plainspeak', args: 'off' } as never)
  expect(await submit()).toEqual([])
})

test('/visuals: receipt on|off changes state; no argument reports; anything else is usage and changes nothing (finding 8)', async ($, on) => {
  on('session.start', (_$, e) => ({ cwd: e.cwd }))
  on('command.register', (_$, e) => ({ value: { command: e.name } }))
  await $.session.start({ cwd: '/tmp', surface: null, isInteractive: true })
  expect((await $.command.run({ command: 'visuals', args: '' } as never)).text).toBe('Receipt: on.')
  // The blocks half of the command is gone: lulawrite draws replies now.
  for (const args of ['bogus', 'on', 'off', 'receipt on now']) {
    expect((await $.command.run({ command: 'visuals', args } as never)).text).toMatch(/^Usage: \/visuals receipt on\|off/)
  }
  expect((await $.command.run({ command: 'visuals', args: 'receipt' } as never)).text).toBe('Receipt: on.')
  await $.command.run({ command: 'visuals', args: 'receipt off' } as never)
  expect((await $.command.run({ command: 'visuals', args: '' } as never)).text).toBe('Receipt: off.')
})

test('a reply with an old wip:* fence or a lulawrite fence is left to the plugins beneath', async ($, on) => {
  const seen: string[] = []
  on('ui.render', { component: 'AssistantMessage' }, ($$, e) => {
    seen.push(e.props.text)
    const { Text } = $$.ui.resolve(e)
    return <Text>beneath</Text>
  })
  const text = 'Pick one.\n```wip:compare\n# A*\n+ x\n```\n```lulawrite\n{"callout":{"tone":"note","text":"n"}}\n```'
  const ui = await $.ui.mount({ plugin: 'wipnote-visuals', component: 'AssistantMessage', surface: 'terminal', props: { text, isFirstOfReply: true } } as never)
  expect(seen).toEqual([text])
  await ui.unmount()
})
