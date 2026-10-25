<!-- Reference copy of the orch README (orch 0.1.0), scrubbed. Not wipnote documentation: nothing here is built, run or imported. See PROVENANCE.md. Relative links point into the original archive and do not resolve here. -->

# orch

## Quick start for another project

Install once. The wrapper runs orch from this checkout through uv, in your current folder:

```bash
ln -s <path>/orch/bin/orch ~/.local/bin/orch
orch --help
```

Run it on the Mac, inside a herdr pane. The registry is `~/.cache/orch/registry.json`. Mailboxes live in `~/work/.orch/mail`, created 0700 inside `~/work`, the one folder the dev container mounts, so container sessions need no setup. To move them, set `ORCH_REGISTRY`, `ORCH_MAIL_ROOT` or `ORCH_SHARED_ROOT` (the mailboxes follow it). Each command prints one JSON line and exits 0 when `ok`, 1 when refused.

```bash
orch spawn w1 --kind claude --cwd ~/work/proj   # --container: Claude Code in the dev container
orch send w1 "Add a test for parse_date and reply with its name"
orch wait w1 --timeout 600000                   # "s": ready, needs-human, asks, human, inspect, error or timeout
orch result w1                                  # "r": the worker's answer to the LAST task
orch status w1                                  # one session; `orch status` alone: every live one
orch list                                       # every session in the registry, stopped ones too
orch inspect w1                                 # current tool, oldest pending question, sub-agents, hook health
orch tree w1                                    # the sub-agent tree
orch questions                                  # everything waiting for a human, across sessions
orch watch --since 0 --timeout 60000            # block until anything changes; pass "seq" back as --since
orch stop w1                                    # the proper shutdown, below
```

**How a worker reports back.** Every task carries instructions telling the worker to write JSON files into `<mail root>/w1/outbox/`. It writes `NAME.tmp`, then renames it to `NAME.json`. Each file has a `type` (progress, question, result or error) and an `in_reply_to` naming the task. `orch result` returns the newest result or error for the last task.

**Stop properly, then verify.** `orch stop w1` asks the session to exit, waits until the pane is back at its shell, closes the pane, and checks every recorded process. `"ok":true,"st":"stopped","surv":[]` means nothing survived. A non-empty `surv` lists survivors: nothing is killed, so you decide. Run `stop` again to resume, and `orch gc` to list orphans and survivors.

**Two accounts.** The Claude in the dev container is signed in on a different subscription account from the Mac orchestrator. Only task text and result files cross between them, never credentials, tokens or environment variables.

**Known limits.**
- herdr cannot see a Claude running inside the container. Container sessions use the mailbox transport, and their state comes from files and hooks.
- Claude Code is fully supported, including the hooks behind `inspect`, `tree`, `questions` and `watch`. Codex (`--kind codex`) and Pi (`--kind pi`) start, take tasks and stop, but have no hooks: `inspect` reports `"hooks":"unsupported"`.
- In progress in the builder's current fix round (not yet recorded as done): the workers-tab layout (`ORCH_WORKSPACE`) and the Mac launcher change (`wipnote claude`).
- Nothing has run end to end on real sessions yet. `scripts/e2e_mac.sh` is that round trip, to run by hand in a herdr pane on the Mac.

## About

