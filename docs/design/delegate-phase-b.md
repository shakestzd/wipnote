# Delegate Phase B: harness and herdr checks (feat-0c976595)

Plan plan-28b4a7f6, slice 2. `docs/design/orch-integration.md` section 6 lists what was
never verified: `--settings` hooks beside the plugin hooks, worker session ids, Codex
hooks, container workers writing the host's `.wipnote/`, and locking across the bind
mount. Slice 6 (feat-39bc46a1) built the herdr adapter from help text and skill text,
so some of its herdr calls were never run. Spike spk-87139e0e
(`docs/design/delegate-interactive-parity.md`) left the Codex TUI result and token
capture on HOLD until it runs with a real login. This page lists the checks that answer
those questions, how to run them, and where the results go.

## How to run

The owner runs one command on the Mac, inside a herdr pane, from the repo root:

```bash
scripts/delegate-phase-b.sh
```

It needs `herdr`, `claude`, `codex`, `wipnote`, `git` and `perl` on PATH. A missing tool
stops the run before any check, with exit 3 and a line such as
`missing tool: codex (needed by B3b B4 B5 D8b C1 C2)`. `go` is needed only for H9.
`orch` is needed only for B2. B6 to B8 need the `devcontainer` CLI, `docker` and a
running devcontainer (`scripts/devcontainer.sh up`). Without them those checks are
NOT RUN, with the reason.

| Flag | Effect |
|---|---|
| `--only B4,D8` | Run only these checks. A group letter (`H`, `B`, `C`, `D`) or a base id (`B3`, `D8`) selects every row under it. |
| `--no-model` | Every check that sends a model prompt is NOT RUN. |
| `--no-interactive` | B9 and D8 (they ask you y/n) are NOT RUN. |
| `--out FILE` | Results table path (default `./phase-b-results.md`). The raw log goes beside it as `phase-b-raw.log`. |
| `--self-test` | Smoke run for a container without herdr or Codex. It does not stop on a missing tool, and it implies `--no-model` and `--no-interactive`. herdr and container checks are NOT RUN; B7L and B8L run. |
| `--list` | Print each check id, what it verifies and what it needs. |
| `--help` | Usage. |

Exit status: 0 when no check failed, 1 when one failed, 2 for bad usage, 3 for a
missing tool.

What a run does:

- Everything happens in a `mktemp -d` scratch dir. Each check makes its own throwaway
  git repo and runs `wipnote init` in it. The real checkout's `.wipnote/` is never
  written. `wipnote init` may add the scratch project to wipnote's cross-project
  registry; `wipnote registry prune` removes it.
- B6 to B8 need a folder the container can see, so their scratch project is a
  temporary `.phase-b-scratch.XXXXXX/` folder at the repo root. It is removed on exit.
- The C checks copy `~/.codex/auth.json` into a scratch `CODEX_HOME` (mode 600). The
  copy is removed on exit.
- Checks that drive a TUI open background herdr tabs named `phase-b-<check>`. On first
  use in a new folder, the script accepts the folder-trust dialog itself (the folder is
  its own scratch dir). If a tab waits on any other dialog, switch to it and answer it.
  An exit trap closes every tab and pane the script opened and stops any orch session
  it spawned.
- Every model prompt is "reply with the word ok", except B9, which asks for an
  AskUserQuestion dialog. `--dangerously-skip-permissions` is never used. C1 and C2
  pass `--dangerously-bypass-hook-trust`, as the parity probe does, so that their
  `-c` dump hooks run in the scratch `CODEX_HOME`.
- `$HOME`, the scratch paths, the repo path, any `/Users/<name>` or `/home/<name>`,
  the user name and the host name are replaced with `~`, `<scratch>`, `<repo>`,
  `<user>` and `<host>` in both output files. That makes them safe to commit.

The script runs on macOS `/bin/bash` 3.2 and on Linux bash. It is split into
`scripts/delegate-phase-b.sh` (flags, preflight, order) and `scripts/delegate-phase-b/`
(`lib.sh`, `herdr.sh`, `checks-host.sh`, `checks-pane.sh`, `checks-container.sh`,
`report.sh`, and `probe.pl` for the file-system and SGR probes).

## The checks

"Needs" uses: herdr (inside a herdr pane), model (sends a prompt), interactive (asks
you y/n), container (devcontainer).

### herdr calls slice 6 could not confirm

