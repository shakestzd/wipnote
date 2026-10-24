import { expect, test } from 'claude-code/testing'

import { attentionSpans, fit, groupSpans } from '../hooks/model'

const row = { id: 'a', type: 'wipnote:feature-coder', description: 'a much longer description', status: 'running', startedAt: 0, tools: 3, lastTool: 'Read a.ts' }

test('work item and attention rows never pass the band width', async () => {
  const lines = [
    groupSpans({ item: 'feat-1', running: 2, waiting: 1, done: 3, failed: 1 }, 'Chase visits by hand', '2/5 steps'),
    attentionSpans({ row, why: 'stalled', since: 0 }, 400_000),
    attentionSpans({ row, why: 'failed', since: 0 }, 60_000),
    attentionSpans({ row, why: 'long-call', since: 0 }, 400_000),
  ]
  const text = (spans: { text: string }[]) => spans.map(s => s.text).join('')
  expect(text(lines[0]!)).toBe('   ▸ Chase visits by hand 2/5 steps   ● 2 running  ◐ 1 waiting  ✔ 3 done  ✘ 1 failed')
  expect(text(lines[1]!)).toContain('no tool call for 6m40s')
  expect(text(lines[1]!)).toContain('↳ Read a.ts')
  expect(text(lines[2]!)).toContain('failed 1m00s ago')
  expect(text(lines[2]!)).not.toContain('↳')
  expect(text(lines[3]!)).toContain('one call running for 6m40s  a much longer description  ↳ Read a.ts')
  for (const spans of lines) {
    const cut = fit(spans, 40).map(s => s.text).join('')
    expect([...cut].length).toBeLessThanOrEqual(40)
    expect(cut.endsWith('…')).toBe(true)
  }
})
