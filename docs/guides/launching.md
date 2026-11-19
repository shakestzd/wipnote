# Launching Claude Code and Codex with wipnote

Moved from `CLAUDE.md` in slice 12 of plan-2622b3f7 (feat-6f99bc47). Load this page when you work on a launcher.

## Claude Code

```bash
wipnote claude   # uses the bundled plugin in ~/.local/share/wipnote/plugin/
```

The launcher resolves each tree in this order. Code: `cmd/wipnote/share_paths.go`.

1. The env override (`WIPNOTE_PLUGIN_DIR`, `WIPNOTE_CODEX_DIR`, `WIPNOTE_ANTIGRAVITY_DIR`, `WIPNOTE_MODS_DIR`).
2. `~/.local/share/wipnote/<tree>/`.
3. `$(brew --prefix)/share/wipnote/<tree>/`.
4. The dev source, found by walking up from the working directory to `go.mod`.

It passes the plugin tree to Claude Code with `--plugin-dir`. Hooks and agents load from that tree. No skills or slash commands ship from `plugin/` any more.

### Mods

The launcher also passes `--plugin-dir <mods>`. That folder holds wipnote's Claude Code mods:

- `wipnote-orchestra`: the band under the prompt.
- `wipnote-plan-review`: a plan's slices and their approval state in a side pane.
- `wipnote-ci`: a pull request's CI in a side pane.
- `wipnote-visuals`: a receipt line under each turn, and side-by-side compare blocks.
- `wipnote-questions`: the in-tree mod beside the others.

Claude Code loads every child of that folder. `WIPNOTE_NO_MODS=1` skips them. `WIPNOTE_MODS_DIR=<path>` loads another folder.

### Shell aliases

Bare `claude` can route through wipnote. Run `wipnote shell-alias >> ~/.zshrc` to opt in.

For short names (`wcc` = `wipnote claude`, `wcx` = `wipnote codex`, `wag` = `wipnote antigravity`), run `wipnote shell-alias --short --install`. It adds one managed block to your shell rc file. `wipnote shell-alias --uninstall` removes it. Per-project `wipnote claude` is the recommended flow.

## Dev mode

```bash
wipnote claude --dev   # loads plugin from in-tree plugin/ via --plugin-dir
wipnote dev            # shortcut for the same
```

Dev mode skips the bundled tree resolver. It points Claude Code at the in-tree `plugin/` directory, so edits there load at once. It also clears any legacy marketplace install.

Full removal is required. Disabling a legacy marketplace plugin only affects hooks. Agent definitions would still load from `~/.claude/plugins/marketplaces/` and shadow your source changes.

## Resuming a session

`wipnote claude`, `wipnote yolo` and `wipnote dev` accept `--resume <session-id>`. Claude Code prints the ID on exit as `claude --resume <id>`.

```bash
wipnote claude --resume <session-id>
wipnote claude --dev --resume <session-id>
wipnote yolo --dev --resume <session-id>
wipnote dev --resume <session-id>
```

`--continue` resumes the most recent session instead. If you pass both, `--resume <id>` wins.

## Codex

```bash
wipnote codex   # launches Codex CLI with the wipnote plugin
```

The plugin's bundled `hooks.json` delivers Codex hooks. `wipnote codex --init` also mirrors wipnote's agent definitions into `~/.codex/agents/`. On every launch, `pruneCodexGlobalHooksFromCache` (`cmd/wipnote/codex_hooks_agents.go`) removes legacy hook entries from `~/.codex/hooks.json`. That keeps the plugin's hooks the single source of truth.
