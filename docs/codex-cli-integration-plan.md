# wipnote Plan for Codex CLI

## Goal

Make wipnote work well in Codex CLI without pretending Codex is Claude Code.

The right target is:

- strong work-item attribution in Codex
- useful session and tool observability in Codex
- installable Codex-native packaging
- first-class Codex skills and agents
- safe automation that respects Codex approvals and sandbox behavior

The wrong target is a file-for-file port of the Claude plugin.

## Executive Summary

wipnote already has a reusable core:

- the `wipnote` CLI is host-neutral
- the hook runner is already a Go binary with a generic event envelope
- the repo already has `AGENTS.md` and `CODEX.md`
- Codex is already recognized as an agent identity in `internal/agent/detect.go`

But the current ecosystem is still mostly Claude-shaped:

- plugin packaging is Claude-only: [plugin/.claude-plugin/plugin.json](../plugin/.claude-plugin/plugin.json:1)
- plugin install UX is Claude-only: [cmd/wipnote/plugin.go](../cmd/wipnote/plugin.go:1)
- session discovery is Claude-only: [internal/ingest/discover.go](../internal/ingest/discover.go:15)
- hook behavior assumes Claude events far beyond what Codex currently emits: [plugin/hooks/hooks.json](../plugin/hooks/hooks.json:1), [cmd/wipnote/hook.go](../cmd/wipnote/hook.go:13)

So the plan should be:

1. Keep the core CLI unchanged.
2. Add a thin Codex adapter layer.
3. Ship a real Codex plugin.
4. Rework observability around the smaller Codex hook surface and transcript ingestion.

## What The Repo Already Has

### Reusable Core

- `wipnote hook ...` is already a compiled hook surface, not a pile of shell scripts: [cmd/wipnote/hook.go](../cmd/wipnote/hook.go:13)
- hook payload parsing is already generic enough to map onto Codex fields like `session_id`, `cwd`, `model`, `tool_name`, and `transcript_path`: [internal/hooks/runner.go](../internal/hooks/runner.go:17)
- session start already stores `transcript_path` when provided by the runtime: [internal/hooks/session_start.go](../internal/hooks/session_start.go:179)
- user-prompt and tool-use hooks already record wipnote events in SQLite: [internal/hooks/user_prompt.go](../internal/hooks/user_prompt.go:14), [internal/hooks/pretooluse.go](../internal/hooks/pretooluse.go:17)
- `selfBinary()` is only mildly Claude-coupled and already falls back to `os.Executable()` and `PATH`: [internal/hooks/task_tracking.go](../internal/hooks/task_tracking.go:12)

### Existing Codex Surface

- repo-level Codex guidance exists: [CODEX.md](../CODEX.md:1)
- repo-local Codex config now exists: [.codex/config.toml](../.codex/config.toml:1)
- the repo already contains an earlier Codex review: [docs/codex-interoperability-review.md](../docs/codex-interoperability-review.md:1)
- the orchestrator prompt already mentions `codex exec ...` as an external coding path: [cmd/wipnote/prompts/system-prompt.md](../cmd/wipnote/prompts/system-prompt.md:45)

### Content That Transfers Well

- `AGENTS.md` transfers directly
- most `plugin/skills/*/SKILL.md` content is portable with modest editing
- the Claude agent roster can be re-encoded as Codex subagents

Current Claude assets:

- commands: `plugin/commands/*.md`
- skills: `plugin/skills/*`
- agents: `plugin/agents/*.md`

## Current Gaps

### 1. Packaging Is Claude-Only

The shipped plugin manifest is `.claude-plugin`, not `.codex-plugin`: [plugin/.claude-plugin/plugin.json](../plugin/.claude-plugin/plugin.json:1)

The CLI installer only knows how to call `claude plugin ...`: [cmd/wipnote/plugin.go](../cmd/wipnote/plugin.go:16)

Impact:

- no Codex-installable plugin bundle
- no Codex marketplace metadata
- no Codex-local plugin test loop

### 2. Hook Coverage Assumes Claude’s Larger Event Model

wipnote currently wires many events that Codex does not expose:

