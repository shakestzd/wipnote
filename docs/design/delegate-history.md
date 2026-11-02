# wipnote delegate: decisions and results history

Sections 4 to 8 of [delegate.md](delegate.md), moved out to keep the live spec short.

## 4. Owner decisions

**Round 3 (2026-10-25):**

- **R. Result.** The worker writes `outbox/<task-id>-result.json`, as a preamble on the brief tells it.
  The Stop hook only signals "turn ended"; no assistant message or transcript text is stored
  (slices 5, 10, 11).
- **S. Codex sandbox.** Read-only by default; `--write` gives workspace-write; danger-full-access only
  for a host named in the user config `delegate.codex.dangerFullAccessHosts`, never a committed file.
  A failed bwrap probe refuses with its output (slice 12; q-codex-sandbox).
- **C. Container.** ccx means container Claude. `--container` runs the worker in the project's own
  devcontainer (`.devcontainer/devcontainer.json`, through `devcontainer up` and `devcontainer exec`),
  never an ad-hoc image. There is no second-account gate (slice 21; q-container-image).
- **L. Layout.** Exactly as orch: one workers tab per caller in the caller's workspace. Each new
  worker is a split inside it, cap 6. The caller's own tab is never split (slices 6, 10, 19;
  q-worker-layout).
- **D. orch defects become two-sided regression tests.** D1 and D1b (a result file or a stop ends
  wait) and D3 (result returns the newest file) in slice 11. D2 (one readiness predicate) and D8 (a
  grey next-prompt suggestion reads as empty, never as working) in slices 10 and 11. D6 (a fresh
  worker is ready at once) in slice 11. D4 (only real Agent or Task sub-agent pairs count) in slice 5.
  D5 (stop states what it closed and why not) in slice 10. D7 (an outbox question file is pending) in
  slices 11 and 17.
- **E. Resume.** `spawn NAME2 --resume NAME` relaunches the recorded session. Parking an idle worker
  is rejected; stop-when-done stays the rule (slice 20; q-resume-mode).
- **P. Projects.** delegate plus herdr is a multi-harness Projects harness. wipnote plans stay the
  durable layer (slices 23 to 25).

**2026-10-26:**

- **Codex workers run interactively.** Inside herdr, a Codex run is an interactive TUI worker, as for
  Claude; outside herdr, `codex exec` stays (slice 26; q-codex-workers). Phase B C1 to C3, B3b, B4 and
  B5 met the condition slice 22 set. Idle detection for a Codex worker still waits on D8b.
- **Lean default profile.** Workers start with a small built-in tool set, the wipnote plugin only, and
  only the MCP servers the task names. Claude: Read, Edit, Write, Glob, Grep, Bash. Codex: shell and
  apply_patch on; web search, view_image, image_generation, browser_use, apps, multi_agent and plugins
  off (q-lean-tools). The task widens it with `--tools`, `--plugin` and `--mcp`, named items only, and
  the run folder records the widened set (q-lean-widen). OAuth users are never put on `--bare`
  (slice 27).
- **Container workers write only files they alone own.** Their own `events-<writer>.ndjson`, result
  file and claim file, replaced by rename. Never a shared `.wipnote/` file such as the sessions or
  gate ledger. wipnote on the host folds them in. The worker's sessions-ledger row reaches the host
  ledger through that fold, with `host=container` and the real harness (slice 21; Phase B B7).
- **Container login.** The owner logs in to Claude Code inside the container once with `/login`. The
  repo's named volume for the Claude config folder keeps the login. `delegate doctor` checks the login
  and that the folder is writable, and names the fix. A launcher-passed token and mounted host
  credentials are rejected for now (slice 21; Phase B B6).
- **Phase B B2 is read-only.** B2 reads orch's registry and each session's `events.ndjson` and
  spawns, sends and stops nothing (slice 2).
- **A `wipnote claude` session is a root session.** Each interactive session started by
  `wipnote claude` is a root session and is recorded with its real harness, never `human`
  (bug-5333ea49). `spawn` scrubs the inherited `WIPNOTE_PARENT_SESSION`, `WIPNOTE_PARENT_EVENT` and
  `WIPNOTE_NESTING_DEPTH`, so a worker is its own root. The caller link travels in
  `WIPNOTE_DELEGATE_CALLER` and the ledger (slice 10).

Other answered questions: q-command-name (`wipnote delegate`), q-live-store (`.wipnote/delegate/`,
gitignored), q-intercept (deny-redirect), q-label (explicit only), q-viewer (Go viewer), q-chained
(v2), q-orch-import (import, then delete orchlink), q-orch-source (README and inventory only),
q-answer-questions (answer by typing into the pane), q-mod-removal (same release), q-antigravity (no),
q-claude-pane (orchestra), q-worker-claim (the worker claims), q-claim-handoff and
q-concurrent-workers (section 5).

