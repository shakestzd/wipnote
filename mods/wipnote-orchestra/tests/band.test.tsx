import { expect, mock, test } from 'claude-code/testing'

const BAND = {
  plugin: 'wipnote-orchestra',
  component: 'PromptHint',
  props: { isDraft: false, isWorking: true, hint: '(shift+tab to cycle) · ? for shortcuts' },
} as const

const ABOVE = {
  plugin: 'wipnote-orchestra',
  component: 'AbovePrompt',
  props: { hasSurvey: false, isWorking: true, maxRows: 10, bodyColumns: 100, scroll: { offset: 0, bodyRows: 9 }, view: {} },
} as const

test('the band shows the work item a shell command started, on every surface', async ($, on) => {
  on('tool.call', () => ({ result: { stdout: 'ok', stderr: '', interrupted: false } }) as never)
  await $.tool.call({ tool: 'Bash', command: 'wipnote feature start feat-12345678', description: 'start' } as never)
  for (const surface of ['terminal', 'desktop'] as const) {
    const ui = await $.ui.mount({ ...BAND, surface })
    expect(await ui.find({ type: 'Text', text: /feat-12345678/ })).toBeDefined()
    expect(await ui.find({ type: 'Text', text: /Bash|wipnote/ })).toBeDefined()
    await ui.unmount()
  }
})

test('agents show as one row per work item, not one row per agent', async ($, on) => {
  mock.clock(on, { now: 1000 })
  on('agent.spawn', () => ({ model: 'claude-haiku-4-5-20251001', agentId: 'a1' }) as never)
  await $.agent.spawn({ subagentType: 'Explore', description: 'Survey types', prompt: 'Feature: feat-abcdef12' } as never)
  for (const surface of ['terminal', 'desktop'] as const) {
    const ui = await $.ui.mount({ ...BAND, surface })
    expect(await ui.find({ type: 'Text', text: /▸ feat-abcdef12.*● 1 running/ })).toBeDefined()
    expect(await ui.find({ type: 'Text', text: /◉ 1 agent/ })).toBeDefined()
    expect(await ui.find({ type: 'Text', text: /Survey types|Haiku 4\.5/ })).toBeUndefined() // Claude Code's own panel lists those
    await ui.unmount()
  }
})

// Advancing the mock clock 12 minutes runs every 5 s refresh in between, about 4.8 s of real
// time on a devcontainer: right at the kit's 5000 ms default, so it failed about half the time.
test('a subagent quiet past the stall time is flagged, and one on a long call says so', { timeoutMs: 20_000 }, async ($, on) => {
  const clock = mock.clock(on, { now: 1000 })
  on('session.start', (_$, e) => ({ cwd: e.cwd }))
  on('command.register', (_$, e) => ({ value: { command: e.name } }))
  on('agent.spawn', () => ({ model: 'claude-haiku-4-5-20251001', agentId: 'a1' }) as never)
  let finish: (v: unknown) => void = () => {}
  on('tool.call', () => new Promise(done => { finish = done }) as never)
  await $.session.start({ cwd: '/tmp', surface: null, isInteractive: true })
  await $.agent.spawn({ subagentType: 'Explore', description: 'Survey types', prompt: 'look' } as never)
  const call = $.tool.call({ tool: 'Bash', command: 'npm test', description: 'test', agentId: 'a1' } as never)
  await clock.advance(6 * 60_000)
  const ui = await $.ui.mount({ ...BAND, surface: 'terminal' })
  expect(await ui.find({ type: 'Text', text: /◷ Explore one call running for 6m0\ds.*↳ \$ npm test/ })).toBeDefined()
  await ui.unmount()
  finish({ result: { stdout: '', stderr: '', interrupted: false } })
  await call
  await clock.advance(6 * 60_000)
  const after = await $.ui.mount({ ...BAND, surface: 'terminal' })
  expect(await after.find({ type: 'Text', text: /⚠ Explore no tool call for 6m0\ds/ })).toBeDefined()
  await after.unmount()
})

