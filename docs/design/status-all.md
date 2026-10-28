# wipnote status --all

`wipnote status --all` prints one block for every project in the cross-project registry. Each block
answers four questions: where the project is, what each agent is doing, why (its work item), and
what waits on the owner. `--json` prints the same data in the schema below. The owner's
project-monitor pane runs the command; nothing is written to feed it. Source: plan plan-56a4f0ad
slice 3 (feat-715f1d4f). Code: `internal/statusall/`, `cmd/wipnote/status_all.go`.

## Usage

```bash
wipnote status --all            # compact text, newest activity first
wipnote status --all --json     # the schema below
wipnote status --all --no-gh    # skip every gh call (no PR or CI)
wipnote status --all --timeout 10s --runs 3
```

| Flag | Default | Meaning |
|------|---------|---------|
| `--all` | off | Read every registered project instead of the current one |
| `--json` | off | Print the JSON document (needs `--all`) |
| `--no-gh` | off | Do not call gh; `sources.gh` is `skipped` |
| `--timeout` | 30s | Bound for the whole run |
| `--runs` | 5 | Recent runs kept per project |

## Guarantees

- **Read-only.** It reads `.wipnote/` files and ledgers directly. It opens no SQLite database,
  writes no `.wipnote/` file, makes no commit, and does not touch the registry. The usual
  per-command session and registry bookkeeping is skipped for `status --all`.
- **Never in a hook.** No hook calls it.
- **Bounded.** Projects are read by a pool of 4 workers. Every git call has a 5 s timeout, every gh
  call 8 s, every herdr call 3 s, and the whole run stops at `--timeout`. A project still reading
  then is printed as `not read: the <timeout> time limit was reached`.
- **Degrades per part.** A missing, signed-out or failing gh marks only `sources.gh`; herdr outside a
  herdr pane, missing or failing marks only `sources.herdr`. A project whose folder or `.wipnote/`
  cannot be read is an error row, and the rest still print.

## Where each part comes from

| Part | Source |
|------|--------|
| project list | `internal/registry` (`projects.json`) |
| plan and slice | `.wipnote/plans/*.yaml` via `plan/planyaml` |
| items in progress | `.wipnote/features`, `bugs`, `spikes` HTML |
| last commit, branch | `git log -1` and `git symbolic-ref` in the project folder |
| PR and CI | `internal/gitwatch` (the `ci status` reader), for the current branch only |
| agents | herdr `agent list` (state), the claims and sessions ledgers (work item) |
| runs | `core/runledger` (slice 1, feat-36658c22) |
| needs you | blocked herdr agents and the runs above |
| orphans | filled by slice 4 (feat-e6e3ef39); always `[]` until then |

**Active plan.** A plan whose status is not `completed` and that has a slice not `done` or
`superseded`. When several qualify: one with a slice whose feature is in progress wins, then
`active`/`finalized` over `review` over `draft`, then the newest `created_at`. The current slice is
the first unfinished slice whose feature is in progress, else the first `promoted` or `in_progress`
slice, else the first unfinished one. `done` counts `done` slices; `total` counts every slice
except `superseded` ones.