- `SessionEnd`
- `SubagentStart`
- `SubagentStop`
- `PostToolUseFailure`
- `PreCompact`
- `PostCompact`
- `TaskCreated`
- `TaskCompleted`
- `PermissionRequest`
- `WorktreeCreate`
- `WorktreeRemove`
- others in [plugin/hooks/hooks.json](../plugin/hooks/hooks.json:1)

Codex’s documented hook surface is smaller and the important constraint is stricter:

- Codex repo-local and global hooks live in `~/.codex/hooks.json` and `<repo>/.codex/hooks.json`
- `PreToolUse` and `PostToolUse` currently only emit `Bash`
- `SessionStart` only matches `startup` and `resume`
- `UserPromptSubmit` and `Stop` ignore `matcher`

Source: OpenAI Codex Hooks docs, especially lines covering locations, matcher behavior, and `Bash`-only tool interception:

- https://developers.openai.com/codex/hooks
- `PreToolUse` / `PostToolUse` only emit `Bash` today: lines 669-682, 744-745, 780-781 in the current docs page

Impact:

- wipnote cannot rely on Codex hooks to observe `Read`, `Write`, `Edit`, MCP, Web, or other tool classes
- many current guardrails cannot be enforced in Codex the same way they are in Claude

### 3. Session Ingestion Is Claude-Specific

Session discovery currently scans `~/.claude/projects/`: [internal/ingest/discover.go](../internal/ingest/discover.go:15)

Impact:

- there is no first-class Codex transcript ingestion path yet
- the session importer is shaped around Claude’s directory layout and JSONL assumptions

### 4. Public Install Story Is Still Claude-First

The README still recommends the Claude plugin first: [README.md](../README.md:24)

Impact:

- users will infer Codex is supported at parity when it is not
- install docs do not explain the current Codex setup path or limitations

### 5. Command UX Is Claude Slash-Command Heavy

The current plugin includes many Claude command docs under `plugin/commands/`.

Codex plugins are organized around:

- skills
- apps
- MCP servers
- explicit plugin or skill invocation with `@`

Source:

- Plugins overview: https://developers.openai.com/codex/plugins
- Skills overview: https://developers.openai.com/codex/skills

Impact:

- Claude command inventory is not the right primary UX surface in Codex
- the Codex plan should prioritize skills and MCP tools over slash-command parity

## Codex Constraints That Should Drive The Design

### Hooks

Codex hooks are useful, but they are not a full observability layer.

Important current facts from official docs:

- hooks are feature-flagged with `[features] codex_hooks = true`
- repo-local hooks belong in `<repo>/.codex/hooks.json`
- multiple matching hooks run concurrently
- `PreToolUse` and `PostToolUse` are `Bash`-only today
- `transcript_path` is a common input field

Source: https://developers.openai.com/codex/hooks

Design implication:

- use hooks for session lifecycle, prompt capture, Bash observability, and policy nudges
- do not design Codex support around complete tool interception

### Skills

Codex skills are a strong fit for wipnote’s workflow content.

Important current facts from official docs:

- a skill is a directory with `SKILL.md` plus optional `scripts/`, `references/`, `assets/`
- skills can be invoked explicitly or matched implicitly by description
- plugins are the distribution unit for bundled skills

Source: https://developers.openai.com/codex/skills

Design implication:

- migrate wipnote workflow UX into skills first
- treat most existing Claude command docs as source material, not as a target format

### Plugins

Codex plugins bundle skills, apps, and MCP servers.

Important current facts from official docs:

- plugins bundle skills, app integrations, and MCP servers
- users install them from the Codex plugin directory
- bundled skills become available immediately
- plugin installation still respects normal approval settings

Source: https://developers.openai.com/codex/plugins

Design implication:

- wipnote should ship one Codex plugin bundle
- that bundle should prioritize skills and an MCP server over command mirroring

### Agents

Codex custom agents are file-based and TOML-backed.

The current docs explicitly show agent files like:

- `.codex/agents/reviewer.toml`

Source: Codex Subagents docs snippet showing `.codex/agents/reviewer.toml` and TOML fields:

- https://developers.openai.com/codex/subagents

Design implication:

- port wipnote agents by re-encoding them as Codex TOML agents
- do not try to reuse Claude markdown/frontmatter agent files directly

### AGENTS.md and Config

