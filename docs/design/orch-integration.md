# orch integration spec (feat-ac66c687)

> **The live spec is [delegate.md](delegate.md).** This page is kept as history for feat-ac66c687,
> which cites it. Sections 1 to 5 describe the first slice as it was built. Sections 6 and 7 now
> carry the Phase B results and the decided answers (plan-28b4a7f6, slice 3).

`orch` is a Python command-line helper that starts Claude and Codex sessions in herdr panes, sends them
tasks through a file mailbox, and records their hook events. This spec says how an orch session meets
wipnote. Status: first slice built; superseded by the native design below. Decisions taken by the owner
are in section 7.

## 0. Current design: one native path

Delegation is now one native wipnote feature, `wipnote delegate`, written in Go in the wipnote binary.
One path covers one-shot runs and interactive sessions. They share one run store
(`.wipnote/delegate/`), one herdr adapter and one canonical record (`.wipnote/delegations-ledger.html`).
orch is the reference for the port. The `codex-herdr-pane` mod is absorbed into `wipnote delegate run`,
and the `wipnote-orchestra` mod is merged into delegate. The earlier split, where the mod covered
one-shot runs and orch covered interactive sessions, is withdrawn. Commands, flags, exit codes, files
and the owner's decisions are in [delegate.md](delegate.md).

## 1. What wipnote already covers, and what orch duplicates

The table shows the state when this spec was written (first slice).

