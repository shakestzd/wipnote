# Delegate interactive parity (spk-87139e0e)

Question from plan-28b4a7f6, slice 8: can a spawned interactive session (a pane the
delegator and a human can both talk to) be given the same load controls as a headless
run (`claude -p`, `codex exec`)? Can the caller still get a final result, an exit
status and token cost from it?

Probed on 2026-10-25 in a Linux container with Claude Code 2.1.310 and Codex CLI
0.164.0 (installed into a scratch dir with npm, no real credentials). All probes ran
from a scratch dir with `WIPNOTE_PROJECT_DIR` and `CLAUDE_PROJECT_DIR` unset. Paths
below are shortened to `<scratch>`.

Legend: **doc** = upstream docs say so. **probe** = seen in a run here. **not run here** =
needs a real Codex login (owner's Mac); the command to run is in the last section.

## Verdict

**Load controls: parity.** Every control on the list is honored the same way by an
interactive launch and a headless launch, in both harnesses. No load control forces
headless.

**Result, exit status and cost: no parity out of the box.** An interactive run gives
none of them on its own process contract. They can be collected from the side
(hooks, status line, transcript files). wipnote has to build that collection.

**Go or no-go for `wipnote delegate run` interactive-only:**

- **Claude Code: conditional GO.** Every needed signal was seen in a real
  interactive run. Slice 8 must build the wrapper listed under "What slice 8 must
  build". Without it, it is a no-go.
- **Codex CLI: HOLD (no-go for now).** Load parity is proven. The result and token
  capture in the TUI is not proven, because no real turn could run here. Keep the
  `codex exec` path until the owner's Mac probes below pass.

## Claude Code parity table

Fixture: a plugin dir with SessionStart, Stop, StopFailure and SessionEnd hooks that
dump their stdin JSON and `PROBE_*` env vars; a `--settings` file with an `env` key,
allow and deny rules and a `statusLine` command that dumps its stdin; a stdio MCP
server with one tool; an appended system prompt holding a codeword. The same flags
went to `claude -p --output-format stream-json --verbose` (headless) and to `claude`
in tmux (interactive):

```
--plugin-dir <scratch>/plug --append-system-prompt "The codeword is PELICAN-7731. ..."
--allowedTools Read --disallowedTools WebFetch --model haiku --effort low
--mcp-config <scratch>/mcp.json --strict-mcp-config --permission-mode plan
--settings <scratch>/settings.json --add-dir <scratch>/extra
```

| Control | Doc | Headless probe | Interactive probe | Parity |
|---|---|---|---|---|
| Plugin dir `--plugin-dir` | [cli-reference](https://code.claude.com/docs/en/cli-reference): "Load a plugin ... for this session only". No mode limit | `system/init.plugins` lists `probeplug@inline`; its hooks fired | Its hooks fired; `/probeplug:probecmd` shows in the slash menu | Yes |
| System prompt append `--append-system-prompt` | [cli-reference, System prompt flags](https://code.claude.com/docs/en/cli-reference): "All five work in both interactive and non-interactive modes" | Asked for the codeword, result `PELICAN-7731` | Same question, reply `PELICAN-7731` | Yes |
| Allowed tools `--allowedTools` | [cli-reference](https://code.claude.com/docs/en/cli-reference), no mode limit | Not printed in init (rules) | `/permissions` Allow tab: `Read`, plus `Bash(echo probe-allow *)` from `--settings` | Yes (interactive shown; headless applies the same rules, see [headless](https://code.claude.com/docs/en/headless)) |
| Denied tools `--disallowedTools` | [cli-reference](https://code.claude.com/docs/en/cli-reference): a bare name removes the tool | `WebFetch` missing from `system/init.tools` | `/permissions` Deny tab: `WebFetch`, plus `Bash(rm *)` from `--settings` | Yes |
| Model `--model` | [cli-reference](https://code.claude.com/docs/en/cli-reference), no mode limit | Without plan mode: `modelUsage` has only `claude-haiku-5-5` | Without plan mode: transcript `"model":"claude-haiku-5-5"`; status line `Haiku 5.5` | Yes. See the plan-mode note below |
| Effort `--effort` | [cli-reference](https://code.claude.com/docs/en/cli-reference); [hooks](https://code.claude.com/docs/en/hooks) common field `effort` | Stop payload `"effort":{"level":"low"}` | Stop payload `"effort":{"level":"low"}`; header "with low effort" | Yes |
| MCP config `--mcp-config` + `--strict-mcp-config` | [cli-reference](https://code.claude.com/docs/en/cli-reference). With `-p` it waits for pending servers before the first turn | `mcp_servers: [{name: probemcp, status: connected}]`, tool `mcp__probemcp__probe_ping` | `/mcp`: `✔ probemcp 1 tool`; `/status`: `MCP servers: 1 connected` | Yes |
| Permission mode `--permission-mode` | [cli-reference](https://code.claude.com/docs/en/cli-reference) | `permissionMode: plan`; Stop payload `permission_mode: plan` | Stop payload `permission_mode: plan` (and `dontAsk` in run 2); footer "plan mode on" | Yes |
| Settings file `--settings` | [settings](https://code.claude.com/docs/en/settings): command line sits above project and user files | `PROBE_SETTINGS_ENV=from-settings` reached hooks | Same env in hooks; status line command ran; deny and allow rules shown; `/status`: `Setting sources: User settings, Command line arguments` | Yes |
| Working dir (process cwd) + `--add-dir` | [cli-reference](https://code.claude.com/docs/en/cli-reference) `--add-dir` | `cwd` = `<scratch>/work` in init and every hook | `cwd` = `<scratch>/work` in hooks and `/status`; status line `workspace.added_dirs` = `[<scratch>/extra]` | Yes |
| Env (launch environment) | [hooks](https://code.claude.com/docs/en/hooks): a hook process inherits the parent env | `PROBE_ENV=launch-env` in hook env | `PROBE_ENV=launch-env` in hook env | Yes |
| Session id `--session-id` | [cli-reference](https://code.claude.com/docs/en/cli-reference) | n/a | Fixed UUID used; transcript file named by it | Yes (caller knows the transcript path in advance) |
| Print-only: `--output-format`, `--json-schema`, `--max-turns`, `--max-budget-usd`, `--no-session-persistence`, `--permission-prompts` | [cli-reference](https://code.claude.com/docs/en/cli-reference): "(print mode only)" | Honored | `--output-format json --max-budget-usd 0.000001 --max-turns 1`: no error, no effect, turn ran and session stayed open | **No.** Silently ignored |

Plan-mode note (probe, ambiguous cause): with `--model haiku --permission-mode plan` the
turn ran on `claude-sonnet-5-5` in **both** modes (headless `modelUsage` and the
interactive transcript), while the interactive header and `/status` still said Haiku.
Without plan mode both ran Haiku. So it is not a parity gap, but a plan-mode delegate
does not run on the model the caller asked for, and the TUI header hides it. The docs I
read do not explain it.

### Claude Code startup differences that matter for a spawned pane

| Case | Headless | Interactive (probe) |
|---|---|---|
| First launch in a new dir | Trust dialog skipped ([cli-reference](https://code.claude.com/docs/en/cli-reference) `-p`) | Theme picker, security notes and the workspace trust dialog all block. On that first launch the positional prompt was not sent after the dialogs were cleared. On the second launch in the trusted dir the positional prompt ran. Ambiguous: dropped prompt seen once only |
| Bad `--permission-mode` | not probed (same flag parser) | `error: option '--permission-mode <mode>' argument 'bogus' is invalid`, exit 1 |
| Missing `--settings` file | not probed | `Error: Settings file not found`, exit 1 |
| Missing `--mcp-config` file | not probed | `Error: Invalid MCP configuration`, exit 1 |
| Missing `--plugin-dir` | Run continues; `system/init.plugin_errors[0].type = path-not-found` | Starts with no message; only `/plugin` shows `Errors (1)` |
| Unknown `--model` | not probed | Session stays open; the TUI shows "There's an issue with the selected model" |

Launcher gotcha (both modes): `--mcp-config` takes many values, so a prompt placed after
it is read as a second config file (`MCP config file not found: <scratch>/work/reply ok`).
Put the prompt before variadic flags or after `--`.

## Codex CLI parity table

Codex has no per-flag plugin dir, append-prompt or tool lists. Those go through config
keys, passed with `-c key=value`. `codex --help` and `codex exec --help` (0.164.0) list the
same shared flags: `-c`, `--enable/--disable`, `-m`, `-p/--profile`, `-s`, `-a` (top level
only), `--approve-for-me`, `-C`, `--add-dir`, `--worktree`, `--dangerously-bypass-hook-trust`.
Exec-only: `--json`, `-o/--output-last-message`, `--output-schema`, `--ephemeral`,
`--ignore-user-config`, `--ignore-rules`, `--skip-git-repo-check`. TUI-only: `--remote`,
`--no-alt-screen`, `--no-daemon`.

Fixture: both modes launched with `-m gpt-probe-model -c model_reasoning_effort="low"
-s workspace-write -c developer_instructions="The codeword is PELICAN-7731."`, a stdio
MCP server via `-c mcp_servers.probemcp.*`, inline SessionStart, Stop and SessionEnd hooks
via `-c hooks.*` with `--dangerously-bypass-hook-trust`, `-C <scratch>/cxwork` and
`--add-dir <scratch>/cxwork/extra` (TUI also `-a never`). A fake API key got past the login
screen, so every turn failed with 401. The rollout file still records the turn context.

| Control | Doc | `codex exec` probe | TUI probe | Parity |
|---|---|---|---|---|
| Plugins | [config-reference](https://learn.chatgpt.com/docs/config-file/config-reference): `plugins.<plugin>.enabled`; `codex plugin --help` (marketplace add/list) | not run here | not run here | Doc: same config in both. Not proven |
| Developer instructions (append-prompt equivalent) | [config-reference](https://learn.chatgpt.com/docs/config-file/config-reference): `developer_instructions` | Rollout developer message holds `PELICAN-7731` | Rollout developer message holds `PELICAN-7731` | Yes |
| Model `-m` | config-reference `model` | `turn_context.model = gpt-probe-model` | Same; `/status`: `Model: gpt-probe-model (reasoning low ...)` | Yes |
| Effort `model_reasoning_effort` | config-reference | `turn_context.effort = low` | `turn_context.effort = low` | Yes |
| MCP servers `mcp_servers.*` | config-reference; `required` = "fail startup/resume if this enabled MCP server cannot initialize" | `required=true` with a bad command: "required MCP servers failed to initialize", exit 1 | `/mcp`: `probemcp: connected (1 tool)`; bad required server: same error, exit 1 | Yes |
| Sandbox `-s` | config-reference `sandbox_mode`; [non-interactive mode](https://learn.chatgpt.com/docs/non-interactive-mode): exec defaults to read-only | `sandbox_policy.type = workspace-write`, writable root `extra` | Same | Yes |
| Approval `-a` | config-reference `approval_policy` | `approval_policy = never` (not passed; exec default) | `approval_policy = never` (passed `-a never`) | Yes. `-a` is not an exec flag; set it in exec with `-c approval_policy=...` |
| Hooks (as a carrier for results) | [hooks](https://learn.chatgpt.com/docs/hooks): SessionStart, Stop, SessionEnd, ... The page does not say whether hooks run in exec | SessionStart and SessionEnd fired | SessionStart and SessionEnd fired | Yes. Stop did not fire in either mode (turn failed before any reply) |
| Config errors | n/a | `-c sandbox_mode="bogus"`: `Error loading config.toml: unknown variant`, exit 1 | Same error, exit 1 | Yes |
| Working dir `-C`, `--add-dir` | `codex --help` | `cwd` = `cxwork` in hooks and rollout | Same; `/status` Directory | Yes |
| Env | config-reference `shell_environment_policy` | `PROBE_ENV=launch-env` in hook env | Same | Yes (hooks). Tool subprocess env follows `shell_environment_policy` in both, by doc |
| Settings file | `-p/--profile` layers `$CODEX_HOME/<name>.config.toml`; `CODEX_HOME` picks the home | `CODEX_HOME` honored (scratch home used) | Same | Yes. `--ignore-user-config` is exec-only |
| Permission mode reported to hooks | hooks doc: `permission_mode` on SessionStart | `bypassPermissions` for `workspace-write` + `never` | `bypassPermissions` | Same in both. The label is odd for a sandboxed run |

## Result, exit status and cost from an interactive run

### Claude Code (all probe unless marked doc)

- **Final result:** the Stop hook payload carries `last_assistant_message`
  (`"PELICAN-7731"`, `"ok"`). Same field in headless and interactive.
  [hooks, Stop input](https://code.claude.com/docs/en/hooks) says to use this field rather
  than the transcript. API errors fire StopFailure instead (doc). A Stop hook is also the
  only clean "turn done" signal; the session does not exit after the turn.
- **Exit status:** `/exit` gives exit 0 with SessionEnd `reason: prompt_input_exit`.
  SIGTERM gives exit 143 with SessionEnd `reason: other` (same as
  [headless](https://code.claude.com/docs/en/headless) documents for `-p`). The exit code
  says nothing about whether the task worked. Headless `--output-format json` has
  `is_error`, `subtype`, `terminal_reason`; interactive has no equivalent.
- **Cost and tokens:** the `statusLine` command gets `cost.total_cost_usd`,
  `context_window.total_input_tokens/total_output_tokens` and `current_usage` on each
  refresh (probe: `0.195216`, `48780` in, `10` out). The transcript JSONL has per-message
  `usage` on assistant records and a `cost-state` record with `totalCostUSD` and
  `modelUsage` (seen in all four runs, headless too). The `cost-state` record is not in
  the docs I read, so treat it as unstable. No hook payload carries cost (doc).
- **Caps:** `--max-budget-usd` and `--max-turns` are print-only and silently ignored
  interactively (probe). wipnote would have to enforce budget itself, for example from the
  status line feed.
- **Structured output:** `--json-schema` is print-only (doc). No interactive equivalent.

### Codex CLI

- **Final result:** the Stop hook has `last_assistant_message` (doc,
  [hooks](https://learn.chatgpt.com/docs/hooks)). The rollout `task_complete` event has a
  `last_agent_message` field (probe; `null` here because the turn failed). Not run here
  with a real reply.
- **Exit status:** after a failed turn, the TUI `/exit` gave exit 0. `codex exec` gave
  exit 1 and a `turn.failed` event (probe). So the TUI exit code hides a failed turn.
- **Tokens:** TUI `/status` shows `Token usage: 0 total (0 input + 0 output)` (probe).
  `codex exec --json` reports usage on `turn.completed` (doc,
  [non-interactive mode](https://learn.chatgpt.com/docs/non-interactive-mode)). Whether the
  TUI rollout gets `token_count` events with real numbers: not run here. No USD cost in
  either mode (doc: tokens only).

## What slice 8 must build (Claude Code GO conditions)

1. Pre-seed onboarding and workspace trust for the worker dir, or the pane blocks on
   dialogs and may drop the first prompt.
2. Pass `--session-id` so the transcript path is known before launch.
3. Ship a delegate hook set in the plugin: Stop writes `last_assistant_message`;
   StopFailure writes the error; SessionEnd writes the reason; a status line command (or a
   transcript reader) writes cost and tokens. All go to one run record per delegation.
4. Synthesize the exit status: wipnote decides done or failed from Stop or StopFailure,
   then ends the session (send `/exit`, or SIGTERM; expect 0 or 143).
5. Enforce turn and budget caps in wipnote; the flags do nothing interactively.
6. Detect silent startup failures: a missing `--plugin-dir` and an unknown model do not
   exit. Fail the run when SessionStart does not fire within a timeout, and check the
   plugin is present (for example by a SessionStart hook from that plugin).
7. Put the prompt before variadic flags such as `--mcp-config`.
8. Watch for the plan-mode model switch if delegates run in plan mode.

## Needs the owner's Mac (Codex, real login)

Run from a scratch dir, with a scratch `CODEX_HOME` that holds a copy of your
`auth.json`:

```bash
mkdir -p /tmp/cxprobe/work && cd /tmp/cxprobe/work && git init -q
export CODEX_HOME=/tmp/cxprobe/home; mkdir -p "$CODEX_HOME"; cp ~/.codex/auth.json "$CODEX_HOME/"
cat > /tmp/cxprobe/dump.sh <<'EOF'
#!/bin/bash
cat > "/tmp/cxprobe/${PROBE_TAG}-$1.json"
EOF
chmod +x /tmp/cxprobe/dump.sh
H='-c hooks.Stop=[{hooks=[{type="command",command="/tmp/cxprobe/dump.sh Stop"}]}] -c hooks.SessionEnd=[{hooks=[{type="command",command="/tmp/cxprobe/dump.sh SessionEnd"}]}]'

# 1. Headless baseline: result, usage, exit code
PROBE_TAG=exec codex exec --json -o /tmp/cxprobe/exec-last.txt $H --dangerously-bypass-hook-trust \
  -c 'developer_instructions="The codeword is PELICAN-7731."' "what is the codeword?" \
  > /tmp/cxprobe/exec.jsonl; echo "exec exit=$?"
grep turn.completed /tmp/cxprobe/exec.jsonl          # usage fields

# 2. Interactive: same, then type /exit after the reply
PROBE_TAG=tui codex $H --dangerously-bypass-hook-trust \
  -c 'developer_instructions="The codeword is PELICAN-7731."' "what is the codeword?"; echo "tui exit=$?"
cat /tmp/cxprobe/tui-Stop.json                       # expect last_assistant_message = PELICAN-7731
grep -h '"token_count"' "$CODEX_HOME"/sessions/*/*/*/rollout-*.jsonl | tail -1   # TUI token usage?
grep -h '"task_complete"' "$CODEX_HOME"/sessions/*/*/*/rollout-*.jsonl | tail -1 # last_agent_message?

# 3. Plugin parity: the wipnote plugin via config in both modes
codex -c 'plugins."wipnote@<marketplace>".enabled=true'       # then run /plugins or /hooks, check it is listed
codex exec -c 'plugins."wipnote@<marketplace>".enabled=true' "reply ok"   # check its SessionStart hook ran
```

Pass condition for lifting the Codex HOLD: the TUI Stop payload has the final text, the
rollout has non-zero `token_count` (or `/status` shows non-zero usage), and the plugin
loads in both modes.

## Raw evidence

Kept in the session scratch dir (not committed): `cc/headless.jsonl`, `cc/out/*.json`
(hook and status line payloads per mode), `cc/i-*.txt` (tmux captures of `/status`, `/mcp`,
`/permissions`, startup errors), `codex-*-help.txt`, `cx-tui-*.txt`, `cx-exec*.jsonl`,
`cxout/*.json`, and the Codex rollout files.
