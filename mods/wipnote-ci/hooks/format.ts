import type { Base, Branch, Check, Release, Review, Snapshot, Worktree } from '../types'

export const RANK: Record<string, number> = { fail: 0, cancel: 1, pending: 2, pass: 3, skipping: 4 }
export const ICON: Record<string, string> = { pass: '✓', fail: '✗', pending: '●', cancel: '–', skipping: '–' }
export const COLOR: Record<string, string> = {
  pass: 'success',
  fail: 'error',
  pending: 'warning',
  cancel: 'inactive',
  skipping: 'inactive',
}

/** The cadence of `wipnote ci watch`: checks while one is pending, else the slow sections. */
export const CHECKS_MS = 15_000
export const SLOW_MS = 60_000
/** How long from the first read an open PR with no checks is still watched for them (ci watch --no-checks-grace). */
export const NO_CHECKS_GRACE_MS = 120_000
/** The note ci watch ends such a PR with. */
export const NO_CHECKS_NOTE = 'no checks reported'

const CLOSED = new Set(['MERGED', 'CLOSED'])

/** Reads `wipnote ci status --json`; undefined when it is not a snapshot. */
export function parseSnapshot(stdout: string): Snapshot | undefined {
  try {
    const raw = JSON.parse(stdout) as Snapshot
    if (typeof raw !== 'object' || raw === null || typeof raw.available !== 'boolean') return undefined
    return raw
  } catch {
    return undefined
  }
}

/** Failed first, then cancelled, running, passed, skipped; by name within each. */
export function sortChecks(checks: Check[] | null | undefined): Check[] {
  return [...(checks ?? [])].sort(
    (a, b) => (RANK[a.bucket] ?? 9) - (RANK[b.bucket] ?? 9) || a.name.localeCompare(b.name),
  )
}

/** A time GitHub gave, or null for none (a check not finished can read as year 1). */
export function time(text: string | null | undefined): number | null {
  if (!text) return null
  const ms = Date.parse(text)
  return Number.isNaN(ms) || ms < Date.UTC(2000, 0, 1) ? null : ms
}

/** "m:ss" from the check's start to its end, or to now while it runs. */
export function span(check: Check, now: number): string {
  const start = time(check.started_at)
  if (start === null) return ''
  const end = time(check.completed_at) ?? now
  const seconds = Math.max(0, Math.round((end - start) / 1000))
  return `${Math.floor(seconds / 60)}:${String(seconds % 60).padStart(2, '0')}`
}

export function cut(text: string, width: number): string {
  return text.length <= width ? text : `${text.slice(0, Math.max(1, width - 1))}…`
}

/** The status line: "CI #85: 3/5 passed · 1 failed · 1 running". */
export function summary(s: Snapshot): string {
  const n = s.counts
  const parts = [`${n.pass}/${n.total} passed`]
  if (n.fail > 0) parts.push(`${n.fail} failed`)
  if (n.pending > 0) parts.push(`${n.pending} running`)
  return `CI #${s.pr?.number ?? '?'}: ${parts.join(' · ')}`
}

/** "branch → base · state · work item", under the title. */
export function prLine(s: Snapshot): string {
  if (s.pr === undefined) return ''
  const parts = [`${s.pr.branch} → ${s.pr.base}`, s.pr.state.toLowerCase()]
  if (s.work_item !== '') parts.push(s.work_item)
  return parts.join(' · ')
}

/** Settled as `wipnote ci watch` settles: the PR merged or closed, or at
 *  least one check and none pending; also when no PR matched. Never while
 *  wipnote cannot read GitHub. */
export function settled(s: Snapshot): boolean {
  if (!s.available) return false
  if (s.pr === undefined) return true
  if (CLOSED.has(s.pr.state)) return true
  return s.counts.total > 0 && s.counts.pending === 0
}

/** How long until the next read. */
export function interval(s: Snapshot | null): number {
  return s !== null && s.counts.pending > 0 ? CHECKS_MS : SLOW_MS
}

