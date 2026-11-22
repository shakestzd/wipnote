# Contributing to wipnote

wipnote is developed using wipnote itself (dogfooding). `.wipnote/` contains real work items — not demos.

## Prerequisites

- Go 1.25+ (see `go.mod`)
- Git
- `ripgrep` (`rg`, used by search and CI checks)

## Branch Strategy

`main` only. All changes go to `main` via pull request.

## Setup

```bash
git clone https://github.com/shakestzd/wipnote.git
cd wipnote

# First build (bootstraps from source into ~/.local/bin/wipnote)
go build -o ~/.local/bin/wipnote ./cmd/wipnote/

# All subsequent rebuilds (rebuilds binary and mirrors bundled trees to ~/.local/share/wipnote/)
wipnote build
```

## Repository Layout

wipnote is a multi-module Go monorepo with a shared plugin build pipeline and companion Claude Code mods:

| Path | Role |
|------|------|
| `cmd/wipnote/` | CLI entry points, harness launchers, and embedded prompts |
| `core/` | Core Go module — models, HTML store/parser, ledgers, hooks, harness registry, worktrees, ingest |
| `plan/` | Planning Go module — CRISPI YAML plans, slices, templates, critique |
| `port/` | Plugin-porting Go module — `pluginbuild` generator and generated `port/packages/{codex-marketplace,antigravity-extension}/` trees |
| `observe/` | Observability Go module — OTel collector/receiver/indexer and pricing |
| `recap/` | Grounded recap HTML templates and renderers |
| `internal/` | CLI-internal logic (delegation, gates, lineage, gitwatch, launcher, status) |
| `packages/plugin-core/` | Single source of truth `manifest.json` for all harness plugin ports |
| `plugin/` | Shared plugin assets (`agents/`, `templates/`, `static/`, `config/`, `output-styles/`) plus generated Claude Code manifest & hooks (no `commands/` or `skills/` ship from the plugin) |
| `mods/` | Bundled Claude Code mods (`wipnote-orchestra`, `wipnote-plan-review`, `wipnote-ci`, `wipnote-visuals`, `wipnote-questions`) |
| `.claude/` | Repo-local Claude Code dev tooling (rules, repo skills, settings) — hand-maintained, not generated |
| `.wipnote/` | Real work items and ledgers (managed via the `wipnote` CLI) |

### Generated Plugin Trees

Do **not** hand-edit generated plugin outputs:
- `plugin/.claude-plugin/plugin.json`
- `plugin/hooks/hooks.json`
- `port/packages/codex-marketplace/`
- `port/packages/antigravity-extension/`

Edit `packages/plugin-core/manifest.json` or shared assets under `plugin/` and regenerate with:

```bash
wipnote plugin build-ports
```

See `packages/plugin-core/README.md` and `.claude/rules/plugin-source-layering.md` for details.

## Dev Mode

```bash
wipnote claude --dev   # or: wipnote dev — loads in-tree plugin/ and mods/ via --plugin-dir
wipnote codex --dev    # registers port/packages/codex-marketplace/ locally
wipnote antigravity --dev  # links port/packages/antigravity-extension/ locally
```

`wipnote claude --dev` points Claude Code at the in-tree `plugin/` and `mods/` directories directly and removes any legacy marketplace install under `~/.claude/plugins/marketplaces/` so stale agents cannot shadow local edits. See `docs/guides/launching.md` for details.

## Quality Gates

Run before every commit:

```bash
go build ./... && go vet ./... && scripts/go-test-streaming.sh ./...
```

Because `core/`, `plan/`, `port/`, and `observe/` are separate Go modules (`go.mod`), also test any module you touch (CI runs all five modules), or run the full gate check:

```bash
wipnote check --gate
```

Use `scripts/go-test-streaming.sh` instead of bare `go test` for full runs so output streams live without per-package buffering. All gates must pass on files you changed.

## Making Changes

1. Create a work item: `wipnote feature create "title" --track <trk-id> --description "..."`
2. Start it: `wipnote feature start <feat-id>`
3. Make changes, regenerate ports if needed (`wipnote plugin build-ports`), and run quality gates
4. Complete: `wipnote feature complete <feat-id>`
5. Push and open a PR to `main`

## Release (Maintainers)

Version source of truth lives in `packages/plugin-core/manifest.json` (synced to `plugin/.claude-plugin/plugin.json` and `.claude-plugin/marketplace.json`).

```bash
./scripts/deploy-all.sh X.Y.Z --no-confirm
```

The deploy script runs quality gates, bumps versions, regenerates plugin ports, tags, pushes, triggers GoReleaser, and rebuilds the local CLI binary and bundled share trees.

## Getting Help

- `wipnote help --compact` — CLI reference
- `docs/guides/launching.md` — launch modes, mods, dev mode, resume
- Issues: https://github.com/shakestzd/wipnote/issues

## License

MIT. Contributions are licensed under the same terms.
