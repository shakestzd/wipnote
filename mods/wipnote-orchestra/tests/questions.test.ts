import { expect, test } from 'claude-code/testing'

import { fresh, itemQuestionsArgv, openArgv, parseOpen, questionLines, questionsText } from '../hooks/questions'

const out = JSON.stringify([
  { item_id: 'feat-1', question_id: 'q-1', text: 'Which store?', blocking: true, recommended: 'a', age: '2h' },
  { item_id: 'feat-1', question_id: 'q-2', text: 'Color?', blocking: false, recommended: '', age: '1d' },
  { bad: true },
])

test('reads open questions and builds the calls', async () => {
  const rows = parseOpen(out)!
  expect(rows.map(r => r.id)).toEqual(['q-1', 'q-2'])
  expect(parseOpen('null')).toEqual([])
  expect(parseOpen('nope')).toBeUndefined()
  expect(openArgv()).toEqual(['wipnote', 'question', 'list', '--open', '--format', 'json'])
  expect(itemQuestionsArgv('feat-1')).toEqual(['wipnote', 'question', 'list', '--item', 'feat-1', '--open', '--format', 'json'])
})

test('the band text shows only with open questions', async () => {
  const rows = parseOpen(out)!
  expect(questionsText(rows)).toBe('2 open questions · 1 blocking')
  expect(questionsText(rows.slice(1))).toBe('1 open question · 0 blocking')
  expect(questionsText([])).toBe('')
  expect(questionsText(undefined)).toBe('')
})

test('only a blocking question not seen before is new', async () => {
  const rows = parseOpen(out)!
  const seen = new Set<string>()
  expect(fresh(rows, seen).map(r => r.id)).toEqual(['q-1'])
  expect(fresh(rows, seen)).toEqual([])
})

test('ref cards list the item questions', async () => {
  expect(questionLines(parseOpen(out)!)).toEqual(['? Which store? · blocking · recommends a', '? Color?'])
})