A small command-line helper for the orchestrating Claude session on the Mac. It starts and stops interactive coding-agent sessions in [herdr](https://herdr.dev) panes, delegates tasks, gets progress and results back, and lets messages go both ways. The sessions stay interactive TUI sessions in a visible pane, so a human can type into them too.

**Status: built and tested against fakes. The herdr command forms listed under "Verification status" were checked on a real herdr on 2026-10-23 (`REAL-HERDR-FINDINGS.md`); the orchestration as a whole has not been run end to end.** See `MANUAL-TEST.md`. The Codex review of this code and what was done about each finding is in `FIX-DONE`.

## Run

```bash
cd orch && uv run orch --help              # from this folder (the `orch` console script)
uv run --project /path/to/orch orch list   # from anywhere
```

Python 3.12+. Output is one compact JSON line per result with short keys (`n` name, `k` kind, `p` pane, `st` state, `own` owner, `d` working directory, `by` launcher, `s` wait outcome, `r` result, `surv` survivors). **Output is bounded:** lists show at most `ORCH_MAX_ROWS` (50) rows and text at most `ORCH_MAX_CHARS` (20,000) characters; when something was cut the reply says so (`"truncated":true` with `total` or `chars`). `--verbose` (before the command) adds the registry record with the full, unshortened `cwd`. Exit code 0 means `ok`, 1 means a refusal or error: `{"ok":false,"err":"<code>","why":"..."}`.

## What herdr already does and what we use

Source: [`report-reinvention.md`](../report-reinvention.md) (herdr docs v0.9.5, read 2026-10-23; nothing in it was run against a live herdr; quotes came through a summarising fetch tool, so confirm against the raw docs before a design depends on one). Verdict: **partly reinventing**. Herdr already runs the core loop (start an agent, prompt it, wait for it to settle, read output, close it). orch adds what herdr lacks.

**Where orch overlaps herdr**

| orch command | herdr equivalent | Position |
|---|---|---|
| `wait` | `agent wait --until done\|blocked\|idle`, `agent prompt --wait` (server-owned, event-driven) | Overlap. orch already calls these for agent-transport sessions; its own settle polling is what to delete. |
| `read` | `agent read --source recent-unwrapped --lines N`, `pane read --source visible\|recent\|recent-unwrapped\|detection` (and `--format ansi` for styling) | Overlap. orch reads the same way and adds only the bounded output. |
| `watch` | `events.subscribe` / `events.wait` on the socket (`pane.agent_status_changed`, `pane.exited`, `pane.agent_detected`) | Overlap, mostly. orch's `watch` today polls hook logs and `agent get`; **rebuilding it on `events.subscribe` is a later change.** Hook events stay for content, since herdr events are invalidation signals, not data. |
| `spawn`, `send`, `status`, `list`, `stop` | `agent start`, `agent prompt`, `agent get\|list`, `send-keys` + `pane close` | Partly. Herdr does the agent half; orch adds placement and the cap, human-owned and unproven-send rules, and the verified shutdown. |

**What herdr does not do, so orch keeps it**

- **Mailbox and result files.** Herdr only has screen reads (its docs suggest a temp file as the fallback).
- **Hook events**, and with them `inspect`, `tree`, `questions`. Herdr's integrations report session identity only; its state comes from screen detection.
- **Sub-agent tree** and cost estimate: herdr has no tool-level or sub-agent view.
- **Container transport.** The docs never mention containers or Docker (SSH machines only), so a container Claude is invisible to herdr.
- **`stop` with a survivor list.** Parts exist in herdr (`send-keys`, `pane close`, `pane process-info`); recording pids with start times, verifying them and reporting survivors is ours. Nothing is signalled.
- **`gc`**: our registry creates that need.
- **`takeover` / `release`.** Herdr's `agent attach --takeover` moves terminal input; it is not an orchestrator "a human owns this" flag.

**Not adopted yet** (report section 4 and 6): `completion_seq` and `agent explain --json` for unknown states, the `agent prompt` preconditions (herdr issue 5480, unreleased in 0.9.5), native session restore and `herdr integration install` (the latter writes into `~/.claude/settings.json`; orch does not run it or edit any settings), `pane report-metadata` for the sidebar. Do not invest further in grid layout: herdr has no equalize.

## Commands

| Command | What it does |
|---|---|
| `spawn NAME --kind K [--cwd DIR] [--container]` | Split a pane, start harness `K` in it, record it. `--container` runs Claude Code in the dev container (mailbox transport). Refuses beyond the cap. |
| `send NAME "text" [--queue] [--wait] [--timeout MS] [--no-result-file] [--assume-ready]` | Deliver one task. Refuses when the session is blocked, unknown, working (unless `--queue`), human-owned, human-active, in an uncertain state (below), or has an unproven earlier send. `--wait` also works for container sessions. |
| `resend NAME [--assume-ready]` | Re-type the pointer for the SAME task id when no acknowledgement came. Only after `read`, after the ack timeout, and at most 3 attempts in all. |
| `wait NAME [--timeout MS]` | Block until the session settles. Outcomes: `ready`, `needs-human` (blocked), `inspect` (unknown), `asks`, `error`, `human`, `timeout`. |
| `result NAME` | The answer to the LAST task (a result or error message that names it in `in_reply_to`). |
| `status [NAME]` | State of one session or all live ones. Same `n,k,own,p,d,by` keys as `list`, then the state fields: `{"ok":true,"n":"w1","k":"claude","own":"orchestrator","p":"w1:p2","d":"~/work/proj","by":"mac:w1:p6","st":"idle"}` |
| `read NAME [--lines N]` | Screen text (default 40, max 400). The only command that prints scrollback. Clears the "unproven send" flag. |
| `takeover NAME` / `release NAME` | Mark the session human-owned (sends refused) / hand it back. |
| `stop NAME [--even-if-human] [--assume-exited] [--assume-ready]` | Proper shutdown (below). Reports survivors; **never kills or signals anything.** |
| `list` | All registry entries. Row: `{"n":"w1","k":"claude","own":"orchestrator","p":"w1:p2","d":"~/work/proj","by":"mac:w1:p6","c":false,"st":"idle","age":120}` |
| `gc` | LIST orphaned, stale, partially started and survivor entries and unregistered mailboxes. Never changes anything. |
| `inspect NAME` | What the session does now, from its hook events: `{"v":1,"name","state","activity":{"tool","agent_id","age_s"},"question","subagents":{"running","done"},"last_event_age_s","hooks"}`. `hooks` is `active`, `silent`, `unknown` or `unsupported`. Read-only. |
| `tree NAME` | The sub-agent tree: `agents[]` of `{"id","parent","type","task","status","tools","tokens","cost_est","src"}`; `main` is the session's own agent. Read-only. |
| `questions [--all]` | Questions and permission requests waiting for a human, across live sessions (`--all`: stopped ones too), oldest first: `{"id","session","kind":"ask"\|"permission","text","options","asked_at","answerable_by":["human"]}`. Read-only. |
| `schema [MODEL]` | The JSON Schema of every public output model (or one) with `schema_version`. A test snapshots it. |
| `probe hooks [--container] [--cwd D] [--timeout MS]` | Starts a throwaway session with ONE prompt hook, sends a trivial prompt, reports `fired`, `blocked`, `timeout` (or `error`), then stops it with `stop`. Run it on the Mac and in the container before relying on hooks. |

Settings come from the environment (defaults in `models.Settings`, names in `models.ENV_NAMES`): `ORCH_MAX_SESSIONS` (6), `ORCH_MAIL_ROOT`, `ORCH_REGISTRY`, `ORCH_HERDR`, `ORCH_DEV`, `ORCH_POLL_S`, `ORCH_STOP_WAIT_MS`, `ORCH_VERIFY_MS`, `ORCH_PID_WAIT_MS`, `ORCH_STALE_HOURS`, `ORCH_WAIT_MS`, `ORCH_LOCK_TIMEOUT_S`, `ORCH_MAX_ROWS`, `ORCH_MAX_CHARS`, `ORCH_MAX_PATH`, `ORCH_SHARED_ROOT` (the folder the container also mounts; default `~/work`), `ORCH_WORKSPACE` (herdr workspace for the workers tab if the caller pane is unknown), `ORCH_LAUNCH_WAIT_MS` (30000: wait for the launched agent to appear), `ORCH_LAUNCH_CHECK_MS` (1500), `ORCH_LAUNCH_READY_MS` (20000: wait for a fresh shell prompt, and for a TUI input line, before typing; 0 = no wait), `ORCH_REPORT_AGENT`, and for observability `ORCH_HOOK_PYTHON` (the interpreter the hook runs with; default: the one running orch), `ORCH_HOOK_SCRIPT`, `ORCH_HOOK_SILENT_S` (120), `ORCH_EVENT_TAIL_BYTES` (1,000,000: only the end of an event log is read), `ORCH_TRANSCRIPTS` (overrides the adapter's transcript folder), `ORCH_TRANSCRIPT_LINES` (5000), `ORCH_PRICE_IN`, `ORCH_PRICE_OUT`, `ORCH_PRICE_CACHE` (USD per million tokens; with all three, `tree` estimates `cost_est`, else null).

`d` is the session's `cwd` with the home directory shown as `~`. When it is longer than `ORCH_MAX_PATH` (60) characters it is cut from the left to `…/` plus the last path components that fit. `by` is `host:pane` of the caller that ran `spawn` (`mac:w1:p6`, `container:w1:p6`). Both are null when unknown: an empty `cwd`, an old record without `launched_by`, or a missing host or pane (never guessed). Container sessions keep the `c` flag. `orch where NAME` and `--verbose` give the full fields.

## Where a session was launched

`spawn` records who ran it, in the same registry transaction as the session record (`launched_by`). `orch where NAME` prints the session's `cwd`, `pane`, `pids` (with host and start time) and `launched_by`. Registries written before this existed still load; `launched_by` is then null.

| Field | Meaning | Source |
|---|---|---|
| `host` | `container` or `mac` | `container` if `/.dockerenv` exists, else `mac` (a marker-file guess, not verified) |
| `pane` | The caller's herdr pane | `HERDR_PANE_ID`, only when `HERDR_ENV=1`; else null |
| `session` | The calling agent's session id | `ORCH_CALLER_SESSION` if the caller set it; else null |
| `agent` | The calling agent's kind | `ORCH_CALLER_AGENT` if the caller set it; else null |
| `cwd` | The caller's working directory | the process's current directory |
| `at` | Epoch seconds | the clock |

Best-effort: only `cwd` and `at` are always known. `session` and `agent` are never guessed; they are null unless the caller exports the two `ORCH_CALLER_*` variables. If resolving the origin fails, the spawn still succeeds and records null fields. The helper reads only the four names above (`models.CALLER_ENV_ALLOWLIST`); the reader refuses any other name, so credentials are never read. `herdr pane current` is not used.

## Hooks and observability

**Status: built and tested with fakes and with the real hook program on sample payloads. No real session has run it. The Mac account is organisation-managed and may allow managed hooks only: run `orch probe hooks` (Mac and `--container`) first.**

**Installation.** `spawn` writes `<mailbox>/hooks.settings.json` (0600) and starts the harness with `--settings <that file>` (Mac: `herdr agent start ... -- --settings FILE`; container: `... exec wipnote claude --settings FILE`). No global settings file and nothing in the container volume is changed. Each hook event has one handler, `<python> -I -S <checkout>/hooks/orch_hook.py <mailbox dir>`, with a 5 s timeout; the matcher is omitted, which the docs define as "every occurrence", so one PreToolUse handler sees every tool including `AskUserQuestion` (a second, matcher-specific handler would write the question twice). Codex and Pi declare no hooks yet; for them `inspect` says `unsupported`.

**The hook program** (`hooks/orch_hook.py`, standard library only) reads one payload on stdin and appends ONE line to `<mailbox>/events.ndjson`. It takes the mailbox lock (`<mailbox>/.lock`, the same `flock` the filelock library takes), allocates `seq` under it, and appends with `O_APPEND` and one `os.write` of the whole line; the file is 0600. Any failure (bad JSON, lock busy for 1 s, payload over 32 MB) is counted in `<mailbox>/hook-errors` (one line: count, epoch, exception class; never payload text) and the exit code is always 0, so a hook can never block or fail the session.

**Why no `uv run` here.** Everything else in orch runs through `uv run`. The hook runs on every tool call, so it is started directly by the interpreter with `-I -S` (no site packages, no environment variables) and loads only `src/orch/hookcore.py`. Measured by `tests/test_hook_program.py::test_hook_latency` (whole process, 30 runs, inside the dev container): from a copy on the local disk, median 11 to 21 ms, p95 17 to 66 ms; from the checkout on the container's bind mount, median 22 to 71 ms, p95 50 to 105 ms (six runs on 2026-10-23) (file access over the mount dominates; the hook's own work is about 0.5 ms). The target was 50 ms. On the Mac the files are local, so the local figure applies (UNVERIFIED on the Mac). The container's hook reads the checkout over the mount and appends to the mailbox on the mount, so it is slower there.

**Cross-mount evidence.** One run of `bench-mount/append_bench.py` (4 Mac plus 4 container writers, 400 lines each, ONE file on the shared mount, `O_APPEND` plus `flock` plus one `os.write` per line) gave 3200 of 3200 lines with no loss, duplicates, interleaving or reordering. That single run is the only cross-mount evidence; `MANUAL-TEST.md` 9b says how to re-run it. The hook locks the mailbox's `.lock` file rather than the events file itself; the flock mechanism is the one the bench exercised.

**The event** (`models.Event`, v=1): `seq, ts, session, kind, sid, agent_id, agent_type, tool, summary, id, text, options, origin`. Kinds (one list, `hookcore.EventKind`): session_start, prompt, tool_start, tool_end, subagent_start, subagent_stop, notification, permission_request, question, stop, error.

| Hook (docs) | Kind | What is stored |
|---|---|---|
| SessionStart | session_start | `source` |
| UserPromptSubmit | prompt | `origin` orch (text starts `[orch m-...]`) or human; a short summary |
| PreToolUse | tool_start | tool name, `tool_use_id`, short summary |
| PreToolUse on AskUserQuestion | question | question text(s), option labels (header-prefixed when there are several questions) |
| PostToolUse | tool_end | tool name, `tool_use_id`, short summary |
| SubagentStart / SubagentStop | subagent_start / subagent_stop | `agent_id`, `agent_type` (never `last_assistant_message`) |
| Notification | notification | `notification_type: message`, short |
| PermissionRequest | permission_request | tool name, short summary (the payload has no `tool_use_id`) |
| Stop | stop | nothing more (never `last_assistant_message`) |
| StopFailure | error | the error type (`rate_limit`, ...); added because Stop does not fire when a turn ends on an API error |

**Privacy: one rule** (`hookcore.short`). Free text is cut to its first line, masked (`Bearer x`, `user:pass@`, `*token*=`, `*secret*`, `*password*`, `*api_key*`, `sk-`/`ghp_`/`AKIA...` prefixes, any 32+ character opaque string become `***`) and clipped to 200 characters. A tool summary takes the first of `description`, `file_path`, `path`, `pattern`, `url` (query string dropped), `query`, `command`, `skill`, `subagent_type`; never file content, tool output, environment or message text.

**Pending questions: one rule** (`service/questions.py`). An `ask` is resolved by the tool_end with the same `tool_use_id`; a `permission` (no id in the payload) by the same agent finishing the same tool. Either is also over at a prompt, stop, error or new session start, and a sub-agent's question when that sub-agent stops. A permission that is denied stays pending until the turn ends.

**Tree.** Hook events give the nodes (`agent_id`, `agent_type`, running/done, tool counts). A parent is found through the tool call that spawned the sub-agent: the transcript meta's `toolUseId` (or a `tool_use_id` in SubagentStart, if the harness sends one: UNVERIFIED) matches a tool_start, whose `agent_id` is the parent (null: `main`). Mac sessions are enriched from `~/.claude/projects/<cwd slug>/<session id>/subagents/agent-<id>.meta.json` (`agentType`, `description`, `toolUseId`, `spawnDepth`) and `agent-<id>.jsonl` (only `message.usage` numbers, at most `ORCH_TRANSCRIPT_LINES` lines, a message split over lines counted once by `message.id`). The slug replaces every non-alphanumeric character with `-` (seen in local folder names; UNVERIFIED as a rule). Container transcripts are not visible from the Mac: container nodes say `src:"hook"` and have no tokens. Transcript files are not a public contract; the reader ignores anything it does not recognise.

**What was confirmed in the docs** (https://code.claude.com/docs/en/hooks, read 2026-10-23): the event names above; common fields `session_id`, `transcript_path`, `cwd`, `permission_mode`, `hook_event_name`, and `agent_id`/`agent_type` inside a sub-agent (also on its tool events); `prompt`; `tool_name`, `tool_input`, `tool_use_id`; PostToolUse `tool_response`; PermissionRequest without `tool_use_id`; Notification `message`, `title`, `notification_type`; SubagentStart `agent_id`, `agent_type`; SubagentStop also `agent_transcript_path`, `last_assistant_message`; Stop `last_assistant_message`; StopFailure `error`; AskUserQuestion `questions[].question/header/options[].label`; omitted matcher = all; command handler `timeout` (default 600 s); exit 2 blocks, other non-zero codes are non-blocking; `allowManagedHooksOnly` blocks user, project and plugin hooks. From https://code.claude.com/docs/en/cli-reference: `--settings` takes "Path to a settings JSON file or an inline JSON string. Values you set here override the same keys in your `settings.json` files for this session." **Risk, unverified:** read literally, our `hooks` key could REPLACE the session's own `hooks` (for example the hooks `wipnote claude` relies on) instead of adding to them. The manual test must check that the worker's own hooks still run. **Assumed, not confirmed:** the SessionStart example JSON (cut off in the fetch; fields from its table); whether SubagentStart carries a `tool_use_id`; whether `agent_id` has an `agent-` prefix (the docs' two examples differ, so ids are compared without it); that `--settings FILE` adds hooks on top of the user's own hooks rather than replacing them (see the risk above).

## Analysing history

The event logs are plain NDJSON and the only source of truth; nothing writes a database. For notebook analysis, DuckDB can read them in place (optional extra: `uv sync --extra analytics`; no orch command needs it):

```sql
create view events as
select * from read_ndjson('<mailbox root>/*/events.ndjson', union_by_name=true, ignore_errors=true);
```

`ignore_errors=true` matters: readers take no lock, so a reader can see a partial last line while a hook is appending, and that line must be skipped, not fail the query. `union_by_name` lines up the optional fields that only some kinds carry. Nothing writes a DuckDB file; the view reads the logs each time. The extra is pinned `duckdb>=1.4,<2` on purpose: DuckDB 2.0 database files cannot be read by 1.x, so notebooks wait for 2.0.1 (due about 2026-11-18). Revisit the bound on 2026-11-18.

## orch watch

`orch watch [--since SEQ] [--timeout MS] [--only NAME ...]` is one blocking call. It returns when anything changes across the registered sessions, or when the timeout passes. The timeout defaults to `ORCH_WAIT_MS`, and `--timeout 0` checks once. It prints one line:

```json
{"ok":true,"v":1,"seq":42,"events":[{"seq":41,"ts":1761226000.1,"session":"w1","kind":"question","src":"hook","ev":17,"question":{"id":"q1","kind":"ask","text":"Ship it?","options":["yes","no"],...}},{"seq":42,"ts":1761226003,"session":"w1","kind":"state","src":"herdr","before":"working","after":"blocked"}],"more":false}
```

- **The cursor.** Pass the `seq` you got back as `--since` next time, and nothing is lost or repeated between calls. Without `--since`, the cursor starts at the current head, so the call returns only changes that happen from now on. When nothing changes before the timeout, `events` is `[]` and `seq` is the same cursor. `--only` limits the output to some sessions; the cursor still moves past the other sessions' entries. Output is capped at `ORCH_MAX_ROWS` entries; `"more":true` means call again at once.
- **Entry kinds.**

  | `kind` | `src` | Meaning |
  |---|---|---|
  | `activity` | hook | A turn began, or a tool started. Consecutive tool starts from one session in one read fold into one entry: the latest tool, with `n` saying how many. |
  | `question` | hook | A question or permission request was asked. `question` has its text and options, in the same shape as `questions`. |
  | `subagent_start`, `subagent_stop` | hook | A sub-agent started or stopped. |
  | `stop`, `error` | hook | The turn ended, normally or on an API error. |
  | `state` | herdr | A Mac session's herdr state changed, `before` -> `after`. The first reading counts as a change. A vanished or replaced agent reads as `error:<code>`. |
  | `stalled` | watch | The session is working, but its hook log has been silent for `silent_s` > `ORCH_HOOK_SILENT_S` seconds. The hooks may be blocked, or the session hung. Reported once per silence. |

  Tool ends and notifications are not entries: a tool end follows its start, and a permission notification repeats the permission request. "Working" is herdr's state for a Mac session. For a container session, which herdr cannot see, it is the hook log's own view: a turn is running.
- **How it works.** Each pass takes the feed's lock and does three things. It reads every registered session's `events.ndjson` forward from a saved byte offset, read-only and without the hook's lock; a partial last line waits for its newline, and a file that shrank was replaced, so it is read from its start. It asks herdr for each live agent-transport session's state, at most every 2 s (`agent get`, one call per session). And it checks for stalls. It appends the new entries to `changes.ndjson`, next to the registry, with one global rising `seq`, and saves the offsets and last states in `changes.state.json`. Both files are 0600. Several watchers can run at once: the lock makes the seq allocation single-file. Entries are appended before the state is saved, so a crash in between repeats some entries under new seqs, never loses them. The seq is never reused, because the head is also read from the feed's last line.
- **Not yet.** The feed file is never compacted: each pass reads it whole. It is also not covered by `orch gc`.

## Layers

Each layer is one module (or package). A layer may import only the layers listed. `tests/test_layers.py` enforces this by parsing imports.

```
cli  ───────────────────────►  everything below
service  ───────────────────►  models, errors, ports, adapters
stores  ────────────────────►  models, errors, hookcore
herdr_client  ──────────────►  models, errors
probes  ────────────────────►  models
adapters  ──────────────────►  models, errors
ports  ─────────────────────►  models
models  ────────────────────►  hookcore (pydantic and the standard library otherwise)
hookcore, clock, errors        (hookcore: standard library only; the hook program loads it alone)
```

| Layer | Files | Job |
|---|---|---|
| errors | `errors.py` | `Refuse`, `HerdrError`, `SeqError` |
| hookcore | `hookcore.py` | The hook protocol, standard library only: event kinds, the hook-name map, the redaction and summary rule, the locked append. Imported by `hooks/orch_hook.py` |
| models | `models/` | Every data shape and rule constant, once: states, owners, message types, settings, `AdapterSpec`, `Message`, registry records, all herdr reply shapes; `models/events.py`: `Event`, `Inspection`, `AgentNode`, `Question`, `TranscriptAgent` |
| ports | `ports.py` | `typing.Protocol`s: `HerdrClient`, `ProcessProbe`, `Clock`, `MailboxStore`, `RegistryStore`, `EnvReader`, `EventStore`, `TranscriptReader` |
| herdr_client | `herdr_client.py` | The only place that runs the `herdr` CLI. Returns pydantic models. Every UNVERIFIED form lives here and is marked (a test enforces it) |
| probes | `probes.py` | Read-only `ps` and `dev exec` behind `ProcessProbe`. **No way to signal a process** |
| stores | `stores/` | File I/O and locking only: registry and per-session locks (filelock), mailbox (locked, atomic, private, contained), event log reader and hook settings writer (`events.py`), transcript reader (`transcripts.py`, read-only) |
| adapters | `adapters/` | One small file per harness; declarative data only |
| service | `service/` | All policy, as logic over the ports: cap, send guards, occupant pinning, takeover, graceful stop, verification, gc, wait mapping, output bounds. No I/O of its own |
| cli | `cli.py`, `__main__.py` | Parse arguments, build the context, call the service, print JSON |

## Adapter contract and adding a harness

The six operations are `spawn`, `send`, `wait`, `result`, `status`, `stop`. The behaviour is shared in `service/transports.py` (two transports) and `service/shutdown.py`; an adapter supplies only what differs, as an `AdapterSpec`:

| Field | Meaning |
|---|---|
| `kind`, `container` | Name and whether it runs in the dev container |
| `transport` | `agent` (herdr detects the harness: use the agent surface) or `mailbox` (herdr cannot: files plus one typed pointer line) |
| `herdr_kind`, `start_args` | What `herdr agent start --kind ... -- ...` needs |
| `quit_plan` | How to ask the session to exit, as steps `("text", "/exit")` or `("key", "ctrl+c")` |
| `launch_cmd`, `launch_inner` | Mailbox transport: the line typed into the pane (`{dir}`, `{script}`), and the command exec'd inside the container |

**To add a harness, add one file** `src/orch/adapters/<name>.py` with a module-level `SPEC = AdapterSpec(...)`. Discovery is automatic. No other file changes. Harness names may appear only in `adapters/` (a test enforces it). A harness that herdr cannot detect and that needs behaviour the two transports do not offer would need a third transport in `service/transports.py`.

## Safety rules

- **Nothing is ever killed or signalled.** A pid can be reused, so even a recorded pid is unsafe to kill. `stop` reports survivors (pid, name, where) and leaves the decision to the orchestrator. The `--term` option was removed, and a test proves no source file can signal a process.
- **Agent names are pinned.** Every agent operation (send, wait, read, stop) first checks that herdr reports the agent in the pane (and terminal) recorded at spawn. A name that now points elsewhere is refused with `occupant_mismatch`; if the lookup fails nothing is typed and there is no fallback to raw input.
- **Uncertain state means no input.** For a container session herdr cannot see the TUI, so its state is a guess unless a hook proves dialogs and human prompts are visible (a hook-written outbox message with `origin: "hook"`). Without that, `send`, `resend` and `stop` refuse (`uncertain`) unless you pass `--assume-ready`, which is your responsibility. A reported dialog (`blocked`) is never typed into, even with `--assume-ready`.
- **Missing information is not permission.** If `pane process-info` omits the process details, the session is NOT treated as being at the shell: `stop` refuses (`cannot_verify`) and closes only with `--assume-exited`. A pane query that fails for any reason other than an explicit "not found" is an error, never "gone".
- **No blind resend.** The delivery is recorded as pending BEFORE any input. An ambiguous failure (timeout, unknown error) marks the session `needs_read` and refuses further input until `read`. A definite failure rolls the record back.
- **Operations are serialised per session** (`send`, `resend`, `stop`, `takeover`, `release`, `spawn` hold a lock file); a second one fails fast with `busy`. `send --wait` holds the lock until herdr returns.
- **Spawn is journalled.** The registry record exists (state `spawning`) before the pane is split and gets the pane id the moment the split returns. If that write fails, the new pane is closed. A record stuck in `spawning` is listed by `gc` as a partial start, and `stop` cleans it up.
- **Private and contained files.** Files are created 0600 and folders 0700, through unique exclusive temp files. Session names and message ids are validated before they become paths; a symlink anywhere in a mailbox path is refused, and symlinked or misnamed message files are ignored.
- Only task text and result files cross between sessions. Never credentials, tokens or environment variables. No network access. No Docker socket. No `docker` command: the container is reached only through `dev exec`.
- The helper closes only panes it created and recorded. After a timeout or stall, `read` the agent before any resend. `read` and the pane contents can show secrets. A corrupt registry file is refused and left untouched, never reset.

## Message schema (mailbox)

One JSON file per message, `<root>/<name>/{inbox,outbox,ack}/<id>.json`. Orchestrator to worker goes in `inbox`; worker (or its hook) to orchestrator goes in `outbox`; `ack/<id>` is touched by the worker when it starts a task.

| Field | Meaning |
|---|---|
| `v` | Schema version, 1 |
| `id` | Unique id, `m-<token>`; letters, digits, `.`, `_`, `-` only, at most 64, not starting with a dot. Writes are idempotent by id: a repeated id returns the stored message |
| `from`, `to` | Sender and recipient names (`orchestrator` or the session name) |
| `type` | `task`, `progress`, `question`, `result`, `error`, `blocked`, `prompt`, `note` |
| `seq` | Per-sender sequence number, allocated and checked under the mailbox lock; must rise |
| `ts` | UTC timestamp |
| `body` | Text. Task text and results only, never credentials |
| `in_reply_to` | The task `id` this answers. `result`, `error` and `question` only count if they name the current task |
| `ack` | True when an acknowledgement is requested |
| `deadline_s`, `ack_timeout_s` | Timeouts (seconds) |
| `origin` | `hook` (written by a hook: proves state is visible), `human` (a prompt a human typed), or absent |

Writes go to a unique exclusive temp file and are renamed into place under the mailbox lock, so a reader never sees a half-written message and two writers never share a temp file. Deviation from `report2.md`: the fields are named `type` and `in_reply_to` (as in brief3), not `kind` and `reply_to`; ids no longer contain the seq.

**Replies are matched to the task, not to time.** The watermark (highest outbox seq) is stored before the task is delivered, so an instant reply is not skipped. A reused session name starts with an empty mailbox: the old one is moved to `<root>/.archive/`.

## Container sessions

**Launch line.** `dev exec -d <dir> sh -c '<script>'`, where the script ends in `exec wipnote claude -- --settings <file>`. wipnote's launcher takes Claude's own flags only AFTER `--`; without it the launch fails with `unknown flag: --settings` (seen in a real probe).

**Shared mount.** The container mounts `~/work` only (`ORCH_SHARED_ROOT`, default `~/work`). A container `spawn` or `probe` refuses with `mailbox_not_shared`, before opening any pane, when the mailbox (and so `hooks.settings.json` and `launcher.pid`) is outside it. The default mail root is `~/work/adhoc/orch-mail` and satisfies this; set `ORCH_MAIL_ROOT` under the shared root, or `ORCH_SHARED_ROOT` to the real shared folder.

**Failed launch.** If the pane is back at the shell prompt a moment after the launch line (3 polls within `ORCH_LAUNCH_CHECK_MS`, 1500; 0 turns the watch off), `spawn` refuses with `launch_failed` and the pane's last output line, and closes the pane (and the workers tab, if it was the only session). UNVERIFIED against a real herdr.

The script writes `"$$ <run id>"` to `<root>/<name>/launcher.pid` and then `exec`s the harness, so the pid file holds the harness launcher's own pid. **That is how ownership is established** (not by matching names, and not by claiming every matching process): the helper records the launcher and its children (`pgrep -P`), each with its start time, only if the run id in the file is this run's. If no pid file appears, `stop` still works but reports `unverified`: nothing inside the container was checked.

For each task the helper prepends a short preamble (what to read, where to write replies, never write credentials) and types one pointer line, `[orch m-3f9a1c2e] read <inbox file> and follow it (skip it if <ack file> exists)`. The worker skips a task whose ack file exists, which makes a re-sent pointer harmless.

**Hook contract.** Without hooks the helper cannot see permission dialogs or a human typing in the container. The send guards still use the older contract: an outbox message with `origin: "hook"` makes the state `confirmed`, and `type=prompt` with `origin: "human"` means a human typed. The step-1 hooks (above, "Hooks and observability") write `events.ndjson` instead and feed only the read-only commands; they do NOT yet confirm state or detect human prompts for the send guards. Wiring `prompt` events with `origin: human` into those guards is a follow-up.

## Proper shutdown (`stop`)

Closing a pane can leave processes behind (a `docker exec` child, a launcher). So `stop`:

1. records the pids with their start times (Mac foreground processes from `pane process-info`; inside the container, the launcher pid file and its children);
2. asks the session to exit: through the agent surface where herdr detects the agent (after pinning the occupant), else `pane send-text` / `pane send-keys` (only if the state is confirmed). It never types into a blocked agent, an unknown one, or a different occupant;
3. waits until the pane's foreground is POSITIVELY the shell again, with a timeout (`ORCH_STOP_WAIT_MS`). Missing process details are "unknown", never "at the shell";
4. closes the pane **only after** step 3. If the session did not return to the shell, the pane stays open and the state is `stopping`;
5. verifies every recorded pid: gone, the same process (same start time: a survivor), or `uncertain` (no start time was recorded). A reused pid has a different start time and is not called a survivor. Survivors are reported and **nothing is killed**.

`stop` is idempotent: run it again to resume or re-verify. `gc` lists survivors. `stop` refuses a human-owned session unless `--even-if-human`. The result has `unverified` entries when nothing could be checked (no pids recorded), and then `ok` is false even though the pane is closed.

## Verification status

Checked on a real herdr (REAL-HERDR-FINDINGS.md, 2026-10-23, scratch pane): `pane process-info --pane <id>` (the pane is an OPTION; the positional form fails with "unknown option") and its reply shape (idle shell = the shell alone; a running command = only that command, the shell is not listed; a short-lived helper such as `path_helper` can appear next to the shell during startup); positional `pane send-text`, `send-keys` (`enter`, `ctrl+c`), `run`, `rename`, `get`, `close`; `pane split --current --direction D --cwd D --no-focus`; `agent start` (with `--` args), `agent prompt --wait`, `agent send-keys ctrl+c` twice (Codex exited and the pane returned to the shell), `agent read` (prints plain text); and that a Claude inside `docker exec` is NOT detected.

**Still unverified:** a positional pane id as the split target; the error code for "no such pane" (guessed `not_found`); `pane read` (assumed to print text like `agent read`); `agent wait`; the effect of `pane report-agent` (accepted silently, not checked against `agent list`/`agent wait`); the stderr error JSON shape; Claude `/exit` as the quit and the Pi quit keys; `agent start --timeout`; and everything container-side (`dev exec`, the launcher pid file, `ps -o lstart=`, `pgrep -P`, 0700 folders on the mount, `pane run` of a pointer line into a TUI, the hooks).

**Foreground rule from the real data.** `ProcessInfo.kind()` classifies a pane as `shell` (only the shell), `command` (the shell is not listed: a command owns the foreground), `settling` (the shell plus something else, for example a startup helper) or `unknown` (details omitted). `stop` waits and retries on `settling`, never types into it, and never records or reports such a helper: it has no recorded ownership.

## Dependencies

Checked against PyPI on 2026-10-23. The three runtime libraries are already used by other projects here (one `<project>` uses `pydantic` and `typer`; another uses `pydantic`).

| Library | Version seen (released) | Replaces | Why it fits |
|---|---|---|---|
| `pydantic` >=2.13 | 2.14.1 (2026-10-11) | Hand-written dict access and validation for the mailbox message, registry record and every herdr reply | Parsing and validation live in models. Actively released, very widely used |
| `filelock` >=3.20 | 4.2.4 (2026-10-19) | A hand-written `fcntl.flock` wrapper | Cross-platform, maintained. Used for the registry, the per-session operation locks and the per-mailbox lock |
| `typer` >=0.27 | 0.27.3 (2026-10-06) | Hand-written `argparse` wiring with 12 subcommands | Type-annotated options, generated `--help`. Chosen over bare `click` because the signature is the schema; typer sits on click |
| `pytest` >=9.1 (dev) | 9.1.1 (2026-06-19) | `unittest` boilerplate | Fixtures and parametrisation |
| `pytest-cov` >=7.1 (dev) | 7.1.0 (2026-03-21) | Manual coverage runs | Coverage for the service layer |
| `hypothesis` >=6.150 (dev) | 6.170.8 (2026-10-22) | Hand-picked examples for the mailbox invariants | Idempotency, rising seq and ack/retry are properties over arbitrary sequences; it shrinks a failure to a minimal case. One test file |
| `duckdb` >=1.4,<2 (optional extra `analytics`) | 1.5.5 in `uv.lock` (not installed by default) | Hand-written log parsing in notebooks | Read-only SQL over `events.ndjson`. Not imported by any orch code (a test checks). `<2` until 2.0.1, see "Analysing history" |
| `jsonschema` >=4.26 (dev) | 4.26.0 (2026-01-07) | Hand-written "does this look like a herdr reply" checks | **Added after the review (finding 13).** Canned reply fixtures are validated against the REAL `api-schema.json` (a copy lives in `tests/data/`), so a fixture cannot drift from the documented shape. A lightweight shape check would only restate what our own models parse. Dev-only, mature, maintained by the JSON Schema org |
| `ruff` >=0.17 (dev) | 0.17.2 (2026-10-22) | flake8, isort, black | Lint and format, configured in `pyproject.toml` |
| `ty` >=0.0.91 (dev) | 0.0.91 (2026-10-22) | mypy | Type check. Pre-1.0, so expect churn |

Not added: `pydantic-settings` (a small settings model with `from_env` is enough), any process library (`subprocess` is enough behind two small adapters). `rich` arrives with typer and is used only for `--help`.

The original brief asked for a standard-library-only script. That was replaced by the later quality brief (pydantic, filelock, typer, tests, ruff, ty), so the review's finding 14 about it is marked DISPUTED in `FIX-DONE`.

## Develop

```bash
uv sync
PYTHONDONTWRITEBYTECODE=1 uv run pytest -q -p no:cacheprovider --cov=orch.service --cov-report=term-missing
uv run ruff check && uv run ruff format --check && uv run ty check
```

Test names that start `test_p1_<n>` / `test_p2_<n>` / `test_p3_<n>` are the regression tests for finding `<n>` of the Codex review.

## Readiness and slow launches

- **Fresh shell first.** `spawn` types the launch line only after the new pane shows ONLY its shell, a prompt, and a steady screen (`ORCH_LAUNCH_READY_MS`); otherwise `shell_not_ready`, nothing typed, pane closed. A line typed into a starting shell is shown but not run, and runs seconds later.
- **Slow is not failed.** The launch is `launch_failed` only when the shell is positively back with a prompt and nothing pending. If a program runs (or text still waits in the shell) at the deadline, the wait is extended once, then `launch_slow`: the record stays `starting` with its pane, the pane's pids are recorded, and the first `status`/`stop` that finds the agent names it after the session.
- **First task.** The first `send` to a Claude TUI waits until its `> ` input line is drawn and the screen is steady (`not_ready` otherwise, nothing typed, no task file). `stop` first checks the input line is EMPTY (a greyed `Try "..."` hint counts as empty); text there is cleared with `ctrl+u` (UNVERIFIED), else `cannot_clear_input` and the pane stays open. The screen patterns are heuristics (UNVERIFIED).
- **Container hooks.** The interpreter is looked up INSIDE the container at spawn (`command -v python3 || uv python find`, an absolute path, UNVERIFIED) because a non-login `sh` there has no `python3`; none found: `no_python_in_container`, nothing opened.

## Hooks proven, stale tabs, background servers

- **First send and `stop` to a container session.** Its state is a guess until a hook MESSAGE proves dialogs are visible. It is also accepted as safe when all three hold: its hooks fired (an event is logged for the session), the foreground is the harness (not a shell), and the screen shows the TUI input line EMPTY (a dialog replaces that line). `--assume-ready` stays for everything else. The screen shapes are UNVERIFIED.
- **Stale workers tab.** A recorded pane that herdr no longer has is not a split target; if none is left, `spawn` makes a new tab. A session that ended with `survivors` has no pane, so it no longer keeps the tab record alive (`pane_not_found` and `tab_not_found` count as "gone").
- **Expected background servers.** An adapter can declare servers its launcher leaves running by design (exact program name plus a marker argument; the container Claude adapter declares `wipnote ... _serve-child`). They appear as `daemons` in the `stop` result and never as `surv`; they never fail `stop`, hold the pane, or get signalled. `gc` adds a read-only `daemons` count per project. Nothing is killed.
- **Benchmarks.** `tests/test_hook_program.py::test_hook_latency` is marked `benchmark` and skipped by default (it fails whenever the machine is loaded); run it with `uv run pytest -m benchmark`.

## The Go hook, dialogs, `probe ready`, the bare launcher (round 4)

- **Go hook binary.** `scripts/install-hook-binaries.sh` copies `orch-hook-go/dist/orch-hook-<os>-<arch>` into `bin/` and records `orch-hook.VERSION` and `orch-hook.SHA256SUMS` next to them (no hand copies; it refuses a build whose `hook_spec.json` event version this orch cannot read). At spawn, `hooks.plan_for` trusts a binary only if it is present, executable, listed with a matching checksum, and `--version` reports the recorded version (a container session is checked inside the container; a Mac session where orch runs). Container sessions use `linux-<arch>`, Mac sessions `darwin-<arch>`. Otherwise the Python hook runs and `spawn`, `status` (once) and `probe hooks` say why (`hook`, `hook_note`). `ORCH_HOOK_COMMAND` (a shell template with `{dir}`) replaces the whole command. One definition: `hooks.hook_command`. The Python hook carries the contract-v2 patch (`orch-hook-go/PYTHON-HOOK-PATCH.diff`); `tests/test_go_hook.py` runs the Go worker's `check_parity.py`, byte parity of both programs on every documented payload (and every eighth edge case; all with `ORCH_GO_FULL=1`) and a 4 + 4 writer interop run.
- **Named dialogs.** `send` and `stop` refuse with `uncertain` and say WHICH dialog is on screen (folder trust, permission, login) and that a person must answer it. orch never answers one, not even with `--assume-ready`. The screen texts are adapter data and UNVERIFIED.
- **`orch probe ready NAME`.** Read-only. Prints each test of the readiness rule (hook message, hook events logged, program in front, input line empty, no dialog), what was seen, `send_allowed` and, if not, why and how to override.
- **Bare launcher (opt-in).** `ORCH_CONTAINER_LAUNCHER=none` starts the container harness directly, without its wrapper launcher (the wrapper leaves a per-project server behind), where the adapter allows it (`bare_inner`). Default `wrapper`: unchanged.

## Round 5: grey suggestions, task preamble, warning wording

- **Grey "next prompt" suggestion.** The TUI shows a suggestion in the input line; plain text cannot tell it from a draft. `readiness.grey_suggestion` re-reads the pane with `herdr pane read --format ansi` (the herdr skill: "Use `--format ansi` when colors and terminal styling are evidence") and calls the line a suggestion ONLY when every visible character after the prompt is dim (SGR 2) or grey (`90`, 256-colour 238-250, or equal RGB in 70-199), apart from at most one reverse-video cursor block. Anything else, an unstyled read, or a failed read stays `text`: a person's unsent draft still blocks `send` and `stop`. UNVERIFIED on a real herdr: that its ansi output carries those codes. If it does not, nothing changes, and `probe ready` says "the text may be a grey suggestion: run `orch read NAME`".
- **`orch stop NAME --clear-input`** sends ctrl+u first (Claude Code docs, interactive mode: "Ctrl+U: delete from cursor to line start") and then runs the normal checks. It deletes a person's draft too, so it is opt-in. UNVERIFIED: that ctrl+u also removes a suggestion.
- **Task preamble.** Every task file starts "Use absolute paths. Do not run a bare `cd`. You are a worker: do the work yourself and start no sub-agents." (`transports.WORKER_RULES`).
- **`send` warning.** "readiness unconfirmed" is printed only when the send went out on `--assume-ready`, not when the state was proven.

## Headless runs

`orch run NAME --kind K [--via local|ccx] (--task TEXT | --task-file FILE) [--cwd DIR] [--model M] [--write] [--allow TOOLS] [--allow-unsafe PATTERN]... [--max-turns N] [--approve-ccx]` starts a one-shot command in a workers-tab pane. The record has `mode: headless`; `status`, `wait`, `result`, `read`, `stop`, `list`, `where`, `gc` work on it through the same service functions (its transport is `HeadlessTransport`). `send` and `takeover` are refused (`headless_run`): a run is the orchestrator's alone. `orch usage [NAME]` sums token numbers and the harness's cost ESTIMATE over runs.

**Interactive or headless?** Use headless for bounded, well-specified work whose permissions can be listed in advance (reviews, research, test runs, one-shot implementation in a worktree): it starts without the project's full skill and plugin load, costs less to monitor, and the log already carries tool calls, sub-agents, usage and the result. Use an interactive session when a person may want to steer it or type into it.

| Executor | Command | Notes |
|---|---|---|
| `--kind claude --via local` | `claude -p --output-format stream-json --verbose --permission-mode default (acceptEdits with --write) [--model M] [--max-turns N] --allowedTools ...` | task on stdin; log in the run folder |
| `--kind codex --via local` | `codex exec --json --sandbox read-only (workspace-write with --write) [--model M] -C DIR --output-last-message FILE` | task on stdin; no allowlist (`--allow` refused) |
| `--kind claude --via ccx` | `ccx task "$(cat task.txt)"` with `CCX_DEVCONTAINER` and `CCX_ALLOWED_TOOLS` | **second, personal account**; log is the newest `ccx_runs/*.jsonl` |

**Account warning.** `--via ccx` sends the work to a second, personal account. It is refused (`account_not_approved`, with the reason) unless `ORCH_ALLOW_CCX=1` or `--approve-ccx` is on that call. Default off. Nothing from the caller's environment, credentials or `~/.claude` is forwarded: only the task text (a file) and the allowlist cross, and only the exit code, log and answer come back. A bypass-permissions flag is never passed. See "Read-only by default" for the allowlist.

**Read-only by default.** A run may use `Read`, `Grep`, `Glob`, `Bash(ls *)`, `Bash(cat *)`, `Bash(git status*)`, `Bash(git diff*)` and `Bash(git log*)`, with permission mode `default` (nothing is auto-accepted). Without `--write`, `--allow` may only narrow that list (`allow_wider`). `--write` switches to `acceptEdits` and adds exactly `Edit` and `Write`; only with `--write` may `--allow` ADD named tools. Refused (`unsafe_allow`, naming the pattern): `Bash`, `Bash(*)`, and any `Bash` pattern that starts a shell, interpreter or package runner (`uv`, `uvx`, `npm`, `npx`, `node`, `python*`, `sh`, `bash`, `curl`, `ssh`, and a few more such as `env`, `xargs`, `sudo`), unless `--allow-unsafe PATTERN` names that exact pattern (one flag per pattern, which must also be in `--allow`); the accepted patterns, `--write` and the model are stored on the run's record. `codex` has no allowlist: `--write` selects its `workspace-write` sandbox. **Model.** `--model M`, else `ORCH_RUN_MODEL`, else the executor's default (`sonnet` for Claude, the harness's own for Codex; the `ccx` executor cannot be told and refuses `--model`). `ORCH_RUN_MODEL` is passed to every executor that takes a model, so set `--model` per call when you mix harnesses. The usage line and `orch usage` name the model (from the log, else the one asked for).

**Files** (`<mailbox>/<name>/run/`, 0700): `task.txt` (0600), `out.log`, `err.log`, `answer.md`, and `exit` (written by the pane's command, temp name then renamed). The task text never enters the shell line. States: starting, running (`working`), done, failed, stopped. `result` returns the run log's final `result` line as `r` (capped at `ORCH_MAX_CHARS` code points); `r` is null, with a `hint`, only when the log has none: the run is still going, it exited 0 but the log is empty or cut short, or it failed (the hint then carries the exit code and the last error line). `wait` says `ready` only for an exit code 0 and never rewrites the run's `done` state. A run whose pane vanished before `exit` appeared is `failed`; a failed run's `result` reports the last error line and the permission denials, not scrollback. Cancel with `orch stop NAME` (one ctrl+c, wait for the shell, verify, close); nothing is signalled. Settings: `ORCH_ALLOW_CCX`, `ORCH_RUN_MODEL`, `ORCH_CCX` (binary), `ORCH_CCX_RUNS` (its log folder), `ORCH_RUN_LINES` (log lines parsed).

Not done yet: `inspect` and `tree` on a run still say hooks are unsupported (the parsed events are used by `result` for tool and sub-agent counts); `usage` does not yet include interactive Mac sessions' transcript usage.
