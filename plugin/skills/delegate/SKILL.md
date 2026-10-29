---
name: delegate
description: "Hands work to another harness with wipnote delegate: one codex exec, codex review, claude -p or ccx task call with run, or an interactive Claude or Codex worker with spawn, send, wait, result and stop. Covers the claim rules, the run page and the delegations ledger. Use when handing a prompt, a review or a work item to another harness."
disable-model-invocation: true
---

# wipnote delegate

`wipnote delegate` is the one way to hand work to another harness. Every run is a row in the delegations ledger.

- **One call:** `wipnote delegate run`. It starts the tool, waits for it and prints the answer.
- **A worker:** `wipnote delegate spawn`. It starts an interactive Claude or Codex session in a herdr pane. You give it tasks with `send` and read its answers with `wait` and `result`.

Do not call `codex exec`, `codex review`, `claude -p` or `ccx task` directly. In the Bash tool, a bare call is denied, and the denial names the `wipnote delegate run` form to use. A command that starts with `wipnote delegate` is never denied.

**Chained commands skip the ledger until v2.** A pipe, `;`, `&&`, a command fed on stdin or `dev exec` is not redirected. It runs as typed and gets no ledger row. `delegate run` refuses such an argv.

## Run one call

```
Delegate run:
- [ ] 1. Tool and prompt chosen (codex exec, codex review, claude -p or ccx task)
- [ ] 2. Command written: wipnote delegate run [flags] -- <tool argv>
- [ ] 3. Run finished; status line and answer read
- [ ] 4. Run page shown to the user, or its path given
```

```bash
wipnote delegate run [--label L] [--work-item ID] [--no-pane] [--write] [--container] -- <tool argv>
```

```bash
wipnote delegate run --label "review auth" -- codex review --base main
wipnote delegate run -- codex exec "Explain why TestFoo fails. Do not edit files."
wipnote delegate run --write -- codex exec "Fix the typo in README.md"
wipnote delegate run --work-item feat-1234abcd -- claude -p "Summarise the open TODOs in core/"
wipnote delegate run -- ccx task "Run the slow integration tests and report failures"
wipnote delegate run --container -- "Run the slow integration tests and report failures"
```

| Flag | What it does |
|---|---|
| `--label L` | The pane title. It is the only label the ledger keeps. |
| `--work-item ID` | The work item of the ledger row. The default is your active claim. |
| `--no-pane` | Run headless, also inside herdr. |
| `--write` | `codex exec` only: `--sandbox workspace-write` instead of `read-only`. |
| `--container` | Run the tool in the project's devcontainer (`.devcontainer/devcontainer.json`). Words after `--` that are not a `claude` or `codex` argv are a task for `claude -p` inside. It is refused without the config or the devcontainer CLI, and for Codex unless this host opted in. |

Rules:

- Put every wipnote flag before `--`. After `--`, all words go to the tool.
- Give one tool argv. Pipes, `;`, `&&` and redirects are refused. Quote a prompt with spaces as one word.
- `--help` and `--version` are refused. They are not runs.
- `claude -p` must not set its own `--output-format`. The run adds `--output-format stream-json --verbose`.
- `codex exec` always gets one `--sandbox`: `read-only`, or `workspace-write` with `--write`. `danger-full-access` is passed only on a host listed in `delegate.codex.dangerFullAccessHosts` in `~/.wipnote/config.json`.
- A bare `ccx task` in the Bash tool is denied. The denial names `wipnote delegate run --container -- <task words>` when the task is plain words and the project has its own devcontainer; otherwise (ccx flags such as `--resume`, or no devcontainer) it names `wipnote delegate run -- ccx task ...`. Run the form it names.
- Inside herdr (`HERDR_ENV=1`) the call runs in a herdr pane. Otherwise it runs headless.
- The command exits with the tool's exit status, or 130 when its pane was closed.
- A long run blocks until it ends. Run it in the background when the harness lets you.

One-shot runs do not claim a work item and do not count as a worker.

## Run an interactive worker

Workers need herdr (`HERDR_ENV=1`). Each caller gets one workers tab, with at most 6 workers.

```
Delegate worker:
- [ ] 1. Work item claimed (the worker co-claims it)
- [ ] 2. Worker started: wipnote delegate spawn
- [ ] 3. Task sent and answer read: send, wait --timeout, result
- [ ] 4. Questions answered when wait says asks
- [ ] 5. Worker stopped: wipnote delegate stop NAME
- [ ] 6. Only then: the work item completed
```

```bash
wipnote delegate spawn --harness claude --brief "Write tests for core/slug"
wipnote delegate spawn api-fix --harness codex --work-item feat-1234abcd --brief-file brief.md
wipnote delegate send api-fix "Now run the gate and report the result"
wipnote delegate wait api-fix --timeout 20m
wipnote delegate result api-fix
wipnote delegate stop api-fix
```

**`wipnote delegate spawn [NAME] (--harness claude|codex | --resume OLD)`** starts a worker and returns at once with `{run, name, pane, harness, status}`. Status is `starting`.

