import { expect, test } from 'claude-code/testing'

// Why the ref rule can be missing from a session's system prompt: what runs above or beneath the
// mod's prompt.compose hook, staged as inline plugins (each self-contained: the kit loads a
// register as source), through the real chain. The outermost plugin appends next.trace as a
// section, so the test reads what the chain decided for the mod's link.
const RULE = 'wipnote-orchestra:ref-rule'
const INPUT = { model: 'claude-test', promptModel: 'claude-test', surfaces: ['terminal'], tools: ['Bash'], outputStyle: null, traits: [] } as const
// Stands in for the engine beneath every plugin: its shared sections first, then a session one
const ENGINE = [
  { id: 'intro', text: 'intro', scope: 'shared' }, { id: 'env', text: 'env', scope: 'session' },
] as const
type Composed = { sections: readonly { id: string; text: string; scope: string }[] }
const ids = (r: Composed) => r.sections.map(s => s.id)
// The trace the outermost plugin appended: one [plugin, tier, outcome, reason] per link
const traceOf = (r: Composed) => JSON.parse(r.sections.at(-1)!.text) as string[][]
const linkOf = (r: Composed, plugin: string) => traceOf(r).find(l => l[0] === plugin)

// An organization's policy plugin (managed, prepend) that sends the event on beneath the user tier
const passesPastUser = {
  name: 'policy-above', tier: 'prepend' as const,
  register(on: any) {
    on('prompt.compose', async ($: unknown, e: unknown, next: any) => {
      const r = await next.to(e, 'append')
      const trace = JSON.stringify(next.trace.map((x: any) => [x.plugin, x.tier, x.outcome, x.reason ?? '']))
      return { sections: [...r.sections, { id: 'policy-above:trace', text: trace, scope: 'session' }] }
    })
  },
}
const plainAbove = {
  name: 'plain-above', tier: 'prepend' as const,
  register(on: any) {
    on('prompt.compose', async ($: unknown, e: unknown, next: any) => {
      const r = await next(e)
      const trace = JSON.stringify(next.trace.map((x: any) => [x.plugin, x.tier, x.outcome, x.reason ?? '']))
      return { sections: [...r.sections, { id: 'plain-above:trace', text: trace, scope: 'session' }] }
    })
  },
}
// A plugin beneath the mod that answers a session section ahead of a shared one
const misorderedBeneath = {
  name: 'misordered-beneath', tier: 'append' as const,
  register(on: any) {
    on('prompt.compose', async ($: unknown, e: unknown, next: any) => {
      const r = await next(e)
      return { sections: [{ id: 'misordered-beneath:x', text: 'x', scope: 'session' }, ...r.sections] }
    })
  },
}
const engine = (on: any) => on('prompt.compose', () => ({ sections: ENGINE }))

test('a managed plugin whose next.to goes beneath the user tier bypasses the hook: no rule', { plugins: [passesPastUser] }, async ($, on) => {
  engine(on)
  const r = await $.prompt.compose(INPUT)
  expect(ids(r)).not.toContain(RULE)
  expect(linkOf(r, 'wipnote-orchestra')).toEqual(['wipnote-orchestra', 'user', 'skipped', 'bypassed by policy-above'])
})

test('the same plugin calling next keeps the rule, after the engine sections', { plugins: [plainAbove] }, async ($, on) => {
  engine(on)
  const r = await $.prompt.compose(INPUT)
  expect(ids(r)).toEqual(['intro', 'env', RULE, 'plain-above:trace'])
  expect(linkOf(r, 'wipnote-orchestra')?.[2]).toBe('returned')
})

test('a misordered list from beneath is refused there and never reaches the hook', { plugins: [plainAbove, misorderedBeneath] }, async ($, on) => {
  engine(on)
  const r = await $.prompt.compose(INPUT)
  expect(ids(r)).toEqual(['intro', 'env', RULE, 'plain-above:trace'])
  expect(linkOf(r, 'misordered-beneath')?.[2]).toBe('kept') // its own answer refused, beneath's stands
  expect(linkOf(r, 'wipnote-orchestra')?.[2]).toBe('returned')
})