/** An open PR still without a check once the grace from `since` has run out. */
export function noChecksExpired(s: Snapshot, since: number, now: number): boolean {
  if (!s.available || s.pr === undefined || CLOSED.has(s.pr.state)) return false
  return s.counts.total === 0 && now - since >= NO_CHECKS_GRACE_MS
}

/** The wait before the next read, cut short at the grace end while there are no checks. */
export function nextWait(s: Snapshot | null, since: number, now: number): number {
  const wait = interval(s)
  if (s === null || !s.available || s.pr === undefined || s.counts.total > 0) return wait
  const left = since + NO_CHECKS_GRACE_MS - now
  return left > 0 ? Math.min(wait, left) : wait
}

/** The toast when a followed PR settles; none without a PR. */
export function toastText(s: Snapshot, noChecks = false): string | undefined {
  if (s.pr === undefined) return undefined
  const n = s.pr.number
  if (noChecks) return `CI #${n}: ${NO_CHECKS_NOTE}`
  if (s.counts.fail > 0) return `CI #${n}: ${s.counts.fail} failed`
  if (s.pr.state === 'MERGED') return `PR #${n} merged`
  return `CI #${n}: all ${s.counts.total} checks finished`
}

/** "review: approved · 2 open threads · merge blocked · requested team". */
export function reviewLine(r: Review): string {
  const merge = r.mergeable === 'computing' ? 'computing' : r.merge_state.toLowerCase()
  let line = `review: ${r.decision.toLowerCase()} · ${r.threads_open} open threads · merge ${merge}`
  if (r.requested && r.requested.length > 0) line += ` · requested ${r.requested.join(', ')}`
  return line
}

/** "branch x: 2 ahead, 1 behind origin/main · 3 dirty · 1 unpushed". */
export function branchLine(b: Branch): string {
  const where = b.base !== '' ? `${b.ahead} ahead, ${b.behind} behind ${b.base}` : b.sync
  const parts = [`branch ${b.name}: ${where}`, b.dirty > 0 ? `${b.dirty} dirty` : 'clean']
  if (b.unpushed > 0) parts.push(`${b.unpushed} unpushed`)
  return parts.join(' · ')
}

/** One line per stale worktree, named by its folder. */
export function staleLines(worktrees: Worktree[] | null | undefined): string[] {
  return (worktrees ?? [])
    .filter(w => (w.stale ?? []).length > 0)
    .map(w => {
      const reasons = (w.stale ?? []).map(r => (r === 'idle' ? `idle ${w.idle_days}d` : r))
      const folder = w.path.split('/').filter(p => p !== '').pop() ?? w.path
      return `stale worktree ${folder}${w.branch ? ` (${w.branch})` : ''}: ${reasons.join(', ')}`
    })
}

export type Line = { text: string; bad: boolean }

/** The base line, then each failing workflow and its link. */
export function baseLines(b: Base): Line[] {
  const lines: Line[] = [{ text: `base ${b.name}: ${b.state}`, bad: b.state === 'red' }]
  for (const f of b.failing ?? []) {
    lines.push({ text: `  ✗ ${f.workflow} (${f.conclusion})`, bad: true })
    if (f.url !== '') lines.push({ text: `    ${f.url}`, bad: false })
  }
  return lines
}

/** "release v1 (2026-10-20), 14 commits since · release: green", as ci status prints it. */
export function releaseLine(r: Release): string {
  if (r.state === 'none') return 'release: none'
  let line = `release ${r.tag ?? ''}`
  if (r.published_at) line += ` (${r.published_at.slice(0, 10)})`
  line += r.commits_since !== undefined ? `, ${r.commits_since} commits since` : ', commits since unknown'
  if (r.workflow) line += ` · ${r.workflow.name}: ${r.workflow.state}`
  return line
}

/** The first line of a failed run, short enough for the pane. */
export function failReason(stderr: string, exitCode: number): string {
  const line = stderr.split('\n').map(l => l.trim()).find(l => l !== '')
  return (line ?? `wipnote exited ${exitCode}`).replace(/^Error:\s*/, '').slice(0, 160)
}
