# Guidance audit

Slice 5 of plan-2622b3f7 (feat-61944b76). Each non-blank line of the system prompt and of every file in `.claude/rules` has a verdict. Line numbers refer to the files before this slice.

Verdicts: **Enforced** means a hook, test or command checks it. **Prose only** means nothing checks it. **Duplicate** means the rule sits in more than one file.


## `cmd/wipnote/prompts/system-prompt.md`

| Line | Text | Verdict | Enforced by | Action |
|---|---|---|---|---|
| 1 | # wipnote | Prose only | none | Keep, shortened. States the core job (lineage). |
| 3 | This repo is tracked by wipnote. Its core job is **causal lineage**... | Prose only | none | Keep, shortened. States the core job (lineage). |
| 5 | ## Track the work | Enforced | PreToolUse claim guard (core/hooks/pretooluse_guards.go); `wipnote relevant` and `feature create` flags check input | Keep one line. |
| 7 | - Before changing code, start the matching work item: `wipnote feat... | Enforced | PreToolUse claim guard (core/hooks/pretooluse_guards.go); `wipnote relevant` and `feature create` flags check input | Keep one line. |
| 8 | - Name the item in every commit subject or a `Refs: <id>` trailer, ... | Prose only | none; commit trailers are read by lineage | Keep. Observed in lineage gaps (plan-2622b3f7). |
| 9 | - A code comment that cites an item id must also state the reason i... | Prose only | `wipnote prose check`; slice 6 style gate | Keep, example cut to one. |
| 10 | - When the work is done: `wipnote check --gate --work-item <id>`, t... | Enforced | `wipnote check --gate`, `feature complete` refusal | Keep one line. |
| 11 | - The `TaskCreated`/`TaskCompleted` hooks mirror your task list ont... | Prose only | The hooks mirror tasks, but nothing makes the agent use TaskCreate | Restored as one line (bug-cb72df80). |
| 13 | ## Delegate when it helps | Prose only | none | Merged into the one Delegate paragraph. |
| 15 | Do small, clear changes yourself. Use subagents when the work is pa... | Prose only | none | Merged into the one Delegate paragraph. |
| 17 | Inside herdr or the Herdr app, hand coding work to a `wipnote deleg... | Enforced | `wipnote delegate` help and spawn hints; denial of bare `codex exec`, `claude -p`, `ccx task` (slice 6) | Two paragraphs replaced by one pointer to `wipnote delegate --help`. |
| 19 | To hand work to another harness, use `wipnote delegate`. Do not cal... | Enforced | `wipnote delegate` help and spawn hints; denial of bare `codex exec`, `claude -p`, `ccx task` (slice 6) | Two paragraphs replaced by one pointer to `wipnote delegate --help`. |
| 21 | When an agent returns mid-step with no summary or commit SHA, it hi... | Prose only | none | Kept, shortened to two sentences. |
| 23 | If you stop making progress, stop and report what you know. | Prose only | none | Kept, shortened to two sentences. |
| 25 | ## Ask the owner with the question tool | Prose only | none; Stop hook nudges only after the fact (core/hooks/stop_question_nudge.go) | Rewritten as the B3 paragraph. It cites the open question records. |
| 27 | When you need a decision from the owner, ask with the harness quest... | Prose only | none; Stop hook nudges only after the fact (core/hooks/stop_question_nudge.go) | Rewritten as the B3 paragraph. It cites the open question records. |
| 29 | ## Write plainly | Enforced | `wipnote prose check` | Kept the limits. Dropped the rules the check does not test. |
| 31 | Sentences hold 25 words or fewer and steps 20 or fewer. Use active ... | Enforced | `wipnote prose check` | Kept the limits. Dropped the rules the check does not test. |
| 32 | Every sentence comes from the user, the branch or the code. Documen... | Prose only | none | Kept, shortened. |
| 33 | Run `wipnote prose check <file>` on a pull request body, issue or R... | Enforced | `wipnote prose check` | Kept. |
| 35 | ## Hard rules | Enforced | store guard (core/hooks/store_guard.go), `wipnote plan rewrite-yaml` | Kept one line. Store-path detail dropped. |
| 37 | - Never hand-edit `.wipnote/`; use the CLI. Plan YAML changes go th... | Enforced | store guard (core/hooks/store_guard.go), `wipnote plan rewrite-yaml` | Kept one line. Store-path detail dropped. |
| 38 | - Never write or overwrite a project's AGENTS.md, CLAUDE.md or GEMI... | Enforced | write guard (core/hooks/wipnote_write_guard_test.go) | Kept one line. |
| 39 | - Run the quality gate before committing (`wipnote check --gate` de... | Enforced | `wipnote check --gate` | Dropped. Same as the Track section. |
| 41 | `wipnote help --compact` prints the CLI. | Prose only | none | Kept. |

## `.claude/rules/code-hygiene.md`

| Line | Text | Verdict | Enforced by | Action |
|---|---|---|---|---|
| 1 | # Code Hygiene - Mandatory Quality Standards | Prose only | none | Kept. |
| 3 | **CRITICAL: Fix every gate failure in the files you changed, whoeve... | Prose only | none | Kept. |
| 5 | ## Philosophy | Prose only | none | Dropped. Philosophy text with no checkable rule. |
| 7 | Maintaining clean, error-free code is non-negotiable. Every commit ... | Prose only | none | Dropped. Philosophy text with no checkable rule. |
| 9 | ## Rules | Duplicate | `wipnote check --gate`; `deploy-all.sh` gates | Merged into five bullets. Rules 1 and 2 said the same. |
| 11 | 1. **Fix All Errors Before Committing** | Duplicate | `wipnote check --gate`; `deploy-all.sh` gates | Merged into five bullets. Rules 1 and 2 said the same. |
| 12 | - Run `go build`, `go vet`, and `go test` before every commit | Duplicate | `wipnote check --gate`; `deploy-all.sh` gates | Merged into five bullets. Rules 1 and 2 said the same. |
| 13 | - Fix failures in files you changed, even ones that were there befo... | Duplicate | `wipnote check --gate`; `deploy-all.sh` gates | Merged into five bullets. Rules 1 and 2 said the same. |
| 14 | - Report any other failure, and start no unrelated refactor | Duplicate | `wipnote check --gate`; `deploy-all.sh` gates | Merged into five bullets. Rules 1 and 2 said the same. |
| 15 | - Never commit with an unresolved failure in a file you changed | Duplicate | `wipnote check --gate`; `deploy-all.sh` gates | Merged into five bullets. Rules 1 and 2 said the same. |
| 17 | 2. **No "I'll Fix It Later" Mentality** | Duplicate | `wipnote check --gate`; `deploy-all.sh` gates | Merged into five bullets. Rules 1 and 2 said the same. |
| 18 | - Errors compound over time | Duplicate | `wipnote check --gate`; `deploy-all.sh` gates | Merged into five bullets. Rules 1 and 2 said the same. |
| 19 | - A failure in a file you changed is yours, whoever introduced it | Duplicate | `wipnote check --gate`; `deploy-all.sh` gates | Merged into five bullets. Rules 1 and 2 said the same. |
| 20 | - A failure elsewhere goes in your report, not in your diff | Duplicate | `wipnote check --gate`; `deploy-all.sh` gates | Merged into five bullets. Rules 1 and 2 said the same. |
| 21 | - Clean as you go - leave code better than you found it | Duplicate | `wipnote check --gate`; `deploy-all.sh` gates | Merged into five bullets. Rules 1 and 2 said the same. |
| 23 | 3. **Deployment Blockers** | Duplicate | `wipnote check --gate`; `deploy-all.sh` gates | Merged into five bullets. Rules 1 and 2 said the same. |
| 24 | - The `deploy-all.sh` script blocks on: | Duplicate | `wipnote check --gate`; `deploy-all.sh` gates | Merged into five bullets. Rules 1 and 2 said the same. |
| 25 | - Go build errors | Duplicate | `wipnote check --gate`; `deploy-all.sh` gates | Merged into five bullets. Rules 1 and 2 said the same. |
| 26 | - Go vet warnings | Duplicate | `wipnote check --gate`; `deploy-all.sh` gates | Merged into five bullets. Rules 1 and 2 said the same. |
| 27 | - Test failures | Duplicate | `wipnote check --gate`; `deploy-all.sh` gates | Merged into five bullets. Rules 1 and 2 said the same. |
| 28 | - This is intentional - maintain quality gates | Duplicate | `wipnote check --gate`; `deploy-all.sh` gates | Merged into five bullets. Rules 1 and 2 said the same. |
| 30 | 4. **Why This Matters** | Duplicate | `wipnote check --gate`; `deploy-all.sh` gates | Merged into five bullets. Rules 1 and 2 said the same. |
| 31 | - **Prevents Error Accumulation** - Small issues don't become large... | Duplicate | `wipnote check --gate`; `deploy-all.sh` gates | Merged into five bullets. Rules 1 and 2 said the same. |
| 32 | - **Better Code Hygiene** - Clean code is easier to maintain | Duplicate | `wipnote check --gate`; `deploy-all.sh` gates | Merged into five bullets. Rules 1 and 2 said the same. |
| 33 | - **Faster Development** - No time wasted debugging old errors | Duplicate | `wipnote check --gate`; `deploy-all.sh` gates | Merged into five bullets. Rules 1 and 2 said the same. |
| 34 | - **Professional Standards** - Production-grade code quality | Duplicate | `wipnote check --gate`; `deploy-all.sh` gates | Merged into five bullets. Rules 1 and 2 said the same. |
| 36 | ## Workflow | Prose only | none | Kept. The one copy of the Go gate command. |
| 39 | # Before every commit: | Prose only | none | Kept. The one copy of the Go gate command. |
| 40 | go build ./... && go vet ./... && go test ./... | Prose only | none | Kept. The one copy of the Go gate command. |
| 42 | # Commit when the checks pass for the files you changed | Prose only | none | Kept. The one copy of the Go gate command. |
| 43 | git commit -m "..." | Prose only | none | Kept. The one copy of the Go gate command. |
| 46 | **Remember: Fixing errors immediately is faster than letting them a... | Prose only | none | Kept. The one copy of the Go gate command. |
| 48 | ## Module Size & Complexity Standards | Enforced | `wipnote check --gate`, size constants in core/hooks/constants.go | Kept. |
| 50 | ### Size Review Triggers | Enforced | `wipnote check --gate`, size constants in core/hooks/constants.go | Kept. |
| 52 | \| Unit \| Review trigger \| | Enforced | `wipnote check --gate`, size constants in core/hooks/constants.go | Kept. |
| 53 | \|------\|----------------\| | Enforced | `wipnote check --gate`, size constants in core/hooks/constants.go | Kept. |
| 54 | \| File (module) \| >500 lines \| | Enforced | `wipnote check --gate`, size constants in core/hooks/constants.go | Kept. |
| 55 | \| Function \| >50 lines \| | Enforced | `wipnote check --gate`, size constants in core/hooks/constants.go | Kept. |
| 56 | \| Struct (class) \| >300 lines \| | Enforced | `wipnote check --gate`, size constants in core/hooks/constants.go | Kept. |
| 58 | These are review triggers, not measures. Crossing one means checkin... | Enforced | `wipnote check --gate`, size constants in core/hooks/constants.go | Kept. |
| 60 | ### Principles | Prose only | none | Kept. |
| 62 | 1. **Single Responsibility**: Each package should have one clear pu... | Prose only | none | Kept. |
| 63 | 2. **No Duplication**: Check `internal/` for shared utilities befor... | Prose only | none | Kept. |
| 64 | 3. **Prefer Existing Dependencies**: Check `go.mod` and stdlib befo... | Prose only | none | Kept. |
| 65 | 4. **Import Direction**: Dependencies flow one way (cmd -> internal... | Prose only | none | Kept. |

## `.claude/rules/debugging.md`

| Line | Text | Verdict | Enforced by | Action |
|---|---|---|---|---|
| 1 | --- | Prose only | none | Kept (front matter and principle). |
| 2 | paths: | Prose only | none | Kept (front matter and principle). |
| 3 | - "cmd/**" | Prose only | none | Kept (front matter and principle). |
| 4 | - "internal/**" | Prose only | none | Kept (front matter and principle). |
| 5 | - "plugin/**" | Prose only | none | Kept (front matter and principle). |
| 6 | --- | Prose only | none | Kept (front matter and principle). |
| 8 | # Debugging Workflow - Research First | Prose only | none | Kept (front matter and principle). |
| 10 | **Core principle: research first, implement second. NEVER guess; al... | Prose only | none | Kept (front matter and principle). |
| 12 | ## Debug Commands | Prose only | none | Dropped. Claude Code built-ins, no observed failure. |
| 15 | claude --debug <command>   # Verbose output | Prose only | none | Dropped. Claude Code built-ins, no observed failure. |
| 16 | /hooks                     # List all active hooks | Prose only | none | Dropped. Claude Code built-ins, no observed failure. |
| 17 | /doctor                    # System diagnostics | Prose only | none | Dropped. Claude Code built-ins, no observed failure. |
| 20 | ## Agents | Prose only | none | Kept. |
| 22 | - **researcher** — documentation research and root cause analysis (... | Prose only | none | Kept. |
| 24 | ## Workflow | Duplicate | none | Gate command replaced by a pointer to code-hygiene.md. |
| 26 | 1. Research (docs, agents) — form a hypothesis from evidence | Duplicate | none | Gate command replaced by a pointer to code-hygiene.md. |
| 27 | 2. Implement the minimal fix | Duplicate | none | Gate command replaced by a pointer to code-hygiene.md. |
| 28 | 3. Validate with `go build ./... && go vet ./... && go test ./...` | Duplicate | none | Gate command replaced by a pointer to code-hygiene.md. |
| 30 | For investigation, dispatch the `wipnote:researcher` agent with the... | Duplicate | none | Gate command replaced by a pointer to code-hygiene.md. |

## `.claude/rules/plugin-development.md`

| Line | Text | Verdict | Enforced by | Action |
|---|---|---|---|---|
| 1 | --- | Enforced | `wipnote plugin build-ports` drift check (port/pluginbuild) | Kept. |
| 2 | paths: | Enforced | `wipnote plugin build-ports` drift check (port/pluginbuild) | Kept. |
| 3 | - "plugin/**" | Enforced | `wipnote plugin build-ports` drift check (port/pluginbuild) | Kept. |
| 4 | - "packages/plugin-core/**" | Enforced | `wipnote plugin build-ports` drift check (port/pluginbuild) | Kept. |
| 5 | - "cmd/**" | Enforced | `wipnote plugin build-ports` drift check (port/pluginbuild) | Kept. |
| 6 | - "internal/**" | Enforced | `wipnote plugin build-ports` drift check (port/pluginbuild) | Kept. |
| 7 | --- | Enforced | `wipnote plugin build-ports` drift check (port/pluginbuild) | Kept. |
| 9 | # Plugin Development | Enforced | `wipnote plugin build-ports` drift check (port/pluginbuild) | Kept. |
| 11 | **Source of truth:** | Enforced | `wipnote plugin build-ports` drift check (port/pluginbuild) | Kept. |
| 13 | - `packages/plugin-core/manifest.json` — plugin metadata, per-targe... | Enforced | `wipnote plugin build-ports` drift check (port/pluginbuild) | Kept. |
| 14 | - `plugin/{commands,agents,skills,templates,static,config}/` — shar... | Enforced | `wipnote plugin build-ports` drift check (port/pluginbuild) | Kept. |
| 15 | - `cmd/` and `internal/` — Go CLI and hook handlers | Enforced | `wipnote plugin build-ports` drift check (port/pluginbuild) | Kept. |
| 17 | **Generated — DO NOT hand-edit (regenerate via `wipnote plugin buil... | Enforced | `wipnote plugin build-ports` drift check (port/pluginbuild) | Kept. |
| 19 | - `plugin/.claude-plugin/plugin.json` | Enforced | `wipnote plugin build-ports` drift check (port/pluginbuild) | Kept. |
| 20 | - `plugin/hooks/hooks.json` | Enforced | `wipnote plugin build-ports` drift check (port/pluginbuild) | Kept. |
| 21 | - everything under `port/packages/codex-marketplace/` | Enforced | `wipnote plugin build-ports` drift check (port/pluginbuild) | Kept. |
| 22 | - everything under `port/packages/antigravity-extension/` | Enforced | `wipnote plugin build-ports` drift check (port/pluginbuild) | Kept. |
| 23 | - everything under `.claude/` (auto-synced from `plugin/`) | Enforced | `wipnote plugin build-ports` drift check (port/pluginbuild) | Kept. |
| 25 | See `packages/plugin-core/README.md` for new-command / new-hook / n... | Enforced | `wipnote plugin build-ports` drift check (port/pluginbuild) | Kept. |
| 27 | ## Directory Structure (generated Claude tree) | Duplicate | none | Dropped. Repeats the source-of-truth list. |
| 29 | - `plugin/.claude-plugin/plugin.json` — manifest (generated) | Duplicate | none | Dropped. Repeats the source-of-truth list. |
| 30 | - `plugin/hooks/hooks.json` + `bin/wipnote` — Go binary hook handle... | Duplicate | none | Dropped. Repeats the source-of-truth list. |
| 31 | - `plugin/agents/` — markdown agent definitions (source asset, edit... | Duplicate | none | Dropped. Repeats the source-of-truth list. |
| 32 | - `plugin/skills/` — skill directories with SKILL.md (source asset,... | Duplicate | none | Dropped. Repeats the source-of-truth list. |
| 33 | - `plugin/commands/` — slash commands (source asset, edit directly) | Duplicate | none | Dropped. Repeats the source-of-truth list. |
| 34 | - `plugin/config/` — classification, drift, validation (source asse... | Duplicate | none | Dropped. Repeats the source-of-truth list. |
| 36 | **CRITICAL:** Don't put `commands/`, `agents/`, `skills/`, or `hook... | Duplicate | none | Dropped. Repeats the source-of-truth list. |
| 38 | ## Workflow | Duplicate | none | Kept. Gate command replaced by a pointer to code-hygiene.md. |
| 40 | 1. Edit shared source: `packages/plugin-core/manifest.json`, `plugi... | Duplicate | none | Kept. Gate command replaced by a pointer to code-hygiene.md. |
| 41 | 2. Regenerate target trees: `wipnote plugin build-ports` | Duplicate | none | Kept. Gate command replaced by a pointer to code-hygiene.md. |
| 42 | 3. Run: `go build ./... && go vet ./... && go test ./...` | Duplicate | none | Kept. Gate command replaced by a pointer to code-hygiene.md. |
| 43 | 4. Build: `wipnote build` | Duplicate | none | Kept. Gate command replaced by a pointer to code-hygiene.md. |
| 44 | 5. Test: `wipnote claude --dev` | Duplicate | none | Kept. Gate command replaced by a pointer to code-hygiene.md. |
| 45 | 6. Deploy: `./scripts/deploy-all.sh X.Y.Z --no-confirm` | Duplicate | none | Kept. Gate command replaced by a pointer to code-hygiene.md. |
| 47 | ## Rules | Duplicate | none | Cut to three bullets. The rest repeats the lists above. |
| 49 | - Edit `packages/plugin-core/manifest.json` (never the generated `p... | Duplicate | none | Cut to three bullets. The rest repeats the lists above. |
| 50 | - Edit Go source in `cmd/` or `internal/` for hook/CLI logic | Duplicate | none | Cut to three bullets. The rest repeats the lists above. |
| 51 | - Add agents to `plugin/agents/`, skills to `plugin/skills/`, comma... | Duplicate | none | Cut to three bullets. The rest repeats the lists above. |
| 52 | - Hooks receive CloudEvent JSON on stdin — process via Go binary | Duplicate | none | Cut to three bullets. The rest repeats the lists above. |
| 53 | - No stderr from hooks (causes "hook error" in Claude Code UI) | Duplicate | none | Cut to three bullets. The rest repeats the lists above. |
| 54 | - Return `{}` to allow, `{"decision":"block","reason":"..."}` to block | Duplicate | none | Cut to three bullets. The rest repeats the lists above. |
| 55 | - Prefer file/branch state over session state for hook gates (see "... | Duplicate | none | Cut to three bullets. The rest repeats the lists above. |
| 57 | ## Claude Code Plugin-Loaded Subagent Field Restrictions | Enforced | `agentFrontmatterFieldSpecs` (port/pluginbuild/agent_frontmatter.go) | Kept. |
| 59 | Per the [Claude Code subagent docs](https://code.claude.com/docs/en... | Enforced | `agentFrontmatterFieldSpecs` (port/pluginbuild/agent_frontmatter.go) | Kept. |
| 61 | **Workaround:** If you need any of these fields, define them at the... | Enforced | `agentFrontmatterFieldSpecs` (port/pluginbuild/agent_frontmatter.go) | Kept. |
| 63 | **Honored frontmatter fields for plugin-loaded subagents:** `name`,... | Enforced | `agentFrontmatterFieldSpecs` (port/pluginbuild/agent_frontmatter.go) | Kept. |
| 65 | `wipnote plugin build-ports` enforces per-harness frontmatter allow... | Enforced | `agentFrontmatterFieldSpecs` (port/pluginbuild/agent_frontmatter.go) | Kept. |
| 67 | - Claude: `name`, `description`, `model`, `effort`, `color`, `tools... | Enforced | `agentFrontmatterFieldSpecs` (port/pluginbuild/agent_frontmatter.go) | Kept. |
| 68 | - Codex: `name`, `description`, `model`, `tools`, `initialPrompt` | Enforced | `agentFrontmatterFieldSpecs` (port/pluginbuild/agent_frontmatter.go) | Kept. |
| 69 | - Antigravity: `name`, `description`, `model`, `tools`, `max_turns`... | Enforced | `agentFrontmatterFieldSpecs` (port/pluginbuild/agent_frontmatter.go) | Kept. |
| 71 | Keep shared agent source frontmatter in `plugin/agents/*.md` within... | Enforced | `agentFrontmatterFieldSpecs` (port/pluginbuild/agent_frontmatter.go) | Kept. |
| 73 | ## Hook Event Names Are Gated Per Harness | Enforced | `hookEventNameSpecs` hard-fail in build-ports | Kept. |
| 75 | No harness validates hook event names. Codex silently drops unrecog... | Enforced | `hookEventNameSpecs` hard-fail in build-ports | Kept. |
| 76 | dispatch; the Antigravity generator skips names it cannot translate... | Enforced | `hookEventNameSpecs` hard-fail in build-ports | Kept. |
| 77 | therefore produces a hook that never fires, forever, with no signal... | Enforced | `hookEventNameSpecs` hard-fail in build-ports | Kept. |
| 78 | bug-e39d408f, where wipnote registered three phantom Codex events a... | Enforced | `hookEventNameSpecs` hard-fail in build-ports | Kept. |
| 79 | consequently emitted no stop or session-end signal at all. | Enforced | `hookEventNameSpecs` hard-fail in build-ports | Kept. |
| 81 | **There is no programmatic backstop, and the allowlist cannot be ge... | Enforced | `hookEventNameSpecs` hard-fail in build-ports | Kept. |
| 82 | candidates were tested against Codex 0.147.0 and all three came bac... | Enforced | `hookEventNameSpecs` hard-fail in build-ports | Kept. |
| 83 | has no hooks diagnostic; unrecognized names are dropped silently at... | Enforced | `hookEventNameSpecs` hard-fail in build-ports | Kept. |
| 84 | `--strict-config` validates neither hook event names nor even unkno... | Enforced | `hookEventNameSpecs` hard-fail in build-ports | Kept. |
| 85 | supplied via file (verified with an isolated `CODEX_HOME` and a con... | Enforced | `hookEventNameSpecs` hard-fail in build-ports | Kept. |
| 86 | `codex --strict-config doctor` and `... exec` both parsed clean). T... | Enforced | `hookEventNameSpecs` hard-fail in build-ports | Kept. |
| 87 | hand-maintained and needs the same per-release re-verification disc... | Enforced | `hookEventNameSpecs` hard-fail in build-ports | Kept. |
| 88 | the upstream-harness monitoring in CLAUDE.md. Re-verify on a cadenc... | Enforced | `hookEventNameSpecs` hard-fail in build-ports | Kept. |
| 89 | the failure mode is silent, so nothing will prompt you. | Enforced | `hookEventNameSpecs` hard-fail in build-ports | Kept. |
| 91 | `wipnote plugin build-ports` now **hard-fails** on any `(event name... | Enforced | `hookEventNameSpecs` hard-fail in build-ports | Kept. |
| 92 | target harness does not dispatch. The source of truth is `hookEvent... | Enforced | `hookEventNameSpecs` hard-fail in build-ports | Kept. |
| 93 | `port/pluginbuild/hook_event_names.go`, which records each canonica... | Enforced | `hookEventNameSpecs` hard-fail in build-ports | Kept. |
| 94 | that dispatch it, the harness-native spelling where it differs, and... | Enforced | `hookEventNameSpecs` hard-fail in build-ports | Kept. |
| 95 | verification. Verified vocabularies: | Enforced | `hookEventNameSpecs` hard-fail in build-ports | Kept. |
| 97 | - **Claude**: `SessionStart`, `SessionEnd`, `UserPromptSubmit`, `Pr... | Enforced | `hookEventNameSpecs` hard-fail in build-ports | Kept. |
| 98 | `PostToolUseFailure`, `Stop`, `SubagentStart`, `SubagentStop`, `Pre... | Enforced | `hookEventNameSpecs` hard-fail in build-ports | Kept. |
| 99 | `PermissionRequest`, `TeammateIdle`, `TaskCreated`, `TaskCompleted`... | Enforced | `hookEventNameSpecs` hard-fail in build-ports | Kept. |
| 100 | `ConfigChange`, `WorktreeCreate`, `WorktreeRemove` | Enforced | `hookEventNameSpecs` hard-fail in build-ports | Kept. |
| 101 | - **Codex** (0.163.0 — exactly 12): `SessionStart`, `SessionEnd`, `... | Enforced | `hookEventNameSpecs` hard-fail in build-ports | Kept. |
| 102 | `PreToolUse`, `PostToolUse`, `PermissionRequest`, `PreCompact`, `Po... | Enforced | `hookEventNameSpecs` hard-fail in build-ports | Kept. |
| 103 | `SubagentStart`, `SubagentStop`, `Stop`, `Interrupt` (added in 0.15... | Enforced | `hookEventNameSpecs` hard-fail in build-ports | Kept. |
| 104 | - **Antigravity** (agy v1.0.8 — 5, translated on emit): `UserPrompt... | Enforced | `hookEventNameSpecs` hard-fail in build-ports | Kept. |
| 105 | `AfterAgent`→`PostInvocation`, `PreToolUse`, `PostToolUse`, `Stop` | Enforced | `hookEventNameSpecs` hard-fail in build-ports | Kept. |
| 107 | Registering a name for only some targets is normal and never an err... | Enforced | `hookEventNameSpecs` hard-fail in build-ports | Kept. |
| 108 | `targets` is for. The gate fires only when a name is registered aga... | Enforced | `hookEventNameSpecs` hard-fail in build-ports | Kept. |
| 109 | cannot dispatch it. To add a harness or an event, add a verified en... | Enforced | `hookEventNameSpecs` hard-fail in build-ports | Kept. |
| 110 | never add one from an unverified source, since an unverifiable name... | Enforced | `hookEventNameSpecs` hard-fail in build-ports | Kept. |
| 111 | failure the gate exists to prevent. | Enforced | `hookEventNameSpecs` hard-fail in build-ports | Kept. |
| 113 | ## Hook State: Prefer File/Branch State Over Session State | Prose only | none; bug-a10ae96a and GH#35 show the failure | Kept. |
| 115 | **Rule:** Hooks should answer questions from durable state (files, ... | Prose only | none; bug-a10ae96a and GH#35 show the failure | Kept. |
| 116 | rather than session state (session_id, tool-name history, task IDs)... | Prose only | none; bug-a10ae96a and GH#35 show the failure | Kept. |
| 117 | brittle — tool names change, sessions rotate, and substring matches... | Prose only | none; bug-a10ae96a and GH#35 show the failure | Kept. |
| 119 | **Why it matters.** Two bugs shared the same root cause shape: | Prose only | none; bug-a10ae96a and GH#35 show the failure | Kept. |
| 121 | - GH#35 asked "is this session the claimant?" — the right question ... | Prose only | none; bug-a10ae96a and GH#35 show the failure | Kept. |
| 122 | correspond to a valid claim?" Checking session_id against a claims ... | Prose only | none; bug-a10ae96a and GH#35 show the failure | Kept. |
| 123 | blocks when sessions rotated (e.g. after reconnect). | Prose only | none; bug-a10ae96a and GH#35 show the failure | Kept. |
| 124 | - GH#36 (bug-a10ae96a, fixed in commit 88d2f51b4) asked "did someon... | Prose only | none; bug-a10ae96a and GH#35 show the failure | Kept. |
| 125 | `%screenshot%`?" — the right question was "does the staged diff con... | Prose only | none; bug-a10ae96a and GH#35 show the failure | Kept. |
| 126 | LIKE match never fired because the Chrome MCP tool name didn't cont... | Prose only | none; bug-a10ae96a and GH#35 show the failure | Kept. |
| 127 | Moving the gate to `git diff --cached --name-only` fixed it cleanly. | Prose only | none; bug-a10ae96a and GH#35 show the failure | Kept. |
| 129 | Both bugs produced unsatisfiable checks or false positives on unrel... | Prose only | none; bug-a10ae96a and GH#35 show the failure | Kept. |
| 130 | is invisible failures: the guard either never fires (permissive) or... | Prose only | none; bug-a10ae96a and GH#35 show the failure | Kept. |
| 131 | trigger (noisy). | Prose only | none; bug-a10ae96a and GH#35 show the failure | Kept. |
| 133 | **How to apply:** | Prose only | none; bug-a10ae96a and GH#35 show the failure | Kept. |
| 135 | 1. Before adding a hook, write down the question it answers. If the... | Prose only | none; bug-a10ae96a and GH#35 show the failure | Kept. |
| 136 | `git diff --cached`, the branch name, the working tree, or config f... | Prose only | none; bug-a10ae96a and GH#35 show the failure | Kept. |
| 137 | Don't approximate it from session history. | Prose only | none; bug-a10ae96a and GH#35 show the failure | Kept. |
| 138 | 2. Session state is fine for *attribution* and *telemetry*, not for... | Prose only | none; bug-a10ae96a and GH#35 show the failure | Kept. |
| 139 | be reproducible across session restarts; telemetry just needs to be... | Prose only | none; bug-a10ae96a and GH#35 show the failure | Kept. |
| 140 | 3. When blocking a commit, first check the staged diff. Short-circu... | Prose only | none; bug-a10ae96a and GH#35 show the failure | Kept. |
| 141 | relevant files are staged — see `hasStagedUIFiles()` in `internal/h... | Prose only | none; bug-a10ae96a and GH#35 show the failure | Kept. |
| 142 | and commit `88d2f51b4` as the canonical pattern. | Prose only | none; bug-a10ae96a and GH#35 show the failure | Kept. |
| 143 | 4. Anchor Bash command matching to the specific command shape, not ... | Prose only | none; bug-a10ae96a and GH#35 show the failure | Kept. |
| 144 | Use `^\s*git\s+commit(\s\|$\|--\|-[^a-z])`, not `LIKE '%commit%'`. | Prose only | none; bug-a10ae96a and GH#35 show the failure | Kept. |
| 146 | ## Failure Bar for New Guidance | Enforced | `port/pluginbuild/guidance_size_test.go` | Kept. |
| 148 | - Add a line to a skill, prompt or rule only if it names the work i... | Enforced | `port/pluginbuild/guidance_size_test.go` | Kept. |
| 149 | - Each skill and prompt has a byte limit in `port/pluginbuild/guida... | Enforced | `port/pluginbuild/guidance_size_test.go` | Kept. |

## `.claude/rules/deployment.md`

| Line | Text | Verdict | Enforced by | Action |
|---|---|---|---|---|
| 1 | --- | Enforced | `scripts/deploy-all.sh`, `verify-versions.sh` | Kept. Not in this slice. Slice 12 may review it. |
| 2 | paths: | Enforced | `scripts/deploy-all.sh`, `verify-versions.sh` | Kept. Not in this slice. Slice 12 may review it. |
| 3 | - "scripts/**" | Enforced | `scripts/deploy-all.sh`, `verify-versions.sh` | Kept. Not in this slice. Slice 12 may review it. |
| 4 | - "plugin/.claude-plugin/**" | Enforced | `scripts/deploy-all.sh`, `verify-versions.sh` | Kept. Not in this slice. Slice 12 may review it. |
| 5 | - ".goreleaser.yml" | Enforced | `scripts/deploy-all.sh`, `verify-versions.sh` | Kept. Not in this slice. Slice 12 may review it. |
| 6 | --- | Enforced | `scripts/deploy-all.sh`, `verify-versions.sh` | Kept. Not in this slice. Slice 12 may review it. |
| 8 | # Deployment & Release Rules | Enforced | `scripts/deploy-all.sh`, `verify-versions.sh` | Kept. Not in this slice. Slice 12 may review it. |
| 10 | **CRITICAL: Use `./scripts/deploy-all.sh` or `/deploy` for all depl... | Enforced | `scripts/deploy-all.sh`, `verify-versions.sh` | Kept. Not in this slice. Slice 12 may review it. |
| 12 | ## Two Independent Install Paths | Enforced | `scripts/deploy-all.sh`, `verify-versions.sh` | Kept. Not in this slice. Slice 12 may review it. |
| 14 | Wipnote has **two components** that must stay in sync after every r... | Enforced | `scripts/deploy-all.sh`, `verify-versions.sh` | Kept. Not in this slice. Slice 12 may review it. |
| 16 | \| Component \| Location \| Mechanism \| | Enforced | `scripts/deploy-all.sh`, `verify-versions.sh` | Kept. Not in this slice. Slice 12 may review it. |
| 17 | \|-----------\|----------\|-----------\| | Enforced | `scripts/deploy-all.sh`, `verify-versions.sh` | Kept. Not in this slice. Slice 12 may review it. |
| 18 | \| **CLI binary** \| `~/.local/bin/wipnote` (copy) \| `plugin/build... | Enforced | `scripts/deploy-all.sh`, `verify-versions.sh` | Kept. Not in this slice. Slice 12 may review it. |
| 19 | \| **Plugin** \| `~/.claude/plugins/cache/wipnote/wipnote/<ver>/` \... | Enforced | `scripts/deploy-all.sh`, `verify-versions.sh` | Kept. Not in this slice. Slice 12 may review it. |
| 21 | The deploy script updates both automatically. **Never update one wi... | Enforced | `scripts/deploy-all.sh`, `verify-versions.sh` | Kept. Not in this slice. Slice 12 may review it. |
| 23 | ### Why Reinstall (Not Update) | Enforced | `scripts/deploy-all.sh`, `verify-versions.sh` | Kept. Not in this slice. Slice 12 may review it. |
| 25 | `claude plugin update` preserves the old `gitCommitSha` in `install... | Enforced | `scripts/deploy-all.sh`, `verify-versions.sh` | Kept. Not in this slice. Slice 12 may review it. |
| 27 | ### Why Copy (Not Symlink) for CLI | Enforced | `scripts/deploy-all.sh`, `verify-versions.sh` | Kept. Not in this slice. Slice 12 may review it. |
| 29 | `build.sh` copies the binary to `~/.local/bin/wipnote`. A symlink w... | Enforced | `scripts/deploy-all.sh`, `verify-versions.sh` | Kept. Not in this slice. Slice 12 may review it. |
| 31 | ## Using the Deployment Script | Enforced | `scripts/deploy-all.sh`, `verify-versions.sh` | Kept. Not in this slice. Slice 12 may review it. |
| 34 | # Recommended: one command, fully automated | Enforced | `scripts/deploy-all.sh`, `verify-versions.sh` | Kept. Not in this slice. Slice 12 may review it. |
| 35 | ./scripts/deploy-all.sh 1.0.0 --no-confirm | Enforced | `scripts/deploy-all.sh`, `verify-versions.sh` | Kept. Not in this slice. Slice 12 may review it. |
| 37 | # Or via slash command: | Enforced | `scripts/deploy-all.sh`, `verify-versions.sh` | Kept. Not in this slice. Slice 12 may review it. |
| 38 | /deploy 1.0.0 | Enforced | `scripts/deploy-all.sh`, `verify-versions.sh` | Kept. Not in this slice. Slice 12 may review it. |
| 41 | **Pre-requisite:** `go test ./...` must pass. | Enforced | `scripts/deploy-all.sh`, `verify-versions.sh` | Kept. Not in this slice. Slice 12 may review it. |
| 43 | **What the Script Does:** | Enforced | `scripts/deploy-all.sh`, `verify-versions.sh` | Kept. Not in this slice. Slice 12 may review it. |
| 44 | 1. **Quality gates** — `go build`, `go vet`, `go test` | Enforced | `scripts/deploy-all.sh`, `verify-versions.sh` | Kept. Not in this slice. Slice 12 may review it. |
| 45 | 2. **Version bump** — Updates `plugin/.claude-plugin/plugin.json` | Enforced | `scripts/deploy-all.sh`, `verify-versions.sh` | Kept. Not in this slice. Slice 12 may review it. |
| 46 | 3. **Git push** — Commits version change, tags, pushes to origin/main | Enforced | `scripts/deploy-all.sh`, `verify-versions.sh` | Kept. Not in this slice. Slice 12 may review it. |
| 47 | 4. **GitHub Release** — Tag triggers GoReleaser via GitHub Actions | Enforced | `scripts/deploy-all.sh`, `verify-versions.sh` | Kept. Not in this slice. Slice 12 may review it. |
| 48 | 5. **Marketplace pull** — `git pull` on `~/.claude/plugins/marketpl... | Enforced | `scripts/deploy-all.sh`, `verify-versions.sh` | Kept. Not in this slice. Slice 12 may review it. |
| 49 | 6. **Plugin reinstall** — `claude plugin uninstall` + `install` (cl... | Enforced | `scripts/deploy-all.sh`, `verify-versions.sh` | Kept. Not in this slice. Slice 12 may review it. |
| 50 | 7. **CLI rebuild** — `plugin/build.sh` copies binary to `~/.local/b... | Enforced | `scripts/deploy-all.sh`, `verify-versions.sh` | Kept. Not in this slice. Slice 12 may review it. |
| 52 | **Available Flags:** | Enforced | `scripts/deploy-all.sh`, `verify-versions.sh` | Kept. Not in this slice. Slice 12 may review it. |
| 53 | - `--no-confirm` — Skip confirmation prompts (recommended for AI) | Enforced | `scripts/deploy-all.sh`, `verify-versions.sh` | Kept. Not in this slice. Slice 12 may review it. |
| 54 | - `--docs-only` — Only commit and push (skip tag/release) | Enforced | `scripts/deploy-all.sh`, `verify-versions.sh` | Kept. Not in this slice. Slice 12 may review it. |
| 55 | - `--build-only` — Only run quality gates | Enforced | `scripts/deploy-all.sh`, `verify-versions.sh` | Kept. Not in this slice. Slice 12 may review it. |
| 56 | - `--dry-run` — Show what would happen without executing | Enforced | `scripts/deploy-all.sh`, `verify-versions.sh` | Kept. Not in this slice. Slice 12 may review it. |
| 58 | ## Version Numbering | Enforced | `scripts/deploy-all.sh`, `verify-versions.sh` | Kept. Not in this slice. Slice 12 may review it. |
| 60 | Semantic Versioning: **MAJOR.MINOR.PATCH** | Enforced | `scripts/deploy-all.sh`, `verify-versions.sh` | Kept. Not in this slice. Slice 12 may review it. |
| 61 | - Patch (X.Y.Z+1): Bug fixes | Enforced | `scripts/deploy-all.sh`, `verify-versions.sh` | Kept. Not in this slice. Slice 12 may review it. |
| 62 | - Minor (X.Y+1.0): New features (backward compatible) | Enforced | `scripts/deploy-all.sh`, `verify-versions.sh` | Kept. Not in this slice. Slice 12 may review it. |
| 63 | - Major (X+1.0.0): Breaking changes | Enforced | `scripts/deploy-all.sh`, `verify-versions.sh` | Kept. Not in this slice. Slice 12 may review it. |
| 65 | **Version file:** `plugin/.claude-plugin/plugin.json` | Enforced | `scripts/deploy-all.sh`, `verify-versions.sh` | Kept. Not in this slice. Slice 12 may review it. |
| 67 | ## Post-Deployment Verification | Enforced | `scripts/deploy-all.sh`, `verify-versions.sh` | Kept. Not in this slice. Slice 12 may review it. |
| 70 | # CI pipelines | Enforced | `scripts/deploy-all.sh`, `verify-versions.sh` | Kept. Not in this slice. Slice 12 may review it. |
| 71 | gh run list --workflow=ci.yml --limit 1 | Enforced | `scripts/deploy-all.sh`, `verify-versions.sh` | Kept. Not in this slice. Slice 12 may review it. |
| 72 | gh run list --workflow=release-go.yml --limit 1 | Enforced | `scripts/deploy-all.sh`, `verify-versions.sh` | Kept. Not in this slice. Slice 12 may review it. |
| 74 | # Installed versions match | Enforced | `scripts/deploy-all.sh`, `verify-versions.sh` | Kept. Not in this slice. Slice 12 may review it. |
| 75 | grep '"version"' plugin/.claude-plugin/plugin.json | Enforced | `scripts/deploy-all.sh`, `verify-versions.sh` | Kept. Not in this slice. Slice 12 may review it. |
| 76 | cat ~/.local/share/wipnote/.binary-version | Enforced | `scripts/deploy-all.sh`, `verify-versions.sh` | Kept. Not in this slice. Slice 12 may review it. |
| 79 | ## Rollback | Enforced | `scripts/deploy-all.sh`, `verify-versions.sh` | Kept. Not in this slice. Slice 12 may review it. |
| 81 | 1. Bump to next patch version with the fix | Enforced | `scripts/deploy-all.sh`, `verify-versions.sh` | Kept. Not in this slice. Slice 12 may review it. |
| 82 | 2. Or delete the release: `gh release delete v1.0.0` (if caught qui... | Enforced | `scripts/deploy-all.sh`, `verify-versions.sh` | Kept. Not in this slice. Slice 12 may review it. |
| 83 | 3. Re-deploy the corrected version | Enforced | `scripts/deploy-all.sh`, `verify-versions.sh` | Kept. Not in this slice. Slice 12 may review it. |

## `.claude/rules/version-sync.md`

| Line | Text | Verdict | Enforced by | Action |
|---|---|---|---|---|
| 1 | --- | Enforced | `scripts/verify-versions.sh` | Kept. Not in this slice. Slice 12 may review it. |
| 2 | paths: | Enforced | `scripts/verify-versions.sh` | Kept. Not in this slice. Slice 12 may review it. |
| 3 | - "scripts/**" | Enforced | `scripts/verify-versions.sh` | Kept. Not in this slice. Slice 12 may review it. |
| 4 | - "plugin/.claude-plugin/**" | Enforced | `scripts/verify-versions.sh` | Kept. Not in this slice. Slice 12 may review it. |
| 5 | --- | Enforced | `scripts/verify-versions.sh` | Kept. Not in this slice. Slice 12 may review it. |
| 7 | # Version Synchronization Rules | Enforced | `scripts/verify-versions.sh` | Kept. Not in this slice. Slice 12 may review it. |
| 9 | **CRITICAL: Plugin version and GitHub Release version MUST always m... | Enforced | `scripts/verify-versions.sh` | Kept. Not in this slice. Slice 12 may review it. |
| 11 | ## Why This Matters | Enforced | `scripts/verify-versions.sh` | Kept. Not in this slice. Slice 12 may review it. |
| 13 | The Go binary is distributed via GitHub Releases. The plugin bootst... | Enforced | `scripts/verify-versions.sh` | Kept. Not in this slice. Slice 12 may review it. |
| 15 | ## Version Files (Must All Match) | Enforced | `scripts/verify-versions.sh` | Kept. Not in this slice. Slice 12 may review it. |
| 18 | plugin/.claude-plugin/plugin.json:                "version": "X.Y.Z" | Enforced | `scripts/verify-versions.sh` | Kept. Not in this slice. Slice 12 may review it. |
| 21 | ## For Claude: Always Check Version | Enforced | `scripts/verify-versions.sh` | Kept. Not in this slice. Slice 12 may review it. |
| 24 | # Check local plugin version | Enforced | `scripts/verify-versions.sh` | Kept. Not in this slice. Slice 12 may review it. |
| 25 | grep '"version"' plugin/.claude-plugin/plugin.json | Enforced | `scripts/verify-versions.sh` | Kept. Not in this slice. Slice 12 may review it. |
| 27 | # Check latest GitHub Release | Enforced | `scripts/verify-versions.sh` | Kept. Not in this slice. Slice 12 may review it. |
| 28 | gh release view --json tagName -q .tagName 2>/dev/null \|\| echo "N... | Enforced | `scripts/verify-versions.sh` | Kept. Not in this slice. Slice 12 may review it. |
| 31 | ## Publishing Requirements | Enforced | `scripts/verify-versions.sh` | Kept. Not in this slice. Slice 12 may review it. |
| 33 | **Before running `./scripts/deploy-all.sh VERSION`:** | Enforced | `scripts/verify-versions.sh` | Kept. Not in this slice. Slice 12 may review it. |
| 35 | 1. Verify all version files match target version | Enforced | `scripts/verify-versions.sh` | Kept. Not in this slice. Slice 12 may review it. |
| 36 | 2. Run: `./scripts/verify-versions.sh X.Y.Z` | Enforced | `scripts/verify-versions.sh` | Kept. Not in this slice. Slice 12 may review it. |
| 37 | 3. Confirm all files were updated | Enforced | `scripts/verify-versions.sh` | Kept. Not in this slice. Slice 12 may review it. |
| 39 | **The publishing script will:** | Enforced | `scripts/verify-versions.sh` | Kept. Not in this slice. Slice 12 may review it. |
| 40 | - FAIL if versions are inconsistent | Enforced | `scripts/verify-versions.sh` | Kept. Not in this slice. Slice 12 may review it. |
| 41 | - Automatically sync all version files | Enforced | `scripts/verify-versions.sh` | Kept. Not in this slice. Slice 12 may review it. |
| 43 | ## Version Update Workflow | Enforced | `scripts/verify-versions.sh` | Kept. Not in this slice. Slice 12 may review it. |
| 45 | When deploying new version: | Enforced | `scripts/verify-versions.sh` | Kept. Not in this slice. Slice 12 may review it. |
| 48 | # 1. Update version files | Enforced | `scripts/verify-versions.sh` | Kept. Not in this slice. Slice 12 may review it. |
| 49 | # 2. Run verification | Enforced | `scripts/verify-versions.sh` | Kept. Not in this slice. Slice 12 may review it. |
| 50 | ./scripts/verify-versions.sh 1.0.0 | Enforced | `scripts/verify-versions.sh` | Kept. Not in this slice. Slice 12 may review it. |
| 52 | # 3. Deploy | Enforced | `scripts/verify-versions.sh` | Kept. Not in this slice. Slice 12 may review it. |
| 53 | ./scripts/deploy-all.sh 1.0.0 --no-confirm | Enforced | `scripts/verify-versions.sh` | Kept. Not in this slice. Slice 12 may review it. |
| 56 | ## For AI Agents | Enforced | `scripts/verify-versions.sh` | Kept. Not in this slice. Slice 12 may review it. |
| 58 | **DIRECTIVE**: When discussing version numbers: | Enforced | `scripts/verify-versions.sh` | Kept. Not in this slice. Slice 12 may review it. |
| 59 | 1. Check plugin.json for the current version | Enforced | `scripts/verify-versions.sh` | Kept. Not in this slice. Slice 12 may review it. |
| 60 | 2. Verify version files are consistent | Enforced | `scripts/verify-versions.sh` | Kept. Not in this slice. Slice 12 may review it. |
| 61 | 3. Recommend running `./scripts/verify-versions.sh` before deployment | Enforced | `scripts/verify-versions.sh` | Kept. Not in this slice. Slice 12 may review it. |

## `CLAUDE.md` (for slice 12)

Not edited here. The owner changes this file in slice 12. Verdicts are proposals.

| Line | Text | Verdict | Enforced by | Action |
|---|---|---|---|---|
| 1 | @AGENTS.md | Prose only | none | Keep. |
| 3 | # wipnote — Claude Code | Prose only | none | Keep. |
| 5 | Local-first observability and coordination platform for AI-assisted... | Prose only | none | Keep. |
| 7 | --- | Prose only | none | Keep. |
| 9 | ## Build | Enforced | `wipnote check --gate`; build and gate commands also in code-hygiene.md | Slice 12: keep one pointer. |
| 11 | **Always use `wipnote build`, never `go build` directly.** | Enforced | `wipnote check --gate`; build and gate commands also in code-hygiene.md | Slice 12: keep one pointer. |
| 14 | wipnote build      # outputs to ~/.local/bin/wipnote (on your PATH) | Enforced | `wipnote check --gate`; build and gate commands also in code-hygiene.md | Slice 12: keep one pointer. |
| 17 | Running `go build -o wipnote ./cmd/wipnote/` puts the binary in CWD... | Enforced | `wipnote check --gate`; build and gate commands also in code-hygiene.md | Slice 12: keep one pointer. |
| 19 | --- | Enforced | `wipnote check --gate`; build and gate commands also in code-hygiene.md | Slice 12: keep one pointer. |
| 21 | ## Quality Gates | Enforced | `wipnote check --gate`; build and gate commands also in code-hygiene.md | Slice 12: keep one pointer. |
| 24 | go build ./... && go vet ./... && go test ./... | Enforced | `wipnote check --gate`; build and gate commands also in code-hygiene.md | Slice 12: keep one pointer. |
| 25 | # Commit only when ALL pass | Enforced | `wipnote check --gate`; build and gate commands also in code-hygiene.md | Slice 12: keep one pointer. |
| 28 | **For anything but a quick cached rerun, use `scripts/go-test-strea... | Enforced | `wipnote check --gate`; build and gate commands also in code-hygiene.md | Slice 12: keep one pointer. |
| 30 | Use `/wipnote:code-quality-skill` for the complete pre-commit workf... | Enforced | `wipnote check --gate`; build and gate commands also in code-hygiene.md | Slice 12: keep one pointer. |
| 32 | --- | Enforced | `wipnote check --gate`; build and gate commands also in code-hygiene.md | Slice 12: keep one pointer. |
| 34 | ## Deploy | Enforced | `scripts/deploy-all.sh`; also deployment.md | Slice 12: drop. Duplicate of deployment.md. |
| 37 | ./scripts/deploy-all.sh X.Y.Z --no-confirm   # full pipeline | Enforced | `scripts/deploy-all.sh`; also deployment.md | Slice 12: drop. Duplicate of deployment.md. |
| 40 | Or `/deploy X.Y.Z` (a repo-local skill in `.claude/skills/deploy/`)... | Enforced | `scripts/deploy-all.sh`; also deployment.md | Slice 12: drop. Duplicate of deployment.md. |
| 42 | ## Launching Claude with wipnote | Prose only | none | Slice 12: review. Launch and dev-mode recipes, no failure cited. |
| 45 | wipnote claude   # uses the bundled plugin in ~/.local/share/wipnot... | Prose only | none | Slice 12: review. Launch and dev-mode recipes, no failure cited. |
| 48 | The launcher resolves the plugin tree (env override → `~/.local/sha... | Prose only | none | Slice 12: review. Launch and dev-mode recipes, no failure cited. |
| 50 | **Mods load with it.** The launcher also passes `--plugin-dir <mods... | Prose only | none | Slice 12: review. Launch and dev-mode recipes, no failure cited. |
| 52 | For users who want bare `claude` to route through wipnote globally,... | Prose only | none | Slice 12: review. Launch and dev-mode recipes, no failure cited. |
| 54 | --- | Prose only | none | Slice 12: review. Launch and dev-mode recipes, no failure cited. |
| 56 | ## Launching Codex with wipnote | Prose only | none | Slice 12: review. Launch and dev-mode recipes, no failure cited. |
| 59 | wipnote codex   # launches Codex CLI with wipnote plugin | Prose only | none | Slice 12: review. Launch and dev-mode recipes, no failure cited. |
| 62 | Codex hooks (such as `SessionStart`, `UserPromptSubmit`, etc.) are ... | Prose only | none | Slice 12: review. Launch and dev-mode recipes, no failure cited. |
| 64 | --- | Prose only | none | Slice 12: review. Launch and dev-mode recipes, no failure cited. |
| 66 | ## Dev Mode | Prose only | none | Slice 12: review. Launch and dev-mode recipes, no failure cited. |
| 69 | wipnote claude --dev   # loads plugin from in-tree plugin/ via --pl... | Prose only | none | Slice 12: review. Launch and dev-mode recipes, no failure cited. |
| 72 | Dev mode bypasses the bundled tree resolver and instead points Clau... | Prose only | none | Slice 12: review. Launch and dev-mode recipes, no failure cited. |
| 74 | **Why full removal of legacy installs is required:** Disabling a le... | Prose only | none | Slice 12: review. Launch and dev-mode recipes, no failure cited. |
| 76 | ## Resuming a Specific Session | Prose only | none | Slice 12: review. Launch and dev-mode recipes, no failure cited. |
| 78 | `wipnote claude`, `wipnote yolo`, and `wipnote dev` all accept `--r... | Prose only | none | Slice 12: review. Launch and dev-mode recipes, no failure cited. |
| 81 | wipnote claude --resume d846b50d-9ce4-45c1-8ad2-0f84da537efd | Prose only | none | Slice 12: review. Launch and dev-mode recipes, no failure cited. |
| 82 | wipnote claude --dev --resume <session-id> | Prose only | none | Slice 12: review. Launch and dev-mode recipes, no failure cited. |
| 83 | wipnote yolo --dev --resume <session-id> | Prose only | none | Slice 12: review. Launch and dev-mode recipes, no failure cited. |
| 84 | wipnote dev --resume <session-id> | Prose only | none | Slice 12: review. Launch and dev-mode recipes, no failure cited. |
| 87 | `--resume <id>` differs from `--continue` (which resumes the most r... | Prose only | none | Slice 12: review. Launch and dev-mode recipes, no failure cited. |
| 89 | ## Dev Mode in Codespaces | Prose only | none | Slice 12: review. Launch and dev-mode recipes, no failure cited. |
| 91 | Codespaces clients disconnect on idle, browser refresh, or network ... | Prose only | none | Slice 12: review. Launch and dev-mode recipes, no failure cited. |
| 93 | wipnote claude --dev --tmux | Prose only | none | Slice 12: review. Launch and dev-mode recipes, no failure cited. |
| 95 | First run creates a tmux session named `wipnote-dev`. On disconnect... | Prose only | none | Slice 12: review. Launch and dev-mode recipes, no failure cited. |
| 97 | ## Yolo Mode in Codespaces | Prose only | none | Slice 12: review. Launch and dev-mode recipes, no failure cited. |
| 99 | Codespaces clients disconnect on idle, browser refresh, or network ... | Prose only | none | Slice 12: review. Launch and dev-mode recipes, no failure cited. |
| 101 | wipnote yolo --dev --tmux | Prose only | none | Slice 12: review. Launch and dev-mode recipes, no failure cited. |
| 103 | First run creates a tmux session named `wipnote-yolo`. On disconnec... | Prose only | none | Slice 12: review. Launch and dev-mode recipes, no failure cited. |
| 105 | --- | Prose only | none | Slice 12: review. Launch and dev-mode recipes, no failure cited. |
| 107 | ## Plugin Source — Single Source of Truth | Enforced | `wipnote plugin build-ports`; also plugin-development.md | Slice 12: drop. Duplicate of plugin-development.md. |
| 109 | wipnote ships one plugin to multiple AI-tool targets (Claude Code, ... | Enforced | `wipnote plugin build-ports`; also plugin-development.md | Slice 12: drop. Duplicate of plugin-development.md. |
| 110 | a single source. Each target's plugin tree is **generated** — never... | Enforced | `wipnote plugin build-ports`; also plugin-development.md | Slice 12: drop. Duplicate of plugin-development.md. |
| 111 | was retired as a launch/generation target — see `AGENTS.md` — but w... | Enforced | `wipnote plugin build-ports`; also plugin-development.md | Slice 12: drop. Duplicate of plugin-development.md. |
| 112 | Gemini CLI session data via the retained ingest read path.) | Enforced | `wipnote plugin build-ports`; also plugin-development.md | Slice 12: drop. Duplicate of plugin-development.md. |
| 114 | **Layering (edit the left column only):** | Enforced | `wipnote plugin build-ports`; also plugin-development.md | Slice 12: drop. Duplicate of plugin-development.md. |
| 116 | \| Layer \| What lives there \| Edit? \| | Enforced | `wipnote plugin build-ports`; also plugin-development.md | Slice 12: drop. Duplicate of plugin-development.md. |
| 117 | \|-------\|------------------\|-------\| | Enforced | `wipnote plugin build-ports`; also plugin-development.md | Slice 12: drop. Duplicate of plugin-development.md. |
| 118 | \| `packages/plugin-core/manifest.json` \| Canonical plugin metadat... | Enforced | `wipnote plugin build-ports`; also plugin-development.md | Slice 12: drop. Duplicate of plugin-development.md. |
| 119 | \| `plugin/commands/`, `plugin/agents/`, `plugin/skills/`, `plugin/... | Enforced | `wipnote plugin build-ports`; also plugin-development.md | Slice 12: drop. Duplicate of plugin-development.md. |
| 120 | \| `cmd/`, `internal/` \| Go CLI + hook handlers (all hooks are thi... | Enforced | `wipnote plugin build-ports`; also plugin-development.md | Slice 12: drop. Duplicate of plugin-development.md. |
| 121 | \| `plugin/.claude-plugin/plugin.json`, `plugin/hooks/hooks.json` \... | Enforced | `wipnote plugin build-ports`; also plugin-development.md | Slice 12: drop. Duplicate of plugin-development.md. |
| 122 | \| `port/packages/codex-marketplace/` \| Generated Codex CLI tree \... | Enforced | `wipnote plugin build-ports`; also plugin-development.md | Slice 12: drop. Duplicate of plugin-development.md. |
| 123 | \| `port/packages/antigravity-extension/` \| Generated Antigravity ... | Enforced | `wipnote plugin build-ports`; also plugin-development.md | Slice 12: drop. Duplicate of plugin-development.md. |
| 124 | \| `~/.claude/plugins/marketplaces/` (user home, legacy install) \|... | Enforced | `wipnote plugin build-ports`; also plugin-development.md | Slice 12: drop. Duplicate of plugin-development.md. |
| 126 | **Repo-root `.claude/` is not a build output.** The Claude target's... | Enforced | `wipnote plugin build-ports`; also plugin-development.md | Slice 12: drop. Duplicate of plugin-development.md. |
| 127 | itself — `build-ports --target claude` writes only the two generate... | Enforced | `wipnote plugin build-ports`; also plugin-development.md | Slice 12: drop. Duplicate of plugin-development.md. |
| 128 | (`plugin/.claude-plugin/plugin.json`, `plugin/hooks/hooks.json`). T... | Enforced | `wipnote plugin build-ports`; also plugin-development.md | Slice 12: drop. Duplicate of plugin-development.md. |
| 129 | (rules, skills, settings, `agent-memory/`) is separate, hand-mainta... | Enforced | `wipnote plugin build-ports`; also plugin-development.md | Slice 12: drop. Duplicate of plugin-development.md. |
| 130 | wipnote code path syncs or deletes — in particular `.claude/agent-m... | Enforced | `wipnote plugin build-ports`; also plugin-development.md | Slice 12: drop. Duplicate of plugin-development.md. |
| 131 | cross-session memory, namespaced `<plugin>-<agent>`, e.g. `wipnote-... | Enforced | `wipnote plugin build-ports`; also plugin-development.md | Slice 12: drop. Duplicate of plugin-development.md. |
| 132 | The only delete-on-sync logic, `removeMarketplaceWipnote` (`cmd/wip... | Enforced | `wipnote plugin build-ports`; also plugin-development.md | Slice 12: drop. Duplicate of plugin-development.md. |
| 133 | targets the user's HOME `~/.claude/plugins/marketplaces/` tree, nev... | Enforced | `wipnote plugin build-ports`; also plugin-development.md | Slice 12: drop. Duplicate of plugin-development.md. |
| 135 | **Regenerate after every manifest or asset edit:** | Enforced | `wipnote plugin build-ports`; also plugin-development.md | Slice 12: drop. Duplicate of plugin-development.md. |
| 138 | wipnote plugin build-ports              # all targets | Enforced | `wipnote plugin build-ports`; also plugin-development.md | Slice 12: drop. Duplicate of plugin-development.md. |
| 139 | wipnote plugin build-ports --target codex | Enforced | `wipnote plugin build-ports`; also plugin-development.md | Slice 12: drop. Duplicate of plugin-development.md. |
| 140 | wipnote plugin build-ports --target claude | Enforced | `wipnote plugin build-ports`; also plugin-development.md | Slice 12: drop. Duplicate of plugin-development.md. |
| 141 | wipnote plugin build-ports --target antigravity | Enforced | `wipnote plugin build-ports`; also plugin-development.md | Slice 12: drop. Duplicate of plugin-development.md. |
| 144 | See `packages/plugin-core/README.md` for the per-task recipes (new ... | Enforced | `wipnote plugin build-ports`; also plugin-development.md | Slice 12: drop. Duplicate of plugin-development.md. |
| 145 | new target) and `.claude/rules/plugin-development.md` for full plug... | Enforced | `wipnote plugin build-ports`; also plugin-development.md | Slice 12: drop. Duplicate of plugin-development.md. |
| 147 | --- | Enforced | `wipnote plugin build-ports`; also plugin-development.md | Slice 12: drop. Duplicate of plugin-development.md. |
| 149 | ## Orchestration | Prose only | none | Slice 12: review. |
| 151 | Read short files (≤200 lines, or one targeted offset/limit range) d... | Prose only | none | Slice 12: review. |
| 153 | --- | Prose only | none | Slice 12: review. |
| 155 | ## Quick Commands | Prose only | none | Slice 12: review. |
| 157 | \| Task \| Command \| | Prose only | none | Slice 12: review. |
| 158 | \|------\|---------\| | Prose only | none | Slice 12: review. |
| 159 | \| View work \| `wipnote snapshot --summary` \| | Prose only | none | Slice 12: review. |
| 160 | \| Run tests \| `go test ./...` \| | Prose only | none | Slice 12: review. |
| 161 | \| Build binary \| `wipnote build` \| | Prose only | none | Slice 12: review. |
| 162 | \| Deploy \| `./scripts/deploy-all.sh VERSION --no-confirm` \| | Prose only | none | Slice 12: review. |
| 163 | \| Status \| `wipnote status` \| | Prose only | none | Slice 12: review. |
| 164 | \| Self-update \| `wipnote upgrade` \| | Prose only | none | Slice 12: review. |
| 166 | --- | Prose only | none | Slice 12: review. |
| 168 | ## Monitoring Upstream Harnesses (Claude Code / Codex / Antigravity) | Prose only | none; upstream-harness monitoring cadence | Slice 12: keep. Needs the cadence. |
| 170 | wipnote ships to three independently-evolving CLIs: Claude Code, Co... | Prose only | none; upstream-harness monitoring cadence | Slice 12: keep. Needs the cadence. |
| 172 | **Periodically review the upstream docs for changes to:** | Prose only | none; upstream-harness monitoring cadence | Slice 12: keep. Needs the cadence. |
| 174 | - **Plugin system / agent manifest** — formats, frontmatter schema,... | Prose only | none; upstream-harness monitoring cadence | Slice 12: keep. Needs the cadence. |
| 175 | - **Hooks** — event names, payload shapes, exit-code semantics (Cla... | Prose only | none; upstream-harness monitoring cadence | Slice 12: keep. Needs the cadence. |
| 176 | - **Skills** — frontmatter fields, activation triggers, invocation ... | Prose only | none; upstream-harness monitoring cadence | Slice 12: keep. Needs the cadence. |
| 177 | - **Agent configuration** — frontmatter schema (name, description, ... | Prose only | none; upstream-harness monitoring cadence | Slice 12: keep. Needs the cadence. |
| 178 | - **Observability** — session metadata, telemetry, cost/token repor... | Prose only | none; upstream-harness monitoring cadence | Slice 12: keep. Needs the cadence. |
| 179 | - **Tool interfaces** — available tools per harness (Claude Code ≠ ... | Prose only | none; upstream-harness monitoring cadence | Slice 12: keep. Needs the cadence. |
| 181 | **Upstream sources to monitor:** | Prose only | none; upstream-harness monitoring cadence | Slice 12: keep. Needs the cadence. |
| 183 | **Claude Code (Anthropic):** | Prose only | none; upstream-harness monitoring cadence | Slice 12: keep. Needs the cadence. |
| 184 | - https://code.claude.com/docs/en/sub-agents — subagent frontmatter... | Prose only | none; upstream-harness monitoring cadence | Slice 12: keep. Needs the cadence. |
| 185 | - https://code.claude.com/docs/en/plugins — plugin manifest and mar... | Prose only | none; upstream-harness monitoring cadence | Slice 12: keep. Needs the cadence. |
| 186 | - https://code.claude.com/docs/en/hooks — hook events, payloads, ex... | Prose only | none; upstream-harness monitoring cadence | Slice 12: keep. Needs the cadence. |
| 187 | - https://code.claude.com/docs/en/skills — skill frontmatter and ac... | Prose only | none; upstream-harness monitoring cadence | Slice 12: keep. Needs the cadence. |
| 188 | - Claude Code release notes / changelog | Prose only | none; upstream-harness monitoring cadence | Slice 12: keep. Needs the cadence. |
| 190 | **Codex CLI (OpenAI):** | Prose only | none; upstream-harness monitoring cadence | Slice 12: keep. Needs the cadence. |
| 191 | - https://learn.chatgpt.com/docs/agent-configuration/subagents — cu... | Prose only | none; upstream-harness monitoring cadence | Slice 12: keep. Needs the cadence. |
| 192 | - https://learn.chatgpt.com/docs/config-file/config-reference — con... | Prose only | none; upstream-harness monitoring cadence | Slice 12: keep. Needs the cadence. |
| 193 | - https://learn.chatgpt.com/docs/config-file/config-advanced — adva... | Prose only | none; upstream-harness monitoring cadence | Slice 12: keep. Needs the cadence. |
| 194 | - https://learn.chatgpt.com/docs/hooks — hook events and payload sh... | Prose only | none; upstream-harness monitoring cadence | Slice 12: keep. Needs the cadence. |
| 195 | - https://learn.chatgpt.com/docs/changelog — Codex CLI changelog; w... | Prose only | none; upstream-harness monitoring cadence | Slice 12: keep. Needs the cadence. |
| 197 | **Antigravity (Google):** | Prose only | none; upstream-harness monitoring cadence | Slice 12: keep. Needs the cadence. |
| 198 | - No verified canonical upstream doc set yet — Antigravity is a new... | Prose only | none; upstream-harness monitoring cadence | Slice 12: keep. Needs the cadence. |
| 200 | **Re-verify on a cadence, not just on suspicion.** These schemas dr... | Prose only | none; upstream-harness monitoring cadence | Slice 12: keep. Needs the cadence. |
| 202 | --- | Prose only | none; upstream-harness monitoring cadence | Slice 12: keep. Needs the cadence. |
| 204 | ## Worktrees and .wipnote/ Artifact Commits | Prose only | none | Slice 12: review. |
| 206 | Conductor-managed worktrees (and any worktree created by `wipnote y... | Prose only | none | Slice 12: review. |
| 207 | gitignore entry that excludes `.wipnote/` from the worktree's own `... | Prose only | none | Slice 12: review. |
| 208 | noise-reduction. The artifact is still committed: `wipnote feature/... | Prose only | none | Slice 12: review. |
| 209 | `commitWipnoteArtifact` which uses `git -C <repoRoot>` with an expl... | Prose only | none | Slice 12: review. |
| 210 | commit the HTML in the main repository, bypassing the exclusion. | Prose only | none | Slice 12: review. |
| 212 | **Conductor limitation:** Conductor archives worktrees directly wit... | Prose only | none | Slice 12: review. |
| 213 | To ensure your work item artifact is committed, you must call `wipn... | Prose only | none | Slice 12: review. |
| 214 | `bug`/`spike`) from within the workspace before Conductor tears dow... | Prose only | none | Slice 12: review. |
| 215 | intercept Conductor's archive step. | Prose only | none | Slice 12: review. |
| 217 | --- | Prose only | none | Slice 12: review. |
| 219 | ## Dogfooding | Prose only | none | Slice 12: review. |
| 221 | This project uses wipnote to develop itself. `.wipnote/` contains r... | Prose only | none | Slice 12: review. |