test('with no room left under the bar, no agent row and no "more" line is drawn', async ($, on) => {
  mock.clock(on, { now: 1000 })
  on('agent.spawn', () => ({ model: 'claude-haiku-4-5-20251001', agentId: 'a1' }) as never)
  await $.agent.spawn({ subagentType: 'Explore', description: 'Survey types', prompt: 'Feature: feat-abcdef12' } as never)
  await $.command.run({ command: 'orchestra', args: 'above' } as never)
  for (const surface of ['terminal', 'desktop'] as const) {
    const tight = await $.ui.mount({ ...ABOVE, surface, props: { ...ABOVE.props, maxRows: 2 } })
    expect(await tight.find({ type: 'Text', text: /◉ 1 agent/ })).toBeDefined()
    expect(await tight.find({ type: 'Text', text: /▸ feat-abcdef12|more/ })).toBeUndefined()
    await tight.unmount()
  }
})

test('a session launched in a mode shifts the band past its pill before any prompt', async ($, on) => {
  on('classic.SessionStart', () => ({}) as never)
  on('tool.call', () => ({ result: { stdout: 'ok', stderr: '', interrupted: false } }) as never)
  await $.tool.call({ tool: 'Bash', command: 'wipnote bug start bug-0000aaaa', description: 'start' } as never)
  const hint = { ...BAND, props: { ...BAND.props, hint: '1 shell · ← 6 agents · ↓ to manage' } }
  const before = await $.ui.mount({ ...hint, surface: 'terminal' })
  const unshifted = JSON.stringify(await before.find({ key: 'band' }))
  await before.unmount()
  await $.classic.SessionStart({ source: 'startup', permission_mode: 'auto' } as never)
  const after = await $.ui.mount({ ...hint, surface: 'terminal' })
  const shifted = JSON.stringify(await after.find({ key: 'band' }))
  await after.unmount()
  expect(unshifted).not.toContain('-18')
  expect(shifted).toContain('-18') // the pill and its " · ": "⏵⏵ auto mode on" is 15 cells, plus 3
})

test('the band sits below the prompt, keeps the hint, and /orchestra above moves it', async ($, on) => {
  on('ui.render', ($, e) => {
    const { Text } = $.ui.resolve(e)
    return <Text>engine hint</Text>
  })
  on('tool.call', () => ({ result: { stdout: 'ok', stderr: '', interrupted: false } }) as never)
  await $.tool.call({ tool: 'Bash', command: 'wipnote bug start bug-0000aaaa', description: 'start' } as never)
  for (const surface of ['terminal', 'desktop'] as const) {
    const below = await $.ui.mount({ ...BAND, surface })
    expect(await below.find({ type: 'Text', text: /bug-0000aaaa/ })).toBeDefined()
    expect(await below.find({ type: 'Text', text: /\? for shortcuts/ })).toBeDefined()
    expect(await below.find({ type: 'Text', text: /^\(shift\+tab to cycle\) · \? for shortcuts$/ })).toBeDefined()
    await below.unmount()
  }
  await $.command.run({ command: 'orchestra', args: 'above' } as never)
  for (const surface of ['terminal', 'desktop'] as const) {
    const above = await $.ui.mount({ ...ABOVE, surface })
    expect(await above.find({ type: 'Text', text: /bug-0000aaaa/ })).toBeDefined()
    await above.unmount()
    const below = await $.ui.mount({ ...BAND, surface })
    expect(await below.find({ type: 'Text', text: /bug-0000aaaa/ })).toBeUndefined()
    expect(await below.find({ type: 'Text', text: /engine hint/ })).toBeDefined()
    await below.unmount()
  }
})

for (const [zone, shown] of [['UTC', 'Sun 18 · 10:28 PM'], [undefined, 'Sun 18 · 6:28 PM']] as const) {
  test(`the clock segment reads the timeZone option (${zone ?? 'default'})`, zone === undefined ? {} : { options: { timeZone: zone } }, async ($, on) => {
    mock.clock(on, { now: Date.UTC(2026, 9, 18, 22, 28) })
    on('session.start', (_$, e) => ({ cwd: e.cwd }))
    on('command.register', (_$, e) => ({ value: { command: e.name } }))
    await $.session.start({ cwd: '/tmp', surface: null, isInteractive: true })
    for (const surface of ['terminal', 'desktop'] as const) {
      const ui = await $.ui.mount({ ...BAND, surface })
      expect(await ui.find({ type: 'Text', text: new RegExp(shown) })).toBeDefined()
      await ui.unmount()
    }
  })
}