Each uses the exact form `internal/delegate/herdr` sends. A FAIL means the adapter's
form or its reply parsing is wrong. The fix goes in `commands.go`, `agent.go` or
`errors.go`. Slices 6, 10, 11, 16 and 17 rely on these calls.

| Id | Proves | Command | Needs |
|---|---|---|---|
| H1 | `TabClose` works and takes the root pane with it | `herdr tab close <tab>`, then `pane get <root>` fails | herdr |
| H2 | `Split` accepts a positional pane id | `herdr pane split <pane> --direction right --cwd D --no-focus` gives `result.pane.pane_id` | herdr |
| H3 | `Layout` reply shape | `herdr pane layout --pane <id>` has `result.layout.panes[].pane_id`, `.rect.width`, `.rect.height` | herdr |
| H4 | `PaneRead` form and plain-text reply | `herdr pane read <pane> --source recent-unwrapped --lines 20` shows a marker echoed with `pane run` | herdr |
| H5 | `AgentRename` | `herdr agent rename <pane> phaseb-h5`, then `agent get phaseb-h5` gives that name (plain `claude` in the pane, no prompt) | herdr |
| H6 | `Rename` positional form | `herdr pane rename <pane> phaseb-h6` exits 0 | herdr |
| H7 | the gone-pane error code `IsAbsent` accepts | `pane get` on a closed pane: exit 1 and `pane_not_found` or `not_found` in the stderr JSON | herdr |
| H8 | `Close` | `herdr pane close <pane>` exits 0 | herdr |
| H9 | the adapter end to end | `HERDR_ENV=1 go test ./internal/delegate/herdr/ -run '^TestHerdr_LiveTabRoundTrip$' -count=1 -v` | herdr, go |

### Hooks and sessions (plan tabs B1 to B9)

| Id | Proves | How | Needs | Slices that use the answer |
|---|---|---|---|---|
| B1 | `--settings` hooks add to the wipnote plugin hooks, not replace them | `wipnote claude --in-place --allow-non-interactive -- --settings F --session-id U -p ...`: F's SessionStart marker fires, and the scratch sessions ledger has row U | model | 8 (orch-style hook file), 10 |
| B2 | an orch-spawned worker's `sid` in `events.ndjson` is its wipnote session id | `orch spawn` + `orch send --wait` in a scratch project; the sids in the mailbox's `events.ndjson` are looked up in the ledger. The orch CLI forms come from `docs/design/orch-inventory.md` and were not run here | herdr, model, orch | import-orch (18); not slice 10 |
| B3a, B3b | herdr's own Claude and Codex integration hooks and the wipnote plugin hooks both report | an interactive `wipnote claude` / `wipnote codex` turn in a tab: `herdr agent get` shows the agent and a status, and the ledger has a new row. NOT RUN when no herdr hook is found in `~/.claude/settings.json` or `~/.codex/` | herdr, model | 10, 12 |
| B4 | interactive `wipnote codex`: SessionStart fires and carries the Codex session id | one turn in a tab; a new codex row in the ledger, also found in `~/.codex/session_index.jsonl` | herdr, model | 10, 12, Codex HOLD |
| B5 | `codex exec` under the wipnote plugin fires SessionStart (bug-3e4c6e02) | `wipnote codex --in-place --allow-non-interactive -- exec ...`; a new codex row in the ledger. Also records the `--dry-run` argv | model | 8 (Codex one-shot) |
| B6 | a container worker started with `wipnote claude` writes its ledger row into the host checkout | `devcontainer exec ... wipnote claude ... -p` in the shared scratch project; the host's ledger has row U with `data-project="."` | container, model | 21 (container workers) |
| B7L | control for B7 on the local disk, so a B7 FAIL is about the mount | the B7 probes with both sides on the host | perl | none |
| B7 | flock is exclusive, O_APPEND loses no line, rename is atomic across the bind mount | `probe.pl`: LOCK_EX on one side, LOCK_NB on the other, both ways; 2 sides x 4 writers x 5000 64-byte lines; one side renames a 4 KiB file over and over while the other reads it | container | 4, 5, 10 (ledger and run-store locking) |
| B8L | control for B8: the claim gate refuses a second session on one side | `WIPNOTE_SESSION_ID=a wipnote feature start F`, then `=b` is refused, naming a | none | none |
| B8 | the claim gate on each side sees the other side's live episode in `.wipnote/claims/` | the same pair, host then container, and container then host | container | 10 (co-claim), q-claim-handoff |
| B9 | `herdr pane send-text <pane> 2` then `pane send-keys <pane> enter` picks AskUserQuestion option 2 | a Claude turn asks for red, green or blue; the script sends 2 and enter; you confirm green was picked | herdr, model, interactive | 17 (answer) |

