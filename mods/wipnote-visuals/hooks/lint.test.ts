import { expect, test } from 'claude-code/testing'

import { feedbackLine, lintReply } from './lint'

const rules = (t: string, mode?: 'strict' | '80' | 'off') => lintReply(t, { mode }).findings.map((f) => f.rule)

test('banned vocabulary fires, including chat bans and openers', async () => {
  expect(rules('This is a crucial step.')).toEqual(['vocab'])
  expect(rules('A robust, cutting-edge game-changer.')).toEqual(['vocab', 'vocab', 'vocab'])
  expect(rules('Great question! The cache is stale.')).toEqual(['vocab'])
  expect(rules('Certainly! Here is the fix.')).toEqual(['vocab'])
  expect(rules('The cache is stale.')).toEqual([])
})

test('context-sensitive words only fire in the tell sense', async () => {
  expect(rules('We leveraged the cache.')).toEqual(['vocab'])
  expect(rules('The competitive landscape shifted.')).toEqual(['vocab'])
  expect(rules('A dynamic team.')).toEqual(['vocab'])
  expect(rules('The landscape photo is 4 MB. Dynamic range is fine. The journey took 3 days.')).toEqual([])
  expect(rules('Financial leverage is a ratio.')).toEqual([])
})

test('filler fires with a fix', async () => {
  const r = lintReply('Run it in order to test. Due to the fact that it failed, retry.')
  expect(r.findings.map((f) => f.fix)).toEqual(['to', 'because'])
  expect(rules('It is important to note that x. In conclusion, y.')).toEqual(['filler', 'filler'])
  expect(rules('Run it to test.')).toEqual([])
})

test('em dashes: more than one per paragraph', async () => {
  const r = lintReply('One — two — three.\n\nOnly one — here.')
  expect(r.findings.filter((f) => f.rule === 'emdash').length).toBe(1)
  expect(r.findings[0].para).toBe(1)
  expect(r.score).toContain('em— 2')
  expect(rules('A single — dash.\n\nAnother — one.')).toEqual([])
})

test('same opener three times in a row', async () => {
  expect(rules('I ran it. I read it. I fixed it.')).toEqual(['opener'])
  expect(rules('I ran it. I read it. Then I fixed it.')).toEqual([])
})

test('negating closer: short at paragraph end only', async () => {
  expect(rules('One surgeon got it right. A whole system does not.')).toEqual(['negclose'])
  expect(rules('A whole system does not. One surgeon got it right.')).toEqual([])
  expect(rules('The test passes locally. The CI job did not finish within the time limit that we configured earlier today.')).toEqual([])
})

test('curly quotes and en dash between words, digit ranges allowed', async () => {
  expect(rules('Use “quotes” here.')).toEqual(['punct', 'punct'])
  expect(rules('Fast–slow tradeoff.')).toEqual(['punct'])
  expect(rules('See pages 10–14 and 2015–2019.')).toEqual([])
})

test('long sentences: 25 words, 20 for step lines', async () => {
  const w = (n: number) => Array(n).fill('word').join(' ')
  expect(lintReply(`${w(26)}.`).longSentences).toBe(1)
  expect(lintReply(`${w(25)}.`).longSentences).toBe(0)
  expect(lintReply(`1. ${w(21)}.`).longSentences).toBe(1)
  expect(lintReply(`- ${w(20)}.`).longSentences).toBe(0)
})

test('code fences, inline code, URLs and tables are ignored', async () => {
  const t = 'Run `crucial robust` now.\n\n```ts\nconst x = "delve" — a — b\n```\n\nSee https://example.com/crucial_robust.\n\n| crucial | robust |\n| --- | --- |'
  expect(lintReply(t).findings).toEqual([])
})

test('modes: 80 limits rules, off is empty', async () => {
  const t = 'It is crucial. “quoted”. I ran it. I read it. I fixed it.'
  expect(rules(t, '80')).toEqual(['vocab'])
  expect(rules(t, 'off')).toEqual([])
  expect(lintReply(t, { mode: 'off' }).score).toBe('tells 0 · long 0 · em— 0')
})

test('empty and whitespace input', async () => {
  for (const t of ['', '   \n\n  ']) {
    const r = lintReply(t)
    expect(r.findings).toEqual([])
    expect(feedbackLine(r)).toBe('')
  }
})

test('feedback line names the worst items and stays short', async () => {
  const line = feedbackLine(lintReply('It is crucial — really — so. ' + Array(27).fill('word').join(' ') + '.'))
  expect(line).toBe('Last reply: "crucial", 2 em dashes in ¶1, 1 sentence >25 words.')
  expect(feedbackLine(lintReply('All good.'))).toBe('')
})

const CLEAN = [
  'Status: the migration finished. 41 rows moved, 2 were skipped because their IDs already existed. I left the old table in place, so rollback is a single rename. Next I will run the index rebuild and report timings.',
  'The bug comes from the cache key. It ignores the locale, so a French user can receive the English page that another user requested first. The fix adds the locale to the key. I also added a test that requests both locales in order.',
  'You have three options.\n\n- Keep the current queue. It works, but retries pile up under load.\n- Switch to a stream. This needs a new consumer and about two days of work.\n- Add a retry cap. It is a small change and fixes most of the pile-up.\n\nI recommend the retry cap first, then revisit the stream if load grows.',
  'To reproduce it:\n\n1. Check out the `main` branch and run `make dev`.\n2. Open the settings page and clear the token field.\n3. Press Save and watch the network tab for the 401 response.\n\nThe request never carries the old token after step 2.',
  'Yes, the flag is read once at startup, so you need to restart the server.',
  'The failing line is `if (!ok) return null`, which hides the error. Here is the change:\n\n```go\nif err != nil {\n    return fmt.Errorf("load: %w", err)\n}\n```\n\nWith that in place the caller sees the real message, and the test at `store_test.go:88` passes.',
]

test('false-positive probe: six realistic replies produce zero findings', async () => {
  for (const t of CLEAN) expect(lintReply(t).findings).toEqual([])
})

test('clean realistic 120-word reply has zero findings', async () => {
  const t =
    'I traced the slow dashboard load to the session query. It scans the whole events table because the filter column has no index. On your dataset that is about 1.2 million rows, which explains the four-second delay.\n\nI added an index on the session ID and timestamp columns, then measured again. The query now returns in 30 ms, and the page renders in under half a second. The change is a single migration, so it is safe to apply on a live database.\n\nOne caveat: the index adds roughly 40 MB of disk. If that matters, I can drop the timestamp column from it and accept a slightly slower sort. Tell me which you prefer and I will update the migration.'
  expect(t.split(/\s+/).length).toBeGreaterThan(110)
  const r = lintReply(t)
  expect(r.findings).toEqual([])
  expect(r.score).toBe('tells 0 · long 0 · em— 0')
})