## 5. Claims

A spawned worker claims its work item through the existing claim-episode model (q-worker-claim:
the worker claims). No claim store or outcome is added. Available (slice 16). Liveness is the
sessions-ledger rule (a session is live until its row records an end), now that bug-01f182e5 is
fixed.

- **How the worker claims.** Its brief begins with `wipnote <kind> start <id>` (the kind follows
  the id's prefix). The item lock, the collision gate and the ledger write run unchanged. Planned:
  `wait` and `list` report it when the episode did not appear.
- **Handoff (q-claim-handoff: co-claim, workers count as family).** The orchestrator keeps its
  episode while the worker holds its own, so the claim never leaves the orchestrator and nothing has
  to return. The claim gate (`liveForeignClaims` and `checkLiveCollisionGate`) treats an episode as
  family in two cases:
  - it is held by a session in `worker_sessions` of an open interactive row whose `caller_session` is
    the caller (the orchestrator can re-start or review);
  - the caller is a worker (`WIPNOTE_DELEGATE_RUN` names an open row) and the episode is held by that
    row's `caller_session` or its root (the worker can claim beside the orchestrator).

  An unrelated live session is still refused. An unreadable delegations ledger fails open.
- **Concurrency (q-concurrent-workers: one worker per item in v1).** `spawn` refuses when an open
  interactive row for the item has a live worker (its pane lives, or its run folder cannot be
  read). A row whose pane is gone goes through the worker-loss path first. One-shot runs do not
  claim and do not count.
- **Stop.** The worker's own SessionEnd closes its episode as `abandoned`; the row records
  `end_reason=stop`.
- **Crash or lost pane.** When herdr reports the pane absent (or holding another terminal), `list`,
  `stop` and `spawn` close the row as `gone`, then close only that worker's sessions' episodes as
  `expired` (`worker_sessions` plus sessions bound in events that no `list` has moved yet). The
  caller's episode is never touched. `wipnote claims reconcile` stays the backstop.
- **wait timeout.** It changes no claim.
- **Orchestrator ends first.** Its SessionEnd closes only its own rows; workers keep their claims.
- **Completion.** Whoever completes the item closes every open episode on it. So stop the worker
  before completing; `<kind> complete` warns (never refuses) while an open interactive row exists
  for the item.
- **After `/clear`.** The new worker session still carries `WIPNOTE_DELEGATE_RUN`, so it is family of
  the caller and of the run's earlier worker sessions, and its next `<kind> start` reopens its
  episode. The next `list` adds it to `worker_sessions`, so the caller's gate treats it as family
  too.

## 6. Interactive parity (spike spk-87139e0e)

Full record: [delegate-interactive-parity.md](delegate-interactive-parity.md).

- **Load controls: parity.** An interactive launch honours every load control a headless launch
  does, in both harnesses.
- **Result, exit status and cost: no parity out of the box.** wipnote collects them from hooks, the
  status line and transcript files.
- **Claude Code: conditional GO.** Inside herdr, a Claude run becomes an interactive worker with the
  spike's wrapper (slice 22). Outside herdr, `claude -p` stays with the same report.
- **Codex: HOLD at the time of the spike, now lifted.** Phase B C1 to C3 passed, and the owner
  decided on 2026-10-26 that Codex runs inside herdr become interactive workers (slice 26).

## 7. wipnote-orchestra merges into delegate

Decision (q-claude-pane: orchestra; slice 19): the `wipnote-orchestra` mod is part of delegate. Its
code and release move under the delegate feature. Slice 19 is merged on main: the band reads
`.wipnote/delegate/runs/` and shows one row per live or ended run, one-shot and interactive, with the
first line of `last.md`. It works outside herdr and never writes to the run store. It replaces the
`codex-herdr-pane` mod's Claude Code run pane.

## 8. Phase B results

Full record: [delegate-phase-b.md](delegate-phase-b.md) (runs 1 to 9, 2026-10-26; herdr 0.9.5,
Claude Code 2.1.310, codex-cli 0.164.0).

- **PASS:** H1 to H9, B1, B2 (run 8, read-only), B3a and B3b (run 6), B4, B5, B7L, B8L, B8 (run 9)
  and C1 to C3.
- **FAIL:** B7 (run 9): across the bind mount, flock is not exclusive and appends from both sides lost
  lines; rename is atomic. This is the source of the container rule in section 4. B6 (run 9) failed
  on the environment: no Claude login in the container and an unwritable Claude config folder.
- **Not run:** B9, D8a and D8b need the owner at the keyboard. Slice 17 waits on B9; Codex idle
  detection waits on D8b.
- Runs 2 to 5 failed on script bugs and on bug-5333ea49, fixed before run 6.
