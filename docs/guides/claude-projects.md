# wipnote in Claude project threads

A Claude project runs each thread as a Claude Code cloud session in a cloud
environment. A thread reads the repository, but it does not load most of the
repository's Claude Code setup. This guide lists what a thread loads and how to
add wipnote to it.

## What a project thread loads

| Loads in a thread | Does not load in a thread |
|---|---|
| `CLAUDE.md` at the repository root | repo hooks (`.claude/settings.json` `hooks`) |
| `.claude/CLAUDE.md` | repo plugins and plugin settings |
| `.claude/rules/*.md` | repo skills, agents and slash commands |
| Project instructions (Project settings > Memory) | `.mcp.json` servers |
| Plugins added in Project settings > Plugins | permission rules and `env` from `.claude/settings.json` |
| Tools the cloud environment's setup script installs | |

Source: the Claude Code docs on Claude projects and on Claude Code on the web
(checked 2026-10-25). Re-check them when this table looks wrong: the contract
can change without notice.

## The four parts wipnote needs

| Part | What it is | How to add it |
|---|---|---|
| binary | `wipnote` on `PATH` | the cloud environment's setup script runs `scripts/cloud-env-setup.sh` |
| plugin | the wipnote plugin | Project settings > Plugins |
| hooks | the plugin's hooks, run in this session | come with the plugin; they call the binary, so add the binary first |
| rules | a file the thread loads that names wipnote | `.claude/rules/wipnote.md` in the repository |

`wipnote doctor --project-thread` checks all four (see below).

### 1. Install the binary with the setup script

`scripts/cloud-env-setup.sh` installs one pinned wipnote release (the binary
and its bundled plugin trees) from the project's GitHub release assets, the
same assets `scripts/install.sh` uses, and checks the tarball against the
release's checksums file. It needs no secrets.

In the cloud environment's settings, set the setup script to:

```bash
bash scripts/cloud-env-setup.sh
```

For a repository other than wipnote, copy `scripts/cloud-env-setup.sh` into it
first, so the pin is reviewed with the rest of the repository.

- **Pinned.** The version is `WIPNOTE_PIN` at the top of the script.
  `WIPNOTE_VERSION=X.Y.Z` overrides it; `latest` is refused.
- **Idempotent.** A second run with the same pin finds the installed version
  and downloads nothing.
- **Never blocks startup.** When the download, checksum or install fails, the
  script prints why and exits 0, so the environment still starts and the doctor
  reports the missing binary. Set `WIPNOTE_SETUP_STRICT=1` to make it exit 1.
- **Where it installs.** `~/.local/bin` and `~/.local/share/wipnote`. If
  `~/.local/bin` is not on the thread's `PATH`, set `WIPNOTE_BIN_DIR` to a
  directory that is (for example `/usr/local/bin` when the script runs as root).

### 2. Add the plugin in Project settings > Plugins

Add the wipnote plugin marketplace (the wipnote GitHub repository, whose
`.claude-plugin/marketplace.json` lists the `wipnote` plugin) and enable the
`wipnote` plugin. The plugin brings wipnote's hooks, agents, skills and slash
commands into every new thread. A plugin enabled in the repository's
`.claude/settings.json` does not count: threads do not read it.

Start a new thread after adding the plugin: a running thread does not pick it up.

### 3. Add a rule the thread loads

Threads load `.claude/rules/*.md`, so a rule is the one part that lives in the
repository. A `CLAUDE.md` that already names wipnote is enough. Otherwise
commit `.claude/rules/wipnote.md`:

```markdown
# wipnote

This repository tracks work with wipnote. Work items, plans and delegations
live in `.wipnote/` and are the source of truth.

- Run `wipnote help --compact` for the CLI reference.
- Before starting work, find or create the work item and claim it.
- In a Claude project thread, run `wipnote doctor --project-thread` first and
  follow the fix it names for any missing part.
- Delegate one-shot calls with `wipnote delegate run -- <codex exec|claude -p ...>`.
```

For a plan's context, paste the output of
`wipnote plan project-instructions <plan-id>` into Project settings > Memory >
Project instructions. The plan in `.wipnote/` stays canonical; the project
holds a copy, so paste it again after the plan changes.

### 4. Check the thread with the doctor

```bash
wipnote doctor --project-thread
```

It prints one line per part, `pass` or `fail`, and under each failed part the
fix. It exits non-zero when a part is missing. It only reads; it changes nothing.

```text
pass  binary: ~/.local/bin/wipnote
fail  plugin: the wipnote plugin is not installed
        fix: add the wipnote plugin in Project settings > Plugins (...)
fail  hooks: no wipnote hook has run in this session (WIPNOTE_SESSION_ID is not set)
        fix: add the wipnote plugin in Project settings > Plugins and start a new thread; (...)
pass  rules: .claude/rules/wipnote.md names wipnote
```

How it decides each part:

- **binary**: `wipnote` is found on `PATH`.
- **plugin**: a `.claude-plugin/plugin.json` named `wipnote` is under
  `~/.claude/plugins` (or at `CLAUDE_PLUGIN_ROOT`), or the hooks ran.
- **hooks**: the SessionStart hook ran for this session: `WIPNOTE_SESSION_ID`
  is set (the hook exports it), or `.wipnote/.active-session` names this
  session. Only SessionStart is checked; every other hook depends on it.
- **rules**: `CLAUDE.md`, `.claude/CLAUDE.md` or a `.claude/rules/*.md` names
  wipnote. `@` imports are not followed.

## Delegation in a thread

A thread has no herdr, so `wipnote delegate run` runs headless. Its record does
not depend on hooks: the run folder under `.wipnote/delegate/runs/` holds the
answer and exit files, the ledger row in `.wipnote/delegations-ledger.html` is
opened and closed by `delegate run` itself, and `delegate view <run-id>` reads
the run back. For an interactive worker, `delegate wait` ends on the worker's
result file and `delegate result` prints it, with no hook event needed. Starting
an interactive worker (`delegate spawn`) needs herdr panes, which a thread does
not have.

Without the hooks, a thread loses session tracking, prompt and tool events,
and automatic attribution of work to the claimed item. The binary and the
ledger keep working.

## See also

- `docs/design/delegate.md`: the delegate design, including decision R (result
  files) that keeps delegation correct without hooks.
- `scripts/install.sh`: the interactive installer for a workstation.
