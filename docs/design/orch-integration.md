# orch integration spec (feat-ac66c687)

`orch` is a Python command-line helper that starts Claude and Codex sessions in herdr panes, sends them
tasks through a file mailbox, and records their hook events. This spec says how an orch session meets
wipnote. Status: first slice built. Decisions taken by the owner are in section 7; open ones are marked.

## 1. What wipnote already covers, and what orch duplicates

| Concern | wipnote today | orch | Overlap |
|---|---|---|---|
| Session identity | `sessions` row keyed by the harness payload `session_id` (unchanged for a Claude UUID, `core/agent/detect.go` `NormaliseSessionID`) | registry record keyed by a name (`w1`) plus `run_id`; events carry the harness id as `sid` | Same id, two keys. `sid` is the join |
| Parent session | `sessions.parent_session_id`, `agent_lineage_trace` (Task sub-agents, nested `wipnote claude`) | `launched_by.session` (only if the caller exports `ORCH_CALLER_SESSION`) | orch's is optional and never guessed |
| Sub-agent tree | `SubagentStart/Stop` hooks write `agent_lineage_trace`; `wipnote lineage sess-*` renders it (`RenderAgentTree`) | `orch tree`, from its own SubagentStart/Stop events plus transcript meta | **Duplicate** when the worker runs under wipnote hooks |
| Tool / prompt events | `agent_events` from every hook | `events.ndjson` (`tool_start`, `tool_end`, `prompt`, `stop`, ...) | **Duplicate** (names and summaries only on orch's side) |
| Work-item attribution | active claim of the session (`feature start`), `feature_id` on events and traces | none | wipnote only |
| Spawn / send / wait / result / stop | none for interactive panes. `mods/codex-herdr-pane` wraps headless `codex exec` / `claude -p` in a pane (run folder: raw.log, clean.log, usage, exit, ...) | all six, with mailbox, pinning, verified stop | orch only (the mod covers one-shot runs, orch covers interactive sessions) |
| Questions waiting for a human | none | `orch questions` (AskUserQuestion, PermissionRequest) | orch only |
| Delegation record | planned: feat-dd8b0cde (tool, label, run dir, usage, exit, linked to work item) | registry record | Same idea. One record type should serve both (question Q4) |

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

## 4. Where the two stores meet

- orch writes `registry.json` (`ORCH_REGISTRY`, default `~/.cache/orch/registry.json`) and
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

## 6. Not verified

- That `--settings <file>` adds orch's hooks on top of `wipnote claude`'s plugin hooks rather than
  replacing them (orch README calls this an open risk). If it replaces them, the worker has no wipnote
  session and the `sid` join finds nothing.
- That the container's `wipnote claude` writes to the same project `.wipnote/` as the Mac orchestrator.
- Anything against a real herdr or real orch run: this was built in a cloud container with neither.
- Codex `sid`: orch declares no Codex hooks, so Codex workers have no `sid` at all.

## 7. Decisions and open questions

- **Q1 (decided):** both. `orch spawn --work-item` is the asserted link; the `sid` join is a derived
  fallback, labelled as such.
- **Q2 (decided):** `$ORCH_REGISTRY`, else `~/.cache/orch/registry.json`, read at query time.
- **Q3 (decided, next slice):** persist orch spawn and stop as run records. Not in this slice, which
  stays read-only. Persisting means wipnote receives the facts (for example from `wipnote` reading the
  registry at `orch stop`, or orch calling a wipnote command); orch's NDJSON stays its own store.
- **Q4 (open):** one record type with feat-dd8b0cde's delegation run, or a separate one. The dashboard
  is being removed, so the answer only affects the record shape and `wipnote lineage`, not a web view.
- The orch README gives two default mail roots. The code (`models/settings.py`) uses
  `~/work/.orch/mail`; README "Container sessions" still says `~/work/adhoc/orch-mail` (stale). The
  reader takes the mailbox path from each registry record, never from a default.
