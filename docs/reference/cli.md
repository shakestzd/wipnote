# CLI Reference

All commands are invoked as `wipnote <command>`. Run `wipnote help --compact` for a compact summary, `wipnote --help` for all top-level commands, or `wipnote <command> --help` for detailed usage on any command.

---

## Work Items

Commands for managing core work items, architectural memory cards, and owner questions. Features, bugs, spikes, and tracks share the same lifecycle subcommands.

| Command | Description |
|---------|-------------|
| `feature [create\|show\|start\|complete\|list\|add-step\|complete-step\|link-commit\|move\|related\|reopen\|research\|reset\|set-description\|update\|delete]` | Feature work items |
| `bug [create\|show\|start\|complete\|list\|add-step\|complete-step\|link-commit\|move\|reopen\|research\|reset\|set-description\|update\|delete]` | Bug tracking |
| `spike [create\|show\|start\|complete\|list\|add-step\|complete-step\|link-commit\|move\|reopen\|research\|reset\|set-description\|update\|delete]` | Time-boxed investigation spikes |
| `track [create\|show\|start\|complete\|list\|add-step\|complete-step\|pr\|set-description\|status\|update\|delete]` | Multi-feature tracks (initiatives) |
| `arch [add\|bootstrap\|deprecate\|edit\|list\|repair\|resolve\|show\|validate\|verify]` | Architectural memory cards |
| `question [add\|answer\|assume\|list]` | Owner questions stored on features, bugs, and spikes |

### Common subcommands

| Subcommand | Usage | Description |
|------------|-------|-------------|
| `create` | `wipnote feature create "Title" --track <trk-id> --description "..."` | Create a new work item |
| `show` | `wipnote feature show <id>` | Display work item details |
| `start` | `wipnote feature start <id>` | Mark as in-progress and set as active |
| `complete` | `wipnote feature complete <id>` | Mark as done |
| `list` | `wipnote feature list [--status todo\|in-progress\|done]` | List work items with optional status filter |
| `add-step` | `wipnote feature add-step <id> "Step description"` | Add an implementation step |
| `delete` | `wipnote feature delete <id>` | Delete a work item |

!!! note "Required flags"
    `feature create` and `bug create` require `--track <trk-id>` and `--description "..."` (pass `--why "..."` and `--done-when "..."` as well; missing values warn).

!!! note "`completion_base` (config key)"
    `complete` needs a linked commit on the default branch. `.wipnote/config.json` key `completion_base` (env `WIPNOTE_COMPLETION_BASE` wins) sets which one: `upstream` (origin default only), `local` (local `main`/`master` or upstream; for projects that merge locally and never push), or `auto` (default: upstream, plus local `main`/`master` while it is ahead of upstream). Unknown values warn and use `auto`.

---

## Query, Lineage & Status

Commands for searching work items, tracing causal lineage, and checking project state.

| Command | Usage | Description |
|---------|-------|-------------|
| `lineage` | `wipnote lineage <id\|sha\|path>` | Walk the unified causal chain (forward + backward edges) for a work item, session, commit, or file |
| `trace` | `wipnote trace <id\|sha\|path>` | Reverse-trace a commit, file, or feature to its related work items and sessions |
| `history` | `wipnote history <id>` | Show the git commit history for a work-item HTML file |
| `blame` | `wipnote blame <path>` | Reverse-lookup which features and tracks touched a file |
| `code-areas` | `wipnote code-areas` | Aggregate file-to-track attribution across the codebase |
| `context-pack` | `wipnote context-pack <id>` | Emit a markdown briefing for a dispatched agent picking up a work item |
| `execute-preview` | `wipnote execute-preview <trk-id>` | Compute the ready set, file overlap, and per-task briefs for a track |
| `find` | `wipnote find <query>` | Query work items with filters |
| `relevant` | `wipnote relevant <path\|sha\|keyword>` | Find work items related to a file path, git SHA, or keyword |
| `search` | `wipnote search <pattern>` | AST-aware structural code search via `ast-grep` |
| `recap` | `wipnote recap [list\|show\|delete]` | Generate a grounded HTML recap artifact for a work item, git range, or session |
| `recommend` | `wipnote recommend` | Consolidated project health, WIP, bottlenecks, and recommendations |
| `snapshot` | `wipnote snapshot [--summary]` | Quick project overview with counts and details |
| `status` | `wipnote status [--all]` | Project status summary (`--all`: every registered project) |
| `wip` | `wipnote wip [show\|reset]` | Show or reset WIP limits and in-progress items |
| `who` | `wipnote who` | Show current session identity and claim attribution |
| `mcp` | `wipnote mcp` | Run the read-only wipnote MCP server on stdio |

