# plugin-core — DRY source of truth for wipnote plugin ports

All wipnote plugin ports (Claude Code, Codex CLI, Gemini CLI) are generated from the
files in this directory so we never edit the same logic twice.

## Source of truth

- **`manifest.json`** — plugin metadata, per-target output paths, hook event
  matrix. `plugin/.claude-plugin/plugin.json`,
  `packages/codex-marketplace/.agents/plugins/wipnote/.codex-plugin/plugin.json`, and
  `packages/gemini-extension/gemini-extension.json` are all generated from it.
- **Assets** (commands, agents, skills, templates, static, config) live in
  `plugin/…/`. Codex skills and commands are copied in their native markdown
  form, while Codex agents are translated from `plugin/agents/*.md` into
  custom-agent TOML under the generated marketplace plugin's `agents/` directory.
  Gemini CLI requires TOML slash commands, so a sub-emitter translates the
  markdown on the way out.
- **Generated trees** — `plugin/` (Claude), `packages/codex-marketplace/` (Codex),
  and `packages/gemini-extension/` (Gemini) are output directories. Treat them
  as build artifacts: do not hand-edit anything under `plugin/.claude-plugin/`,
  `plugin/hooks/hooks.json`, `packages/codex-marketplace/`, or
  `packages/gemini-extension/`. Regenerate instead.

## Build

    wipnote plugin build-ports              # regenerate all targets
    wipnote plugin build-ports --target codex
    wipnote plugin build-ports --target claude
    wipnote plugin build-ports --target gemini

The command writes each target's tree under the `outDir` declared in
`manifest.json → targets.<name>`.

Codex custom agents have a second runtime step: `wipnote codex --init`,
`wipnote codex`, and `wipnote codex --dev` mirror the generated
`packages/codex-marketplace/.agents/plugins/wipnote/agents/*.toml` files into
Codex's documented custom-agent lookup directory. Normal installs use
`~/.codex/agents`; dev launches also refresh project-local `.codex/agents`.
The launcher additionally passes explicit `-c agents.<name>.config_file=...`
overrides so fresh Codex CLI sessions do not rely only on plugin-cache discovery.

Current limitation: some tool-backed Codex sessions still expose only generic
`default`, `explorer`, and `worker` spawn roles even when custom-agent TOML files
exist on disk. Treat plugin/cache file presence as generation proof, not runtime
spawn proof; verify runtime behavior with an actual `spawn_agent` smoke test.

### Agent roles and model policy

Agent names describe responsibilities, not model families. Use stable role names
such as `patch-coder`, `feature-coder`, and `architect-coder`; keep provider
model choices in frontmatter or target-specific emitters.

Current mapping:

| Role | Purpose | Claude model alias | Codex model | Gemini model |
|------|---------|--------------------|-------------|--------------|
| `patch-coder` | Small, clear edits | `haiku` | `gpt-5.4-mini`, low effort | `flash-lite` |
| `feature-coder` | Moderate implementation | `sonnet` | `gpt-5.4`, medium effort | `flash` |
| `architect-coder` | Complex architecture/high risk | `opus` | `gpt-5.5`, high effort | `pro` |

This follows each harness's documented shape: Claude subagents use a role `name`
plus separate model configuration, Codex custom-agent TOML identifies agents by
`name` and supports separate `model` / `model_reasoning_effort`, and Gemini
subagents use role slugs with optional model overrides.

## Hooks — thin wrappers

Every hook resolves to `wipnote hook <handler>`. Business logic lives in the
Go CLI (`internal/hooks/`); the plugin manifests only declare which events route
to which handler and on which target. Events whose `targets` list omits a given
target are not emitted to that target's hooks file.

### Hook event matrix

Derived from `manifest.json → hooks.events`. Update this table whenever you
edit the manifest.

| Event | Handler | Claude | Codex | Gemini | Notes |
|-------|---------|:---:|:---:|:---:|-------|
| `SessionStart` | `session-start` | x | x | x | |
| `SessionStart` | `session-resume` | x | | | matcher: `resume` |
| `SessionEnd` | `session-end` | x | | | |
| `UserPromptSubmit` | `user-prompt` | x | x | x | |
| `UserPromptSubmit` | `timestamp` | x | | | shell `command:` only — injects local timestamp |
| `PreToolUse` | `pretooluse` | x | x | x | |
| `PostToolUse` | `posttooluse` | x | x | x | |
| `PostToolUse` | `exit-plan-mode` | x | | | matcher: `ExitPlanMode` |
| `PostToolUseFailure` | `posttooluse-failure` | x | | | |
| `SubagentStart` | `subagent-start` | x | | | |
| `SubagentStop` | `subagent-stop` | x | | | |
| `Stop` | `stop` | x | | x | |
| `PreCompact` | `pre-compact` | x | | | |
| `PostCompact` | `post-compact` | x | | | |
| `TeammateIdle` | `teammate-idle` | x | | | |
| `TaskCompleted` | `task-completed` | x | | | |
| `TaskCreated` | `task-created` | x | | | |
| `InstructionsLoaded` | `instructions-loaded` | x | | | |
| `WorktreeCreate` | `worktree-create` | x | | | |
| `WorktreeRemove` | `worktree-remove` | x | | | |
| `PermissionRequest` | `permission-request` | x | | | |
| `ConfigChange` | `config-change` | x | | | |
| `TaskStarted` | `task-started` | | x | | Codex-specific |
| `TaskComplete` | `stop` | | x | | Codex-specific — reuses `stop` handler |
| `TurnAborted` | `task-aborted` | | x | | Codex-specific |