- NAME is optional. Without it, the worker is named `<item-id>-<slug of its title>`, cut to 32 characters. A NAME is `[a-z0-9-]`, 1 to 32 characters, starts with a letter and is unique among live workers.
- `--work-item ID` sets the item. The default is your active claim. Without one, spawn refuses.
- `--brief TEXT` or `--brief-file PATH` (`-` for stdin) is the first task. It goes by file, never on the typed line.
- `--container` starts the worker in the project's devcontainer. A Codex worker there needs this host in `delegate.codex.dangerFullAccessHosts`.
- `--resume OLD` continues the stopped worker OLD on its last harness session, in its folder and place. NAME is required with `--resume`. It refuses while OLD is live, when OLD's run folder was swept, when no session id was recorded, when another run already continues that session, and for a Codex resume with a brief.

**`wipnote delegate send NAME [TEXT | --file PATH|-]`** gives a live worker a task. The task goes to the worker's inbox first, then herdr types a hint. It is refused, and writes nothing, unless the worker accepts input and its input line holds no draft.

**`wipnote delegate wait NAME --timeout D`** blocks until the worker settles. `--timeout` is required. It prints `{run, name, turn, outcome, task, detail, asks}`. The outcome is `ready`, `done`, `asks`, `idle_no_result`, `idle_shell`, `blocked`, `timeout`, `gone` or `stopped`. A timeout exits 124 and changes no claim: wait again, or look with `list`. `gone` exits 3: the worker is lost, so spawn or resume one. `blocked` exits 4: the worker waits at a dialog in its pane, so answer it there. Every other outcome exits 0, so read `outcome` before you trust the result.

**`wipnote delegate watch NAME [--timeout D]`** is for Claude Code callers. Run it under the Monitor tool: each state change is one line (`run=R name=N state=working|asks|blocked|done|...`), so you see a question while the worker keeps going. Unlike `wait`, `state=asks` does not end the watch: read it with `delegate questions NAME`, answer it by typing into the worker's pane (`send` refuses while a dialog is open), and the watch goes on. It ends on any other outcome with `wait`'s exit codes. The default timeout is 25 minutes, under Monitor's 30-minute cap. On `timeout`, run `watch` again. Codex callers keep using `wait`.

**`wipnote delegate result NAME`** prints the newest result the worker wrote and the task it answers, or `{"result": "none"}`.

**`wipnote delegate questions NAME`** lists the worker's open questions and permission prompts. Answer one by typing into the worker's pane.

**`wipnote delegate inbox`** runs inside a worker. It reads the worker's own mailbox, including tasks whose hint was lost.

**`wipnote delegate reply`** runs inside a worker. It writes the worker's answer to its task (`--task`, default the newest), or with `--ask` a question for the caller. The task preamble tells the worker to use it, because the worker's own hooks block writes into `.wipnote/`.

**`wipnote delegate stop NAME [--wait D]`** asks the worker to exit, waits (20s by default) until only the shell is left, then closes the pane. It never kills a process. It exits 1 when the pane stays open, and `why` says why.

## Claim rules

These are the rules for a worker and its work item:

1. **Co-claim.** The worker claims the same item as you, its caller. You keep your claim. The two claims count as one family, so the claim gate refuses neither of you. An unrelated session is still refused.
2. **There is one live worker per item.** spawn refuses when a live worker already holds the item. A worker whose pane is gone is closed first. One-shot runs do not count.
3. **Stop the worker before you complete the item.** Completing an item closes every open claim on it, the worker's too. Run `wipnote delegate stop NAME` first.

A stopped worker's claim ends as `abandoned`. When a worker crashed or lost its pane, `wait`, `list` or `stop` closes its claim as `expired`.

## See runs

- **`wipnote delegate list [--all] [--json]`** lists open runs (`--all` adds ended ones) with their state and Overview group.
- **`wipnote delegate tree [--all] [--json]`** shows callers, their runs, worker sessions and sub-agents.
- **`wipnote delegate view <run-id>`** follows a run's log until it ends, then prints its answer.

## Check the host

**`wipnote delegate doctor [--container]`** checks the codex binary and Codex's bwrap sandbox, and shows the sandbox a worker gets. It exits non-zero when a check fails. `--container` also checks the project's devcontainer: it runs `devcontainer up`, then checks wipnote, a hook, `~/.claude` and the Claude login inside. Each failure names its fix.

## Import old orch runs

**`wipnote delegate import-orch [--mail-root DIR]`** copies orch's run history into the ledger, one closed row per past run. A rerun adds nothing.

## Where the results live

- **The report.** `delegate run` prints a status line, the answer (or the end of the log on failure) and `run page: <dir>/run.html`. Long sections are cut, and a line names the file with the rest.
- **The run page** (`run.html`) is the full run as one HTML page. Show it to the user, or give them its path.
- **The run folder** is `.wipnote/delegate/runs/<run-id>/`. It is not committed. Finished runs are removed after `keepDays` (7 days by default).
- **The delegations ledger** is `.wipnote/delegations-ledger.html`. It is committed. Each run is one row: run id (`dlg-...`), mode, harness, label, work item, start and end, exit, end reason and token counts. Never edit it by hand.

## Settings

The `delegate` object in the wipnote config sets run behaviour: `layout`, `direction`, `maxPanes`, `closePane`, `closeDelay`, `tailLines`, `maxReturnLines`, `leanProfile`, `leanProfileName`, `ccxRunsDir`, `ccxWaitSeconds` and `keepDays`. Each key is merged from `~/.wipnote/config.json`, then `.wipnote/config.json`, then `.wipnote/config.local.json`. `ccxRunsDir` is a host path, so it is read only from the user file and the local file. `CCX_RUNS_DIR` overrides it.

Each verb's `--help` prints the same facts.
