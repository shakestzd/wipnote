# Commit Outbox — Serialized wipnote Artifact Commit Queue

Status: implemented (MVP) — feat-76504033 (track trk-b1d06a84)

## Problem

Today every agent/CLI that completes a work item writes the canonical
`.wipnote` HTML/YAML and then commits it to git directly on its own hot path
(`commitWipnoteArtifact`, `commitPlanChange`). Under concurrency that puts every
agent on git's index-writing path. The repo-scoped advisory lock
(`runGitMutation`) serializes those writers, but each agent still blocks on git.

## Model: write-first, commit-later outbox

1. The canonical `.wipnote` write completes FIRST (unchanged).
2. A **commit intent** is appended to a durable outbox. This is the only extra
   work a producer does — it never touches git.
3. A single serialized committer (`wipnote commit-queue flush`) later drains the
   outbox in FIFO order, committing each artifact under the repo-scoped advisory
   git lock via `runGitMutation`, so it serializes against all other wipnote git
   writers and no agent sits on the git hot path.

## Location — per-user cache dir, NOT inside `.wipnote/`

The outbox is derived, local, and never committed — exactly like the SQLite
read-index and the `git-mutation.lock`. It lives in the per-user cache dir:

```
~/.cache/wipnote/<path-hash>/commit-outbox.ndjson
~/.cache/wipnote/<path-hash>/commit-outbox.deadletter.ndjson
```

The path is derived from `storage.CanonicalDBPath(repoRoot)` so the path-hash
keying is reused, not re-invented (`commitOutboxPath` in
`cmd/wipnote/commit_queue.go`). Putting the outbox inside `.wipnote/` would make
it itself need committing — recursion. Keeping it out of the working tree also
means it can never be accidentally staged.

## Format — append-only NDJSON

One JSON `Intent` per line:

```json
{"repo_root":"/repo","rel_paths":[".wipnote/features/feat-1.html"],
 "message":"wipnote: complete feat-1","work_item_id":"feat-1",
 "action":"complete","enqueued_at":"2026-05-26T...Z","attempts":0}
```

Append-only means a crash mid-write loses at most the last partial line; earlier
intents are intact. `readIntents` skips blank/partial/corrupt lines so one bad
trailing line never wedges the drain. Each append is an exclusive
`flock(LOCK_EX)` + `fsync` (mirrors `internal/otel/sink/ndjson`).

## Cross-operation locking

A dedicated sibling lock file (`commit-outbox.ndjson.lock`) serializes whole
operations. Both `Append` and the *entire* `Flush` snapshot → commit → rewrite
cycle run under `withLock`. The lock spans the whole flush — not just the
individual file writes — because `Flush` reads a snapshot, commits, then rewrites
the pending file with what remains; without a lock covering that whole window, an
`Append` landing between the snapshot and the rewrite would be silently dropped
by the stale-snapshot rewrite (a lost-update race). The lock file is *separate*
from the data file because `Flush` swaps the data file via `rename`, which would
shed a lock held on the data-file inode; the stable lock-file inode persists.

## Ordering, recovery, idempotency

- **FIFO**: intents drain oldest-first.
- **Drain under the lock**: production committer (`outboxCommitter`) stages and
  commits via `runGitMutation`.
- **Confirm-then-remove**: an intent is removed from the pending file only after
  its commit returns success. The pending file is rewritten once per pass via an
  atomic temp-file + rename, so a reader never sees a half-rewritten queue.
- **Restartable / idempotent**: if a flush is interrupted after some commits but
  before the rewrite, the next flush re-runs those intents. The underlying
  artifact commit is idempotent — an already-committed artifact yields "nothing
  to commit", treated as success — so re-running causes no double-commit harm
  and the queue converges to empty.

## Dead-letter / skip semantics

Each intent carries an `attempts` counter. On commit failure the counter is
incremented and the intent stays queued. Once `attempts` reaches `max-attempts`
(default 5) the intent is moved to the dead-letter NDJSON sibling and dropped
from pending. The drain continues with the next intent in the SAME pass, so one
poison commit can never freeze the ordered queue. `commit-queue flush` and
`commit-queue status` both surface the dead-letter depth.

## `.gitignore` and `.wipnote/` — what may be ignored, what must be committed

Every queued intent names a work-item artifact, so an intent against a path git
ignores can never succeed (GH#172). The privacy instinct behind ignoring
`.wipnote/` is right — session transcripts contain prompts verbatim — but the
rule must be targeted:

| Path | Git status | Why |
|------|-----------|-----|
| `.wipnote/features/`, `bugs/`, `spikes/`, `tracks/`, `plans/`, `*.html` work items | **must be committed** | canonical work-item state; every artifact-commit intent points here |
| `.wipnote/sessions/`, `events/`, `logs/`, `*.db*`, `*.jsonl`, `*.log`, pid/lock/offset markers | **may be ignored** | runtime telemetry and prompt transcripts; already listed in the managed `.wipnote/.gitignore` |

Ignore the runtime paths by name in the repo's own `.gitignore` (for example
`.wipnote/sessions/`) rather than relying only on `.wipnote/.gitignore`, which
wipnote may regenerate. Never ignore `.wipnote/` or `.wipnote` wholesale.
Caveat: a trailing-slash `.wipnote/` rule stops git descending, so any `!`
negation beneath it is silently ignored; only `.wipnote/*` lets a
`!.wipnote/features/` negation apply.

When wipnote detects an ignored artifact path it says so: `wipnote init` warns
about an over-broad repo-level rule, the enqueue prints the matching rule, a
flush reports the intent as `ignored` (it is neither retried nor
dead-lettered — retrying cannot help), and `check --gate` prints the gitignore
explanation instead of suggesting a flush.

## CLI

```
wipnote commit-queue status                 # pending / dead-letter depths + path
wipnote commit-queue flush                  # drain FIFO under the advisory lock
wipnote commit-queue flush --max-attempts N # override dead-letter threshold
```

## Commit policy (`WIPNOTE_ARTIFACT_COMMIT_POLICY`)

| Value | Behaviour |
|-------|-----------|
| `defer` (default) | write the artifact, record an intent, commit on `flush` |
| `separate` | legacy: commit the artifact directly on every transition |
| `none` | write the artifact only — no commit, no intent; commit `.wipnote/` by hand. For "never auto-commit" projects and sandboxes whose per-user cache is unwritable |

Under `defer`, an unwritable outbox (EPERM/EACCES/EROFS — e.g. a Codex
workspace-write sandbox where `~/Library/Caches` is off-limits, GH#149) does
not reopen a completed item: the canonical artifact is already written, so the
item stays done and a pending-sync warning names the manual commit command.

## Scope and follow-ups (out of scope here)

This change ADDS the outbox mechanism, the `recordCommitIntent` producer API,
and the flush command. It does NOT change the existing direct-commit default —
the outbox is the durable alternative. Explicit follow-ups:

- Make the outbox the default autocommit path (route `commitWipnoteArtifact` /
  `commitPlanChange` through `recordCommitIntent`).
- A daemon/hook driver that flushes automatically (e.g. on `SessionStop`)
  instead of requiring a manual `flush` invocation.
- Surface pending/dead-letter depth in `wipnote status` alongside the writer
  queue line.
