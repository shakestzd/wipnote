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
- The `wipnote-orchestra` mod is merged into delegate (section 7 of [delegate-history.md](delegate-history.md)).
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
| `delegate spawn` | available (slice 10) | Start an interactive worker in the caller's workers tab |
| `delegate boot` | available (slice 10), hidden, internal | Runs in the worker's pane: reads the brief, starts the launcher |
| `delegate stop` | available (slice 10) | Verified stop of a worker |
| `delegate list` | available (slice 10) | List runs and worker readiness |
| `delegate tree` | available (slice 10) | Caller, runs, workers and sub-agents as a tree |
| `delegate send` | available (slice 11) | Put a task in a worker's inbox, with a herdr hint |
| `delegate wait` | available (slice 11) | Block until a durable signal or the timeout |
| `delegate result` | available (slice 11) | Print the newest result file a worker wrote |
| `delegate questions` | available (slice 11) | List a worker's pending questions |
| `delegate inbox` | available (slice 11) | The worker's own read of its mailbox |
| `delegate doctor` | available (slices 12, 21) | Preflight checks (Codex sandbox; `--container`: the devcontainer; profile in slice 27) |
| `delegate answer` | planned (slice 17) | Answer a pending worker question by typing into its pane |

Errors in every available command: wipnote prints the reason on stderr and exits 1. That includes a
bad flag, a wrong argument count and a refused input.

### 2.1 `wipnote delegate run` (available)

```
wipnote delegate run [--label L] [--work-item ID] [--no-pane] [--write] [--container] -- <codex exec|codex review|claude -p|ccx task> ...
```

Runs one tool call. It opens a ledger row, creates a run folder and runs the tool in a herdr pane when
`HERDR_ENV=1`, else headless. It prints a status line, the answer (or the end of the cleaned log on a
failure) and the run page. Then it closes and enriches the row and sweeps old runs.

| Flag | Meaning |
|---|---|
| `--label L` | Pane title. It is the only label the ledger keeps (q-label: explicit only). Without it the title is `<id>: <title>` of the run's work item (cut to 48 runes), or one derived from the prompt when there is none; that title goes only to the run folder's `label` file |
| `--work-item ID` | Work item for the row. Default: the caller's active claim |
| `--no-pane` | Run headless even inside herdr |
| `--container` | Run the tool in the project's own devcontainer (section 2.7) |
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

Codex sandbox (slice 12, decision S): `codex exec` always gets exactly one `--sandbox`:
`read-only` by default, `workspace-write` with `--write`. The caller's own `--sandbox`/`-s`,
`--full-auto` or bypass flag is read as the rung it asks for and replaced by that one flag.
`danger-full-access` is passed, with a warning on stderr, only on a host listed in
`delegate.codex.dangerFullAccessHosts` in `~/.wipnote/config.json` (the key is ignored, with a
note, in the project and local files). Before the run, a bwrap probe (cached per host for ten
minutes in `.wipnote/delegate/`, bounded to 5 s) runs on Linux; a definite bwrap failure refuses the
run with the probe output, before any ledger row or run folder is written, unless the host opted in.
A probe that times out does not refuse. `codex review` runs as given. The thread id from
`thread.started` goes into the row's worker sessions, and the last `turn.completed` usage fills
input, cached, output and reasoning.

Planned changes to `run`:

- `--tools`, `--plugin`, `--mcp`: widen the lean default profile by named items. Planned (slice 27).
- Inside herdr, a Claude run becomes an interactive worker (spawn, send, wait, stop) with the same
  report. Planned (slice 22). Outside herdr, `claude -p` stays (q-no-herdr).
- Inside herdr, a Codex run becomes an interactive TUI worker with the same report. Planned
  (slice 26). Outside herdr, `codex exec` stays. `codex review` and `ccx task` stay one-shot.
- A bare `codex exec` or `claude -p` in the Bash tool is denied, and the reason holds the exact
  `wipnote delegate run -- <same argv>`. A bare `ccx task` gets the `--container` form
  (`wipnote delegate run --container -- <task words>`) only when its arguments are plain task words
  and the project has its own devcontainer (`.devcontainer/devcontainer.json` and the devcontainer
  CLI); a ccx flag such as `--resume`, or no devcontainer, gets the exact-argv form instead. This is a PreToolUse hook, not a command.
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

### 2.5 Interactive workers (available, slice 10)

