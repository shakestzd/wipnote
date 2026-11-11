---
name: delegate
description: "Hands work to a worker with wipnote delegate: one codex exec, codex review, claude -p or ccx task call with run, or an interactive Claude or Codex worker with spawn, send, wait, result and stop. Covers worker setup, parallel worktrees, the claim rules, the run page and the delegations ledger. Use for coding work inside Herdr, or when handing a prompt, a review or a work item to another harness."
disable-model-invocation: true
---

# wipnote delegate

`wipnote delegate` is the one way to hand work to another harness, and inside herdr or the Herdr app it is the first choice for coding work: a `wipnote:*-coder` subagent call is denied there, and the denial names the spawn command. `WIPNOTE_NATIVE_CODERS=1` in the session's environment turns that off. Every run is a row in the delegations ledger.

- **One call:** `wipnote delegate run`. It starts the tool, waits for it and prints the answer.
- **A worker:** `wipnote delegate spawn`. It starts an interactive Claude or Codex session in a herdr pane. You give it tasks with `send` and read its answers with `wait` and `result`.

Do not call `codex exec`, `codex review`, `claude -p` or `ccx task` directly. In the Bash tool, a bare call is denied, and the denial names the `wipnote delegate run` form to use. A command that starts with `wipnote delegate` is never denied.

Do not start Claude or Codex with `hc run` or `herdr agent start`, even when `hc --skill` or `herdr --skill` shows that form. It is denied and the denial names `wipnote delegate spawn`. Use `hc` itself for plain programs: tests, builds and servers.

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
- Inside the herdr TUI (`HERDR_ENV=1`) the call runs in a herdr pane. Inside the Herdr app or bare `hc` (`HC_TERMINAL_ID`) it runs in an hc terminal. Otherwise it runs headless.
- The command exits with the tool's exit status, or 130 when its pane was closed.
- A long run blocks until it ends. Run it in the background when the harness lets you.

One-shot runs do not claim a work item and do not count as a worker.

## Run an interactive worker

Workers need the herdr TUI (`HERDR_ENV=1`) or an hc terminal (`HC_TERMINAL_ID`). Each project gets one workers tab, with at most 6 workers, and every delegator in the project shares it.

In an hc terminal, a pane is a terminal address such as `t7`:

- In the Herdr app the workers tab shows in the tab bar, and each worker is a child of your terminal. The wipnote-main layout (`wipnote delegate install-layout`) lists each worker once, nested under you; Herdr's own main layout also gives each worker a row of its own. Click the tab, or a worker (`terminal.peek`), to see it; `hc read t7` works too. Under bare `hc`, see workers with `hc ls`.
- A worker outlives its caller and types no notes into the caller. Hear from it with `wait`, as in the TUI.
- Claude reports its status to `hc`, so the app shows it as an agent.

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

**`wipnote delegate spawn [NAME] (--harness claude|codex | --resume OLD)`** starts a worker and returns at once with `{run, name, pane, harness, status, profile, model, effort, tools, mcp, plugins}`. Status is `starting`.

- NAME is optional. Without it, the worker is named `<item-id>-<slug of its title>`, cut to 32 characters. A NAME is `[a-z0-9-]`, 1 to 32 characters, starts with a letter and is unique among live workers. Name a worker after its task, or omit the name to get one from the work item.
- `--work-item ID` sets the item. The default is your active claim. Without one, spawn refuses.
- `--brief TEXT` or `--brief-file PATH` (`-` for stdin) is the first task. It goes by file, never on the typed line.
- `--container` starts the worker in the project's devcontainer. A Codex worker there needs this host in `delegate.codex.dangerFullAccessHosts`.
- `--resume OLD` continues the stopped worker OLD on its last harness session, in its folder and place. NAME is required with `--resume`. It refuses while OLD is live, when OLD's run folder was swept, when no session id was recorded, when another run already continues that session, and for a Codex resume with a brief.

**Choose the worker's setup, per task.** You pick it at spawn; spawn never guesses from the brief. A Claude worker starts lean: no `~/.claude/CLAUDE.md`, MCP, auto memory, skills, subagents, Chrome or git status in the prompt, and only Bash, Read, Edit, Write, Grep and Glob. That is about a fifth of the usual first-request prompt (14k against 75k tokens); on short tasks it cost 19% to 51% less than a native subagent on the same model and effort. On one longer multi-file edit, the default worker cost about 35% less than a native `wipnote-feature-coder`, in a small sample of three runs each on a repository with large rules (`docs/design/delegate-token-benchmark.md`). Pick the lowest effort that suits the task. The prompt cache lives 5m, which suits short workers; use `--cache-ttl 1h` for a worker that will sit idle between tasks. Add only what the task needs.

