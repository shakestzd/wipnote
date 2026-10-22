import { expect, test } from 'claude-code/testing'
import { RULE, parseCallout, parseCompare, parseSteps, splitBlocks } from '../hooks/blocks'

test('steps: done, next and todo prefixes, CRLF and blank lines', () => {
  expect(parseSteps('x Scaffold\r\n\r\n> Parsers\r\nRender\n')).toEqual([
    { state: 'done', text: 'Scaffold' },
    { state: 'next', text: 'Parsers' },
    { state: 'todo', text: 'Render' },
  ])
  expect(parseSteps('')).toBeNull()
  expect(parseSteps('x \n> ')).toBeNull()
})

test('compare: options, pros, cons, recommended star', () => {
  const o = parseCompare('# Redis*\n+ fast\n- extra service\n\r\n# SQLite\n+ simple\n')
  expect(o).toEqual([
    { name: 'Redis', recommended: true, pros: ['fast'], cons: ['extra service'] },
    { name: 'SQLite', recommended: false, pros: ['simple'], cons: [] },
  ])
  expect(parseCompare('+ orphan pro')?.[0]?.pros).toEqual(['orphan pro'])
  expect(parseCompare('just words')).toBeNull()
  expect(parseCompare('#*')?.[0]).toMatchObject({ recommended: true })
})

test('callout: kind, continuation lines, fallback to note', () => {
  expect(parseCallout('decision: Use SQLite\nbecause simple')).toEqual({ kind: 'decision', text: 'Use SQLite\nbecause simple' })
  expect(parseCallout('Warning: careful')).toEqual({ kind: 'warning', text: 'careful' })
  expect(parseCallout('plain text')).toEqual({ kind: 'note', text: 'plain text' })
  expect(parseCallout('note:')).toBeNull()
  expect(parseCallout('\n\n')).toBeNull()
})

test('parsers never throw on odd input', () => {
  for (const bad of [undefined, null, 42, '\u0000', '```', '#'.repeat(1e4)] as never[]) {
    expect(() => { parseSteps(bad); parseCompare(bad); parseCallout(bad); splitBlocks(bad) }).not.toThrow()
  }
})

test('splitBlocks finds closed fences and leaves open or empty ones as text', () => {
  const md = 'Plan:\n```wip:steps\nx a\n> b\n```\nDone.'
  const segs = splitBlocks(md)
  expect(segs.map(s => s.kind)).toEqual(['text', 'block', 'text'])
  const open = 'Plan:\n```wip:steps\nx a'
  expect(splitBlocks(open)).toEqual([{ kind: 'text', text: open }])
  const empty = '```wip:callout\n\n```'
  expect(splitBlocks(empty)).toEqual([{ kind: 'text', text: empty }])
  expect(splitBlocks('a\r\n```wip:callout\r\nwarning: x\r\n```\r\n')[1]).toMatchObject({ kind: 'block' })
})

test('fences indented up to a list item, with CRLF ends, still parse; a closer with trailing text does not close (finding 10)', () => {
  const kinds = (md: string) => splitBlocks(md).map(s => s.kind)
  // Three spaces of indent (CommonMark allows up to three on a fence)
  expect(kinds('Plan:\n   ```wip:compare\n   # A*\n   + x\n   ```\nDone.')).toEqual(['text', 'block', 'text'])
  // Inside a list item: the fence and its body share the item's indent
  const inList = '- Pick one:\n  ```wip:compare\n  # SQLite*\n  + no server\n  # Postgres\n  - extra service\n  ```\n- Then run it.'
  const segs = splitBlocks(inList)
  expect(segs.map(s => s.kind)).toEqual(['text', 'block', 'text'])
  expect(segs[1]).toMatchObject({ block: { kind: 'compare', options: [{ name: 'SQLite', recommended: true }, { name: 'Postgres' }] } })
  // CRLF everywhere, including a closer at the very end with no final newline
  expect(kinds('```wip:compare\r\n# A*\r\n+ x\r\n```')).toEqual(['block'])
  expect(kinds('a\r\n```wip:compare\r\n# A*\r\n+ x\r\n```\r\nb')).toEqual(['text', 'block', 'text'])
  // A closing fence must stand alone on its line
  const trailing = '```wip:compare\n# A*\n+ x\n``` and more'
  expect(splitBlocks(trailing)).toEqual([{ kind: 'text', text: trailing }])
  // An empty body has nothing to draw: it stays text, by design
  const empty = '  ```wip:compare\n  ```'
  expect(splitBlocks(empty)).toEqual([{ kind: 'text', text: empty }])
})

test('the rule stays under 100 tokens (chars / 4) and asks for compare only', () => {
  expect(RULE.length / 4).toBeLessThan(100)
  expect(RULE).toContain('wip:compare')
  expect(RULE).toContain('recommend one of several options')
  // steps and callout still parse and draw for old replies, but are no longer requested
  expect(RULE).not.toContain('wip:steps')
  expect(RULE).not.toContain('wip:callout')
})
