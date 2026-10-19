import { expect, test } from 'claude-code/testing'

import { applyWipnote, baseName, describeCall, limitSpans, lineSpans, localTime, modelLabel, pillIndent, wrapSegments, elapsed, parseGitStatus, shortDir, visibleAgents, workItemIn, zoneOf } from '../hooks/model'

test('tracks wipnote work items started and completed in shell commands', async () => {
  const started = applyWipnote([], 'wipnote feature start feat-1234abcd && wipnote bug start bug-99aa00ff')
  expect(started).toEqual(['feat-1234abcd', 'bug-99aa00ff'])
  expect(applyWipnote(started, 'wipnote feature complete feat-1234abcd')).toEqual(['bug-99aa00ff'])
})

test('finds the work item a subagent brief names', async () => {
  expect(workItemIn('Feature: feat-02e10247. Claim it first.')).toBe('feat-02e10247')
  expect(workItemIn('no item here')).toBeUndefined()
})

test('parses git status like the vcs segment', async () => {
  const info = parseGitStatus('## main...origin/main [ahead 2, behind 1]\n M a.ts\n?? b.ts\n')
  expect(info).toEqual({ branch: 'main', dirty: 2, ahead: 2, behind: 1 })
})

test('shortens dirs and describes calls', async () => {
  expect(shortDir('/home/u/a/b/c/d', '/home/u')).toBe('…/b/c/d')
  expect(shortDir('/home/u/x', '/home/u')).toBe('~/x')
  expect(describeCall('Bash', { command: 'npm test\nmore' })).toBe('$ npm test')
  expect(describeCall('Read', { file_path: '/a/b/c.ts' })).toBe('Read c.ts')
  expect(elapsed(125_000)).toBe('2m05s')
  expect(baseName('/Users/me/work/_shared/ionna-rca')).toBe('ionna-rca')
  expect(baseName('/x/ionna-rca-wt-entitymap/')).toBe('ionna-rca-wt-entitymap')
  expect(modelLabel('claude-opus-5-5')).toBe('Opus 5.5')
  expect(modelLabel('claude-haiku-4-5-20251001')).toBe('Haiku 4.5')
  expect(modelLabel('claude-sonnet-5-5[1m]')).toBe('Sonnet 5.5 1M')
  expect(modelLabel('gpt-x')).toBe('gpt-x')
})

test('lists live agents first and drops long-finished ones', async () => {
  const row = (id: string, status: string, startedAt: number, endedAt?: number) =>
    ({ id, type: 't', description: '', status, startedAt, endedAt, tools: 0 })
  const shown = visibleAgents(
    [row('old', 'completed', 0, 1000), row('b', 'running', 50), row('done', 'completed', 10, 99_000), row('a', 'running', 20)],
    100_000,
    90_000,
  )
  expect(shown.map(one => one.id)).toEqual(['a', 'b', 'done'])
})

test('measures the mode pill the band shifts left past', async () => {
  expect(pillIndent('auto', '(shift+tab to cycle) · esc to interrupt')).toBe(18)
  expect(pillIndent('plan', '(shift+tab to cycle)')).toBe(17)
  expect(pillIndent(null, '(shift+tab to cycle)')).toBe(18)
  expect(pillIndent('auto', '? for shortcuts')).toBe(0)
})

test('wraps segments between segments to the width, with rounded caps on every line', async () => {
  const seg = (text: string) => ({ text: ` ${text} `, bg: '#000', fg: '#fff' })
  const segs = [seg('ionna-rca'), seg('main !3'), seg('Opus 5.5'), seg('ctx 33%')]
  const width = (line: ReturnType<typeof lineSpans>) => line.reduce((n, s) => n + [...s.text].length, 0)
  expect(wrapSegments(segs, 200)).toHaveLength(1)
  const narrow = wrapSegments(segs, 26)
  expect(narrow.map(line => line.map(s => s.text.trim()))).toEqual([['ionna-rca', 'main !3'], ['Opus 5.5', 'ctx 33%']])
  for (const line of narrow) {
    const spans = lineSpans(line)
    expect(spans[0]?.text).toBe('\uE0B6')
    expect(spans.at(-1)?.text).toBe('\uE0B4')
    expect(width(spans)).toBeLessThanOrEqual(26)
  }
  const [only] = wrapSegments([seg('a-very-long-branch-name-that-cannot-fit')], 20)
  expect(width(lineSpans(only ?? []))).toBeLessThanOrEqual(20)
})

test('shows the local time in the person zone, daylight saving included', async () => {
  expect(localTime(Date.UTC(2026, 9, 18, 22, 28))).toBe('6:28 PM')
  expect(localTime(Date.UTC(2026, 0, 15, 14, 5))).toBe('9:05 AM')
})

test('the usage-limits row shows each window, colored by use, with its local reset', async () => {
  const now = Date.UTC(2026, 9, 18, 23, 37)
  const spans = limitSpans(
    [
      { kind: 'five_hour', percentUsed: 42.4, resetsAt: '2026-10-19T01:40:00Z' },
      { kind: 'seven_day', percentUsed: 83, resetsAt: '2026-10-23T19:00:00Z' },
    ],
    now,
  )
  const text = spans.map(s => s.text).join('')
  expect(text).toBe(' \uF0E4 5h 42% resets 9:40 PM   7d 83% resets Fri 3:00 PM')
  expect(spans.find(s => s.text === '83%')?.color).toBe('#d70000')
  expect(limitSpans([], now)).toEqual([])
})

test('the timeZone option sets the zone, and an unknown zone falls back to US Eastern', async () => {
  expect(zoneOf('Europe/London')).toBe('Europe/London')
  expect(zoneOf(' UTC ')).toBe('UTC')
  expect(zoneOf('Not/A_Zone')).toBe('America/New_York')
  expect(zoneOf('')).toBe('America/New_York')
  expect(zoneOf(undefined)).toBe('America/New_York')
  expect(localTime(Date.UTC(2026, 9, 18, 22, 28), 'UTC')).toBe('10:28 PM')
  const spans = limitSpans([{ kind: 'five_hour', percentUsed: 10, resetsAt: '2026-10-19T01:40:00Z' }], Date.UTC(2026, 9, 18, 23, 37), 'UTC')
  expect(spans.map(s => s.text).join('')).toBe(' \uF0E4 5h 10% resets 1:40 AM')
})
