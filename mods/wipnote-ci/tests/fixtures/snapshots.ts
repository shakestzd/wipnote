// Snapshots as `wipnote ci status --json` prints them, written by hand from
// internal/gitwatch's structs (snapshot.go, checks.go, review.go, base.go,
// release.go, branch.go, worktrees.go).
import type { Check, Snapshot } from '../../types'

/** The mock clock's start: five minutes after the checks began. */
export const NOW = Date.parse('2026-10-25T23:05:00Z')

const check = (name: string, bucket: string, completed: string | null, link: string): Check => ({
  name, workflow: 'CI', bucket, required: true,
  started_at: bucket === 'skipping' ? null : '2026-10-25T23:00:00Z', completed_at: completed, link,
})

const PR = { number: 85, title: 'CI pane over the CLI', branch: 'feat/ci-pane', base: 'main', state: 'OPEN', url: 'https://example.test/pr/85' }

/** Every section filled: a failed, a running, a passed and a skipped check. */
export const FULL: Snapshot = {
  available: true,
  pr: PR,
  work_item: 'feat-8354cf43',
  checks: [
    check('lint', 'pending', null, 'https://example.test/run/3'),
    check('build', 'pass', '2026-10-25T23:04:10Z', 'https://example.test/run/1'),
    check('go-test', 'fail', '2026-10-25T23:03:00Z', 'https://example.test/run/2'),
    check('docs', 'skipping', null, 'https://example.test/run/4'),
  ],
  counts: { pass: 1, fail: 1, pending: 1, other: 1, total: 4 },
  review: { decision: 'approved', requested: ['core-team'], threads_open: 2, threads_outdated: 1, mergeable: 'MERGEABLE', merge_state: 'BLOCKED' },
  base: {
    name: 'main', state: 'red', base_red_too: true,
    failing: [{ workflow: 'Release', head_sha: 'abc123', status: 'completed', conclusion: 'failure', url: 'https://example.test/run/9', created_at: '2026-10-25T22:00:00Z' }],
    runs: [],
  },
  release: {
    state: 'published', tag: 'v0.82.1', published_at: '2026-10-20T12:00:00Z', url: 'https://example.test/rel', commits_since: 14,
    workflow: { name: 'release', state: 'green' },
  },
  fetched_at: '2026-10-25T23:05:00Z',
  branch: { name: 'feat/ci-pane', base: 'origin/main', ahead: 2, behind: 1, dirty: 3, unpushed: 1, upstream: 'origin/feat/ci-pane', sync: 'ahead' },
  worktrees: [
    { path: '/w/main', branch: 'main', main: true, idle_days: 0 },
    { path: '/w/old-pane', branch: 'feat/old-pane', idle_days: 9, stale: ['merged', 'idle'] },
  ],
  notes: ['release: gh release view: HTTP 502'],
}

/** Only the checks and the PR differ; slow sections left out. */
export function withChecks(checks: Check[], state = 'OPEN'): Snapshot {
  const by = (b: string) => checks.filter(c => c.bucket === b).length
  const counts = { pass: by('pass'), fail: by('fail'), pending: by('pending'), other: 0, total: checks.length }
  counts.other = counts.total - counts.pass - counts.fail - counts.pending
  return { available: true, pr: { ...PR, state }, work_item: 'feat-8354cf43', checks, counts, fetched_at: '2026-10-25T23:05:00Z' }
}

export const RUNNING = withChecks([
  check('build', 'pass', '2026-10-25T23:04:10Z', 'https://example.test/run/1'),
  check('lint', 'pending', null, 'https://example.test/run/3'),
])
export const FAILED = withChecks([
  check('build', 'pass', '2026-10-25T23:04:10Z', 'https://example.test/run/1'),
  check('go-test', 'fail', '2026-10-25T23:03:00Z', 'https://example.test/run/2'),
])
export const GREEN = withChecks([
  check('build', 'pass', '2026-10-25T23:04:10Z', 'https://example.test/run/1'),
  check('lint', 'pass', '2026-10-25T23:04:30Z', 'https://example.test/run/3'),
])
export const MERGED = withChecks([check('build', 'pass', '2026-10-25T23:04:10Z', 'https://example.test/run/1')], 'MERGED')

/** Mergeability still being computed, no threads, nothing requested. */
export const COMPUTING: Snapshot = {
  ...RUNNING,
  review: { decision: 'none', requested: null, threads_open: 0, threads_outdated: 0, mergeable: 'computing', merge_state: 'UNKNOWN' },
  branch: { name: 'feat/ci-pane', base: '', ahead: 0, behind: 0, dirty: 0, unpushed: 0, sync: 'local only' },
  base: { name: 'main', state: 'green', base_red_too: false, failing: null, runs: null },
  release: { state: 'none' },
}

/** gh missing: the reason, and the branch health read without it. */
export const UNAVAILABLE: Snapshot = {
  available: false,
  reason: 'gh is not installed',
  work_item: '',
  checks: [],
  counts: { pass: 0, fail: 0, pending: 0, other: 0, total: 0 },
  fetched_at: '2026-10-25T23:05:00Z',
  branch: { name: 'feat/ci-pane', base: 'origin/main', ahead: 0, behind: 0, dirty: 0, unpushed: 0, sync: 'up to date' },
}

/** gh works but no pull request matched. */
export const NO_PR: Snapshot = {
  available: true,
  reason: 'no pull request for branch feat/ci-pane',
  work_item: '',
  checks: [],
  counts: { pass: 0, fail: 0, pending: 0, other: 0, total: 0 },
  fetched_at: '2026-10-25T23:05:00Z',
}
