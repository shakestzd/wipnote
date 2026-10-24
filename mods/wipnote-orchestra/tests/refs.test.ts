import { expect, test } from 'claude-code/testing'

import { CARD_TTL_MS, Latest, changedRefs, classifyExit, failureText, needsFetch, parseCard, parseWho, pickRefresh, refsInMessages, showArgv, splitRefs, stepBar, stepCount } from '../hooks/refs'

test('splits a closed wipnote-ref fence out of the text', () => {
  const md = 'Done.\n\n```wipnote-ref\nfeat-65bcc27c\n```\nThanks.'
  expect(splitRefs(md)).toEqual([
    { kind: 'text', text: 'Done.\n\n' },
    { kind: 'refs', ids: ['feat-65bcc27c'] },
    { kind: 'text', text: '\nThanks.' },
  ])
})

test('reads ids from the info line and dedupes them', () => {
  const segs = splitRefs('```wipnote-ref bug-fa036758\nbug-fa036758 spk-5a716533\n```')
  expect(segs).toEqual([{ kind: 'refs', ids: ['bug-fa036758', 'spk-5a716533'] }])
})

test('leaves an open (streaming) fence and other fences as text', () => {
  const open = 'Working\n```wipnote-ref\nfeat-65bc'
  expect(splitRefs(open)).toEqual([{ kind: 'text', text: open }])
  const other = '```go\nfeat-65bcc27c\n```'
  expect(splitRefs(other)).toEqual([{ kind: 'text', text: other }])
  const empty = '```wipnote-ref\nnothing here\n```'
  expect(splitRefs(empty)).toEqual([{ kind: 'text', text: empty }])
})

test('maps id prefixes to the read-only show command', () => {
  expect(showArgv('feat-65bcc27c')).toEqual(['wipnote', 'feature', 'show', 'feat-65bcc27c', '--format', 'json'])
  expect(showArgv('spk-5a716533')[1]).toBe('spike')
  expect(showArgv('trk-0677c709')[1]).toBe('track')
  expect(showArgv('bug-fa036758')[1]).toBe('bug')
})

// Shape captured from `wipnote feature show feat-03132978 --format json`.
const SHOW = JSON.stringify({
  id: 'feat-03132978', title: 'Add recommended answers', type: 'feature', status: 'in-progress',
  steps: [
    { step_id: 'task-1', description: 'a', completed: true },
    { step_id: 'task-2', description: 'b', completed: false },
    { step_id: 'task-3', description: 'c', completed: true },
  ],
})

test('parses show JSON into a card with step progress', () => {
  const c = parseCard('feat-03132978', SHOW)
  expect(c.title).toBe('Add recommended answers')
  expect(c.status).toBe('in-progress')
  expect(stepCount(c.steps)).toBe('2/3 steps')
  expect(stepBar(c.steps)).toBe('███████░░░')
})

test('an item without steps reads cleanly', () => {
  const c = parseCard('feat-65bcc27c', JSON.stringify({ id: 'feat-65bcc27c', title: 'x', status: 'done' }))
  expect(stepCount(c.steps)).toBe('')
  expect(stepBar(c.steps)).toBe('')
})

test('cards are fetched when missing or older than the TTL', () => {
  expect(needsFetch(undefined, 1_000)).toBe(true)
  expect(needsFetch({ state: 'loaded', at: 1_000 }, 1_000 + CARD_TTL_MS)).toBe(false)
  expect(needsFetch({ state: 'loaded', at: 1_000 }, 1_001 + CARD_TTL_MS)).toBe(true)
  expect(needsFetch({ state: 'failed', at: 1_000 }, 1_001 + CARD_TTL_MS)).toBe(true)
  expect(needsFetch({ state: 'loading', at: 1_000 }, 2_000)).toBe(false)
})

// Output captured from `wipnote feature show feat-deadbeef --format json` (exit 1).
const MISSING = 'work item "feat-deadbeef" not found on disk — the ID resolved but the HTML file is missing'