| Concern | wipnote today | orch | Overlap |
|---|---|---|---|
| Session identity | `sessions` row keyed by the harness payload `session_id` (unchanged for a Claude UUID, `core/agent/detect.go` `NormaliseSessionID`) | registry record keyed by a name (`w1`) plus `run_id`; events carry the harness id as `sid` | Same id, two keys. `sid` is the join |
| Parent session | `sessions.parent_session_id`, `agent_lineage_trace` (Task sub-agents, nested `wipnote claude`) | `launched_by.session` (only if the caller exports `ORCH_CALLER_SESSION`) | orch's is optional and never guessed |
| Sub-agent tree | `SubagentStart/Stop` hooks write `agent_lineage_trace`; `wipnote lineage sess-*` renders it (`RenderAgentTree`) | `orch tree`, from its own SubagentStart/Stop events plus transcript meta | **Duplicate** when the worker runs under wipnote hooks |
| Tool / prompt events | `agent_events` from every hook | `events.ndjson` (`tool_start`, `tool_end`, `prompt`, `stop`, ...) | **Duplicate** (names and summaries only on orch's side) |
| Work-item attribution | active claim of the session (`feature start`), `feature_id` on events and traces | none | wipnote only |
| Spawn / send / wait / result / stop | none for interactive panes. `mods/codex-herdr-pane` wraps headless `codex exec` / `claude -p` in a pane (run folder: raw.log, clean.log, usage, exit, ...) | all six, with mailbox, pinning, verified stop | orch only at the time. **Now:** one native path, `wipnote delegate`, for one-shot and interactive runs (section 0) |
| Questions waiting for a human | none | `orch questions` (AskUserQuestion, PermissionRequest) | orch only |
| Delegation record | planned: feat-dd8b0cde (tool, label, run dir, usage, exit, linked to work item) | registry record | Same idea. One record type serves both (Q4, decided) |

## 2. Mapping

- **orch session -> wipnote session.** One orch run (`name` + `run_id`) hosts one harness session at a
  time. Its `sid` in `events.ndjson` is the wipnote session id when the worker runs under wipnote hooks
  (container sessions launch `wipnote claude`; Mac sessions launch bare `claude` today). A `/clear` or
  restart inside the pane gives a new `sid`, so one orch run can map to several wipnote sessions.
- **orch session -> work item.** orch stores nothing about work items. Two possible sources (Q1):
  an explicit tag written by orch at spawn, or inference through `sid` -> wipnote session -> its claim.
- **orch session -> orchestrator.** `launched_by.session` names the calling wipnote session when set.

## 3. Which orch events become wipnote records

First slice: **none are copied.** wipnote reads orch's files at query time and renders them. Nothing
is written to `.wipnote/` or to the SQLite index about orch. Later candidates, each a decision (Q3):

| orch event | Possible wipnote record |
|---|---|
| spawn / stop of a run | delegation-run record (feat-dd8b0cde) |
| `question`, `permission_request` | a "needs human" signal on the work item |
| `subagent_start/stop` | nothing: wipnote's own hooks already record them when the worker runs under wipnote |
| `tool_*`, `prompt`, `stop`, `error` | nothing: duplicates `agent_events` |

Q3 is now decided (section 7): spawn and stop become delegation-run rows; the other events are not copied.

## 4. Where the two stores meet

- orch writes `registry.json` (`ORCH_REGISTRY`, default: a file in orch's per-user cache folder) and
  `<mail root>/<name>/events.ndjson`. Those stay the only write path for orch facts.
- wipnote reads them read-only, without orch's lock: a partial last NDJSON line is skipped, an
  unreadable file is reported as unreadable (not as "no sessions"), and a missing file means "orch not
  in use here".
- The orch store lives outside the project (per user, per host). It is never copied into `.wipnote/`,
  and no absolute host path is written into a work item.

## 5. First slice (implemented in this change)

- **orch:** `orch spawn NAME --kind K --work-item feat-1a2b3c4d` stores `work_item` on the registry
  record (an optional field, so older registries still load) and echoes it as `wi`. A malformed id is
  refused (`bad_work_item`) before any herdr call or registry write. orch stays the only writer.
- **wipnote:** `internal/orchlink` reads the registry and each session's `events.ndjson`.
  `wipnote lineage <work-item>` adds an "orch sessions" section (and `orch_sessions` in `--json`):
  name, kind, state, host or container, the harness session ids seen, sub-agents started, and why it
  is shown: `tag` (named by `--work-item`) or `sid` (derived: untagged, and one of its session ids is
  a direct `implemented_in` session of the work item). A session tagged with another work item is
  never derived. An unreadable registry prints `orch sessions: unreadable (...)` / `orch_error`; a
  missing one prints nothing. No host path is printed. Lineage for sessions, commits and files is
  unchanged.

## 6. Not verified at the time; Phase B results

The first slice was built in a cloud container with no herdr and no orch. Phase B
(plan-28b4a7f6 slice 2, feat-0c976595) checked each point. The full record of runs 1 to 9 is in
[delegate-phase-b.md](delegate-phase-b.md). Versions: herdr 0.9.5, Claude Code 2.1.310,
codex-cli 0.164.0.

- **`--settings <file>` hooks beside `wipnote claude`'s plugin hooks.** Result: **they add, they do
  not replace** (B1 PASS, run 1, and in the Linux devcontainer). `wipnote claude` rejects `--settings`
  as its own flag; a Claude flag goes after `--`. Native workers start through wipnote's launchers and
  need no `--settings` file.
- **orch worker `sid` equals the wipnote session id.** Result: **yes** (B2 PASS, runs 7 and 8; run 8
  is read-only: 34 of 35 sids are in a wipnote ledger, and the miss is a test value). Imported orch
  runs can link by `sid` (slice 18).
- **The container's `wipnote claude` writes the same project `.wipnote/` as the Mac.** Result: **not
  as one shared writer.** Across the bind mount, flock is not exclusive and appends from both sides
  lost lines; rename is atomic (B7 FAIL, run 9). The claim gate works across the mount, because each
  session writes its own claim file (B8 PASS, run 9). B6 failed on the environment: Claude Code was
  not logged in inside the container, and the container user could not write its Claude config
  folder. The rule that follows is in [delegate.md](delegate.md): a container worker writes only files
  it alone owns, and the host folds them in.
- **Anything against a real herdr or real orch run.** Result: every herdr call the adapter sends works
  on herdr 0.9.5 (H1 to H9 PASS, run 1).
- **Codex `sid`.** Result: **Codex hooks fire and carry the session id**, in the TUI (B4 PASS) and
  under `codex exec` (B5 PASS). herdr's Codex hooks and wipnote's plugin hooks both report (B3b PASS).
  The Codex parity probes C1 to C3 pass.
- **Rows tagged `human` (found in Phase B runs 4 and 5).** bug-5333ea49: an inherited
  `WIPNOTE_PARENT_SESSION` made a top-level `wipnote claude` session a subagent. The fix is confirmed
  for interactive Claude (B3a), orch workers (B2) and Codex (B3b) in runs 6 to 8.
- **Not run yet:** B9, D8a and D8b need the owner at the keyboard.

## 7. Decisions and open questions

- **Q1 (decided):** both. `orch spawn --work-item` is the asserted link; the `sid` join is a derived
  fallback, labelled as such.
- **Q2 (decided):** `$ORCH_REGISTRY`, else orch's default registry file in its per-user cache, read at
  query time.
- **Q3 (decided, plan-28b4a7f6 slice 4):** spawn and stop persist as delegation-run rows in
  `.wipnote/delegations-ledger.html`. Spawn opens the row; exit or stop closes it; usage and session
  ids enrich it. Tool, prompt and turn-stop events are not copied, because they duplicate
  `agent_events`. orch's own history enters the ledger once through `wipnote delegate import-orch`
  (rows with `end_reason=imported`), and Phase B B2 confirms that imported rows can link by `sid`.
- **Q4 (decided, plan-28b4a7f6 slice 4, q-one-type):** one record type for one-shot and interactive
  runs, with `mode=oneshot` or `mode=interactive`. Both share tool, label, work item, times, exit and
  usage. Only lineage and the CLI read it.
- The orch README gives two default mail roots. orch's settings module uses one; the README's
  "Container sessions" section names another (stale). The reader takes the mailbox path from each
  registry record, never from a default.
- The owner's other answers (the plan's top-level questions) are listed in [delegate.md](delegate.md).
