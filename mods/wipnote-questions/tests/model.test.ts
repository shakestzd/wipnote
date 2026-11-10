import { expect, test } from 'claude-code/testing'

import { answerArgv, countLine, failReason, listArgv, parseQuestions, rowLine } from '../hooks/model'

const json = JSON.stringify([
  {
    item_id: 'feat-1', item_title: 'Pane', question_id: 'q-1', text: 'Which store?',
    options: [{ key: 'a', label: 'File' }, { key: 'b', label: 'DB', tradeoff: 'slower' }],
    recommended: 'a', blocking: true, status: 'open', age: '2h',
  },
  { item_id: 'feat-2', item_title: 'Band', question_id: 'q-2', text: 'Color?', options: [], recommended: '', blocking: false, status: 'open', age: '1d' },
  { nope: 1 },
])

test('reads question list JSON and skips malformed rows', async () => {
  const rows = parseQuestions(json)!
  expect(rows.map(r => r.id)).toEqual(['q-1', 'q-2'])
  expect(rows[0]!.options[1]).toEqual({ key: 'b', label: 'DB', tradeoff: 'slower' })
  expect(parseQuestions('[]')).toEqual([])
  expect(parseQuestions('null')).toEqual([])
  expect(parseQuestions('not json')).toBeUndefined()
  expect(parseQuestions('{}')).toBeUndefined()
})

test('builds the CLI calls', async () => {
  expect(listArgv()).toEqual(['wipnote', 'question', 'list', '--open', '--format', 'json'])
  expect(answerArgv('q-1', 'a')).toEqual(['wipnote', 'question', 'answer', 'q-1', 'a'])
})

test('describes rows and the count', async () => {
  const rows = parseQuestions(json)!
  expect(rowLine(rows[0]!)).toBe('feat-1 · 2h · blocking · recommends a')
  expect(rowLine(rows[1]!)).toBe('feat-2 · 1d')
  expect(countLine(rows)).toBe('2 open · 1 blocking')
  expect(countLine([])).toBe('No open questions')
})

test('names why a read failed', async () => {
  expect(failReason('Error: boom\nmore', 1)).toBe('Error: boom')
  expect(failReason('', 3)).toBe('wipnote exited 3')
})
