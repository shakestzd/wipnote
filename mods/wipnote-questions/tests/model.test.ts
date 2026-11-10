import { expect, test } from 'claude-code/testing'

import {
  answerArgv, countLine, failReason, fullOutput, listArgv, parseAnswerArgs, parseQuestions, rowLine, toastLine,
} from '../hooks/model'

const json = JSON.stringify([
  {
    item_id: 'feat-1', item_title: 'Pane', question_id: 'q-1', text: 'Which store?',
    options: [{ key: 'a', label: 'File' }, { key: 'b', label: 'DB', tradeoff: 'slower' }],
    recommended: 'a', blocking: true, status: 'open', age: '2h',
  },
  { item_id: 'feat-2', item_title: 'Band', question_id: 'q-2', text: 'Color?', options: [], recommended: '', blocking: false, status: 'open', age: '1d', free_text: true },
  { nope: 1 },
])

test('reads question list JSON and skips malformed rows', async () => {
  const rows = parseQuestions(json)!
  expect(rows.map(r => r.id)).toEqual(['q-1', 'q-2'])
  expect(rows[0]!.options[1]).toEqual({ key: 'b', label: 'DB', tradeoff: 'slower' })
  expect(rows[0]!.freeText).toBe(false)
  expect(rows[1]!.freeText).toBe(true)
  expect(parseQuestions('[]')).toEqual([])
  expect(parseQuestions('null')).toEqual([])
  expect(parseQuestions('not json')).toBeUndefined()
  expect(parseQuestions('{}')).toBeUndefined()
})

test('builds the CLI calls', async () => {
  expect(listArgv()).toEqual(['wipnote', 'question', 'list', '--open', '--format', 'json'])
  expect(answerArgv('q-1', { key: 'a' })).toEqual(['wipnote', 'question', 'answer', 'q-1', 'a'])
  expect(answerArgv('q-2', { text: 'ship it' })).toEqual(['wipnote', 'question', 'answer', 'q-2', '--text', 'ship it'])
})

test('describes rows and the count', async () => {
  const rows = parseQuestions(json)!
  expect(rowLine(rows[0]!)).toBe('feat-1 · 2h · blocking · recommends a')
  expect(rowLine(rows[1]!)).toBe('feat-2 · 1d · free text')
  expect(countLine(rows)).toBe('2 open · 1 blocking')
  expect(countLine([])).toBe('No open questions')
})

test('names why a read failed', async () => {
  expect(failReason('Error: boom\nmore', 1)).toBe('Error: boom')
  expect(failReason('', 3)).toBe('wipnote exited 3')
})

test('reads /answer-question arguments: a key, or --text and a reply', async () => {
  expect(parseAnswerArgs('q-1 a')).toEqual({ id: 'q-1', answer: { key: 'a' } })
  expect(parseAnswerArgs('  q-2   --text  ship it now ')).toEqual({ id: 'q-2', answer: { text: 'ship it now' } })
  expect(parseAnswerArgs('q-1')).toBeUndefined()
  expect(parseAnswerArgs('q-1 --text   ')).toBeUndefined()
  expect(parseAnswerArgs('')).toBeUndefined()
})

const overturn = [
  'Answered q-1 on feat-1: a',
  'Assumption overturned on q-1: work on feature feat-1 assumed "b" (DB), the answer is "a" (File). Reopened feat-1; redo the work built on "b" (DB) to fit "a" (File).',
  '',
].join('\n')

test('toasts the overturn or reopen line when there is one, else the first line', async () => {
  expect(toastLine(overturn)).toBe(overturn.split('\n')[1])
  expect(toastLine('Answered q-1 on feat-1: a\n')).toBe('Answered q-1 on feat-1: a')
  expect(toastLine('\n  \n')).toBe('')
})

test('keeps the whole output for the pane', async () => {
  expect(fullOutput(overturn)).toEqual(overturn.split('\n').slice(0, 2))
  expect(fullOutput(' \n')).toEqual([])
})
