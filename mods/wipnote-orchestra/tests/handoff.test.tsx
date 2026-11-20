import { expect, test } from 'claude-code/testing'

const ID = 'feat-1a2b3c4d'
const LULA = '```lulawrite\n{"callout":{"tone":"note","text":"Ship it."}}\n```'
const REF = `\`\`\`wipnote-ref\n${ID}\n\`\`\``

const mount = (text: string) =>
  ({ plugin: 'wipnote-orchestra', component: 'AssistantMessage', props: { text, isFirstOfReply: true }, surface: 'terminal' }) as never

// The fake below stands for the plugins beneath this mod and the engine: it records each text it is asked to draw.
test('a reply with a wipnote-ref fence and a lulawrite fence hands the lulawrite fence to next, not to Markdown', async ($, on) => {
  const beneath: string[] = []
  on('ui.render', { component: 'AssistantMessage' }, ($$, e) => {
    beneath.push(e.props.text)
    const { Text } = $$.ui.resolve(e)
    return <Text>beneath:{e.props.text.length}</Text>
  })
  const before = `Done.\n${LULA}\n`
  const after = `\nSee the note.\n${LULA}`
  const ui = await $.ui.mount(mount(`${before}${REF}${after}`))
  // Both stretches of text reach the plugins beneath with their fences whole, in order.
  expect(beneath.length).toBe(2)
  expect(beneath[0]).toContain(LULA)
  expect(beneath[1]).toContain(LULA)
  expect(beneath.join('')).not.toContain('wipnote-ref')
  // The mod draws the ref itself (no card is loaded yet, so the id shows) and no Markdown of its own.
  expect(await ui.find({ type: 'Text', text: new RegExp(ID) })).toBeDefined()
  expect(await ui.find({ type: 'Text', text: /^beneath:/ })).toBeDefined()
  expect(await ui.find({ type: 'Markdown' })).toBeUndefined()
  await ui.unmount()
})

test('a reply without a wipnote-ref fence passes through whole', async ($, on) => {
  const beneath: string[] = []
  on('ui.render', { component: 'AssistantMessage' }, ($$, e) => {
    beneath.push(e.props.text)
    const { Text } = $$.ui.resolve(e)
    return <Text>beneath</Text>
  })
  const ui = await $.ui.mount(mount(`Plain.\n${LULA}`))
  expect(beneath).toEqual([`Plain.\n${LULA}`])
  await ui.unmount()
})

test('empty text around a ref is not sent beneath', async ($, on) => {
  const beneath: string[] = []
  on('ui.render', { component: 'AssistantMessage' }, ($$, e) => {
    beneath.push(e.props.text)
    const { Text } = $$.ui.resolve(e)
    return <Text>beneath</Text>
  })
  const ui = await $.ui.mount(mount(`${REF}\n`))
  expect(beneath).toEqual([])
  await ui.unmount()
})
