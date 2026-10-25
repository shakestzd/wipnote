# orch inventory: the port checklist for `wipnote delegate`

plan-28b4a7f6 slice 1 (feat-0031fd91). Source of truth: the Python orch at `herdr-research/orch/` in the
owner's archive (version 0.1.0, no git metadata; see `docs/reference/orch/PROVENANCE.md` for file hashes). The
source stays outside this repository (q-orch-source); only its README is here, at `docs/reference/orch/README.md`.
Paths below are relative to the archive root; `orch/...` is short for `herdr-research/orch/...`. Line numbers are
of the hashed copy. Nothing from orch is copied as code; file, function and line names are pointers only.

Rows follow the plan as finalized on 2026-10-25, including owner decisions R (the result comes from a file the
worker writes, slice 11), C (ccx means container Claude; `--container` runs the worker in the project's own
devcontainer, no second-account gate, slices 8, 9, 21) and the answer path (typed into the worker's herdr pane,
slice 17). Section 0 is the short list of what slices 5, 10 and 11 copy.

Where the TypeScript port (`herdr-research/orch-ts/`) or the Go hook (`herdr-research/orch-hook-go/`) differ, the
"Variants" section says so. orch-ts ports an older Python snapshot (2026-10-23, kept at
`orch-ts/reference/python-orch-snapshot-20261023/`): 18 commands, no `run`, `usage`, `watch` or `probe ready`, no
workers tab, no dialog names, no readiness module.

## 0. Behaviours the port copies (slices 5, 10, 11)

| Behaviour | orch file:function (line) | Slice | Kept, changed or dropped |
|---|---|---|---|
| Mailbox folders inbox, outbox, ack; one JSON file per message | `orch/src/orch/stores/mailbox.py` `FileMailboxStore` (28), `dir` (56), `ensure` (61) | 5 | kept: `<root>/mail/<name>/{inbox,outbox,ack}/` |
| Message model and id rule | `orch/src/orch/models/__init__.py` `Message` (208), `ID_PATTERN` (72); `orch/src/orch/service/mail.py` `new_id` (11), `post` (16) | 5 | changed: ids carry 12 or more hex (P-6) |
| Atomic private write (temp file, fsync, chmod, rename) | `orch/src/orch/stores/atomic.py` `private_mkdir` (19), `write_atomic` (28) | 5 | kept |
| Reader skips misnamed, temp, symlinked or invalid files | `orch/src/orch/stores/mailbox.py` `read_all` (79), `_safe` (33) | 5 | kept |
| Idempotent publish and rising per-sender seq under a lock | `orch/src/orch/stores/mailbox.py` `_publish` (96), `post_next` (111), `_lock` (67) | 5 | changed: no lock, one writer per folder |
| Ack file touched when the worker starts a task | `orch/src/orch/stores/mailbox.py` `ack` (117), `acked` (121) | 5, 11 | kept |
| Result preamble ("how to answer") appended to every task | `orch/src/orch/service/transports.py` `howto` (51), `WORKER_RULES` (46) | 11 (decision R) | kept, always on: the worker writes `outbox/<task-id>-result.json` |
| Reply watermark before delivery; replies must name the task | `orch/src/orch/service/messaging.py` `_deliver` (67); `orch/src/orch/service/transports.py` `replies` (151) | 11 | kept for wait; `result` returns the newest result file regardless (D3) |
| Send guards (blocked, working, human-owned) | `orch/src/orch/service/messaging.py` `guard` (39), `_sendable` (30) | 11 | partly: `--queue` kept, human-owned dropped |
| Pinning: pane id plus terminal_id on every agent call, no raw-input fallback | `orch/src/orch/service/transports.py` `check_occupant` (70), `pinned` (77) | 10, 17 | kept |
| Late agent adopted, renamed and pinned | `orch/src/orch/service/launch.py` `adopt_late_agent` (107), `launch_agent` (67) | 10 | kept, run from the next `wait` or `list` |
| Shell settled before typing | `orch/src/orch/service/readiness.py` `wait_shell_ready` (112), `wait_stable` (87) | 10 | kept |
| Workers tab per caller, splits inside it, cap 6 | `orch/src/orch/service/placement.py` `place` (68), `split_target` (42), `release_if_empty` (82) | 6, 10 (decision L) | kept |
| Verified stop: pids with start times first, exit, positive shell, close, verify, never signal | `orch/src/orch/service/shutdown.py` `stop` (204), `record_pids` (49), `graceful` (80), `wait_shell` (107), `_shutdown_pane` (135), `survivors_and_daemons` (117), `_verdict` (175); `orch/src/orch/probes.py` `start_of` (68), `status` (74) | 10 | kept; output names why (D5) |
| Empty input line before `/exit` | `orch/src/orch/service/readiness.py` `clear_input` (180) | 10 | kept as a refusal; `--clear-input` not ported |
| Person-only dialogs are never typed into | `orch/src/orch/adapters/claude.py` `CLAUDE_DIALOGS`; `orch/src/orch/service/readiness.py` `dialog_name` (215) | 10, 17 | kept |
| Pending questions and when they resolve | `orch/src/orch/service/questions.py` `resolves` (20), `pending` (32), `as_question` (42) | 11, 17 | kept; slice 17 adds answering |
| Hook kinds and the redaction rule | `orch/src/orch/hookcore.py` `HOOK_KIND` (39), `normalise` (177), `short` (101), `redact` (95) | 5, 11 | changed: wipnote hooks, RE2 redaction (issue R) |