| Task | Use |
|---|---|
| Small, well-defined edit, a lookup, a typo | `--profile simple` (haiku, low effort) |
| Normal coding, tests, a review | the default, `--profile standard` (sonnet, medium) |
| Hard design, a tricky bug, a large refactor | `--profile deep` (opus, high) |
| It must behave as your own session (your CLAUDE.md, MCP, skills, orchestrating) | `--profile full` |
| Fetching a page, running subagents | `--tool WebFetch`, `--tool Agent` |
| It needs one MCP server or connector | `--mcp NAME` (a `claude.ai Notion` connector, a `plugin:x:y` server, or a local server) |
| It needs one plugin's skills | `--plugin NAME` (or `name@marketplace`) |
| It must follow your global rules | `--user-rules`, or better, put the rule in the brief |
| It must follow project conventions | `--project-rules`, or better, put the rules that matter in the brief |

```bash
wipnote delegate spawn fix-typo --harness claude --profile simple --brief "Fix the typo in README.md"
wipnote delegate spawn notes --harness claude --mcp "claude.ai Notion" --brief "Summarize the Q3 page"
wipnote delegate spawn review --harness claude --profile deep --tool WebFetch --brief "Review core/slug for races"
```

- `~/.claude/CLAUDE.md` is off in every profile but `full` (or with `--user-rules`), so the brief must carry any rule the worker needs.
- The project's `CLAUDE.md` and `.claude/rules/` are off in `simple` and `standard` (keep them with `--project-rules`). This is the largest saving on a repository with long rules. The worker is told and can read them, but put any rule that must hold, such as a safety rule or a quality gate, in the brief.
- `--model` and `--effort` override the profile and also work for Codex. Codex refuses `--profile`, `--tool`, `--mcp`, `--plugin`, `--user-rules` and `--project-rules`. Other harness flags go after `--` and win over the profile, for example `--tools`, `--settings`, `--mcp-config`. A one-shot takes them in its argv: `wipnote delegate run -- claude -p "..." --model haiku`.
- An unknown profile, MCP server or plugin refuses the spawn and lists the valid names. `--mcp` and `--plugin` turn on exactly what you name. ToolSearch is added with any MCP server and Skill with any plugin.
- `-p`, `--resume`, `--session-id` and `--name` are refused, because they break the worker's session. Giving the model twice is refused.
- Your own profiles go in `~/.wipnote/config.json` under `delegate.claude.profiles.<name>` with `{model, effort, tools, mcp, plugins, userRules, cacheTTL}`. A profile of a built-in's name replaces it. `delegate.claude.defaultProfile` picks the default.
- spawn prints the `profile`, `model`, `effort`, `tools`, `mcp` and `plugins` the worker got. They are on the ledger row too, and a `--resume` with none of these flags keeps the old run's setup.

**`wipnote delegate send NAME [TEXT | --file PATH|-]`** gives a live worker a task. The task goes to the worker's inbox first, then herdr types a hint. It is refused, and writes nothing, unless the worker accepts input and its input line holds no draft.

**`wipnote delegate wait NAME --timeout D`** blocks until the worker settles. `--timeout` is required. It prints `{run, name, turn, outcome, task, detail, asks}`. The outcome is `ready`, `done`, `asks`, `idle_no_result`, `idle_shell`, `blocked`, `timeout`, `gone` or `stopped`. A timeout exits 124 and changes no claim: wait again, or look with `list`. `gone` exits 3: the worker is lost, so spawn or resume one. `blocked` exits 4: the worker waits at a dialog in its pane, so answer it there. Every other outcome exits 0, so read `outcome` before you trust the result.

**`wipnote delegate watch [NAME] [--timeout D]`** is for Claude Code callers. Arm it once after your first spawn, under the Monitor tool: with no NAME one watch covers all your live workers, and workers spawned later join it. Each state change is one line (`run=R name=N state=working|asks|blocked|done|...`), so you see a question while the worker keeps going. Unlike `wait`, `state=asks` does not end the watch: read it with `delegate questions NAME`, answer it with `wipnote delegate answer NAME QUESTION-ID "text"` (this records the answer and clears the question; `send` refuses while a question is open), and the watch goes on. With no NAME it ends when no worker is left to watch; with a NAME it ends on any other outcome with `wait`'s exit codes. The default timeout is 25 minutes, under Monitor's 30-minute cap. On `timeout`, run `watch` again. Other harnesses run it with `run_in_background`. Codex callers keep using `wait`.

