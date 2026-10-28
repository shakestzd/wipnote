# wipnote delegate: the live spec

`wipnote delegate` is wipnote's one native delegation path. It runs one-shot tool calls and drives
interactive Claude and Codex workers in herdr panes. Every run is one canonical row in
`.wipnote/delegations-ledger.html`. This page is the live spec. Its source of truth is plan
plan-28b4a7f6 and the owner's answers in it. History: [orch-integration.md](orch-integration.md)
(feat-ac66c687). Port checklist: [orch-inventory.md](orch-inventory.md).

**Status words.** "Available" means the command is on main today, and its flags and exit codes here
are taken from the code. "Planned (slice N)" means plan-28b4a7f6 slice N will add it. A planned
command does not exist yet. Do not call it.

## 1. One path

The earlier split (the `codex-herdr-pane` mod for one-shot runs, orch for interactive sessions) is
withdrawn. One path now covers both:

- One-shot runs (`codex exec`, `codex review`, `claude -p`, `ccx task`) and interactive workers share
  one run store, one herdr adapter and one record type.
- The `codex-herdr-pane` mod is absorbed into `wipnote delegate run` and is removed in the release
  that ships `delegate run` and the Bash redirect (q-mod-removal, slice 14).
- The `wipnote-orchestra` mod is merged into delegate (section 7).
- orch is the reference, not a dependency (q-adopt-vs-port: port orch natively in Go).
- There is no daemon. Every verb is a short CLI process, a bounded wait, or a process in a herdr
  pane. A hook writes one small file and returns.
- A wait settles on durable signals only: the worker's result file, an exit file, or the Stop
  hook's "turn ended" signal. herdr agent state is a hint. Nobody parses a pane's screen to decide
  that a worker finished.
- Antigravity workers are not in v1 (q-antigravity).

## 2. Commands

| Command | Status | What it does |
|---|---|---|
| `delegate run` | available (slice 8) | One tool call as a recorded run, in a herdr pane or headless |
| `delegate run-pane` | available (slice 8), hidden, internal | The pane worker that `run` starts in its pane |
| `delegate view` | available (slice 7) | Follow a run's log until it ends, then show the answer |
| `delegate import-orch` | available (slice 18) | Copy orch's run history into the ledger once |
| `delegate spawn` | planned (slice 10) | Start an interactive worker in the caller's workers tab |
| `delegate boot` | planned (slice 10), internal | Runs in the worker's pane: reads the brief, starts the launcher |
| `delegate stop` | planned (slice 10) | Verified stop of a worker |
| `delegate list` | planned (slice 10) | List runs and worker readiness |
| `delegate tree` | planned (slice 10) | Caller, runs, workers and sub-agents as a tree |
| `delegate send` | planned (slice 11) | Put a task in a worker's inbox, with a herdr hint |
| `delegate wait` | planned (slice 11) | Block until a durable signal or the timeout |
| `delegate result` | planned (slice 11) | Print the newest result file a worker wrote |
| `delegate questions` | planned (slice 11) | List a worker's pending questions |
| `delegate inbox` | planned (slice 11) | The worker's own read of its mailbox |
| `delegate doctor` | planned (slice 12) | Preflight checks (Codex sandbox; container in slice 21; profile in slice 27) |
| `delegate answer` | planned (slice 17) | Answer a pending worker question by typing into its pane |

Errors in every available command: wipnote prints the reason on stderr and exits 1. That includes a
bad flag, a wrong argument count and a refused input.

### 2.1 `wipnote delegate run` (available)

```
wipnote delegate run [--label L] [--work-item ID] [--no-pane] -- <codex exec|codex review|claude -p|ccx task> ...
```

Runs one tool call. It opens a ledger row, creates a run folder and runs the tool in a herdr pane when
`HERDR_ENV=1`, else headless. It prints a status line, the answer (or the end of the cleaned log on a
failure) and the run page. Then it closes and enriches the row and sweeps old runs.

| Flag | Meaning |
|---|---|
| `--label L` | Pane title. It is the only label the ledger keeps (q-label: explicit only). Without it the title is `<id>: <title>` of the run's work item (cut to 48 runes), or one derived from the prompt when there is none; that title goes only to the run folder's `label` file |
| `--work-item ID` | Work item for the row. Default: the caller's active claim |
| `--no-pane` | Run headless even inside herdr |
| `--` | Everything after it is the tool's argv. wipnote parses no flag after the first argument |

