import { expect, test } from 'claude-code/testing'

import { workSegments } from '../hooks/band'
import { parseStatusCounts, summaryText, viewOf, visibleWork } from '../hooks/workview'

const STATUS = [
  'wipnote status  (project)',
  '',
  '  features    528 total  (todo:91  active:34  blocked:0  done:403  stale:0)',
  '  bugs        627 total  (todo:169  active:7  blocked:0  done:451  stale:0)',
  '  spikes      57 total  (todo:6  active:9  blocked:0  done:42  stale:0)',
  '  tracks      52 total  (todo:37  active:2  blocked:0  done:13  stale:0)',
].join('\n')

test('reads the in-progress and open counts from wipnote status, items only', () => {
  expect(parseStatusCounts(STATUS)).toEqual({ active: 50, open: 266 })
  expect(parseStatusCounts('nothing useful')).toBeUndefined()
})

test('the summary line names both counts and is absent when they are unknown', () => {
  expect(summaryText({ active: 50, open: 266 })).toBe('50 in progress, 266 open')
  expect(summaryText(undefined)).toBe('')
})

test('options: active by default, list with a row cap of 3, off hides the block', () => {
  expect(viewOf({})).toEqual({ mode: 'active', rows: 3 })
  expect(viewOf({ workItems: 'list', workItemRows: 5 })).toEqual({ mode: 'list', rows: 5 })
  expect(viewOf({ workItems: 'list', workItemRows: '2' })).toEqual({ mode: 'list', rows: 2 })
  expect(viewOf({ workItems: 'list', workItemRows: 0 })).toEqual({ mode: 'list', rows: 3 })
  expect(viewOf({ workItems: 'bogus' })).toEqual({ mode: 'active', rows: 3 })
  expect(viewOf({ workItems: 'off' }).mode).toBe('off')
})

test('visible work: the active item only, a capped list, or nothing', () => {
  const work = ['a', 'b', 'c', 'd', 'e']
  expect(visibleWork(work, { mode: 'active', rows: 3 })).toEqual(['a'])
  expect(visibleWork(work, { mode: 'list', rows: 3 })).toEqual(['a', 'b', 'c'])
  expect(visibleWork(work, { mode: 'off', rows: 3 })).toEqual([])
})

test('the band segments follow the view', () => {
  const work = ['feat-1', 'feat-2', 'feat-3', 'feat-4']
  expect(workSegments(work, [], { mode: 'active', rows: 3 }, undefined)).toHaveLength(1)
  const list = workSegments(work, [], { mode: 'list', rows: 2 }, { active: 4, open: 9 })
  expect(list.map(s => s.text).join('|')).toMatch(/feat-1.*feat-2.*4 in progress, 9 open/)
  expect(list.map(s => s.text).join('|')).not.toMatch(/feat-3/)
  expect(workSegments(work, [], { mode: 'off', rows: 3 }, { active: 4, open: 9 })).toEqual([])
})
