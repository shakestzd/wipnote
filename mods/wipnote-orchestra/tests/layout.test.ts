import { expect, test } from 'claude-code/testing'

import { agentSpans, columnsOf, fit } from '../hooks/model'

const row = (type: string, model: string, description: string) =>
  ({ id: type, type, model, description, status: 'running', startedAt: 0, tools: 3, lastTool: 'Read a.ts' })

test('agent rows line up and never pass the band width', async () => {
  const rows = [row('Explore', 'Haiku 4.5', 'short'), row('wipnote:feature-coder', 'Opus 5.5', 'a much longer description')]
  const cols = columnsOf(rows)
  const lines = rows.map(one => agentSpans(one, cols, '├', ['●', '#fff'], '12s'))
  const head = (spans: { text: string }[]) => spans.slice(0, -2).map(s => s.text).join('')
  expect([...head(lines[0]!)].length).toBe([...head(lines[1]!)].length)
  for (const spans of lines) {
    const cut = fit(spans, 40).map(s => s.text).join('')
    expect([...cut].length).toBeLessThanOrEqual(40)
    expect(cut.endsWith('…')).toBe(true)
  }
})
