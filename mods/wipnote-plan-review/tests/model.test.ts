import { expect, test } from 'claude-code/testing'

import { detailLine, failReason, parseSlices, planCallIn, tallyLine } from '../hooks/model'

test('finds the plan a shell command reviews', async () => {
  expect(planCallIn('wipnote plan slices plan-3a88d8a9')).toEqual({ planId: 'plan-3a88d8a9', opens: true })
  expect(planCallIn('wipnote plan approve-slice plan-1 2 && wipnote plan reject-slice plan-2 3 --changes-requested'))
    .toEqual({ planId: 'plan-2', opens: true })
  expect(planCallIn('wipnote plan promote-slice plan-3a88d8a9 1')).toEqual({ planId: 'plan-3a88d8a9', opens: false })
  expect(planCallIn('wipnote plan list')).toBeUndefined()
  expect(planCallIn('go test ./...')).toBeUndefined()
})

test('reads wipnote plan slices JSON', async () => {
  const parsed = parseSlices(JSON.stringify({
    plan_id: 'plan-1', title: 'Pane', status: 'review',
    slices: [
      { num: 1, title: 'CLI', complexity: 'trivial', approval_status: 'approved', execution_status: 'promoted', feature_id: 'feat-1' },
      { num: 2, title: 'Mod', complexity: 'standard', effort: 'M', risk: 'Low', approval_status: 'odd' },
    ],
  }))
  expect(parsed?.title).toBe('Pane')
  expect(parsed?.slices.map(s => s.approval)).toEqual(['approved', 'pending'])
  expect(detailLine(parsed!.slices[0]!)).toBe('trivial · promoted')
  expect(detailLine(parsed!.slices[1]!)).toBe('standard · M · Low')
  expect(tallyLine(parsed!.slices)).toBe('1 approved · 1 pending')
  expect(parseSlices('not json')).toBeUndefined()
  expect(parseSlices('{"plan_id":"x"}')).toBeUndefined()
})

test('a failed run keeps its first error line', async () => {
  expect(failReason('Error: load plan: open x: no such file\nUsage: ...', 1)).toBe('load plan: open x: no such file')
  expect(failReason('', 2)).toBe('wipnote exited 2')
})
