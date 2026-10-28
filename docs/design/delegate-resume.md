# Delegate resume (plan-28b4a7f6 slice 20)

`wipnote delegate spawn NAME2 --resume NAME` starts a new interactive worker that
continues a stopped worker's harness session: the same conversation, in the same
working folder and the same place (host or the project's devcontainer), under
wipnote's own launchers and hooks. The owner chose resume (option A, 2026-10-25,
q-resume-mode). Parking an idle worker between tasks is rejected: stop-when-done
stays the rule, and resume is how a later task reuses a worker's context.

Legend: **doc** = the upstream page says so (access date given). **probe** = seen
here (Claude Code 2.1.312 `--help`, 2026-10-28). **UNVERIFIED** = neither; it needs
a live check on the owner's Mac (last section).

## 1. Which harness flag

### Claude Code

Source: https://code.claude.com/docs/en/cli-reference (accessed 2026-10-28) and
https://code.claude.com/docs/en/hooks (accessed 2026-10-28).

| Flag | What it does (doc) | For delegate |
|---|---|---|
| `--resume <id>`, `-r` | Resumes a specific session by ID or name (or a transcript path); with no value it shows a picker. With an ID it searches the current project folder and its git worktrees first, then every other project on the machine. `claude -r "<session>" "query"` takes a first prompt. | **Used.** Exact: the id names one session, whatever other workers ran in the same folder. |
| `--continue`, `-c` | Loads the most recent conversation in the current folder. | **Rejected.** Several workers share one folder, so "the most recent" can be another worker's session. |
| `--fork-session` | With `--resume` or `--continue`, creates a new session ID instead of reusing the original. | **Not used.** See 1.1. |
| `--name`, `-n` | Sets the session's display name (shown in `/resume` and the terminal title). | **Not passed on resume.** See 1.2. |

#### 1.1 Fork or not

Resume does not fork. The old run is closed before a resume is allowed, so only one
live worker ever holds the conversation, and keeping the id means the new row's
`worker_sessions` names the same session the old row ended with: the chain reads
from `resumed_from` alone. A fork would give a second id for the same history,
which is only useful when two workers continue one conversation at once, and that
is the case this slice refuses (a run that was already resumed cannot be resumed
again; resume the newest run of the chain instead).

#### 1.2 The session's name

The first spawn passes `--name NAME`. A resume passes no `--name`: the launcher's
own rule (`resolveSessionName` in `cmd/wipnote/claude.go`) is that a resume keeps
the recorded session's name rather than renaming it. The herdr agent is still
renamed to NAME2 by the next `delegate list`, as for any worker.

### Codex CLI

Sources: https://learn.chatgpt.com/codex/developer-commands?surface=cli (accessed
2026-10-28), https://learn.chatgpt.com/docs/non-interactive-mode (accessed
2026-10-28), https://learn.chatgpt.com/docs/hooks (accessed 2026-10-28).

| Form | What it does (doc) | For delegate |
|---|---|---|
| `codex resume <SESSION_ID>` | Resumes the given session (uuid or session name). Accepts model and sandbox overrides, not `--worktree`. | **Used** (`wipnote codex --resume <id>` builds `codex ... resume <id>`). |
| `codex resume --last` | The most recent chat from the current folder (`--all` widens it). | **Rejected**, as `--continue`. |
| `codex exec resume <id> [PROMPT]` | Continues a non-interactive run. | Not this slice (one-shot runs). |
| `codex fork` | Opens a picker to fork a session. | Not used (as 1.1). |

The interactive `codex resume` page lists no PROMPT argument (only `codex exec
resume` has one). So a Codex resume with a brief is **refused** until that is
checked; resume without a brief, then send the task with `delegate send`.
UNVERIFIED: whether `codex resume <id> "<prompt>"` takes a first prompt.

When the folder differs from the session's saved folder, Codex asks which one to
use (doc; `tui.resume_cwd` or `--cd` skip the question). Delegate always resumes
in the recorded folder, so the question should not appear. UNVERIFIED on a live
run.

## 2. Hooks and settings on a resumed session

