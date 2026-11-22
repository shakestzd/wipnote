# wipnote Installation Guide

## Prerequisites

- Go 1.25+ (for building from source; see `go.mod`)
- Git

---

## Install the CLI

```bash
# Universal installer (recommended — installs binary + bundled plugin trees & mods)
curl -fsSL https://raw.githubusercontent.com/shakestzd/wipnote/main/scripts/install.sh | bash

# POSIX sh alternative (supports --version, --install-dir, optional cosign verification)
curl -fsSL https://raw.githubusercontent.com/shakestzd/wipnote/main/install.sh | sh

# Or build from source
git clone https://github.com/shakestzd/wipnote.git
cd wipnote && go build -o ~/.local/bin/wipnote ./cmd/wipnote/
```

The release tarball installs the `wipnote` binary in `~/.local/bin/` and the bundled trees (`plugin/`, `codex-marketplace/`, `antigravity-extension/`, `mods/`) in `~/.local/share/wipnote/`.

### Upgrading

```bash
wipnote upgrade            # latest release
wipnote upgrade --check    # check without installing
```

---

## Claude Code Integration

The Claude Code plugin and companion mods are bundled in the release tarball — there is no separate `claude plugin install` step. `wipnote claude` passes the bundled `plugin/` and `mods/` trees to Claude Code via `--plugin-dir` automatically:

```bash
wipnote claude            # launch Claude Code with bundled plugin and mods
wipnote claude --init     # launch in init mode pinned to the current directory
```

See `docs/guides/launching.md` for tree resolution order, `WIPNOTE_NO_MODS=1`, and shell aliases (`wipnote shell-alias`).

### Reply visuals: install lulawrite

wipnote draws work-item cards, the turn receipt and the writing score. It draws no other block in a reply. Steps, options and callouts come from the lulawrite plugin, which checks and draws them. Install it once, inside Claude Code:

```
/plugin marketplace add shakestzd/luladoc
/plugin install lulawrite@luladoc
/lulawrite-install
```

Then select the `lulawrite:Precise` output style with `/config`. Claude Code runs one output style at a time. Precise carries the plain-writing rules of wipnote's own `Plain English` style and adds the block rules, so use Precise when lulawrite is installed.

### Dev mode (dogfooding from source)

```bash
wipnote claude --dev      # links local plugin source and launches Claude Code
```

### Resume sessions

```bash
wipnote claude --continue              # resume the last session
wipnote claude --resume <session-id>   # resume a specific session by UUID
```

---

## Antigravity Integration

> Antigravity supersedes the Gemini CLI harness, which was retired as a launch/generation
> target (feat-02f25a24). wipnote still reads historical Gemini CLI session data (the
> ingest/classification read path is retained), but `wipnote gemini` is no longer a command.

The wipnote Antigravity extension is bundled with each release tarball
(`antigravity-extension/`, installed to `~/.local/share/wipnote/antigravity-extension/`)
and installed locally via the `agy` CLI's own plugin mechanism — there is no separate
git-ref/tag download step.

### Install

```bash
wipnote antigravity --init          # installs the bundled extension via `agy plugin install`
wipnote antigravity                 # launch Antigravity with wipnote context
```

```bash
wipnote antigravity --init --force  # reinstall over an existing install
```

### Resume sessions

```bash
wipnote antigravity --continue          # agy --continue (resume the most recent conversation)
wipnote antigravity --resume <id>       # agy --conversation <id> (resume a specific conversation)
```

### Dev mode (dogfooding from source)

```bash
wipnote antigravity --dev               # links port/packages/antigravity-extension/ and launches
```

Dev mode links the in-tree `port/packages/antigravity-extension/` (idempotent) before
launching, so changes to the generated tree are picked up without reinstalling. Regenerate
it with `wipnote plugin build-ports --target antigravity` after editing shared `plugin/`
assets or `packages/plugin-core/manifest.json`.

---

## Codex CLI Integration

```bash
wipnote codex --init      # registers the wipnote Codex marketplace
wipnote codex             # launch Codex CLI with wipnote context
```

### Resume sessions

```bash
wipnote codex --continue             # codex resume --last
wipnote codex --resume <session-id>  # codex resume <id>
```

### Dev mode

```bash
wipnote codex --dev       # registers port/packages/codex-marketplace/ locally and launches Codex
```

---

## Initialize in a project

After installing the CLI and at least one AI tool integration:

```bash
cd /your/project
wipnote init              # creates the store under ~/.wipnote/projects/ (--in-repo: .wipnote/ in the project)
```

---

## Verify installation

```bash
wipnote version           # prints version information
wipnote status            # project health overview
```
