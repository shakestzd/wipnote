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

test('each subagent row shows its model and work item', async ($, on) => {
  mock.clock(on, { now: 1000 })
  on('agent.spawn', () => ({ model: 'claude-haiku-4-5-20251001', agentId: 'a1' }) as never)
  await $.agent.spawn({ subagentType: 'Explore', description: 'Survey types', prompt: 'Feature: feat-abcdef12' } as never)
  for (const surface of ['terminal', 'desktop'] as const) {
    const ui = await $.ui.mount({ ...BAND, surface })
    expect(await ui.find({ type: 'Text', text: /Haiku 4\.5/ })).toBeDefined()
    expect(await ui.find({ type: 'Text', text: /feat-abcdef12/ })).toBeDefined()
    await ui.unmount()
  }
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