### D8: styled screen reads

| Id | Proves | How | Needs |
|---|---|---|---|
| D8a | `herdr pane read --format ansi` keeps the dim or grey SGR codes of Claude Code's next-prompt suggestion, and a typed draft reads in the default colour | after one turn: one read is checked for dim or grey runs; the script types `phaseb typed draft`, reads again and checks the draft's style; you confirm both by eye | herdr, model, interactive |
| D8b | the same for Codex's composer | the same in `wipnote codex` | herdr, model, interactive |

PASS needs the machine check and your answers to agree. If they disagree, the read
dropped or changed the styling. Delegate's idle detection (decision D8) cannot then
tell a suggestion from a human draft. The flag form `--format ansi` is itself
unverified; a usage error (exit 2) is a FAIL that answers that too.

### C: Codex parity probes (spk-87139e0e, last section of the parity doc)

These run plain `codex`, not `wipnote codex`, in a scratch `CODEX_HOME` with `-c`
Stop and SessionEnd hooks that dump their payloads.

| Id | Proves | Needs |
|---|---|---|
| C1 | headless baseline: `codex exec --json -o last.txt` exit code, final text, `turn.completed` usage, and whether the Stop hook fires in exec | model |
| C2 | the TUI gives the same facts: Stop `last_assistant_message`, non-zero `token_count` in the rollout, `task_complete`, and the exit code after `/exit` | herdr, model |
| C3 | the wipnote plugin loads in both modes. Derived: B4 (TUI) and B5 (exec) both PASS | B4, B5 |

The Codex HOLD in the parity doc lifts when C2 and C3 pass.

## Reading the table

The script prints a markdown table and writes it to `phase-b-results.md`:

| Column | Meaning |
|---|---|
| Check | id and what it verifies |
| Result | PASS, FAIL or NOT RUN |
| Command | the command that decided the result (redacted, cut to 240 characters) |
| Observed | exit codes and the facts the verdict used, or the NOT RUN reason (cut to 400 characters) |

The full output of every command is in `phase-b-raw.log`. Below the table the script
prints one `wipnote arch add` command per check group that ran. Those commands are not
run by the script. Review them, then run them from the real checkout to store each
result as an arch card (slice done_when).

## Evidence so far

From the plan's mac-evidence notes (2026-10-25), before this script existed:

- **B1 partly:** `wipnote claude --settings F` fails with "unknown flag".
  `wipnote claude -- --settings F` works, and orch's hooks fired. Whether wipnote's own
  plugin hooks still fire beside them was open. Finding for slices 8 and 10: anything
  that must reach claude goes after `--`.
- **B6 partly:** container hooks wrote 800 of 800 lines to the shared mount. No
  sessions-ledger row was checked.
- **B7 partly:** 4+4 writers with flock plus O_APPEND lost nothing. A 1 s flock under
  load lost 172 of 800. O_APPEND alone and rename across the mount were untested.
- **B2 to B5 and B8:** no evidence. **B9:** new, unverified (slice 17 assumes a digit
  key selects an option).

From this slice's runs in the Linux devcontainer (2026-10-26; not the Mac, no herdr, no
Codex):

- B1 passed with Claude Code 2.1.310: the `--settings` SessionStart marker fired, and
  the wipnote plugin wrote the session's ledger row. On Linux at least, `--settings`
  hooks add to the plugin hooks.
- B7L and B8L passed on the local disk.
- The `--self-test` run exits 0, with every herdr, Codex and container check NOT RUN
  and the reason given.

## Results (owner's Mac run)

### Run 1 (2026-10-26, `--no-interactive`, no devcontainer)

- Versions: wipnote 0.82.1, Claude Code 2.1.310, codex-cli 0.164.0, herdr 0.9.5, macOS (Darwin 27).
- Totals: 18 pass, 2 fail, 6 not run.
- The first results file leaked two macOS temp-folder paths in cut lines. The
  script now redacts any `/var/folders/...` path as `<tmp>`; the table below is
  scrubbed.