**`wipnote delegate result NAME`** prints the newest result the worker wrote and the task it answers, or `{"result": "none"}`.

**`wipnote delegate questions NAME`** lists the worker's open questions and permission prompts. Answer one with `wipnote delegate answer NAME QUESTION-ID "text"`, which records the answer and clears the question.

**`wipnote delegate inbox`** runs inside a worker. It reads the worker's own mailbox, including tasks whose hint was lost.

**`wipnote delegate reply`** runs inside a worker. It writes the worker's answer to its task (`--task`, default the newest), or with `--ask` a question for the caller. The task preamble tells the worker to use it, because the worker's own hooks block writes into `.wipnote/`.

**Worker questions.** Tell a worker in its brief to raise every question for the owner with `wipnote delegate reply --ask "<question>" --option "<choice>" --option "<choice>" --recommended "<choice>"` (2 to 4 options; `--recommended` takes an option's text or its 1-based number), and not to list questions in its report. Questions written in report prose are not filed. When you run `wipnote delegate result NAME`, each unfiled `--ask` question and AskUserQuestion call of the run, answered or not, becomes a question record on the worker's work item (`question list --item ID`). The ids print as `questions_filed`. Running it again files nothing new. Permission prompts are not filed. A worker with no work item gets a warning that names its questions.

**Parallel workers.** Read-only workers, and workers on different items, can run in parallel as they are. When workers edit overlapping files, start each with `wipnote delegate spawn NAME --worktree [--base REF]`. Each gets its own git worktree at `.claude/worktrees/NAME` on its own branch `delegate/NAME` (cut from `--base`, default HEAD), so each has its own branch to merge, and they may share one work item. The cap of 6 still holds. Their first request costs a little more, because sessions in different folders do not share the prompt cache. `--worktree` refuses `--container` and `--resume`. `stop` removes a worktree worker's folder and branch only when it has no changes, no untracked files and no commits; otherwise it keeps them and prints `worktree`, `branch` and `kept: true`, so merge or discard them yourself. A resume of a stopped worker runs in its worktree while that folder exists.

**`wipnote delegate stop NAME [--wait D]`** asks the worker to exit, waits (20s by default) until only the shell is left, then closes the pane. It never kills a process. It exits 1 when the pane stays open, and `why` says why.

## Claim rules

These are the rules for a worker and its work item:

1. **Co-claim.** The worker claims the same item as you, its caller. You keep your claim. The two claims count as one family, so the claim gate refuses neither of you. An unrelated session is still refused.
2. **There is one live worker per item, unless each has its own worktree.** spawn refuses when a live worker already holds the item, except with `--worktree`, where every worker has its own checkout and several may share the item. A worker whose pane is gone is closed first. One-shot runs do not count.
3. **Stop the worker before you complete the item.** Completing an item closes every open claim on it, the worker's too. Run `wipnote delegate stop NAME` first.

## Outside requests

A worker, one-shot run or researcher you start must never put the user's email address or any other personal identifier into a request to an outside service. That covers web fetches, search queries, API calls and `User-Agent` or contact fields. When a source needs an email to be read (for example a paywalled-paper lookup API), the worker reports the source as unreadable instead of sending the address. Put this rule in every brief you write for a worker.

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
- **The run folder** is `.wipnote/delegate/runs/<run-id>/` in the store (`wipnote status` prints where the store is; it may be under `~/.wipnote/projects/`). It is not committed. Finished runs are removed after `keepDays` (7 days by default).
- **The delegations ledger** is `.wipnote/delegations-ledger.html` in the store. It is committed to the store's repo. Each run is one row: run id (`dlg-...`), mode, harness, label, work item, start and end, exit, end reason and token counts. Never edit it by hand.

## See agents in the Herdr app

**`wipnote delegate install-layout`** copies the `wipnote-main` layout and its mods (the agents list and a pane header) into the Herdr app's home. Your own layouts stay as they are. It refuses when a target exists; `--replace` moves the existing files to a dated folder first.

## Settings

The `delegate` object in the wipnote config sets run behaviour: `layout`, `direction`, `maxPanes`, `closePane`, `closeDelay`, `tailLines`, `maxReturnLines`, `leanProfile`, `leanProfileName`, `ccxRunsDir`, `ccxWaitSeconds` and `keepDays`. Each key is merged from `~/.wipnote/config.json`, then `.wipnote/config.json`, then `.wipnote/config.local.json`. `ccxRunsDir` is a host path, so it is read only from the user file and the local file. `CCX_RUNS_DIR` overrides it.

Each verb's `--help` prints the same facts.
