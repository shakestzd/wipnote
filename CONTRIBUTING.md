# Contributing to wipnote

wipnote is developed using wipnote itself (dogfooding). `.wipnote/` contains real work items.

## Branch Strategy

`main` only. All changes go directly to `main` via pull request.

## Setup

```bash
git clone https://github.com/shakestzd/wipnote.git
cd wipnote

# First build (bootstraps from source)
go build -o wipnote ./cmd/wipnote/

# All subsequent rebuilds
wipnote build
# equivalent: plugin/build.sh
# outputs to: plugin/hooks/bin/wipnote
```

## Layout

| Path | Role |
|------|------|
| `cmd/wipnote/` | CLI entry points |
| `internal/` | Business logic |
| `plugin/` | Agents, skills, hooks, commands — single source of truth |
| `.wipnote/` | Work items and session data (generated, not edited directly) |

**Never edit `.claude/`** — it is auto-synced from `plugin/` and changes are lost.

## Dev Mode

```bash
wipnote claude --dev   # loads plugin from plugin/ via --plugin-dir
```

Uninstalls the marketplace plugin, clears cache, and launches Claude Code with `--plugin-dir plugin/`. Reinstalls on exit.

## Quality Gates

Run before every commit:

```bash
go build ./... && go vet ./... && go test ./...
```

All three must pass. No exceptions — fix pre-existing errors too.

## Making Changes

1. Create a work item: `wipnote feature create "title" --track <trk-id> --description "..."`
2. Start it: `wipnote feature start <feat-id>`
3. Make changes and run quality gates
4. Complete: `wipnote feature complete <feat-id>`
5. Push and open a PR to `main`

## Release (Maintainers)

Version lives in `plugin/.claude-plugin/plugin.json`.

```bash
./scripts/deploy-all.sh X.Y.Z --no-confirm
```

The deploy script updates both the CLI binary and the plugin. Never update one without the other.

## Getting Help

- `wipnote help --compact` — CLI reference
- Issues: https://github.com/shakestzd/wipnote/issues

## License

MIT. Contributions are licensed under the same terms.