---

## Planning

Commands for creating, critiquing, reviewing, and executing structured CRISPI plans.

| Command | Usage | Description |
|---------|-------|-------------|
| `plan create` | `wipnote plan create "Title" --track <trk-id>` | Create a new plan |
| `plan create-yaml` | `wipnote plan create-yaml "Title" --track <trk-id>` | Create a v2 YAML plan file |
| `plan show` | `wipnote plan show <id>` | Display plan details |
| `plan start` / `complete` | `wipnote plan start <id>` | Transition plan lifecycle |
| `plan list` / `list-yaml` | `wipnote plan list-yaml` | List plans (`list-yaml` sorts YAML plans by `created_at`) |
| `plan generate` | `wipnote plan generate <trk-id>` | Generate a CRISPI YAML plan for a track |
| `plan rewrite-yaml` | `wipnote plan rewrite-yaml <id> --file <path>` | Validated atomic update of plan YAML |
| `plan validate-yaml` | `wipnote plan validate-yaml <id>` | Validate a YAML plan's schema |
| `plan next` | `wipnote plan next <id>` | Inspect the next actionable step or slice in a plan |
| `plan slices` | `wipnote plan slices <id>` | Inspect slice status and review workflow steps |
| `plan critique` | `wipnote plan critique <id>` | Run or manage plan critique passes |
| `plan blocks` | `wipnote plan blocks` | Print the live visual block catalog for plans |
| `plan elicit-decisions` | `wipnote plan elicit-decisions <plan-id> <num>` | Elicit scope/decisions/context for a slice |
| `plan project-instructions` | `wipnote plan project-instructions <plan-id>` | Export plan context for Claude project instructions |

### v2 Slice Lifecycle

Commands for per-slice review and incremental promotion (v2 plans only).

| Command | Usage | Description |
|---------|-------|-------------|
| `plan approve-slice` | `wipnote plan approve-slice <plan-id> <num>` | Set `approval_status=approved` for a slice |
| `plan reject-slice` | `wipnote plan reject-slice <plan-id> <num> [--changes-requested]` | Set `approval_status=rejected` (or `changes_requested`) |
| `plan answer-slice-question` | `wipnote plan answer-slice-question <plan-id> <num> <question-id> <answer-key>` | Record answer to a slice-local question |
| `plan set-slice-status` | `wipnote plan set-slice-status <plan-id> <num> <status>` | Set execution status (`not_started\|promoted\|in_progress\|done\|blocked\|superseded`) |
| `plan promote-slice` | `wipnote plan promote-slice <plan-id> <num> [--waive-deps]` | Promote an approved slice to a feature work item |

---

## Specifications & Quality

Commands for feature specs, test generation, code review, CI health, and quality enforcement.

| Command | Usage | Description |
|---------|-------|-------------|
| `spec` | `wipnote spec [create\|generate\|show\|list\|start\|complete\|delete] <feature-id>` | Manage and generate feature specifications |
| `tdd` | `wipnote tdd <feature-id>` | Generate test stubs from spec acceptance criteria |
| `review` | `wipnote review` | Structured diff summary against base branch |
| `compliance` | `wipnote compliance [auto] <feature-id>` | Score implementation against spec |
| `check` | `wipnote check [--gate\|accepted-advisory\|cross-project\|dups\|host-paths\|incomplete\|orphans]` | Run automated quality gates and consistency checks |
| `ci` | `wipnote ci [init\|status\|watch]` | Pull request CI, review, and branch health |
| `doctor` | `wipnote doctor [--project-thread]` | Check a Claude project thread or local wipnote background processes |
| `guard` | `wipnote guard [init]` | Manage the project guard profile (`.wipnote/guard-profile.yaml`) |
| `health` | `wipnote health` | Code health metrics (module sizes, function lengths) |
| `prose` | `wipnote prose [check\|rules]` | Check text against plain-writing rules |
| `reconcile` | `wipnote reconcile` | Reconcile session-exit drift (uncommitted artifacts, port drift, orphans) |
| `research` | `wipnote research start <id>` | Research-first gate: start a research spike for a work item |