## 1. Commands and flags

Every command prints one compact JSON line, exit 0 on `"ok":true`, 1 on a refusal `{"ok":false,"err":CODE,"why":...}`
(`orch/src/orch/cli.py` `emit` line 57, `_failure` line 63). Global option: `--verbose` before the command
(`main_options` line 98) adds the full registry record.

| orch command and flags (`cli.py` line) | `wipnote delegate` target | Reason |
|---|---|---|
| `spawn NAME --kind K` (106) | `spawn NAME --harness claude\|codex` (slice 10) | `--kind pi` not ported: v1 is Claude and Codex only. orch kinds: `claude`, `codex`, `pi` (agent transport), `claude --container` (mailbox transport) |
| `spawn --cwd DIR` | not in slice 10's flag list | Gap: orch defaults to the caller's cwd and records it (`sessions.spawn` line 97). Decide whether delegate takes `--cwd` or always uses the caller's |
| `spawn --container` | `spawn NAME --container` (slice 21) | Ported with a different mechanism. The worker runs inside the project's own devcontainer (`.devcontainer/devcontainer.json`) through `devcontainer up` and `devcontainer exec --workspace-folder <root>`, with wipnote's hooks inside writing to `.wipnote/delegate/` over the workspace mount. orch's container transport is not copied: its `dev exec` launch line (`orch/src/orch/adapters/container_claude.py` `launch_cmd`), the typed pointer line (`transports.MailboxTransport.send` line 215), `launcher.pid` ownership (`stores/mailbox.py` `launcher_pid` line 135) and the `mailbox_not_shared` check. stop records container pids through `devcontainer exec` and never signals them. No second-account gate |
| `send NAME TEXT` (117) | `send NAME` (slice 11) | Store first, then hint |
| `send --queue` | port as a flag on `send` | orch refuses a send while the worker is `working` unless `--queue` (`messaging.guard` line 62) |
| `send --wait`, `--timeout MS` | `wait NAME --timeout` | Separate verb in delegate; `--timeout` becomes mandatory (slice 11) |
| `send --no-result-file` | flag not ported; the preamble it turns off IS ported (slice 11, decision R) | orch appends a "how to answer" preamble to every task unless this flag is passed (`transports.howto` line 51; `messaging.send` line 91). delegate always appends it: the worker writes `outbox/<task-id>-result.json` by temp file plus rename, and that file is the only durable result. The Stop hook records only "turn ended", never the assistant message. With no result file there is nothing to settle on, so there is no opt-out |
| `send --assume-ready` | not ported | orch needs it only where no hook proves readiness (container). Native workers run wipnote hooks |
| `resend NAME [--assume-ready]` (131) | not ported | Replaced by a store-first mailbox plus `inbox` (slice 11). Risk: a lost hint leaves the worker idle until someone retypes it; consider `send --rehint` |
| `wait NAME [--timeout MS]` (137) | `wait NAME --timeout` (slice 11) | Outcome map: orch `ready` -> `done` when a result file for the current task exists, else `idle_no_result` (or `idle_shell`, or `ready` on a worker with no task, D6); `asks`/`needs-human` -> `question`; `timeout` -> `timeout` (exit 124); `error` -> `gone` or an error; `human`/`inspect` not ported. A result file ends the wait even when hooks are silent (D1) |
| `result NAME` (142) | `result NAME` (slice 11) | Both read the worker-written outbox result file, not a Stop payload. orch returns only a result naming the LAST task and null otherwise (`messaging.result` line 164); delegate returns the newest result file whatever the state or watermark, and names its task (D3) |
| (none) | `answer NAME <qid> <text\|option>` (slice 17) | orch marks every question `answerable_by: ["human"]` and has no answer verb. delegate checks the question is pending and the pin (`transports.check_occupant`) still holds, refuses while a person-only dialog shows (`adapters/claude.py` `CLAUDE_DIALOGS`), then types the answer into the worker's herdr pane with `pane send-text` and `pane send-keys enter`. No blocking hook, no reply file |
| (none) | `inbox` (slice 11) | Worker-side read of its own mailbox, named by `WIPNOTE_DELEGATE_RUN`. orch's worker reads the file the pointer line names |
| `status [NAME]` (147) | `list` (slice 10) plus readiness in `wait` | One read verb instead of two |
| `read NAME [--lines N]` (152) | not ported as a verb | Settling never reads the screen (goal). The adapter keeps `Read` for doctor and debugging |
| `takeover NAME`, `release NAME` (157, 162) | not ported | No human-owned flag in the plan. Note: orch refuses `send` and `stop` on a human-owned session; delegate has no such guard |
| `stop NAME` (167) | `stop NAME` (slice 10) | Verified stop, section 4 |
| `stop --even-if-human` | not ported | No human-owned flag |
| `stop --assume-exited` | port | Needed when `pane process-info` omits the foreground (orch refuses `cannot_verify` otherwise) |
| `stop --assume-ready` | not ported | Container-only readiness override |
| `stop --clear-input` | not ported in v1 | ctrl+u before `/exit`; deletes a human draft. orch refuses `cannot_clear_input` instead by default |
| `list` (184) | `list` (slice 10) | |
| `gc` (189) | not ported as a verb | Worker-loss close in `wait`/`list`/`stop` (slices 10, 16) plus the run sweep (slice 5) |
| `where NAME` (194) | not ported | Prints cwd and pids (host paths). `lineage dlg-<id>` shows the caller (slice 13) |
| `inspect NAME` (200) | not ported as a verb | Its parts land in `questions` and `list` |
| `tree NAME` (206) | `tree NAME` (slice 10, `RenderAgentTree`) | |
| `questions [--all]` (212) | `questions` (slice 11) | `--all` (stopped sessions too) optional |
| `watch [--since SEQ] [--timeout MS] [--only NAME]` (227) | not ported | Needs a change feed with a global seq. The orchestra band (slice 19) and `wait` cover it |
| `run NAME --kind K` (250) | `run [--label] [--work-item] -- <argv>` (slice 8) | delegate takes the tool argv directly, so the executor flags below are not ported |
| `run --via local\|ccx`, `--approve-ccx` | `--via ccx` becomes `run --container` (slices 8, 21); `--approve-ccx` not ported | ccx means container Claude (decision C): a one-shot run in the project's own devcontainer. A bare `ccx task ...` in the Bash tool is denied with the translated `wipnote delegate run --container -- <task args>` (slice 9). orch's second-account gate (`service/headless.py` `check_account` line 42, `ORCH_ALLOW_CCX`, `--approve-ccx`) is not ported: the owner set no gate |
| `run --task TEXT \| --task-file FILE` | not ported | Part of argv. orch never puts task text in the shell line (section 5) |
| `run --cwd`, `--model M`, `--max-turns N`, `--write`, `--allow TOOLS`, `--allow-unsafe PATTERN` | not ported | Part of argv. orch's read-only default allowlist is `adapters/claude_p.py` `DEFAULT_ALLOW` line 19 |
| `usage [NAME]` (286) | ledger usage columns plus lineage (slices 4, 13) | Not a verb |
| `schema [MODEL]` (292) | not ported | wipnote commands have `--json` |
| `probe hooks [--container] [--cwd] [--timeout MS]` (302) | one `doctor` check (slice 12); the container side in slice 21 | orch spawns a throwaway session with one UserPromptSubmit hook: fired, blocked or timeout (`service/probe.py`). Slice 21's doctor checks `devcontainer up`, wipnote inside and that a hook fires |
| `probe ready NAME` (313) | not ported as a verb | Readiness rule for orch's container transport (`service/ready.py` `probe_ready` line 49). delegate has one readiness predicate shared by wait, probe and send (slice 10, D2) |
| hook program `orch/hooks/orch_hook.py <mailbox>` and `orch/bin/orch-hook-<os>-<arch>` | replaced by wipnote's plugin hooks writing `events-<writer>.ndjson` (slices 5, 10, 11) | Native workers run under wipnote's launchers |
| `orch/bin/orch` (uv wrapper), `scripts/install-hook-binaries.sh`, `scripts/e2e_mac.sh` | not ported | Packaging and a manual round-trip script. `e2e_mac.sh` is a model for a live test (HERDR_ENV=1 only) |

