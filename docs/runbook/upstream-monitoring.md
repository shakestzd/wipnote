# Monitoring upstream harnesses

Moved from `CLAUDE.md` in slice 12 of plan-2622b3f7 (feat-6f99bc47).

wipnote ships to three CLIs that change on their own schedules: Claude Code, Codex CLI and Antigravity. Gemini CLI is retired as a launch and generation target (feat-02f25a24). Each harness is an integration surface with its own contract for plugins, hooks, skills, agents and observability.

A doc audit on 2026-05-15 found silent drift in all three harnesses then shipped. A field in an agent config can stop being honored with no error. Check on a cadence, not only on suspicion.

## Cadence

Re-run the cross-harness doc audit at least once per release cycle, or from a scheduled routine. Use `agentFrontmatterFieldSpecs` in `port/pluginbuild/agent_frontmatter.go` as the checklist. It lists agent frontmatter fields, supported harnesses, output-name translations and provenance links. Hook event names are hard-checked at build time by `hookEventNameSpecs` in `port/pluginbuild/hook_event_names.go`.

## What to review

- Plugin system and agent manifest: formats, frontmatter schema, marketplace and config lookup paths.
- Hooks: event names, payload shapes, exit-code semantics.
- Skills: frontmatter fields, activation triggers, invocation patterns.
- Agent configuration: name, description, model, color, tools, timeout or maxTurns, memory, max_depth.
- Observability: session metadata, telemetry, cost and token reporting, transcript format.
- Tool interfaces: the tools differ per harness.

## Sources

Claude Code (Anthropic):

- https://code.claude.com/docs/en/sub-agents: subagent frontmatter schema
- https://code.claude.com/docs/en/plugins: plugin manifest and marketplace structure
- https://code.claude.com/docs/en/hooks: hook events, payloads, exit codes
- https://code.claude.com/docs/en/skills: skill frontmatter and activation
- Claude Code release notes and changelog

Codex CLI (OpenAI):

- https://learn.chatgpt.com/docs/agent-configuration/subagents: custom agent TOML schema and lookup paths (`~/.codex/agents/`, `.codex/agents/`)
- https://learn.chatgpt.com/docs/config-file/config-reference: config keys including the `[agents]` section (max_threads, max_depth, job_max_runtime_seconds). There is no per-agent interactive turn cap.
- https://learn.chatgpt.com/docs/config-file/config-advanced: advanced options
- https://learn.chatgpt.com/docs/hooks: hook events and payload shapes
- https://learn.chatgpt.com/docs/changelog: changelog, and where model retirements are announced

Antigravity (Google):

- No verified canonical doc set yet. Antigravity inherits Gemini CLI conventions through the compat helpers in `port/pluginbuild/gemini_compat.go`. Treat `port/pluginbuild/antigravity.go` as the executable spec until a doc audit sets canonical sources.

## Where a fix lands

When a contract changes, fix it in `plugin/agents/*.md`, `packages/plugin-core/manifest.json`, `port/pluginbuild/` or `cmd/wipnote/prompts/system-prompt.md`. Never fix it in AGENTS.md or CLAUDE.md. Those files describe and do not configure.