---

## Sessions & Observability

Commands for session management, analytics, and work item relationships.

| Command | Usage | Description |
|---------|-------|-------------|
| `session` | `wipnote session [list\|show\|start\|end\|ledger\|archive\|prune\|restore]` | Manage and inspect recorded sessions |
| `sessions resumable` | `wipnote sessions resumable` | List resumable prior sessions |
| `report` | `wipnote report` | Timeline of a session's activity |
| `analytics summary` | `wipnote analytics summary` | Work analytics overview |
| `analytics velocity` | `wipnote analytics velocity` | Development velocity insights |
| `link add` | `wipnote link add <from-id> <to-id> --type <type>` | Create a typed edge between work items |
| `link remove` | `wipnote link remove <from-id> <to-id>` | Remove an edge |
| `link list` | `wipnote link list <id>` | List edges for a work item |

---

## Data Management

Commands for store initialization, import/export, maintenance, and corpus verification.

| Command | Usage | Description |
|---------|-------|-------------|
| `init` | `wipnote init [--in-repo]` | Create a project's wipnote store (app-owned under `~/.wipnote/projects/` by default; `--in-repo` creates `.wipnote/` in the repo) |
| `archive` | `wipnote archive` | Compact old DONE work items into HTML table-ledger artifacts |
| `batch apply` / `export` | `wipnote batch apply <file.yaml>` | Bulk apply or export work items via YAML |
| `cache stats` / `prune` | `wipnote cache [stats\|prune]` | Report and reclaim leftover on-disk SQLite caches from older versions |
| `claims` | `wipnote claims [list\|who\|reconcile\|archive]` | Query and maintain the durable claim-episode ledger |
| `clean` / `cleanup` | `wipnote clean` / `wipnote cleanup orphan-sessions` | Clean stale artifacts while respecting HTML-canonical invariants |
| `commit-queue` | `wipnote commit-queue [status\|flush\|dead-letter]` | Serialized outbox for wipnote artifact commits |
| `ingest` | `wipnote ingest [gemini]` | Ingest Claude Code JSONL transcripts (or historical Gemini CLI transcripts) |
| `migrate` / `migrate-tracks` | `wipnote migrate [arch-cards\|normalize-paths\|restore-paths]` | One-shot data migrations and track attribution backfill |
| `prune` | `wipnote prune` | Rotate oversized logs and archive ingested session NDJSON |
| `registry` | `wipnote registry [prune]` | Manage the cross-project registry |
| `reindex` | `wipnote reindex [--verbose]` | Reparse every canonical `.wipnote/` artifact into an ephemeral in-memory SQLite projection to verify integrity and report counts (writes no DB file to disk) |
| `stale` | `wipnote stale` | List or mark open work items with no recent updates |
| `sync` | `wipnote sync` | Batch-commit dirty `.wipnote/` files in one commit |

---

## Harnesses, Delegation & Operations

Commands for launching harnesses, delegating across harnesses, building, and maintenance.

| Command | Usage | Description |
|---------|-------|-------------|
| `claude` | `wipnote claude [--dev] [--init] [--tmux] [--continue\|--resume <session-id>]` | Launch Claude Code with bundled plugin and mods |
| `dev` | `wipnote dev [--tmux] [--continue\|--resume <session-id>]` | Shortcut for `wipnote claude --dev` |
| `codex` | `wipnote codex [--init] [--dev] [--continue\|--resume <session-id>]` | Launch Codex CLI with bundled marketplace and agents |
| `antigravity` | `wipnote antigravity [--init] [--dev] [--tmux] [--continue\|--resume <id>]` | Launch Antigravity with bundled extension |
| `yolo` | `wipnote yolo --feature <id> [--track <id>] [--resume <session-id>]` | Autonomous dev mode with engineering guardrails |
| `delegate` | `wipnote delegate [run\|spawn\|send\|wait\|result\|questions\|inbox\|list\|tree\|view\|stop\|doctor\|profiles\|import-orch]` | Delegate one-shot or interactive work to Codex/Claude workers and record runs in `.wipnote/delegations-ledger.html` |
| `run` / `sh` | `wipnote run -- <cmd>` / `wipnote sh <cmd>` | Run a command recorded on the work item, or with verbose output compression |
| `build` | `wipnote build` | Rebuild the `wipnote` binary into `~/.local/bin/wipnote` and refresh bundled share trees |
| `plugin` | `wipnote plugin build-ports [--target claude\|codex\|antigravity]` | Regenerate plugin port trees from `packages/plugin-core/manifest.json` |
| `harness` | `wipnote harness list` | Inspect registered harness configurations |
| `shell-alias` | `wipnote shell-alias [--short --install\|--uninstall]` | Print or install shell aliases for wipnote launchers |
| `agent-init` | `wipnote agent-init` | Output shared agent context (safety, attribution, quality gates) |
| `statusline` | `wipnote statusline --session <id>` / `--cache` | Print the active work item (icon + title, with track progress) as one line for native `/statusline` |
| `worktree` | `wipnote worktree list [--json]` / `wipnote worktree clean [--yes] [--json]` | List the repo's worktrees with dirty and merged state (including squash merges); `clean` dry-runs by default and with `--yes` removes clean, merged worktrees and their local branches |
| `upgrade` / `update` | `wipnote upgrade [--check] [--version 0.95.0]` | Self-update CLI from GitHub releases |