## Recipes

### Add a new slash command / agent / skill

Drop the markdown file into the matching `plugin/` subtree and regenerate:

```bash
# examples
$EDITOR plugin/commands/mycmd.md
$EDITOR plugin/agents/my-agent.md
$EDITOR plugin/skills/my-skill/SKILL.md

wipnote plugin build-ports
```

Every target picks the new asset up automatically — no manifest edit needed,
because `manifest.json → assetSources` already points at `plugin/{commands,agents,skills,…}`.

### Add a new hook event

Three places, always in this order:

1. **Manifest** (`packages/plugin-core/manifest.json`) — add one entry to
   `hooks.events`, listing the event name, handler, and targets:

   ```json
   { "name": "MyNewEvent", "handler": "my-new-event", "targets": ["claude", "codex"] }
   ```

   Optional keys: `matcher` (e.g. `"ExitPlanMode"`), `timeout` (seconds),
   `command` (escape hatch for shell-only hooks; bypasses `handler`).

2. **Go handler** (`internal/hooks/my_new_event.go`) — implement the handler with
   the signature matching the wiring you'll use:

   ```go
   package hooks

   func MyNewEvent(event *CloudEvent, db *sql.DB) (*HookResult, error) {
       // business logic
       return &HookResult{}, nil
   }
   ```

3. **Route** (`cmd/wipnote/hook.go`) — register the CLI subcommand so
   `wipnote hook my-new-event` resolves to the Go handler:

   ```go
   hookSubcmd("my-new-event", "Handle MyNewEvent event", emptyResult, hooks.MyNewEvent),
   ```

   Use `hookSubcmdWithProject(...)` instead when the handler needs the project
   dir passed through (see `session-start` for the pattern).

Then run `wipnote plugin build-ports && wipnote build` and update the
**Hook event matrix** table above.

### Add a new target

Gemini CLI is the current reference — see `internal/pluginbuild/gemini.go` for
the canonical sub-emitter registration pattern.

1. **Manifest** — add a `targets.<name>` entry. Alongside `outDir`,
   `manifestPath`, `hooksPath`, and the optional `mcpPath`, the schema also
   supports:

   - `contextFile` — path (relative to the repo root) of a context/instruction
     file that should be copied into the target tree. Gemini uses this for its
     `GEMINI.md` file.
   - `commandNamespace` — sub-directory under `commands/` that holds the
     target's slash commands. Gemini groups its translated TOML commands under
     a namespace so they don't collide with other extensions.

   Example:

   ```json
   "mytool": {
     "outDir": "packages/mytool-extension",
     "manifestPath": "mytool-extension.json",
     "hooksPath": "hooks/hooks.json",
     "contextFile": "MYTOOL.md",
     "commandNamespace": "wipnote"
   }
   ```

   Then tag each applicable hook event in `hooks.events` with `"mytool"` in its
   `targets` list.

2. **Adapter** — implement the `Adapter` interface in a new file under
   `internal/pluginbuild/` (model it on `claude.go` / `codex.go` / `gemini.go`):

   ```go
   package pluginbuild

   type mytoolAdapter struct{}

   func init() { Register(mytoolAdapter{}) }

   func (mytoolAdapter) Name() string { return "mytool" }

   func (mytoolAdapter) Emit(m *Manifest, repoRoot, outDir string) error {
       // 1. write the target-specific plugin.json from m (use writeJSON)
       // 2. write the target-specific hooks.json from m.Hooks.Events
       //    (filter with HookEvent.AppliesTo("mytool"))
       // 3. copy assets with copyAssetTree(...) using m.AssetSources
       return nil
   }
   ```

   The `Adapter` interface is defined in `internal/pluginbuild/adapter.go`:

   - `Name() string` — must match the manifest `targets.<name>` key.
   - `Emit(m *Manifest, repoRoot, outDir string) error` — write the full tree
     rooted at `outDir`.

   `init()` must call `Register(...)` so the target is discoverable by
   `wipnote plugin build-ports --target <name>`. Duplicate registrations panic.

   **Sub-emitters for format translation.** If the target needs per-asset
   translation (e.g. Gemini's markdown-to-TOML slash commands), do **not**
   extend `copyAssetTree` — add a sub-emitter file instead (for example,
   `gemini_commands.go`, `gemini_assets.go`, `gemini_hooks.go`) that registers
   a callback in `init()`. The parent adapter iterates its sub-emitter slice,
   so each phase or format converter can land independently. See
   `internal/pluginbuild/gemini.go` for the canonical registration pattern
   (the `geminiSubEmitters` slice and `GeminiSubEmitter` signature).

3. **Regenerate and verify**:

   ```bash
   wipnote build
   wipnote plugin build-ports --target mytool
   ```