Codex already understands repo-level `AGENTS.md`, supports fallback filenames, and exposes config keys for:

- `sandbox_mode`
- `approval_policy`
- `sandbox_workspace_write.writable_roots`
- `projects.<path>.trust_level`

Sources:

- AGENTS.md docs: https://developers.openai.com/codex/guides/agents-md
- config reference: https://developers.openai.com/codex/config-reference
- rules docs: https://developers.openai.com/codex/rules

Design implication:

- keep operational guidance in `AGENTS.md` and `CODEX.md`
- keep enforcement and approval shortcuts in `.codex/config.toml` and rules files

## Recommended Architecture

## 1. Host-Neutral Core

Keep these as the stable shared layer:

- work-item CRUD and graph logic
- SQLite schema and event persistence
- dashboard and API
- `wipnote` CLI
- generic hook event persistence helpers

## 2. Host Adapter Layer

Add an explicit host adapter boundary:

- `internal/host/claude/...`
- `internal/host/codex/...`

Responsibilities:

- hook payload normalization
- runtime-specific session discovery
- transcript ingestion adapters
- agent identity normalization
- runtime-specific install/setup helpers

This is better than sprinkling more `CLAUDE_*` assumptions across `internal/hooks`.

## 3. Codex Plugin Layer

Create a real Codex plugin tree, separate from `plugin/`:

Suggested path:

- `plugins/wipnote-codex/`

Suggested contents:

- `.codex-plugin/plugin.json`
- `skills/`
- `.mcp.json`
- optional `.app.json`
- optional local `README.md`

## 4. Codex Repo Layer

Use repo-local files for dogfooding and dev:

- `.codex/config.toml`
- `.codex/hooks.json`
- `.codex/agents/*.toml`
- `AGENTS.md`
- `CODEX.md`

## Phased Plan

## Phase 0: Define Scope and Stop Pretending At Parity

Goal:

Write down what “works for Codex CLI” means for v1.

Definition of done for v1:

- repo-local Codex usage is documented and stable
- wipnote records Codex sessions, prompts, and Bash tool events
- wipnote ships a Codex plugin with core skills
- wipnote ships a small set of Codex custom agents
- public docs accurately describe Codex support and limitations

Non-goals for v1:

- parity with Claude-only hook events
- full non-Bash tool interception
- direct port of every Claude command

## Phase 1: Establish The Codex Adapter Surface

Deliverables:

- add a Codex-specific host adapter package
- remove direct `CLAUDE_*` assumptions from reusable code paths where practical
- add a normalized runtime enum: `claude`, `codex`, `copilot`, `gemini`

Key tasks:

1. Refactor `selfBinary()` and related hook helpers to accept a runtime-specific plugin root env var rather than assuming `CLAUDE_PLUGIN_ROOT` only.
2. Split event normalization from hook business logic.
3. Make session and transcript metadata explicitly runtime-tagged in the DB if they are not already.

Why first:

Without a clear adapter seam, Codex support will become a pile of `if runtime == "codex"` branches.

## Phase 2: Ship Repo-Local Codex Hooks

Deliverables:

- `.codex/hooks.json` in this repo
- hook wiring only for supported Codex events:
  - `SessionStart`
  - `UserPromptSubmit`
  - `PreToolUse`
  - `PostToolUse`
  - `Stop`

Key tasks:

1. Add a Codex hook manifest that shells out to `wipnote hook ...`.
2. Add a Codex event normalization shim if needed so stdin payloads map onto the existing `CloudEvent` structure.
3. Restrict matcher usage correctly:
   - `SessionStart`: `startup|resume`
   - `PreToolUse`: `Bash`
   - `PostToolUse`: `Bash`
   - no matcher assumptions for `UserPromptSubmit` and `Stop`
4. Audit hook outputs so they use Codex-supported fields and fail open where Codex ignores unsupported fields.

Important design choice:

- For Codex, `PreToolUse` and `PostToolUse` should be positioned as Bash-policy hooks, not broad safety enforcement.

## Phase 3: Add Codex Transcript Discovery and Ingestion

Deliverables:

- Codex session discovery
- Codex transcript parser or adapter
- ingestion tests with captured Codex transcript fixtures