Accepted argv: `codex exec` (or `codex e`), `codex review`, `claude` with `-p` or `--print`, and
`ccx task`. Refused before anything is written, with exit 1:

- an empty argv, or any other tool or subcommand (for example a `claude` subcommand such as `mcp`);
- `--help`, `-h`, `--version` or `-V` for the tool;
- shell syntax: a pipe, `;`, `&&`, a redirect, or one quoted shell string (q-chained: chained runs
  are v2);
- `claude --output-format` with a value other than stream-json, because the run reads stream-json.

| Exit | When |
|---|---|
| the tool's exit | The tool ran. 0 on success |
| 128+n | The tool ended on signal n. An interrupt stops a headless tool; the row ends with `end_reason=stop` |
| 130 | The herdr pane closed before the tool finished (`end_reason=pane_closed`) |
| 127 | The tool was not found |
| 126 | The tool could not start for another reason |
| 1 | A refused argv or another error. Also an interrupt of the caller while the tool runs in a pane: the run goes on in the pane and the error names the pane and the run folder |

Ledger harness values: `codex`, `codex-review`, `claude-print`, `ccx`. A `ccx` run is recorded with
`host=container` (decision C).

Settings: the `delegate` object, merged per key from the user-level wipnote config file, then
`.wipnote/config.json`, then `.wipnote/config.local.json` (gitignored). Keys and defaults:
`layout` (auto), `direction` (right; legacy, used only when layout is auto or unset), `maxPanes` (6),
`closePane` (always), `closeDelay` (3 s), `tailLines` (200), `maxReturnLines` (60), `leanProfile`
(true), `leanProfileName` (lean), `ccxRunsDir` (no default; read only from the user and local files;
`CCX_RUNS_DIR` wins), `ccxWaitSeconds` (30) and `keepDays` (7).

Planned changes to `run`:

- `--write`: Codex runs with `--sandbox workspace-write` instead of the read-only default. Planned
  (slice 12).
- `--container`: the tool runs in the project's own devcontainer. Planned (slice 21).
- `--tools`, `--plugin`, `--mcp`: widen the lean default profile by named items. Planned (slice 27).
- Inside herdr, a Claude run becomes an interactive worker (spawn, send, wait, stop) with the same
  report. Planned (slice 22). Outside herdr, `claude -p` stays (q-no-herdr).
- Inside herdr, a Codex run becomes an interactive TUI worker with the same report. Planned
  (slice 26). Outside herdr, `codex exec` stays. `codex review` and `ccx task` stay one-shot.
- A bare `codex exec` or `claude -p` in the Bash tool is denied, and the reason holds the exact
  `wipnote delegate run -- <same argv>`. A bare `ccx task` gets the `--container` form, and until
  slice 21 ships `--container`, the exact-argv form too. This is a PreToolUse hook, not a command.
  Chained commands, a command fed on stdin, `dev exec`, and anything else `delegate run` would
  refuse run as typed; a command that starts with `wipnote delegate` is never denied. Available
  (slice 9; q-intercept: deny-redirect; recogniser in `internal/delegate/detect`).

### 2.2 `wipnote delegate run-pane <run-id>` (available, hidden)

Internal. `delegate run` types it into the run's pane. It runs the run folder's `job.json` while the
Go viewer follows the log, closes the run's ledger row when it is still open, then closes its pane
per `closePane`. Exit: the tool's exit; 2 when the run folder or its `job.json` cannot be read.

### 2.3 `wipnote delegate view <run-id>` (available)

Follows the run's `clean.md` until the `exit` file appears, then prints the status and the answer.
It replaces the mod's Textual viewer (q-viewer: the Go viewer only). It writes nothing.

| Exit | When |
|---|---|
| 0 | The run ended and the answer was printed |
| 1 | Unknown run id, a read error, or an interrupt before the run ended |

### 2.4 `wipnote delegate import-orch` (available)

```
wipnote delegate import-orch [--mail-root DIR]
```

