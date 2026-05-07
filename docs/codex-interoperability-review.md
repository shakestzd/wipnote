# wipnote Codex Interoperability Review

## Summary

Codex can support a large part of wipnote's Claude plugin, but not as a straight port. The best path is to treat wipnote as a host-neutral core with a Codex adapter, not to mirror the Claude plugin file-for-file.

## What Transfers Cleanly

- `AGENTS.md` guidance transfers directly. Codex natively reads layered `AGENTS.md` files, including nested overrides and fallback filenames. That makes wipnote's attribution, safety, and workflow rules portable without much change.
- Skills transfer well. Codex skills are `SKILL.md`-based reusable workflows, which maps closely to wipnote's current skill content like `agent-context`, `execute`, `plan`, `deploy`, and `diagnose`.
- Plugin packaging transfers. Codex plugins can bundle skills, MCP servers, and app mappings via `.codex-plugin/plugin.json`, `.mcp.json`, and `.app.json`. That is a good fit for distributing wipnote workflows.
- Multi-agent patterns transfer conceptually. Codex supports subagents and custom agents, so wipnote's role-based orchestration can be recreated.

## What Needs Adaptation

- Claude-style custom slash commands do not appear portable. wipnote currently relies on commands like `/wipnote:*`. Codex docs only describe built-in slash commands plus skill and plugin invocation. Treat plugin-defined slash commands as unsupported for now and replace them with skills, prompts, or MCP tools.
- Agent definitions need reformatting. wipnote's Claude agents are markdown/frontmatter files under `plugin/agents/`. Codex custom agents are TOML files under `.codex/agents/` or `~/.codex/agents/`.
- Hook coverage is narrower in Codex. The current Claude plugin hooks cover many events in `plugin/hooks/hooks.json`, but Codex currently documents `SessionStart`, `UserPromptSubmit`, `PreToolUse`, `PostToolUse`, and `Stop` only.
- Tool observability is weaker via hooks. Codex `PreToolUse` and `PostToolUse` currently only emit `Bash`, not `Read/Edit/Write`-level events. wipnote cannot get Claude-like fine-grained tool telemetry from hooks alone in Codex.

## What Does Not Port 1:1

- These Claude-specific hook events in `plugin/hooks/hooks.json` have no direct Codex equivalent today: `SessionEnd`, `SubagentStart`, `SubagentStop`, `PostToolUseFailure`, `PreCompact`, `TeammateIdle`, `TaskCompleted`, `InstructionsLoaded`, `WorktreeCreate`, `WorktreeRemove`, `PostCompact`, `TaskCreated`, and `PermissionRequest`.
- Pre-tool enforcement is weaker in Codex. Codex can deny Bash before execution, but not broadly intercept all tool classes the way wipnote's Claude integration is designed to.
- Automatic delegation behavior differs. Codex only spawns subagents when explicitly asked. If wipnote expects automatic orchestration from hooks or prompt magic, that must become explicit skill behavior.

## Best Improvements for wipnote Interoperability

1. Keep the Codex surface current. Repo-local settings now live in `.codex/config.toml`, and `CODEX.md` has been trimmed to match the files that actually exist in this checkout.
2. Create a real Codex plugin, separate from the Claude plugin, under something like `plugins/wipnote-codex/` with `.codex-plugin/plugin.json`, `skills/`, and optionally `.mcp.json`.
3. Split plugin logic into two layers:
   - host-neutral core: work-item lifecycle, attribution policy, quality gates, graph operations
   - host adapters: Claude hooks and commands vs Codex hooks, skills, and agents
4. Port the highest-value Claude skills first:
   - `agent-context`
   - `code-quality-skill`
   - `plan`
   - `diagnose`
   - `deploy`
5. Add repo-local Codex hooks at `.codex/hooks.json` that shell out to `wipnote hook ...` for the supported Codex events only: `SessionStart`, `UserPromptSubmit`, `PreToolUse`, `PostToolUse`, and `Stop`.
6. Add a thin wipnote MCP server. This is the biggest win. Instead of asking Codex to shell out for everything, expose structured tools like:
   - `wipnote_status`
   - `wipnote_feature_list`
   - `wipnote_feature_start`
   - `wipnote_feature_complete`
   - `wipnote_snapshot`
   - `wipnote_review`
   - `wipnote_session_link_transcript`
7. Recreate the Claude agent roster as Codex custom agents in `.codex/agents/*.toml` for roles like researcher, executor, reviewer, and test-runner.
8. Add transcript and telemetry ingestion for Codex-native data. Since hook coverage is Bash-only, wipnote should also ingest:
   - `transcript_path` from Codex hooks
   - optional Codex OTel events for approvals and tool decisions if enabled
9. Ship a recommended Codex profile and rules file for safe wipnote automation:
   - `workspace-write`
   - `on-request` or `untrusted`
   - safe prefix rules for `wipnote ...`
10. Add a repo marketplace entry at `.agents/plugins/marketplace.json` so the plugin is installable in Codex the way Codex expects.

## Design Conclusion

Port skills and guidance directly, port agents by re-encoding them, port hooks selectively, and replace Claude-only slash commands with Codex skills plus MCP tools.

The biggest technical limitation is that Codex hooks currently only see Bash, so wipnote should rely on MCP and transcript ingestion for rich observability instead of expecting Claude-level tool event parity.

## Sources

- wipnote local files:
  - `AGENTS.md`
  - `README.md`
  - `CODEX.md`
  - `docs/reference/cli.md`
  - `plugin/hooks/hooks.json`
  - `plugin/skills/agent-context/SKILL.md`
  - `plugin/skills/execute/SKILL.md`
  - `plugin/skills/plan/SKILL.md`
  - `plugin/skills/deploy/SKILL.md`
  - `plugin/skills/diagnose/SKILL.md`
  - `plugin/agents/sonnet-coder.md`
- OpenAI Codex docs:
  - `https://developers.openai.com/codex/guides/agents-md`
  - `https://developers.openai.com/codex/skills`
  - `https://developers.openai.com/codex/plugins`
  - `https://developers.openai.com/codex/plugins/build`
  - `https://developers.openai.com/codex/subagents`
  - `https://developers.openai.com/codex/hooks`
  - `https://developers.openai.com/codex/agent-approvals-security`
  - `https://developers.openai.com/codex/rules`