**Agents.** herdr's `agent list` reports each pane's `cwd` and, when herdr can resolve it, the
`foreground_cwd` of the process controlling the pane
([herdr CLI reference](https://herdr.dev/docs/cli-reference/)). An agent is placed in the project
whose folder holds its `foreground_cwd`, else its `cwd`: the deepest project folder, or the project
whose main repository that folder is a linked worktree of. With neither, the agent is placed by the
work item its label names (`<id>: <title>`, set by slice 2). An agent placed nowhere is not shown. Each open
claim episode whose root session has not ended (sessions ledger) is a `ledger` agent. A herdr agent
is joined to the claim on the same work item; an unlabelled pane is joined to the one claim of its
harness only when it is the only unlabelled pane of that harness. A session that crashed without
an end keeps its claim until `wipnote claims reconcile` closes it.

**Needs you.** `blocked_agent`: a herdr agent whose state is `blocked`. `failed_run`: a run that
failed in the last 24 h and was not re-run green since (a later run with the same label that
completed). `lost_run`: a run started in the last 24 h with no end whose session has ended
(runledger status `unknown`); it may still be running. Idle, working and done agents never need you.
Not yet sourced: held runs (a guardrail hold waiting on the owner) and worker questions waiting for
an answer. The plan's notes list both as needs-you entries; no ledger records them yet, so they are
left out until one does.

**Activity.** `last_activity` is the newest of the last commit, the runs, the agents' `since` and the
in-progress items' update times; the registry's last-seen time when there is none.

## JSON schema (`wipnote.status-all/v1`)

Top level:

| Key | Type | Meaning |
|-----|------|---------|
| `schema` | string | `wipnote.status-all/v1` |
| `generated_at` | RFC 3339 | When the run started |
| `projects` | [Project] | Newest `last_activity` first, then by name; rows that could not be read last |

Project:

| Key | Type | Meaning |
|-----|------|---------|
| `name` | string | Registry name |
| `id` | string | Registry id (8 hex) |
| `last_activity` | RFC 3339? | See Activity |
| `branch` | string? | Checked-out branch; absent when detached |
| `plan` | Plan or null | Active plan |
| `in_progress` | [Item] | In-progress work items, by id |
| `last_commit` | Commit or null | Last commit on the branch |
| `pr` | PR or null | PR for the current branch |
| `agents` | [Agent] | Live agents |
| `runs` | [Run] | Recent runs, newest first |
| `needs_you` | [Need] | What waits on the owner; blocked agents first |
| `orphans` | [] | Filled by slice 4 (feat-e6e3ef39) |
| `sources` | {string: string} | State of each part: `git`, `gh`, `herdr` |
| `errors` | [string] | Parts that could not be read; for an unreadable project, its only content |

`sources` values: `ok`; `none` (gh: no PR for the branch, or no branch); `skipped` (gh with
`--no-gh`); `not available: <reason>`.

Plan: `id`, `title`, `slice` (Slice or null), `done` (int), `total` (int).
Slice: `num` (int), `title`, `feature_id` (string?), `status` (the slice's `execution_status`,
`not_started` when empty).

Item: `id`, `title`, `type` (`feature`, `bug` or `spike`).

Commit: `sha` (full), `subject`, `at` (RFC 3339, committer time).

PR: `number`, `title`, `state` (`OPEN`, `MERGED`, `CLOSED`), `url`, `checks` (`pass`, `fail`,
`pending`, `other`, `total`), `failing` (names of failing checks), `review` (GitHub review decision,
`""` when it could not be read), `mergeable` (`MERGEABLE`, `CONFLICTING`, `computing`, or `""`).

Agent:

| Key | Type | Meaning |
|-----|------|---------|
| `source` | string | `herdr`, `ledger`, or `herdr+ledger` when joined |
| `harness` | string | `claude`, `codex`, ... |
| `state` | string | herdr's `idle`, `working`, `blocked`, `done`, `unknown`; `claimed` for a ledger-only agent |
| `work_item` | string? | The work item it works on |
| `title` | string? | That item's title |
| `last_line` | string? | Last screen line (herdr), else the last output line of its session's newest run |
| `pane` | string? | herdr pane id |
| `session` | string? | Session holding the claim |
| `since` | RFC 3339? | When the claim started |

Run (the runledger record; see plan-56a4f0ad slice 1): `id`, `kind`, `description`, `command`,
`folder`, `status` (`running`, `completed`, `failed`, `killed`, `unknown`), `exit_code`,
`started_at`, `ended_at`, `commit`, `log_path`, `last_line`, `first_error`, `work_item`, `session`,
`harness`, and `line` (the one-line summary, e.g. `gate exit 0 at 16:31 on commit abc1234`).
Optional keys are left out when empty.

Need: `kind` (`blocked_agent`, `failed_run`, `lost_run`), `what` (one line), `since` (RFC 3339?),
`work_item` (string?).

The golden file `internal/statusall/testdata/report.golden.json` is a full example; a test fails
when a key in it is missing from this page.