| Question | Claude Code | Codex |
|---|---|---|
| Does SessionStart fire? | Yes, with `source: "resume"` (`"fork"` with `--fork-session`). doc (hooks). | Yes, `source` is `startup`, `resume`, `clear`, `compact` or `fork`; a resumed thread uses `resume`. doc (hooks). |
| Is `session_id` the same? | Yes unless `--fork-session` ("create a new session ID instead of reusing the original"). doc (cli-reference). | Implied (resume continues the thread). UNVERIFIED. |
| Do past turns' hooks rerun? | No: for `UserPromptSubmit` and `PostToolUse` the saved injected text is replayed, so old timestamps go stale; SessionStart runs again. doc (hooks). | Not stated. |
| System prompt | The prompt recorded on the conversation's first request is reused until compaction, even when the resume passes different `--append-system-prompt*` text. doc (cli-reference, "System prompt flags in resumed conversations"). | Not stated. |
| Plugin hooks (`--plugin-dir`) | Loaded for "this session only" on each launch, so the resume's own `--plugin-dir` applies. The doc does not say this for resumes specifically. UNVERIFIED. | The plugin's `hooks.json` is loaded per launch. UNVERIFIED for resume. |
| `--settings` | Applies "for this session" on each launch. Delegate passes none (q-settings). | n/a |

What this means for wipnote:

- **The bind.** The delegate bind hook (`core/hooks/delegate_bind.go`) records any
  SessionStart whatever its source, so a `resume` start binds the session to the
  new run named by `WIPNOTE_DELEGATE_RUN` (pinned by
  `TestDelegateBind_ResumeSourceBindsSameSession`). The old run folder is not
  written: the env names only the new run.
- **The preamble.** The resumed session keeps the system prompt and the earlier
  brief's preamble from its first run. The new brief carries its own preamble with
  the new name's result path, so a later task's answer goes to NAME2's outbox.
- **Claims.** The resumed worker claims again through its brief (slice 16). One
  worker per item still holds: the old run is closed.

## 3. The flow

1. Refuse, before anything is written, when:
   - NAME has no interactive run on record;
   - NAME's newest run is still open ("live": stop it first, or run `delegate list`
     to close a run whose pane is gone);
   - NAME's run folder or its `session.json` is gone (the sweep removes finished
     run folders after 7 days): the cwd and place are known only there;
   - no harness session id was recorded (the ledger's `worker_sessions` and the
     run folder's bind events are both empty), or the id is not one safe token;
   - the recorded folder no longer exists;
   - `--harness` or `--container` disagrees with the recorded run, or a container
     run belongs to another project root;
   - a Codex resume has a brief (section 1);
   - another run already resumed NAME's run (checked again under the names lock).
2. Open the new row with `resumed_from` = the old run's `dlg-` id and the old run's
   harness and place. The old row stays closed. The work item defaults to the old
   row's (the context belongs to it); `--work-item` overrides.
3. The run folder's `session.json` records `resume_session`. Boot execs
   `wipnote claude --in-place --resume <id> [-- <brief>]` or
   `wipnote codex --in-place --resume <id>`, in the recorded folder, or the same
   lines through `devcontainer exec` for a container worker.
4. SessionStart (source resume) binds the session to the new run; the next `list`
   moves it into the new row's `worker_sessions`.

The cwd is never stored in the ledger (a host path); it is read from the old run
folder only.

## 4. Live checks on the owner's Mac

- `TestResume_LiveClaudeKeepsContext` (HERDR_ENV=1): spawn, tell the worker a
  codeword, stop, resume, ask for the codeword.
- Plugin hooks fire on a resumed Claude and Codex session (a new
  `events-<sid>.ndjson` with `session_start` in the new run folder, same sid).
- Codex: the resumed thread keeps its id; whether `codex resume <id> "<prompt>"`
  accepts a first prompt (if it does, lift the brief refusal).
- A container resume: the session transcript lives in the container's home, so
  it survives only while the devcontainer is the same container (a rebuilt
  container loses it unless the home is a volume).
