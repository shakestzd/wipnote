// The JSON `wipnote ci status [pr|branch] --json` prints (internal/gitwatch:
// Snapshot and its sections). Go's nil slices arrive as null.

export type Check = {
  name: string
  workflow: string
  /** pass, fail, pending, skipping or cancel */
  bucket: string
  required: boolean
  started_at: string | null
  completed_at: string | null
  link: string
}

export type Counts = { pass: number; fail: number; pending: number; other: number; total: number }

export type PR = { number: number; title: string; branch: string; base: string; state: string; url: string; base_oid?: string }

export type Review = {
  decision: string
  requested: string[] | null
  threads_open: number
  threads_outdated: number
  /** "computing" while GitHub is still working out mergeability */
  mergeable: string
  merge_state: string
}

export type BaseRun = { workflow: string; head_sha: string; status: string; conclusion: string; url: string; created_at: string }

export type Base = { name: string; state: string; base_red_too: boolean; failing: BaseRun[] | null; runs: BaseRun[] | null }

export type ReleaseWorkflow = { name: string; state: string; status?: string; conclusion?: string; url?: string; created_at?: string }

export type Release = {
  state: string
  tag?: string
  published_at?: string
  url?: string
  commits_since?: number
  workflow?: ReleaseWorkflow
}

export type Branch = {
  name: string
  base: string
  ahead: number
  behind: number
  dirty: number
  unpushed: number
  upstream?: string
  sync: string
}

export type Worktree = {
  path: string
  branch?: string
  main?: boolean
  locked?: boolean
  prunable?: boolean
  idle_days: number
  stale?: string[] | null
}

export type Snapshot = {
  available: boolean
  reason?: string
  pr?: PR
  work_item: string
  checks: Check[] | null
  counts: Counts
  review?: Review
  base?: Base
  release?: Release
  fetched_at: string
  branch?: Branch
  worktrees?: Worktree[] | null
  notes?: string[] | null
}

/** What the pane follows and the last snapshot it read. */
export type CiView = {
  /** The argument /ci was given ('' for the current branch); null before the first /ci or after /ci stop */
  arg: string | null
  snap: Snapshot | null
  /** When the last read finished, in milliseconds since the epoch */
  fetchedAt: number
  /** The last read's problem, empty when it worked */
  error: string
  /** Settled (no check pending, the PR merged or closed, no PR matched, or no checks after the grace): polling has stopped */
  done: boolean
  /** When the first read of this /ci finished: the no-checks grace runs from here */
  since: number
  /** Settled because the PR still had no checks when the grace ran out */
  noChecks: boolean
}

declare module 'claude-code' {
  interface PluginState {
    'wipnote-ci': { view: Shaped<CiView>; now: Shaped<number> }
  }
}