Global flags: `--verbose` (adds the registry record) and typer's `--help`; delegate commands use `--json` instead.

Settings: 39 `ORCH_*` variables (`orch/src/orch/models/settings.py` `ENV_NAMES` line 23, defaults in `Settings`
line 74). The ones a port should keep as wipnote settings: `max_sessions` 6 (cap), `poll_s` 0.5, `stop_wait_ms`
15000, `verify_ms` 3000, `pid_wait_ms` 2000, `wait_ms` 600000, `lock_timeout_s` 10, `launch_ready_ms` 20000,
`launch_wait_ms` 30000, `hook_silent_s` 120, `event_tail_bytes` 1000000, `max_rows` 50, `max_chars` 20000.
Not kept: `ORCH_ALLOW_CCX` (no gate, decision C), `ORCH_DEV`, `ORCH_CONTAINER_LAUNCHER`, `ORCH_SHARED_ROOT` and
`ORCH_HOOK_*` (orch's own container launcher and hook program), `ORCH_PRICE_*` (wipnote has its own cost view).

## 2. Files on disk (orch's layout)

| Path | Writer | Content |
|---|---|---|
| `<registry>` = `~/.cache/orch/registry.json` (`ORCH_REGISTRY`) | orch, under `registry.json.lock` | section 8 |
| `<registry>.<name>.oplock` | orch | per-session operation lock (`stores/registry.py` `session_lock` line 45); a second op fails fast `busy` |
| `changes.ndjson`, `changes.state.json`, `changes.ndjson.lock` next to the registry | `watch` | global change feed (`stores/changes.py`) |
| `<mail_root>` default `<shared_root>/.orch/mail`, shared root `~/work` (`settings.py` lines 14-20; `ORCH_MAIL_ROOT`, `ORCH_SHARED_ROOT`) | orch | The README also names an older default under `adhoc/`, and the conformance FINDINGS P-5 one user's absolute path: the default moved twice, so import-orch must honour `ORCH_MAIL_ROOT` and probe the defaults |
| `<mail_root>/<name>/inbox/<id>.json` | orch (task) | one message per file |
| `<mail_root>/<name>/outbox/<id>.json` | the WORKER (told how in the task text) or a hook | progress, question, result, error, blocked, prompt |
| `<mail_root>/<name>/ack/<id>` | worker touches it when it starts the task | body is a timestamp |
| `<mail_root>/<name>/.lock` | orch and the hook | per-mailbox flock |
| `<mail_root>/<name>/events.ndjson` | the hook only (O_APPEND under `.lock`) | section 3 |
| `<mail_root>/<name>/hook-errors` | the hook | one line `<count> <epoch> <exception class>` |
| `<mail_root>/<name>/hooks.settings.json` | orch at spawn | passed as `--settings` |
| `<mail_root>/<name>/launcher.pid` | container launcher script | `<pid> <run_id>` |
| `<mail_root>/<name>/run/` (headless) | orch and the pane command | section 5 |
| `<mail_root>/.archive/<name>-<epoch>-<token>/` | orch at spawn when a name is reused | the whole previous mailbox, events included |

All files 0600, folders 0700, created through `stores/atomic.py` `private_mkdir` line 19 and `write_atomic` line 28
(mkstemp in the same folder, write, flush, fsync, chmod, rename). Symlinks anywhere below the root are refused
(`stores/mailbox.py` `_safe` line 33).

## 3. Mailbox layout and messages (slice 5 refine, slice 11)

Store: `orch/src/orch/stores/mailbox.py` `FileMailboxStore` (line 28). Message model: `orch/src/orch/models/__init__.py`
`Message` (line 208). Helpers: `orch/src/orch/service/mail.py` `new_id` (line 11), `post` (line 16).

- Message fields: `v` 1, `id` (`ID_PATTERN` line 72: `^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`), `from` (alias of `sender`),
  `to`, `type` (task, progress, question, result, error, blocked, prompt, note; `MsgType` line 79), `seq` (>= 1,
  per sender), `ts` (ISO UTC, no fraction), `body`, `in_reply_to`, `ack`, `deadline_s`, `ack_timeout_s` (30),
  `origin` (`hook`, `human` or absent).
- File name: `<id>.json`; the reader ignores a file whose stem differs from the `id` inside, temp files, symlinks
  and invalid JSON (`read_all` line 79).
- Publication: under the per-mailbox lock, an existing id returns the stored message (idempotent); otherwise the
  sender's seq must rise above its maximum, else `SeqError` (`_publish` line 96, `post_next` line 111).
- Ids: `m-` + 8 hex of a uuid4 (`mail.new_id`; `clock.py` `token`). 32 bits: see section 7, P-6.
- Watermark: before delivery, orch stores `wait_after` = the outbox's max seq, so an instant reply is not skipped
  (`messaging._deliver` line 67). Replies count only after the watermark and only if a result, error or question
  names the current task in `in_reply_to` (`transports.replies` line 151).
- Delivery for agent-transport workers: the full task text plus the `howto` preamble is typed with
  `herdr agent prompt NAME "[orch m-xxxx] <text>"` (`AgentTransport.send` line 112). For container workers: one
  pointer line `[orch m-xxxx] read <inbox file> and follow it (skip it if <ack file> exists)` through
  `herdr pane run` (`MailboxTransport.send` line 215). The hook marks a prompt `origin: orch` when it starts with
  `[orch m-...]` (`hookcore.py` `ORCH_PROMPT` line 57, `_prompt` line 155).
- Every task starts with fixed worker rules: absolute paths, no bare `cd`, no sub-agents (`transports.WORKER_RULES`
  line 46). A real worker once skipped its result because a bare `cd` was blocked (orch-issues-report item L).

Differences from plan slice 5 (`<root>/mail/<name>/{inbox,outbox}/<msg-id>.json`, `ack/<msg-id>`): the plan keeps
orch's three folders and the `<task-id>-result` id for the result file (decision R), but drops the per-mailbox
lock: each folder has one writer (the caller writes inbox, the worker writes outbox and ack), so a sender's seq
rises without one. The plan's `hinted`, `turn` and `created` fields have no orch counterpart; orch's `v`, `ack`,
`deadline_s`, `ack_timeout_s` and `origin` have no plan counterpart. Worker-to-orchestrator messages are the
result, question and progress files in outbox, as in orch.

As built in slice 5 (`internal/delegate/runstore`, feat-7565fbf7): messages are
`<root>/mail/<name>/{inbox,outbox}/<seq>-<msg-id>.json` with seq zero-padded to six digits, ids `msg-` plus 16 hex
(12 to 64 accepted), the result is `outbox/<task-id>-result.json` (id `<task-id>-result`, outside the seq order)
and the ack is `ack/<msg-id>` holding a timestamp. Kept from orch: `v`, `in_reply_to`, an existing id returning the
stored message, a seq that must rise per folder, and the reader skipping temp, misnamed, symlinked and invalid
files and files whose content names another id or seq than the file name. Not kept: `ack`, `deadline_s`,
`ack_timeout_s` and `origin` (no consumer yet). Events go to `<root>/runs/<run-id>/events-<writer>.ndjson`, one
file per writer process; a writer's next seq is one above the highest seq of every events file of the run, so the
merged order (seq, then writer name) follows what each writer had seen. A torn last line is never parsed and is
closed with an invalid marker before the next append, and the own file is scanned whole for its last seq (P-7).

## 4. Pinning and verified stop (slice 10, slice 17)

Pinning: `orch/src/orch/service/transports.py` `check_occupant` (line 70) and `pinned` (line 77).
- spawn records the pane id and herdr's `terminal_id` (`SessionRecord.pane`, `.terminal_id`).
- Every agent call first runs `herdr agent get NAME` and refuses `occupant_mismatch` if the pane id or terminal id
  differs; a failed lookup refuses `occupant_unknown` and types nothing. There is no fallback to raw pane input.
- A slow launch is adopted later: an agent herdr lists in the recorded pane under an automatic name is renamed
  after the session and pinned (`launch.py` `adopt_late_agent` line 107).

Spawn and launch for Claude: `service/sessions.py` `spawn` (line 97), `service/launch.py` `launch_agent` (line 67).
`herdr agent start --kind claude` is NOT used: a bare `claude` opened a background-session dashboard on the Mac
(`adapters/claude.py` lines 32-35). orch splits a pane, waits for the shell to settle (`readiness.wait_shell_ready`),
types `wipnote claude -- --settings <file>` with `herdr pane run`, polls `herdr agent list` until herdr lists a
`claude` agent in that pane in idle, blocked or done (30 s, extended once), then `herdr agent rename <pane> <name>`.
Codex and Pi use `herdr agent start NAME --kind codex --pane P -- <args>` (`AgentTransport.spawn` line 98).

Verified stop: `orch/src/orch/service/shutdown.py` `stop` (line 204), helpers `record_pids` (49), `foreground` (34),
`graceful` (80), `wait_shell` (107), `_shutdown_pane` (135), `survivors_and_daemons` (117), `_verdict` (175).
Order:
1. Record pids BEFORE any input: `herdr pane process-info --pane P` foreground processes that are not the shell,
   each with its start time from `ps -p PID -o lstart=` (`probes.py` `start_of` line 68).
2. Ask the session to exit with the adapter's quit plan: Claude `/exit` then Enter (as one `agent prompt`), others
   ctrl+c twice (`AdapterSpec.quit_plan`). Never type into a blocked or unknown agent, a different occupant, or a
   screen that shows a person-only dialog (folder trust, permission, login; `adapters/claude.py` `CLAUDE_DIALOGS`).
   Before `/exit`, the TUI input line must be empty (`readiness.clear_input` line 180) or the pane is left open.
3. Wait (15 s) until process-info shows POSITIVELY the shell alone. Missing process details are `unknown`, never
   "at the shell"; a startup helper next to the shell is `settling` (`models.ProcessInfo.kind` line 374).
4. Close the pane only after step 3.
5. Verify every recorded pid: gone, same (same start time: a survivor) or uncertain (`probes.py` `status` line 74).
   Report survivors; never signal or kill anything. Expected daemons (exact name plus a marker argument) are
   informational (`service/daemons.py`).
`stop` is idempotent and resumes. `ok` is false when nothing could be verified (no pids recorded).

Plan slice 10 now follows this order (finding 2): record the pids with start times first, because process-info
for a pane is gone once it is closed, and verify them by pid after the close. Differences: the delegate boot line
(`wipnote delegate boot <run-id>`) replaces orch's typed `wipnote claude -- --settings <file>`, and no `--settings`
hook file is written (q-settings). For a container worker (slice 21), herdr sees only host processes, so container
pids are recorded and checked through `devcontainer exec` in place of orch's `launcher.pid` plus `pgrep -P`.

## 5. Headless run folder (slice 8 comparison)

`orch/src/orch/stores/runs.py` `FileRunStore.paths` (line 25): `<mail_root>/<name>/run/` with `task.txt`, `out.log`,
`err.log`, `answer.md`, `exit` (and `exit.tmp`). The pane line is built in `service/headless.py` `build_line`
(line 52): `cd DIR && ( <argv> < task.txt > out.log 2> err.log; echo $? > exit.tmp && mv exit.tmp exit )`; every
part is shell-quoted and the task text is only ever a file path (stdin, or `"$(cat FILE)"` for `ccx task`).
Status comes from the exit file, then pane presence (`service/runs.py` `HeadlessTransport.status` line 40): a pane
gone before `exit` is `failed`. Log parsers: `stores/streamlog.py` (stream-json for `claude -p`, json-items for
`codex exec --json`). These file names differ from the feat-a1df60c2 contract the plan keeps (raw.log, clean.log,
clean.md, last.md, usage, exit, start, end, label, host, run.html); only `exit` and its temp-then-rename write match.

## 6. Hook events (slices 10 and 11)

Map: `orch/src/orch/hookcore.py` `HOOK_KIND` (line 39); normaliser `normalise` (line 177).

| Hook | Kind | Stored |
|---|---|---|
| SessionStart | session_start | `source` |
| UserPromptSubmit | prompt | `origin` orch or human, short summary |
| PreToolUse | tool_start | tool, `tool_use_id` (as `id`), summary |
| PreToolUse on AskUserQuestion | question | question texts joined by ` / `, option labels (header-prefixed when there are several questions), at most 16 |
| PostToolUse | tool_end | tool, id, summary |
| SubagentStart / SubagentStop | subagent_start / subagent_stop | agent_id, agent_type |
| Notification | notification | `notification_type: message` |
| PermissionRequest | permission_request | tool, summary (no tool_use_id in the payload) |
| Stop | stop | nothing more |
| StopFailure | error | the error type; Stop does not fire on an API error |

Every event: `v`, `seq`, `ts`, `session` (the mailbox name), `kind`, `sid` (payload `session_id`), `agent_id`,
`agent_type`. Free text goes through one rule: first line, secrets masked, clipped to 200 (`short` line 101,
`redact` line 95). orch deliberately never stores `last_assistant_message` (README hook table).

Pending questions: `orch/src/orch/service/questions.py` `resolves` (line 20), `pending` (line 32), `as_question`
(line 42). An ask is answered by the tool_end with the same tool_use_id; a permission by the same agent finishing the
same tool; either ends at a prompt, stop, error or session start, and a sub-agent's question when it stops.
Question ids: the tool_use_id for an ask (`ask-<seq>` if missing), `perm-<seq>` for a permission. orch marks every
question `answerable_by: ["human"]` and refuses to send while the agent is blocked (`messaging.guard` line 53).
delegate keeps the same pending rule and adds `answer` (slice 17): the orchestrator types the answer into the
worker's herdr pane (`pane send-text`, then `pane send-keys enter`; `agent prompt` refuses a blocked agent). A
permission, folder-trust or login dialog is still never answered by the orchestrator. That a digit key selects an
AskUserQuestion option is unverified (Phase B check B9).

Hook installation: `service/hooks.py` `install` (line 132), `settings_for` (127): one handler per event, no matcher,
5 s timeout, passed with `--settings <mailbox>/hooks.settings.json`. Not ported: native workers start under
`wipnote claude` or `wipnote codex`, whose plugin hooks fire. Codex and Pi declare no hooks in orch
(`adapters/codex.py`), so orch has no Codex hook evidence.

## 7. Bugs and pitfalls the port must not copy

From `herdr-research/orch-conformance/FINDINGS.md`, `orch-issues-report.md` and `orch-review-codex.md`:
- F-1 a long `wait` ends with a state patch outside the session lock and can mark a stopped session idle.
- F-2 `resend` uses up an attempt when nothing was typed. F-3 `send --wait` leaves the record `working`.
- F-4 the bounded tail read drops a whole line when the window starts on a line boundary.
- P-6 message ids are 32 bits (8 hex); an id collision is treated as "already stored" and points the worker at the
  older task. The plan now requires 12 or more hex for `msg-` and `dlg-` ids (slices 4 and 5).
- P-7 the hook restarts `seq` at 1 when the log tail has no parseable line. P-8 a herdr syntax error
  (`unknown option: --timeout`, exit 2) is read as a timeout: classify exit 2 before scanning text.
- Issue J: one shared events file with a 1 s flock lost 172 of 800 events from 8 hooks on the bind mount (each file
  call costs 2 to 6 ms there). The plan's one-writer-per-file layout avoids it; do not reintroduce a shared lock.
- Issue R: regex redaction with backtracking is quadratic (8000 characters took 4.9 s in Python); Go's RE2 is linear.
- Issue Q: JSON depth limits differ between interpreters; the Go hook refuses nesting deeper than 256 before parsing.
- Codex review (all fixed in orch, keep as invariants): never signal processes; pin agent names to pane and terminal;
  missing process info is not "at the shell"; persist a pending delivery before input and refuse a blind resend
  after an ambiguous failure; take the reply watermark before delivery; only an explicit not-found means a pane is
  gone (`herdr_client.py` `pane_exists` line 150, `ABSENT_CODES`).
- ccx: orch treats `ccx task` as a second account and refuses it by default (`service/headless.py`
  `check_account` line 42). Do not port that gate. Owner decision C (2026-10-25): ccx means container Claude, and
  `--container` runs the worker in the project's own devcontainer with no second-account gate (slices 8, 9, 21).
  orch's `ORCH_CCX_RUNS` log folder recipe is revisited in slice 21.

## 8. Registry schema (slice 18, import-orch)

`orch/src/orch/models/__init__.py` `Registry` (line 301): `{"v": 1, "sessions": {NAME: SessionRecord}, "tab":
WorkersTab|null}`. Store: `orch/src/orch/stores/registry.py` `FileRegistryStore` (line 17); a corrupt file is
refused, never reset. `SessionRecord` (line 263):

| Field | Type | Import |
|---|---|---|
| `name` | `[a-z][a-z0-9_-]{0,31}` | `name` |
| `kind` | claude, codex, pi, claude_p, codex_exec, ccx_task | `harness`: claude, codex, (pi: skip or keep raw), claude-print, codex, ccx |
| `container` | bool | `host`: container or host (container also for `ccx_task` runs) |
| `pane`, `terminal_id`, `tab` | herdr ids | drop |
| `cwd` | absolute path | drop (host path) |
| `created_at` | epoch seconds | `started` |
| `mailbox` | absolute path | read events from it, then drop |
| `state` | idle, done, working, blocked, unknown, starting, asks, spawning, stopping, survivors, stopped, failed | open vs closed; no end time is stored |
| `owner` | human, orchestrator | drop |
| `pids[]` | `{pid, name, where: mac\|inside, started}` | drop |
| `needs_read`, `last_id`, `pending`, `wait_after`, `hum_ack_seq`, `report_seq`, `sent_at`, `attempts`, `hooks`, `ready` | delivery state | drop |
| `run_id` | 8 hex | idempotency key for the derived `dlg-` id |
| `launched_by` | `{host, pane, session, agent, cwd, at}`; `session` only if the caller exported `ORCH_CALLER_SESSION` | `caller_session` from `session`; drop `cwd`, `pane` |
| `hook_impl`, `hook_note` | go, python, custom | drop |
| `mode` | interactive, headless | `mode`: interactive, oneshot |
| `run_model`, `run_write`, `run_unsafe` | headless only | drop (or note model) |

Not in the registry: a work-item field, an end time, usage, worker session ids. Worker session ids come from the
`sid` of `<mailbox>/events.ndjson` lines; usage only from headless run logs.

## 9. Variants

- **orch-ts** (`herdr-research/orch-ts/`, Node, `FINAL-REPORT.md`): same service layer as the older snapshot, async
  ports, zod models. Differences: locking uses `proper-lockfile`, which does not exclude Python's flock, so the two
  must never share a registry or mailbox; JSON Schema dialect differs; numbers print as JS prints them; a
  non-domain error is `{"ok":false,"err":"internal"}`; it keeps the Python hook. Tested against fakes only;
  conformance run 3: 423 pass, 29 fail, 4 error of 460.
- **orch-hook-go** (`herdr-research/orch-hook-go/`): a Go rewrite of the hook only, rules generated from
  `hook_spec.json`. Its current spec is event version 2: no lock, no `seq`, one O_APPEND write; the reader derives
  `seq` from the byte offset (`INTEGRATION.md` section 8). orch's reader still requires `seq`, and
  `orch/scripts/install-hook-binaries.sh` (lines 19-28) refuses a version-2 build; the binaries bundled in
  `orch/bin/` differ from `orch-hook-go/dist/` and are version 1. HANDOFF.md's "event format v2" line does not
  match the bundled binaries. For slice 18: read both formats, v1 lines carry `seq`, v2 lines do not (derive the
  order from the byte offset).

## 10. Scrubbed

Owner-identifying values found in the archive (paths, names, accounts, other projects, test-fixture secrets) are
not reproduced here; `docs/reference/orch/PROVENANCE.md` lists the categories.
