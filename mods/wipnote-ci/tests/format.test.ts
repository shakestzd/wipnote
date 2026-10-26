import { expect, test } from 'claude-code/testing'

import {
  NO_CHECKS_GRACE_MS, baseLines, branchLine, interval, nextWait, noChecksExpired, prLine, releaseLine, reviewLine, settled, sortChecks, span, staleLines, summary, toastText,
} from '../hooks/format'
import { COMPUTING, FAILED, FULL, GREEN, MERGED, NOW, NO_CHECKS, NO_PR, RUNNING, UNAVAILABLE } from './fixtures/snapshots'

test('failed checks sort first, then cancelled, running, passed, skipped', async () => {
  expect(sortChecks(FULL.checks).map(c => c.name)).toEqual(['go-test', 'lint', 'build', 'docs'])
  expect(sortChecks(null)).toEqual([])
})

test('elapsed time runs to now while a check is going and stops at its end', async () => {
  const [lint, build, goTest, docs] = FULL.checks!
  expect(span(lint!, NOW)).toBe('5:00')
  expect(span(lint!, NOW + 1000)).toBe('5:01')
  expect(span(goTest!, NOW + 60_000)).toBe('3:00')
  expect(span(build!, NOW)).toBe('4:10')
  expect(span(docs!, NOW)).toBe('')
  expect(span({ ...goTest!, completed_at: '0001-01-01T00:00:00Z' }, NOW)).toBe('5:00')
})

test('the status line and the PR line', async () => {
  expect(summary(FULL)).toBe('CI #85: 1/4 passed · 1 failed · 1 running')
  expect(summary(GREEN)).toBe('CI #85: 2/2 passed')
  expect(prLine(FULL)).toBe('feat/ci-pane → main · open · feat-8354cf43')
  expect(prLine({ ...FULL, work_item: '' })).toBe('feat/ci-pane → main · open')
})

test('a snapshot settles with no check pending, a merged or closed PR, or no PR', async () => {
  expect(settled(RUNNING)).toBe(false)
  expect(settled(FAILED)).toBe(true)
  expect(settled(GREEN)).toBe(true)
  expect(settled(MERGED)).toBe(true)
  expect(settled({ ...RUNNING, pr: { ...RUNNING.pr!, state: 'CLOSED' } })).toBe(true)
  expect(settled({ ...GREEN, checks: [], counts: { pass: 0, fail: 0, pending: 0, other: 0, total: 0 } })).toBe(false)
  expect(settled(NO_PR)).toBe(true)
  expect(settled(UNAVAILABLE)).toBe(false)
})

test('the cadence is 15 s while a check is pending, else 60 s', async () => {
  expect(interval(RUNNING)).toBe(15_000)
  expect(interval(UNAVAILABLE)).toBe(60_000)
  expect(interval(null)).toBe(60_000)
})

test('the toast on settle names failures, a merge, or the finished count', async () => {
  expect(toastText(FAILED)).toBe('CI #85: 1 failed')
  expect(toastText(MERGED)).toBe('PR #85 merged')
  expect(toastText(GREEN)).toBe('CI #85: all 2 checks finished')
  expect(toastText(NO_PR)).toBeUndefined()
})

test('review, branch, base and release lines', async () => {
  expect(reviewLine(FULL.review!)).toBe('review: approved · 2 open threads · merge blocked · requested core-team')
  expect(reviewLine(COMPUTING.review!)).toBe('review: none · 0 open threads · merge computing')
  expect(branchLine(FULL.branch!)).toBe('branch feat/ci-pane: 2 ahead, 1 behind origin/main · 3 dirty · 1 unpushed')
  expect(branchLine(COMPUTING.branch!)).toBe('branch feat/ci-pane: local only · clean')
  expect(staleLines(FULL.worktrees)).toEqual(['stale worktree old-pane (feat/old-pane): merged, idle 9d'])
  expect(staleLines(null)).toEqual([])
  expect(baseLines(FULL.base!)).toEqual([
    { text: 'base main: red', bad: true },
    { text: '  ✗ Release (failure)', bad: true },
    { text: '    https://example.test/run/9', bad: false },
  ])
  expect(baseLines(COMPUTING.base!)).toEqual([{ text: 'base main: green', bad: false }])
  expect(releaseLine(FULL.release!)).toBe('release v0.82.1 (2026-10-20), 14 commits since · release: green')
  expect(releaseLine(COMPUTING.release!)).toBe('release: none')
  expect(releaseLine({ state: 'published', tag: 'v1' })).toBe('release v1, commits since unknown')
})

test('an open PR with no checks settles only once the grace has run out', async () => {
  expect(NO_CHECKS_GRACE_MS).toBe(120_000)
  expect(noChecksExpired(NO_CHECKS, NOW, NOW + 119_000)).toBe(false)
  expect(noChecksExpired(NO_CHECKS, NOW, NOW + 120_000)).toBe(true)
  expect(noChecksExpired(RUNNING, NOW, NOW + 600_000)).toBe(false)
  expect(noChecksExpired(UNAVAILABLE, NOW, NOW + 600_000)).toBe(false)
  expect(noChecksExpired({ ...NO_CHECKS, pr: { ...NO_CHECKS.pr!, state: 'MERGED' } }, NOW, NOW + 600_000)).toBe(false)
})

test('the wait is cut short at the grace end so the grace ends on a fresh read', async () => {
  expect(nextWait(NO_CHECKS, NOW, NOW)).toBe(60_000)
  expect(nextWait(NO_CHECKS, NOW, NOW + 90_000)).toBe(30_000)
  expect(nextWait(RUNNING, NOW, NOW + 90_000)).toBe(15_000)
  expect(nextWait(UNAVAILABLE, NOW, NOW + 90_000)).toBe(60_000)
})
