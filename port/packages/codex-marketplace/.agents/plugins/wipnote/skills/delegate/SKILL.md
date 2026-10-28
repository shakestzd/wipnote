---
name: delegate
description: "Runs one delegated codex exec, codex review, claude -p or ccx task call with wipnote delegate run, and finds its result on the run page and in the delegations ledger. Preview: one-shot runs only. Use when handing a single prompt or review to another harness and recording the run."
disable-model-invocation: true
---

# wipnote delegate (preview)

**Release 1 is a preview.** It has one-shot runs only: one tool call that starts, runs and ends. Interactive workers (`spawn`, `send`, `wait`, `result`, `stop`, `questions`, `tree`, `list`, `inbox`, `doctor`) come in Release 2. Do not call those verbs; they do not exist yet.

## Two one-shot paths until Release 2

1. **`wipnote delegate run`** (this skill). Use it for new work. It records every run in the delegations ledger.
2. **The codex-herdr-pane mod.** It stays installed in Release 1. It wraps a bare `codex exec`, `codex review`, `claude -p` or `ccx task` call that you type in the Bash tool. Release 2 removes it, and this skill then covers the full delegate command.

The mod does not wrap a `wipnote delegate run` call, so a run never goes through both paths.

## Run one call

Copy this checklist and tick it off:

```
Delegate run:
- [ ] 1. Tool and prompt chosen (codex exec, codex review, claude -p or ccx task)
- [ ] 2. Command written: wipnote delegate run [flags] -- <tool argv>
- [ ] 3. Run finished; status line and answer read
- [ ] 4. Run page shown to the user, or its path given
- [ ] 5. Ledger row checked when the run is for a work item
```

The form is:

```bash
wipnote delegate run [--label L] [--work-item ID] [--no-pane] [--container] -- <tool argv>
```

Examples:

```bash
wipnote delegate run --label "review auth" -- codex review --base main
wipnote delegate run -- codex exec "Explain why TestFoo fails. Do not edit files."
wipnote delegate run --work-item feat-1234abcd -- claude -p "Summarise the open TODOs in core/"
wipnote delegate run -- ccx task "Run the slow integration tests and report failures"
wipnote delegate run --container -- "Run the slow integration tests and report failures"
```

| Flag | What it does |
|---|---|
| `--label L` | The pane title. It is the only label the ledger keeps. Without it, a title made from the prompt goes to the run folder only. |
| `--work-item ID` | The work item of the ledger row. The default is your active claim. |
| `--no-pane` | Run headless, also inside herdr. |
| `--container` | Run the tool in the project's own devcontainer (`.devcontainer/devcontainer.json`). Words after `--` that are not a `claude` or `codex` argv are a task for `claude -p` inside. It is refused without the config or the devcontainer CLI, and for Codex unless this host opted in. `wipnote delegate doctor --container` checks the container. |

Rules:

- Put every wipnote flag before `--`. After `--`, all words go to the tool.
- Give one tool argv. Pipes, `;`, `&&` and redirects are refused. A prompt with spaces must be one quoted word.
- `--help` and `--version` are refused: they are not runs. Call the tool directly for them.
- `claude -p` must not set its own `--output-format` (only `stream-json` is accepted). The run adds `--output-format stream-json --verbose` itself.
- For `codex exec`, the run adds `--json` and `--output-last-message` (unless you set `-o`), and `-p <leanProfileName>` (default `lean`) when you set no profile and that profile file exists in the Codex home. `codex review` and `ccx task` run as given.
- Inside herdr (`HERDR_ENV=1`) the call runs in a herdr pane. Otherwise it runs headless.
- A bare `ccx task` in the Bash tool is denied with the `--container` form: run that instead.
- The command exits with the tool's exit status, or 130 when its pane was closed.
- A long run blocks until it ends. Run the command in the background when the harness lets you, and read the report when it ends.

## Where the results live

**The report.** When the run ends, the command prints:

- a status line: `codex exited 0 after 42s, ...; raw log <dir>/raw.log, answer file <dir>/last.md` (ccx prints its own status line instead);
- the answer from `last.md` when the run succeeded, or the end of `clean.log` when it failed. Long sections are cut, and a line names the file with the rest;
- the run page: `run page: <dir>/run.html`.

**The run page** (`run.html`) is the full run as one HTML page: status, answer and log. Show it to the user, or give them its path. To follow a live run in a terminal, use `wipnote delegate view <run-id>`.

**The run folder** is `.wipnote/delegate/runs/<run-id>/`. It holds `raw.log`, `clean.log`, `clean.md`, `last.md`, `usage`, `exit` and `run.html`. It is not committed. Finished runs are removed after `keepDays` (7 days by default).

**The delegations ledger** is `.wipnote/delegations-ledger.html`. It is committed. Each run is one row: run id (`dlg-...`), mode `oneshot`, harness (`codex`, `codex-review`, `claude-print` or `ccx`), label, work item, start and end, exit, end reason (`exit`, `stop`, `pane_closed` or `gone`) and token counts. Read it to see which runs a work item had. Never edit it by hand.

## Settings

The `delegate` object in the wipnote config sets run behaviour: `layout`, `maxPanes`, `closePane`, `closeDelay`, `tailLines`, `maxReturnLines`, `leanProfile`, `leanProfileName`, `ccxRunsDir`, `ccxWaitSeconds` and `keepDays`. Each key is merged from `~/.wipnote/config.json`, then `.wipnote/config.json`, then `.wipnote/config.local.json`. `ccxRunsDir` is a host path, so it is read only from the user file and the local file. `CCX_RUNS_DIR` overrides it.

`wipnote delegate run --help` prints the same facts.