!!! note "`launch.claude.autoCompactWindow` (config key)"
    `wipnote claude`, `yolo`, `dev` and `delegate spawn` Claude workers pass `--autocompact 300k` to Claude Code. Claude Code compacts 1M-context models at about 967K by default, and cost per turn grows with context. Set `launch.claude.autoCompactWindow` in `~/.wipnote/config.json` to another size (`"400k"` or `400000`, from 100k to 1M), or to `"auto"` or `"off"` to add no flag. An invalid value warns once and uses `300k`. wipnote adds nothing when you pass `--autocompact` yourself or set `CLAUDE_CODE_AUTO_COMPACT_WINDOW`, `DISABLE_AUTO_COMPACT` or `DISABLE_COMPACT`. Codex and Antigravity launches are unchanged.

---

## Work Item Types

| Type | Prefix | Purpose |
|------|--------|---------|
| Feature | `feat-` | Units of deliverable work |
| Bug | `bug-` | Defects to fix |
| Spike | `spk-` | Time-boxed investigations |
| Track | `trk-` | Initiatives grouping related work |
| Plan | `plan-` | CRISPI implementation plans |

---

## Deployment Profiles

wipnote launchers apply per-profile defaults based on the detected runtime environment.
Profiles are derived automatically — no manual configuration required for basic use.

| Profile | Runtime | Rollout Mode | Cleanup |
|---------|---------|--------------|---------|
| `host-production` | Host / bare metal | warn-only | manual |
| `devcontainer-dev` | VS Code devcontainer / Codespace | config-gated | auto-worktree |
| `local-plugin-dev` | Host + `--dev` / generated port | warn-only | manual |
| `ci-test` | CI / GitHub Actions | config-gated | auto-worktree |

**Staged rollout:** Host production stays warn-only until slice-9 migration tooling and runbooks
exist. Devcontainer and CI profiles are config-gated: isolation enforcement is off by default
and may be opted into via a config key. No profile flips to default-on in this release.

**Status output:** The `wipnote status` command reports fstype and journal-mode diagnostics
independently (plan-ae0c37b2 slice-4). Deployment profile information is a launcher concern
and does not duplicate that output.

---

## Launcher Doctor

`wipnote launcher doctor` diagnoses launcher/worktree health and prints
non-destructive remediation steps.

```bash
wipnote launcher doctor
```

**What it checks:**

| Check | Description |
|-------|-------------|
| Git state | Dirty main branch, ahead/behind origin |
| Managed worktrees | Stale entries whose directory has been removed |
| Session family | Legacy sessions (no family-id), canonical-root mismatch |
| Rollout gate | Whether `WIPNOTE_ENFORCE_ISOLATION` is active |

**Delegation:** The doctor reports that you should run
`wipnote cleanup orphan-sessions` and `wipnote reconcile` — it does NOT
run them automatically. This keeps it non-destructive by default.

**Legacy sessions:** Sessions created before `session_family_id` was
introduced are labeled **"legacy"** in the doctor output. They remain fully
visible to the CLI and require no migration.

**Rollout gate:** Host-production stays warn-only by default. To advance to
enforced isolation after doctor checks pass:

```bash
export WIPNOTE_ENFORCE_ISOLATION=true
```

See `docs/runbook/launcher-isolation.md` for the full step-by-step guide.