Key tasks:

1. Add a Codex discovery path parallel to `internal/ingest/discover.go`.
2. Support Codex session/transcript locations rather than only `~/.claude/projects/`.
3. Add parser support for Codex rollout/session JSONL shape.
4. Preserve `transcript_path` from hooks as the bridge between runtime hooks and ingestion.
5. Ingest Codex subagent transcripts if the runtime writes them separately.

Success criteria:

- `wipnote ingest` or equivalent sees Codex sessions as first-class inputs
- dashboard session views can show Codex session data without Claude-specific assumptions

## Phase 4: Ship A Minimal Codex Plugin

Deliverables:

- `plugins/wipnote-codex/.codex-plugin/plugin.json`
- bundled core skills
- local plugin dev/test instructions
- marketplace metadata if you want distribution through Codex-native plugin discovery

Suggested first bundled skills:

- `agent-context`
- `diagnose`
- `plan`
- `deploy`
- `code-quality`

Key tasks:

1. Create the Codex plugin skeleton.
2. Port the existing `SKILL.md` assets with Codex-oriented wording and invocation patterns.
3. Strip Claude-only instructions from those skills.
4. Add any small helper scripts needed by Codex skills.
5. Document how to invoke:
   - implicitly by description
   - explicitly via `@plugin` or `@skill`

Important design choice:

- Do not spend early time porting `plugin/commands/*.md` one-for-one.
- Treat those command docs as source content for skills or MCP tools.

## Phase 5: Port The Agent Roster To Codex

Deliverables:

- `.codex/agents/researcher.toml`
- `.codex/agents/reviewer.toml`
- `.codex/agents/test-runner.toml`
- one or two coder variants only if they are materially different

Suggested first set:

- `researcher`
- `reviewer`
- `test-runner`
- `executor` or `coder`

Key tasks:

1. Translate each Claude markdown agent into TOML plus concise instructions.
2. Keep the roster small; Codex does not benefit from a long list of overlapping near-duplicates.
3. Align each agent with wipnote work attribution rules from `AGENTS.md`.
4. Add tests or smoke checks that the agent files parse and are discoverable.

Important design choice:

- prefer fewer, sharper agents over a large Claude-style roster

## Phase 6: Add A Thin wipnote MCP Server For Codex

This is the highest-leverage medium-term step.

Why:

- Codex hook coverage is too narrow to be the full integration surface
- skills alone are not enough for structured graph operations
- MCP gives Codex stable typed tools instead of shelling out for everything

Suggested first tools:

- `wipnote_status`
- `wipnote_feature_list`
- `wipnote_feature_start`
- `wipnote_feature_complete`
- `wipnote_find`
- `wipnote_snapshot`
- `wipnote_session_show`
- `wipnote_review`

Design rule:

- thin MCP tools over existing CLI/core logic
- do not re-implement business logic in the MCP layer

## Phase 7: Add Codex-Safe Approval and Sandbox Defaults

Deliverables:

- recommended `.codex/config.toml` snippets
- optional rules file for safe command prefixes
- docs for repo-local and user-global setup

Key tasks:

1. Document the recommended default as:
   - `workspace-write`
   - `on-request`
2. Use rules for common `wipnote ...` command prefixes where that improves UX.
3. Keep repo-local exceptions narrow.
4. Treat `danger-full-access` as an environment-specific workaround, not the general recommendation.

Important current repo note:

- this repo currently pins `danger-full-access` because the present Linux environment cannot initialize `bubblewrap`: [.codex/config.toml](../.codex/config.toml:1)

That is a local compatibility fix, not the long-term recommended wipnote default.

## Phase 8: Update Public Docs and Product Positioning

Deliverables:

- README section for Codex CLI
- installation docs for Codex plugin
- explicit support matrix: Claude vs Codex vs others

Key tasks:

1. Stop calling the Claude plugin the universal recommended path.
2. Publish a host support matrix:
   - work-item CRUD
   - session capture
   - tool observability
   - hooks
   - skills
   - custom agents
   - plugin install
3. Document limitations clearly:
   - Codex hooks are narrower
   - non-Bash tool observability is incomplete without transcript ingestion
   - some Claude-only automations have no Codex equivalent

