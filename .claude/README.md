# Repo-Local Claude Code Configuration

`.claude/` holds hand-maintained development tooling for working on **this repository**:

- `.claude/rules/*.md` — path-scoped rules loaded by Claude Code (code hygiene, debugging, deployment, plugin layering, version sync, worktree artifacts)
- `.claude/skills/deploy/` — repo-local `/deploy` release skill (not shipped to plugin users)
- `.claude/settings.json` — shared repo settings (committed to git)
- `.claude/settings.local.json` — personal overrides (gitignored)

## Not a Build Output

`.claude/` is **not** synced from `plugin/` and is never overwritten by `wipnote plugin build-ports`.

- The distributable Claude Code plugin lives in `plugin/` (generated manifest/hooks + shared assets).
- Companion Claude Code mods live in `mods/`.
- When developing wipnote locally, launch with `wipnote claude --dev` (or `wipnote dev`), which passes the in-tree `plugin/` and `mods/` via `--plugin-dir`.

See `.claude/rules/plugin-source-layering.md` and `docs/guides/launching.md` for details.
