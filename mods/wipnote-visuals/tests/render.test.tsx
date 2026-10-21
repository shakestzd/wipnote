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