```
wipnote delegate spawn [NAME] --harness claude|codex [--work-item ID] [--brief TEXT | --brief-file PATH|-]
wipnote delegate stop NAME [--wait D]
wipnote delegate list [--all] [--json]
wipnote delegate tree [--all] [--json]
```

**spawn** needs herdr (`HERDR_ENV=1`). It places the worker in the caller's workers tab (the first
worker creates the tab, later ones split it; cap 6; the caller's own tab is never split). In the
Herdr app (hc) the Core client labels each worker terminal herdr.space and herdr.tab, names the tab
(tab.rename) and then puts it away with `herdr-app do tab.settle`, best effort; hc also records the
caller as the worker's parent. The wipnote board layout (`wipnote delegate install-board`) hides settled
tabs (mods/wipnote-tabs, Herdr's tabs mod with that one change) and its board mod draws a tree from
the parent field: the delegator, a "workers" row with the count, one row per worker. A click on a
worker runs terminal.peek, since showing a settled tab brings it back; `hc read` works too.
**Limit:** Herdr's own main layout and its tabs and agents mods ignore settled, so there the
workers tab and a second Agents row per worker still show. It waits until the new pane's shell is positively a bare shell, types one
short line (`WIPNOTE_DELEGATE_RUN=<run> WIPNOTE_DELEGATE_CALLER=<sid> <wipnote> delegate boot
--project-dir <dir> <run>`) and prints `{run, name, pane, harness, status}` with status
`starting`. The brief is posted as the first inbox task and written to the run folder's
`brief.md` after a result preamble that names the task's result file (decision R); it is never
typed. Refused before anything is written: a bad NAME (`[a-z0-9-]`, 1 to 32, starting with a
letter), a harness other than claude or codex, no work item (`--work-item` or the active claim),
a NAME an open row holds while its pane lives (a gone pane's row is closed first, `end_reason=gone`),
and "at cap" (6 live workers in the tab). A shell that does not settle in 2 s is never typed
into: the row closes as gone.

**boot** (hidden) runs in the pane. It reads `session.json` and `brief.md`, removes the inherited
session env (`WIPNOTE_PARENT_SESSION`, `WIPNOTE_PARENT_EVENT`, `WIPNOTE_NESTING_DEPTH`, the
session and agent ids, `WIPNOTE_PROJECT_DIR`, and stale delegate variables), and execs
`wipnote claude --in-place --name NAME -- <brief>` or `wipnote codex --in-place -- <brief>`.

**Bind.** A worker's SessionStart, UserPromptSubmit, Stop, SubagentStart and SubagentStop hooks
append one line each to `events-<session>.ndjson` in the run folder when `WIPNOTE_DELEGATE_RUN`
names a run folder. The hook takes no lock and fails open. The next `list` moves bound sessions
(one more after each `/clear`) into the row's `worker_sessions`.

**list** prints open runs (all with `--all`). An interactive run's state comes from one
predicate (D2): `gone`, `blocked` (a folder-trust dialog, named by its labels; delegate presses no
key on it), `starting` (no session bound), `question`, `working` (a prompt event with no stop
after it), `done` (the turn ended and the newest task's result file exists), `idle_shell`,
`idle_no_result`, or `ready`. Input-line text never makes a worker working (D8). `list` also
renames the herdr agent to NAME and pins its terminal id once herdr lists an agent in the pane.

**Overview groups (slice 25).** list, tree, the orchestra band and `run.html` also name each run's
Projects Overview group (decision P). `internal/delegate/session/groups.go` holds the one table:

| Group | States |
|-------|--------|
| Working | `starting`, `working`, `idle_shell`, `running` (an open one-shot run) |
| Waiting on you | `question`, `blocked`, `idle_no_result`, `gone` (incl. a run ended `gone`), `failed` (ended with a non-zero exit) |
| Ready for review | `done` (the result file waits to be read); any idle or ended run with an open PR |
| Landing | an idle or ended run whose PR is approved or queued |
| Idle | `ready` |
| Resolved | `ended` (exit 0, `stop`, `pane_closed`, `imported`); a merged PR |

Working and Waiting on you win over a PR. Waiting on you matches the run and agent kinds of
`status --all`'s needs_you (`blocked_agent`, `failed_run`, `lost_run`). The band runs no
readiness predicate: `list` keeps each open worker's observed state in its run folder
(`state.json`), and the band and tree read it until the ledger row closes. No PR source feeds a
run yet, so the PR rows are mapped but not shown.

**stop** records the pane's foreground pids and their start times, refuses to type when the
input line may hold a draft (plain or mixed text, or a failed styled read; an all-grey
suggestion reads as empty), types `/exit` (Claude) or `/quit` (Codex), waits until process-info
positively shows the shell alone, closes the pane, then reports recorded pids still running with
the same start time. It never signals. It prints `{run, name, pane_closed, tab_closed,
survivors[], why}`; `why` always names the reason when the pane stays open. The workers tab
closes after its last worker. When a pinned worker's pane id now holds another terminal, stop types
nothing and closes nothing, and the row closes as gone. The name check and the row open run under one
project-wide lock (`.wipnote/delegate/names.lock`), so concurrent spawns of one name from any callers
yield one worker.

**Parallel worktree workers (feat-4310a889).** `spawn --worktree [--base REF]` creates a git
worktree at `<root>/.claude/worktrees/<name>` on a new branch `delegate/<name>` before placing the
worker, under `.wipnote/delegate/worktrees.lock`, and runs the worker there through `--work-dir`
and `--project-dir` (hooks and the ledger stay on the root; workspace trust is pre-seeded). A taken
name or branch refuses the spawn; any later failure removes the fresh worktree. Because each worker
has its own checkout, the one-worker-per-item rule does not apply to it, and the sibling workers of
one caller are family for the claim gate; the cap of 6 and the per-session episode close are
unchanged. `stop` removes the worktree and branch only when untouched (no changes, no untracked
files, no commits past the recorded base) and never forces; else it reports `worktree`, `branch`
and `kept`. A worker spawned from a linked worktree likewise runs in it (WorkDir = cwd).

| Exit | When |
|---|---|
| 0 | spawn, list and tree succeeded; stop closed the pane |
| 1 | a refusal or error; stop left the pane open (the JSON still prints) |

**Names by work item (slice 15).** Without NAME, spawn names the worker `<item-id>-<slug of the
item title>`, cut to 32 characters on a word boundary (`core/slug.WorkerName`). That one name is
the mailbox name, the ledger `name`, the Claude session name (`--name`) and the herdr agent name
`list` sets. A given NAME is kept as is. `wipnote claude` names a fresh launch for a work item the
same way; with no item it keeps `<project>-<timestamp>`, and `--name`, `--resume` and `--continue`
keep their names. A root Claude session that later claims an item (`<kind> start`) is renamed
`<item-id> <title>` (cut to 48) on its next prompt, once, through the UserPromptSubmit hook's
`hookSpecificOutput.sessionTitle` (code.claude.com/docs/en/hooks). The claim leaves a mark in
`.wipnote/sessions/<sid>/`; the hook reads and removes it, and renames only while the session still
holds that item. Subagents and delegate workers are never renamed by a claim.

**Codex: recorded as unsupported (checked 2026-10-28).** learn.chatgpt.com/docs/hooks documents
only `additionalContext` (and a block decision) as UserPromptSubmit and SessionStart output, with no
title or name field, and the Codex emitter's field set is pinned because an unknown field drops the
whole response. No launch flag that names a Codex session is documented; the changelog's `/title`
(codex-cli 0.164.0) is a TUI command, not a launch or hook contract. So a Codex worker is named only
in wipnote (mailbox, ledger) and in herdr (agent name); boot passes it no name flag. Revisit when a
Codex doc names a session-title contract.

`spawn --container` starts the worker in the project's devcontainer (section 2.7).

`spawn NAME2 --resume NAME` (slice 20, `delegate-resume.md`) starts NAME2 on NAME's last harness
session: `wipnote claude --in-place --resume <id>` or `wipnote codex --in-place --resume <id>`, in
NAME's recorded cwd and place, with NAME's harness and work item unless given. The new row has
`resumed_from` = NAME's run id; the old row stays closed. It refuses, before anything is written, a
live NAME, a swept run folder, no recorded session id, a gone working folder, `--harness` or
`--container` that disagree with the old run, a run already resumed, and a Codex resume with a brief.

Slice 16 adds the claim rules of section 5 of [delegate-history.md](delegate-history.md): spawn also refuses a second live worker on the same
work item (its id resolved first, after a resume's default), the brief and every sent task begin
with the worker's own claim line, and list, stop, wait and spawn close a lost worker (folded first,
like every close) with its run folder finished and its claim episodes expired.

**Worker setup (feat-d6db84fc).** A Claude worker starts lean, and the delegator chooses its setup per
task; spawn never infers it from the brief. `--profile` is one of `simple` (haiku, low), `standard`
(sonnet, medium; the default), `deep` (opus, high) or `full` (Claude's own start: no lean flags, the
user's model and effort, the orchestrator prompt). A user profile in `~/.wipnote/config.json`
(`delegate.claude.profiles.<name>`: model, effort, tools, mcp, plugins, userRules) replaces a built-in
of the same name, and `delegate.claude.defaultProfile` picks the default. `--model`, `--effort`,
`--tool T`, `--mcp NAME`, `--plugin NAME` and `--user-rules` add to or override the profile; raw flags
after `--` win. Codex refuses all but `--model` and `--effort`. Lean workers cache for 5 minutes
(`promptCacheTtl`) unless `--cache-ttl 1h` or a profile's `cacheTTL` says otherwise. Measured results
against native subagents, and their limits, are in
[delegate-token-benchmark.md](delegate-token-benchmark.md).

What lean drops, and the flags that do it (they follow the brief, as BootArgv requires; `--tools` is
one comma-joined value because the flag is variadic): no `~/.claude/CLAUDE.md`, auto memory, git
status in the prompt or plugin sync (the `--settings` overlay, with `claudeMdExcludes` JSON-encoded
from the OS home); no MCP servers (`--strict-mcp-config`); no skills or subagents and only Bash,
Read, Edit, Write (`--tools`); no Chrome (`--no-chrome`); no orchestrator or research-routing prompt
(the hidden launcher flag `--no-orchestrator-prompt`; the mods' rules stay). `--setting-sources` is
not used: `project` hangs a worker on an "Allow external CLAUDE.md file imports?" dialog.
Measured first request (cache write tokens): 75,100 default, 19,043 native subagent, 15,134 lean.

`--mcp`: a local server (`~/.claude.json` `mcpServers`, its `projects[<root>]`, or `.mcp.json`) stays
under `--strict-mcp-config` with a generated `mcp.json` in the run folder (mode 0600; only the path is
on argv). A claude.ai connector (`claude.ai NAME`) or plugin server (`plugin:x:y`) cannot load under
strict, so strict is dropped and every other server `claude mcp list` shows is denied by exact name
(`deniedMcpServers` in the settings; `allowedMcpServers` removes all connectors). ToolSearch is added
to `--tools` with any MCP server (159,656 tokens of definitions without it, 18,474 with). `--plugin`:
`enabledPlugins` only overlays, so every plugin in `~/.claude/settings.json` is set false except the
requested ones, and Skill is added. An unknown profile, server or plugin refuses the spawn and lists
the valid names. The setup is in spawn's JSON, in `session.json`, and in the ledger's `setup` column
(names only); a `--resume` that chooses nothing keeps it. Container workers get no
`claudeMdExcludes` (the container's home is not the host's).

Planned on top: `--write` (slice 12).

### 2.6 Mailbox verbs (available, slice 11)

```
wipnote delegate send NAME [TEXT | --file PATH|-]
wipnote delegate wait NAME --timeout D
wipnote delegate result NAME
wipnote delegate questions NAME
wipnote delegate inbox            # inside a worker
```

**send** refuses, writing nothing, unless the readiness predicate accepts input and the input line
holds no draft (the same `SendCheck` as slice 10; a grey suggestion reads as empty), and while the
newest inbox task is not acked yet. The whole decision (row still open, readiness, input line,
ack) and the post run under `mail/<name>/send.lock`. It writes
`inbox/<seq>-<task>.json` first, with the result preamble (decision R) that also names the question
file `outbox/<task>-q<n>.json`, then types the same text with `herdr agent prompt` as a hint and
marks the message `hinted`. A failed hint keeps the task in the inbox (`hinted: false`,
`hint_error`). Prints `{run, name, task, turn, hinted, hint_error}`. `turn` counts the worker's tasks.

**wait** needs `--timeout` and settles, in this order: a closed row (`stopped`, or `gone` for
`end_reason=gone`) at once; a lost pane (`gone`: the row closes as gone only if still open, and only
the worker sessions' own claim episodes are expired; a harness that exited in a live pane counts
the same); the current task's result file (`done`, even
with silent hooks or a turn stuck at working, D1 and D1b); a pending ask (`asks`, with the rows of
`questions`, D7); a folder-trust dialog (`blocked`, even before the first task is acked); then the
predicate's `ready`, `idle_no_result` or `idle_shell`. While the newest task is not acked, the hook
turn state belongs to an older turn and never settles the wait; the ack is read before the events.
Every herdr call is bounded by the wait's deadline, which still ends in `timeout` and exit 124.
`gone` exits 3 and `blocked` exits 4, so `wait && result` never reads a lost or held worker as
finished; every other outcome exits 0 (bug-da9bde6f).
It polls at most twice a second, never reads the input line (D8) and writes no run state; its only
writes are the two ledger ones under the ledger lock on a still-open row (a `/clear` session joins
`worker_sessions`; the worker-loss close). Prints `{run, name, turn, outcome, task, detail, asks}`.

**Run scoping.** A mailbox is keyed by worker name, so a later worker with the same name shares
it. Every message carries `run`, and send, wait, list, result, questions, inbox and the worker's ack
hook read only their own run's tasks (and the result and question files of those tasks).

**result** prints the newest result file's body and the task it answers whatever the state
(D3), or `"result": "none"`. **questions** prints the current session's pending AskUserQuestion
and PermissionRequest calls and the newest task's unanswered question files as rows `{id, kind, task,
tool, text, options}`; a question file is answered once an inbox message has `in_reply_to` set to
its id (slice 17 writes it). `delegate view` of an interactive run prints each new set of pending
asks. **inbox** reads the mailbox of the run named by `WIPNOTE_DELEGATE_RUN`; `lost: true` marks a
task that was neither hinted nor acked.

**Worker hooks** (in addition to the bind events): PreToolUse writes `tool_start`, or `question`
for AskUserQuestion with a bounded summary of the questions and options; PostToolUse and
PostToolUseFailure write `tool_end`; PermissionRequest writes `permission_request` when the prompt
reaches the person; UserPromptSubmit, after its `prompt` event, acks the newest inbox task. The
Stop event records only that the turn ended.

| Exit | When |
|---|---|
| 0 | send, result, questions and inbox succeeded; wait settled on `done`, `ready`, `asks`, `idle_no_result`, `idle_shell` or `stopped` |
| 3 | wait: `gone`, the worker is lost (pane or harness gone, or its run ended `gone`) |
| 4 | wait: `blocked`, the worker waits at a dialog in its pane (folder trust) |
| 124 | wait: the timeout passed |
| 1 | a refusal or error, including wait without `--timeout` |

### 2.7 Container workers (available, slice 21)

`delegate run --container` and `delegate spawn --container` run the worker in the project's own
devcontainer (decision C): `.devcontainer/devcontainer.json`, `devcontainer up --workspace-folder
<root>`, then `devcontainer exec --workspace-folder <root>`. wipnote never names an image, a config
override or a docker command. There is no second-account gate.

- **Refusals, before anything is written:** no `.devcontainer/devcontainer.json` or no
  `devcontainer` CLI (each names the fix), a working folder outside the project, `ccx task` under
  `--container`, and Codex unless this host is in `delegate.codex.dangerFullAccessHosts` (bwrap fails
  in containers, so slice 12's ladder refuses; opted in, `codex exec` and a Codex worker get
  `--sandbox danger-full-access` with a warning). `codex review` is refused under `--container`.
- **run:** an argv that is not `claude` or `codex` is a ccx task's arguments and runs as
  `claude -p` (harness `claude-print`). The run (pane or headless) runs `devcontainer up`, writes
  `container.json` (place, workspace folder) and `up.log`, then the tool through `devcontainer exec`
  with run-folder paths and the cwd mapped into the container's workspace folder. The output comes
  back through exec, so the logs and the answer are written on the host. The host's codex profiles
  are not used inside.
- **spawn:** returns `starting` at once with the same boot line. The boot, in the pane, runs
  `devcontainer up` (minutes the first time) and execs `devcontainer exec --workspace-folder <root>
  [--remote-env ...] wipnote claude|codex --in-place ...`, with the cwd and the brief's result path
  mapped. `session.json` records `place=container`, the root and the workspace folder.
- **Inside:** the worker carries `WIPNOTE_DELEGATE_PLACE=container`, `WIPNOTE_DELEGATE_RUN` and
  `WIPNOTE_DELEGATE_CALLER`. Its hooks write only files it alone owns in its run folder, each replaced
  by rename (`core/containerwrite`): `events-<session>.ndjson`, `session-<session>.json` (its
  sessions-ledger row and family entry), `gate-<id>.json` (gate runs, which its own completion gate
  also reads), and its own claim shard. It never writes the sessions, gate or delegations ledger,
  the family index, `.active-session` or `.wipnote/.gitignore`.
- **Fold:** every close of a container worker's row (stop, wait's or list's loss close, spawn's
  cleanup) and the end of a one-shot run first fold those files into the host's sessions ledger
  (`host=container`, the real harness, each run and its end, a resumed run's folder included), the
  family index and the gate ledger. The host records what it folded in the run folder's
  `folded.json`, so nothing is folded twice. A file the worker writes after its row's last fold (a
  late SessionEnd) is folded by the next `delegate list` or before a sweep, and a sweep keeps a run
  folder until its files are folded.
- **Claims inside:** a container worker's claim shards are its own, in `<run>/claims/`; a
  completion that would close another session's shard leaves a pending close
  (`claim-close-<id>.json`). The fold merges the shards and applies each pending close once, under
  each shard's lock.
- **Paths:** every task sent to a container worker (the brief and each `delegate send`) names its
  result and question files in the container's workspace folder; a send before the boot recorded
  that folder is refused.
- **stop:** herdr sees only the host's `devcontainer exec`. stop also records the worker's processes
  inside (found by its run id in `/proc`, through `devcontainer exec`) before it types anything, and
  after the pane closes reports any still running with the same start as a survivor with
  `where=container`. It never signals. A failed probe is named in `unchecked`.

### 2.8 Planned commands

None of these exist yet. Flags come from the plan. Where the plan sets no exit code, the row says
"not set in the plan"; that is open, and the plan has no question id for it.

**`delegate doctor [--container]`, available (slices 12, 21).** Pass, warn, fail or skip per check, with the exact
stderr. Slice 12: `codex` (on PATH, its version), `bwrap sandbox` (a fresh probe, which refreshes
the cache `run` reads; skipped off Linux), `danger-full-access opt-in` (this host's name and
whether it is listed), `sandbox ladder` (the `--sandbox` a worker gets by default and with
`--write`) and `herdr`. Exits 1 when a check fails. With `--container` (slice 21; it starts the
container): `devcontainer` (the config and the CLI), `devcontainer up`, `wipnote inside`, `hook inside`
(a `wipnote hook user-prompt` fired inside as a worker of a probe run, whose events file must reach
the host's `.wipnote/delegate/`; the probe folder is removed after), `claude config writable` (the
container user's `~/.claude`; fix: chown the named volume) and `claude login` (`claude auth status`;
fix: log in once inside with `/login`). Without `--container` it prints one skip line. Slice 27: the
effective profile, and anything it could not turn off.

**`delegate answer NAME <qid> <text|option>`, planned (slice 17).** Checks that the question is still
pending and that the pane and terminal_id match. Then it types the answer with `herdr pane send-text`
and `herdr pane send-keys enter`. It refuses a pane or terminal_id mismatch, a permission,
folder-trust or login dialog, a stale or unknown qid, and any call outside herdr. Until Phase B B9
passes, an option number is refused with that reason. Exit codes: not set in the plan.

Related planned commands outside `delegate`: `wipnote doctor --project-thread` (slice 23).
`wipnote plan project-instructions <plan-id>` is on main (slice 24).

## 3 to 8. Decisions, claims, parity, orchestra and Phase B results

Moved to [delegate-history.md](delegate-history.md): section 4 Owner decisions, 5 Claims, 6 Interactive
parity, 7 wipnote-orchestra merges into delegate, 8 Phase B results. Section numbers there are unchanged.

## 9. Open

- Whether `spawn` NAME becomes optional when slice 15 derives `<item-id>-<slug>`. The plan has no
  question id for it.
- Exit codes for every planned command. The plan sets none and has no question
  id for them.
- Slice 27's notes leave four facts to verify on the real CLIs: whether `--strict-mcp-config` also
  drops plugin-bundled MCP servers; whether `codex -c mcp_servers={}` replaces the table; whether the
  key is `tools.view_image` or `features.view_image`; and which servers a trusted project's Codex
  config can still add.