## Priority Order

If you want the fastest path to “wipnote works in Codex CLI,” do the work in this order:

1. repo-local `.codex/hooks.json`
2. Codex transcript discovery and ingestion
3. Codex plugin skeleton with 3-5 core skills
4. `.codex/agents/*.toml`
5. thin wipnote MCP server
6. public docs and support matrix cleanup

If you do plugin packaging before transcript ingestion, the result will install cleanly but feel shallow.

If you do transcript ingestion before plugin packaging, the result will already provide real value to Codex users during dogfooding.

## Recommended v1 Deliverable Set

For a realistic first release, I would target exactly this:

- repo-local `.codex/hooks.json`
- Codex session ingestion from transcript files
- Bash-only tool-event capture through Codex hooks
- 4 bundled Codex skills
- 3 bundled Codex agents
- README + docs support matrix

I would explicitly defer:

- command parity with Claude slash commands
- full policy parity with Claude hooks
- broad plugin marketplace polish
- aggressive multi-agent orchestration behavior

## Risks

### Risk 1: Overfitting To Claude

If you keep Codex support inside the current Claude plugin tree, the result will be brittle.

Mitigation:

- separate `plugin/` from `plugins/wipnote-codex/`
- add a host adapter boundary in code

### Risk 2: Treating Hooks As The Whole Product

Codex hooks are useful but incomplete.

Mitigation:

- build around transcripts and MCP, not hooks alone

### Risk 3: Porting UX Instead Of Outcomes

Trying to reproduce every Claude slash command in Codex will waste time.

Mitigation:

- prioritize skills and typed MCP tools

### Risk 4: Unsafe Defaults

Using `danger-full-access` as the public recommendation would be a mistake.

Mitigation:

- document `workspace-write` + `on-request` as the default recommendation
- use rules for targeted exceptions

## Success Criteria

wipnote should count as “working for Codex CLI” when:

- a Codex user can install or enable wipnote without touching Claude tooling
- Codex sessions show up in wipnote session views
- prompts and Bash actions are attributed to work items
- a user can invoke wipnote workflows through Codex skills
- at least one review-oriented and one execution-oriented Codex agent are available
- docs describe the real limitations accurately

## Suggested Next Implementation Milestone

If you later decide to execute this plan, the first milestone should be:

### Milestone 1

- add `.codex/hooks.json`
- wire `SessionStart`, `UserPromptSubmit`, `PreToolUse`, `PostToolUse`, `Stop`
- add a Codex transcript discovery package
- ingest one real Codex session end-to-end

That milestone proves the hard part:

- wipnote can observe and persist real Codex activity in a Codex-native way

## Sources

### Repo

- [docs/codex-interoperability-review.md](../docs/codex-interoperability-review.md:1)
- [CODEX.md](../CODEX.md:1)
- [README.md](../README.md:1)
- [plugin/hooks/hooks.json](../plugin/hooks/hooks.json:1)
- [plugin/.claude-plugin/plugin.json](../plugin/.claude-plugin/plugin.json:1)
- [cmd/wipnote/plugin.go](../cmd/wipnote/plugin.go:1)
- [cmd/wipnote/hook.go](../cmd/wipnote/hook.go:13)
- [internal/hooks/runner.go](../internal/hooks/runner.go:17)
- [internal/hooks/session_start.go](../internal/hooks/session_start.go:101)
- [internal/hooks/user_prompt.go](../internal/hooks/user_prompt.go:14)
- [internal/hooks/pretooluse.go](../internal/hooks/pretooluse.go:17)
- [internal/hooks/task_tracking.go](../internal/hooks/task_tracking.go:12)
- [internal/ingest/discover.go](../internal/ingest/discover.go:15)

### Official Codex docs

- Hooks: https://developers.openai.com/codex/hooks
- Plugins: https://developers.openai.com/codex/plugins
- Build plugins: https://developers.openai.com/codex/plugins/build
- Skills: https://developers.openai.com/codex/skills
- AGENTS.md guidance: https://developers.openai.com/codex/guides/agents-md
- Subagents: https://developers.openai.com/codex/subagents
- Rules: https://developers.openai.com/codex/rules
- Config reference: https://developers.openai.com/codex/config-reference