| Check | Result | What it showed |
|---|---|---|
| H1–H8 | PASS | Every form slice 6 sends works on herdr 0.9.5: tab close takes its root pane; positional `pane split`; `pane layout` reply has `result.layout.panes[].pane_id` and `.rect`; `pane read --source recent-unwrapped --lines N`; `agent rename`; `pane rename`; a gone pane gives exit 1 with `pane_not_found`; `pane close`. |
| H9 | PASS | `TestHerdr_LiveTabRoundTrip` against real herdr. |
| B1 | PASS | `wipnote claude -- --settings F -p ...`: the marker hook fired and the wipnote session row was written. `--settings` hooks add to the plugin hooks on macOS too. |
| B2 | FAIL | `orch spawn` worked; `orch send --wait` answered `blocked`: the worker sat at the first-run folder-trust dialog in orch's own pane, which the script did not answer. Script gap, not an orch or wipnote defect. Fixed: the script now accepts that dialog in orch's pane before sending, and dumps the pane on failure. |
| B3a | FAIL | Claude under `wipnote claude` with herdr's Claude hooks installed: no wipnote session row, and herdr's `agent get` showed no agent. The cut screen line ends in Claude's `❯` prompt, so Claude started. See run 2: most likely the bare enter chose "No, exit". The script now dumps the full screen into the raw log on a FAIL. |
| B3b | PASS | herdr's Codex hooks and wipnote's plugin hooks both report (`agent=codex status=done`, a wipnote session row). |
| B4 | PASS | Interactive `wipnote codex` fires SessionStart with a session id. |
| B5 | PASS | `codex exec` under `wipnote codex` runs SessionStart, UserPromptSubmit and Stop, and writes a session row (closes bug-3e4c6e02's question). |
| B7L, B8L | PASS | Local controls: flock busy both ways, O_APPEND 40000/40000, rename torn=0; the claim gate refuses a second session. |
| C1 | PASS | `codex exec --json`: final text, `turn.completed` usage, exit 0, Stop hook `last_assistant_message`. |
| C2 | PASS | Codex TUI: Stop final text, non-zero `token_count`, `task_complete`, exit 0. |
| C3 | PASS | The wipnote plugin loads in both Codex modes (B4 and B5). |
| B9, D8a, D8b | NOT RUN | Need the owner's y/n (`--only B9,D8`). |
| B6, B7, B8 | NOT RUN | The devcontainer was not running. |

### Run 2 (2026-10-26, `--only B2,B3a`, commit ea0a48f4f)

No check passed; both failures were script bugs, not delegate or harness facts.

- **B3a, B3b:** `herdr tab create --workspace ''` gave `workspace_not_found`. Only the
  H checks set the workspace. Fixed: `tab_open` now resolves it for every pane check.
- **B2:** after the script's enter, the trust dialog in orch's pane was still up with
  the cursor on "No, exit"; `orch send --wait` then gave `occupant_unknown`.
- **Run 1's B3a, re-read:** the same bare enter most likely chose "No, exit", Claude
  quit, and the `❯` in the cut screen line was the shell prompt, not Claude's.
- Fixed: the script now moves the cursor to the "Yes" row (up keys, then the digit 1),
  presses enter only when "Yes" is highlighted, and checks the dialog cleared. When it
  cannot, it leaves the dialog, dumps the screen and fails the check. B2 never sends
  while the dialog is up. A stub-herdr test covers cursor-on-Yes, cursor-on-No and a
  cursor that will not move (no enter is sent).
- Run 2 left two orch worker panes at the trust dialog on the owner's machine.

**Finding for slice 10:** a bare enter on a trust dialog is unsafe; the default row
is not always "Yes". `delegate spawn` must select the option explicitly and verify.

Side observation: B1's output shows another plugin's security hook running first
and passing events past wipnote-orchestra's classic hooks ("its classic.Stop hook
was skipped"). The wipnote plugin's own hooks still ran. Not a Phase B failure.

Follow-ups by slice:

- **Slice 6 (herdr adapter forms):** all confirmed. No change needed.
- **Slices 5, 10, 12:** B1, B4, B5 hold. B3a (Claude beside herdr's Claude hooks) is still open.
- **Slice 8 and the Codex hold:** C1–C3 pass, so the condition slice 22 set for Codex interactive workers is met; the owner decides whether Codex workers move to interactive.
- **Slice 17 (B9)** and **D8:** wait for the interactive run.
- **Slice 21 (container workers):** B6–B8 wait for a run with the devcontainer up.
- **New for slice 10:** a spawned worker's pane can stop at the folder-trust dialog (B2). `delegate spawn` must detect it and either accept it for a folder it created or report the worker as blocked.
