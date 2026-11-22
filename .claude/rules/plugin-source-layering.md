---
paths:
  - "packages/plugin-core/**"
  - "plugin/**"
  - "port/pluginbuild/**"
---

# Plugin Source Layering

wipnote ships one plugin to Claude Code, Codex CLI and Antigravity from a single source. Each target's tree is generated. Never hand-edit it. Gemini CLI is retired as a target (see `AGENTS.md`), but wipnote still reads old Gemini session data through the ingest read path.

| Layer | What lives there | Edit? |
|-------|------------------|-------|
| `packages/plugin-core/manifest.json` | Plugin metadata, per-target output paths, hook event matrix | YES |
| `plugin/agents/`, `plugin/templates/`, `plugin/static/`, `plugin/config/`, `plugin/output-styles/` | Shared assets, copied verbatim into every target tree | YES |
| `cmd/`, `core/hooks/`, `internal/` | Go CLI and hook handlers (each hook is a thin wrapper over `wipnote hook <handler>`) | YES |
| `plugin/.claude-plugin/plugin.json`, `plugin/hooks/hooks.json` | Generated Claude Code files | NO, regenerate |
| `port/packages/codex-marketplace/` | Generated Codex CLI tree | NO, regenerate |
| `port/packages/antigravity-extension/` | Generated Antigravity tree | NO, regenerate |
| `~/.claude/plugins/marketplaces/` (user home, legacy install) | Wiped by `removeMarketplaceWipnote` before every `--dev` launch | NO, do not install here |

`plugin/commands/` and `plugin/skills/` no longer exist, and no skill ships (a test in `port/pluginbuild/guidance_size_test.go` fails if one returns).

**The repo-root `.claude/` is not a build output.** The Claude target's `outDir` is `plugin/` itself. `build-ports --target claude` writes only the two generated files above. This repo's `.claude/` (rules, skills, settings) is hand-kept dev tooling that no wipnote code path syncs or deletes. `removeMarketplaceWipnote` in `cmd/wipnote/claude.go` targets the user's home `~/.claude/plugins/marketplaces/`, never the repo.

## Regenerate after every manifest or asset edit

```bash
wipnote plugin build-ports                      # all targets
wipnote plugin build-ports --target codex       # or claude, antigravity
```

See `packages/plugin-core/README.md` for the per-task recipes and `.claude/rules/plugin-development.md` for the full structure.
