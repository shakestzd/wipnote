# wipnote Installation Guide

## Prerequisites

- Go 1.21+ (for building from source)
- Git

---

## Install the CLI

```bash
# Universal installer (recommended)
curl -fsSL https://raw.githubusercontent.com/shakestzd/wipnote/main/install.sh | sh

# Or build from source
git clone https://github.com/shakestzd/wipnote.git
cd wipnote && go build -o ~/.local/bin/wipnote ./cmd/wipnote/
```

### Upgrading

```bash
wipnote upgrade            # latest release
wipnote upgrade --check    # check without installing
```

---

## Claude Code Integration

Install the wipnote plugin from the Claude Code marketplace:

```bash
wipnote claude --init     # registers the marketplace and installs the plugin
wipnote claude            # launch Claude Code with wipnote context
```

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

## Gemini CLI Integration

The wipnote Gemini extension is distributed via the `gemini-extension-dist` branch of
this repository, published automatically on every release as a `gemini-extension-v<version>`
tag.

### Install

```bash
wipnote gemini --init     # installs the extension matching the wipnote binary version
wipnote gemini            # launch Gemini CLI with wipnote context
```

The `--init` command runs:

```
gemini extensions install shakestzd/wipnote --ref gemini-extension-v<version> --consent --skip-settings
```

Where `<version>` matches the currently installed `wipnote` binary. Pass `--ref` to
override:

```bash
wipnote gemini --init --ref gemini-extension-v0.55.6   # pin a specific version
wipnote gemini --init --force                          # reinstall over existing
```

### Resume sessions

Gemini uses session **indices** (integers), not UUIDs. List sessions to find the index:

```bash
wipnote gemini --list-sessions    # gemini --list-sessions
wipnote gemini --continue         # gemini --resume latest
wipnote gemini --resume 3         # gemini --resume 3
```

### Dev mode (dogfooding from source)

```bash
wipnote gemini --dev              # links packages/gemini-extension/ as a live pointer
wipnote gemini --dev --isolate    # also passes -e wipnote to suppress other extensions
```

Dev mode runs `gemini extensions link /abs/path/to/packages/gemini-extension` (idempotent)
before launching. The live link means changes to `packages/gemini-extension/` are picked
up immediately without reinstalling.

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
wipnote codex --dev       # registers packages/codex-marketplace/ locally and launches Codex
```

---

## Initialize in a project

After installing the CLI and at least one AI tool integration:

```bash
cd /your/project
wipnote init              # creates .wipnote/ and installs hooks
```

---

## Verify installation

```bash
wipnote version           # prints version information
wipnote status            # project health overview
wipnote serve             # starts the local dashboard at localhost:4000
```