test('a missing item is told apart from other failures', () => {
  expect(classifyExit(0, '{}', '')).toEqual({ ok: true, stdout: '{}' })
  expect(classifyExit(1, '', MISSING)).toEqual({ ok: false, kind: 'missing' })
  expect(classifyExit(1, MISSING, '')).toEqual({ ok: false, kind: 'missing' })
  expect(classifyExit(1, '', 'database is locked')).toEqual({ ok: false, kind: 'error' })
  expect(failureText('feat-1a2b3c', 'missing')).toBe('feat-1a2b3c · not in this checkout (pull to update)')
  expect(failureText('feat-1a2b3c', 'unavailable')).toBe('feat-1a2b3c · wipnote unavailable')
  expect(failureText('feat-1a2b3c', 'error')).toBe('feat-1a2b3c · details unavailable')
})

test('an unreadable who result is unknown, not "no claim"', () => {
  expect(parseWho('{"session_id":"s","work_item":"feat-1a2b3c"}')).toBe('feat-1a2b3c')
  expect(parseWho('{"session_id":"s"}')).toBe('')
  expect(parseWho('not json')).toBe(null)
})

test('the refresh cap takes the stalest open cards, so none is starved', () => {
  const card = (id: string, status: string) => ({ id, title: id, status, type: 'feature', steps: [] })
  const entries: [string, import('../types').CardEntry][] = [
    ['feat-aaaaaa', { state: 'loaded', card: card('feat-aaaaaa', 'in-progress'), at: 300 }],
    ['feat-bbbbbb', { state: 'loaded', card: card('feat-bbbbbb', 'done'), at: 100 }],
    ['feat-cccccc', { state: 'failed', reason: 'missing', at: 200 }],
    ['feat-dddddd', { state: 'loaded', card: card('feat-dddddd', 'todo'), at: 50 }],
  ]
  expect(pickRefresh(entries, 2)).toEqual(['feat-dddddd', 'feat-cccccc'])
  expect(pickRefresh(entries, 10)).toEqual(['feat-dddddd', 'feat-cccccc', 'feat-aaaaaa'])
})

test('saved assistant references are read back from the transcript', () => {
  const msgs = [
    { role: 'user', text: '```wipnote-ref\nfeat-111111\n```' },
    { role: 'assistant', text: 'Done.\n```wipnote-ref\nfeat-222222\n```' },
    { role: 'assistant', text: '```wipnote-ref\nbug-333333\nfeat-222222\n```' },
  ]
  expect(refsInMessages(msgs, 10)).toEqual(['bug-333333', 'feat-222222'])
  expect(refsInMessages(msgs, 1)).toEqual(['feat-222222'])
})

test('only the newest of overlapping lookups may write', () => {
  const latest = new Latest()
  const older = latest.take('status')
  const newer = latest.take('status')
  expect(latest.isNewest('status', older)).toBe(false)
  expect(latest.isNewest('status', newer)).toBe(true)
  // Keys are independent: a card lookup does not cancel the status lookup.
  const card = latest.take('card:feat-1a2b3c')
  expect(latest.isNewest('card:feat-1a2b3c', card)).toBe(true)
  expect(latest.isNewest('status', newer)).toBe(true)
})

test('finds the ids a shell command changes steps or status on', () => {
  expect(changedRefs('wipnote feature complete-step feat-65bcc27c 2')).toEqual(['feat-65bcc27c'])
  expect(changedRefs('wipnote bug add-step bug-fa036758 "x" && wipnote bug complete bug-fa036758')).toEqual(['bug-fa036758'])
  expect(changedRefs('wipnote spike start spk-5a716533; wipnote track complete trk-0677c709')).toEqual(['spk-5a716533', 'trk-0677c709'])
  expect(changedRefs('wipnote feature show feat-65bcc27c --format json')).toEqual([])
  expect(changedRefs('go test ./...')).toEqual([])
})