A one-shot, idempotent copy of orch's history (q-orch-import). It reads orch's registry
(`$ORCH_REGISTRY`, else orch's default registry file) and its mailboxes, archived ones included. It
writes one closed row per past orch run with `end_reason=imported`. A rerun adds nothing. Host paths
and derived labels are dropped. Event format v1 and v2 are read; any other version is refused by name
before anything is written.

| Flag | Meaning |
|---|---|
| `--mail-root DIR` | orch's mail root. Without it: `$ORCH_MAIL_ROOT`, then `$ORCH_SHARED_ROOT/.orch/mail`, then orch's known default mail roots |

| Exit | When |
|---|---|
| 0 | Done. It prints the count imported, the count already present and each skip reason |
| 1 | An error, such as an unknown event format. It names how many rows were imported before it stopped |

### 2.5 Planned commands

None of these exist yet. Flags come from the plan. Where the plan sets no exit code, the row says
"not set in the plan"; that is open, and the plan has no question id for it.

**`delegate spawn NAME --harness claude|codex`, planned (slice 10).** Starts an interactive worker
under `wipnote claude` or `wipnote codex`. It returns within 3 s with status `starting` and prints
`{run, name, pane, harness, status}`. It types one short boot line, never the brief. The brief goes by
file in the run folder.

| Flag | Meaning | Slice |
|---|---|---|
| `NAME` | `[a-z0-9-]`, 1 to 32 characters, unique among live runs | 10 |
| `--harness claude\|codex` | The worker's harness | 10 |
| `--work-item ID` | Required, or else the caller's active claim; refuses when neither exists | 10 |
| `--write` | Codex `workspace-write` instead of read-only | 12 |
| `--resume NAME` | Relaunch a stopped run's last session (`spawn NAME2 --resume NAME`) | 20 |
| `--container` | Run the worker in the project's devcontainer | 21 |
| `--tools`, `--plugin`, `--mcp` | Widen the lean profile by named items | 27 |

Refusals: a 7th live worker ("at cap", reported before anything is created), a live NAME, no work
item, a second live worker on the item (section 5). Slice 15 names a worker `<item-id>-<slug>` (32
characters at most) "unless NAME is given". Whether NAME becomes optional is open; the plan has no
question id for it. Exit codes: not set in the plan.

**`delegate boot <run-id>`, planned (slice 10), internal.** Runs in the worker's pane. It reads the
brief file and passes it to the launcher as argv. Exit codes: not set in the plan.

**`delegate stop NAME`, planned (slice 10).** Records the worker's pids first, asks the agent to exit,
waits until the pane shows a bare shell, closes the pane, then checks each recorded pid. It never
signals or kills. It prints `{pane_closed, tab_closed, survivors[]}`; `pane_closed` false always
names why (D5). The workers tab closes when its last worker stops. Exit codes: not set in the plan.

**`delegate list`, planned (slice 10).** Lists runs with readiness: `starting`, `ready`, `working`,
`question`, `idle_no_result`, `idle_shell`, `gone`. It renames and pins a late agent. Slice 25 groups
rows as Working, Waiting on you, Ready for review, Landing, Idle and Resolved. Exit codes: not set in
the plan.

**`delegate tree [--json]`, planned (slice 10).** A text tree of caller, runs, workers and sub-agents,
or JSON. Exit codes: not set in the plan.

**`delegate send NAME <task>`, planned (slice 11).** Writes the inbox file first, with the result
preamble (decision R), then sends a `herdr agent prompt` hint. A person's draft on the input line
blocks it (D8). Exit codes: not set in the plan.

**`delegate wait NAME --timeout D`, planned (slice 11).** Blocks until a durable signal. `--timeout` is
required. It prints `{run, turn, outcome}` with outcome `ready`, `done`, `asks`, `idle_no_result`,
`idle_shell`, `timeout`, `gone` or `stopped`. It polls at most twice a second. It writes no run state.

| Exit | When |
|---|---|
| 124 | The timeout passed |
| other | Not set in the plan |

**`delegate result NAME`, planned (slice 11).** Prints the newest result file's body and the task it
answers, whatever the state (D3). Exit codes: not set in the plan.

**`delegate questions NAME`, planned (slice 11).** Prints pending AskUserQuestion and
PermissionRequest calls, and outbox question files, as JSON rows. Answered ones are dropped. Exit
codes: not set in the plan.

**`delegate inbox`, planned (slice 11).** Runs in the worker. It reads the worker's own mailbox, named
by `WIPNOTE_DELEGATE_RUN`, including messages whose hint was lost. Exit codes: not set in the plan.

**`delegate doctor`, planned (slice 12).** Pass or fail per check, with the exact stderr. Slice 12:
the Codex sandbox probe. Slice 21: inside the container, Claude Code is logged in and the container
user's Claude config folder is writable. Slice 27: the effective profile, and anything it could not
turn off. Exit codes: not set in the plan.

**`delegate answer NAME <qid> <text|option>`, planned (slice 17).** Checks that the question is still
pending and that the pane and terminal_id match. Then it types the answer with `herdr pane send-text`
and `herdr pane send-keys enter`. It refuses a pane or terminal_id mismatch, a permission,
folder-trust or login dialog, a stale or unknown qid, and any call outside herdr. Until Phase B B9
passes, an option number is refused with that reason. Exit codes: not set in the plan.

Related planned commands outside `delegate`: `wipnote doctor --project-thread` (slice 23).
`wipnote plan project-instructions <plan-id>` is on main (slice 24).

## 3. Files

| Data | Where | Committed | Writer |
|---|---|---|---|
| One row per run | `.wipnote/delegations-ledger.html` | yes, canonical | `delegate run`, `run-pane`, `import-orch`; later spawn, stop, wait and list |
| Run folders, events, mailbox | `.wipnote/delegate/` | no, gitignored | the caller, the pane worker and the worker's hooks |
| Claim episodes | `.wipnote/claims/` | yes, unchanged format | each session for itself |
| Tool and prompt events | `agent_events` | as today | wipnote hooks; not copied |

No absolute host path, user name or home folder enters a ledger row. The writer refuses one.

**Ledger row** (`core/delegationledger`): `id` (`dlg-` plus at least 12 hex), `mode`
(`oneshot` or `interactive`), `harness` (`claude`, `claude-print`, `codex`, `codex-review`, `ccx`),
`name`, `label`, `work_item`, `link` (`tag` or `sid`), `caller_session`, `worker_sessions` (the
harness's own ids, oldest first), `host` (`host` or `container`), `started`, `ended`, `exit`,
`end_reason` (`exit`, `stop`, `pane_closed`, `gone`, `imported`), usage counts and `resumed_from`.

**Run folder** (`.wipnote/delegate/runs/<run-id>/`, the feat-a1df60c2 contract): `raw.log`,
`clean.log`, `clean.md`, `last.md`, `usage`, `start`, `end`, `exit` (its presence ends the run),
`label`, `host`, `run.html`, and `events-<writer>.ndjson`. A one-shot run also uses `job.json` (what
the pane worker runs) and, for ccx, `out.txt`.

**Mailbox** (`.wipnote/delegate/mail/<name>/`): `inbox/` (written by the caller), `outbox/` (written by
the worker; results are `<task-id>-result.json`, questions `<task-id>-q<n>.json`) and `ack/<msg-id>`
(touched by the worker when it starts a task). Message files are `<seq>-<msg-id>.json`.

**One writer per file.** Each message is its own file, written by temp file, fsync and rename. Each
process appends only to its own events file. There is no shared append and no lock across the bind
mount. Phase B B7 confirms that this is required (section 8).

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
the worker claims). No claim store or outcome is added. Planned (slice 16; blocked by bug-01f182e5).

- **How the worker claims.** Its brief begins with `wipnote <kind> start <id>`. The item lock, the
  collision gate and the ledger write run unchanged. `wait` and `list` report it when the episode
  did not appear.
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
  interactive row for the item has a live worker. A row whose pane is gone is closed first. One-shot
  runs do not claim and do not count.
- **Stop.** The worker's own SessionEnd closes its episode as `abandoned`; the row records
  `end_reason=stop`.
- **Crash or lost pane.** `wait`, `list` or `stop` confirms no worker process remains, then closes only
  that worker session's episodes as `expired`. `wipnote claims reconcile` stays the backstop.
- **wait timeout.** It changes no claim.
- **Orchestrator ends first.** Its SessionEnd closes only its own rows; workers keep their claims.
- **Completion.** Whoever completes the item closes every open episode on it. So stop the worker
  before completing; `feature complete` warns while a live delegation row exists.
- **After `/clear`.** The new worker session is added to `worker_sessions`, so the gate still treats
  it as family.

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

## 9. Open

- Whether `spawn` NAME becomes optional when slice 15 derives `<item-id>-<slug>`. The plan has no
  question id for it.
- Exit codes for every planned command except `wait`'s 124. The plan sets none and has no question
  id for them.
- Slice 27's notes leave four facts to verify on the real CLIs: whether `--strict-mcp-config` also
  drops plugin-bundled MCP servers; whether `codex -c mcp_servers={}` replaces the table; whether the
  key is `tools.view_image` or `features.view_image`; and which servers a trusted project's Codex
  config can still add.
