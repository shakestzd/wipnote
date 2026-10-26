#!/usr/bin/env bash
# Phase B of the delegate plan (feat-0c976595): verify harness hooks, herdr
# calls and Codex parity on the owner's machine. Run from the repo root inside
# a herdr pane. See docs/design/delegate-phase-b.md for what each check proves.
#
# Runs on macOS /bin/bash 3.2 and Linux bash. Not `set -e`: every check traps
# its own failure and records PASS, FAIL or NOT RUN.
# shellcheck source-path=SCRIPTDIR
set -u

usage() {
  cat <<'EOF'
Usage: scripts/delegate-phase-b.sh [flags]

Run from the repo root, inside a herdr pane (HERDR_ENV=1).

Flags:
  --only IDS        run only these checks, comma separated (B4,D8,H). A group
                    letter (H, B, C, D) or a base id (B3, D8) selects its rows.
  --no-model        mark every check that sends a model prompt NOT RUN
  --no-interactive  mark the checks that ask you y/n questions NOT RUN
  --out FILE        results table (default ./phase-b-results.md); the raw log
                    goes beside it as phase-b-raw.log
  --self-test       container smoke run: no preflight exit, implies --no-model
                    and --no-interactive, skips herdr and container checks
  --list            print the check ids and what each verifies
  -h, --help        this help

Exit: 0 no check failed, 1 a check failed, 2 bad usage, 3 a tool is missing.
EOF
}

ALL_IDS="H1 H2 H3 H4 H5 H6 H7 H8 H9 B1 B2 B3a B3b B4 B5 B6 B7L B7 B8L B8 B9 D8a D8b C1 C2 C3"

title_of() {
  case "$1" in
    H1) echo "herdr tab close <tab> closes the tab and its root pane" ;;
    H2) echo "herdr pane split <pane-id> (positional target) returns the new pane" ;;
    H3) echo "herdr pane layout --pane: reply has result.layout.panes[].pane_id and .rect" ;;
    H4) echo "herdr pane read <pane> --source recent-unwrapped --lines N returns the text" ;;
    H5) echo "herdr agent rename <target> <name> renames the pane's agent" ;;
    H6) echo "herdr pane rename <pane> <title> is accepted" ;;
    H7) echo "pane get on a gone pane: exit 1 with pane_not_found or not_found" ;;
    H8) echo "herdr pane close <pane> closes the pane" ;;
    H9) echo "go test TestHerdr_LiveTabRoundTrip passes against real herdr" ;;
    B1) echo "--settings hooks (after --) add to the wipnote plugin hooks" ;;
    B2) echo "orch-spawned claude worker: events.ndjson sid equals its wipnote session id" ;;
    B3a) echo "herdr claude integration hooks and wipnote plugin hooks both report" ;;
    B3b) echo "herdr codex integration hooks and wipnote plugin hooks both report" ;;
    B4) echo "wipnote codex, interactive: SessionStart fires and carries session_id" ;;
    B5) echo "codex exec under the wipnote plugin: SessionStart writes a session row" ;;
    B6) echo "container worker (wipnote claude): its ledger row appears in the host checkout" ;;
    B7L) echo "control: flock, O_APPEND and rename on the local disk" ;;
    B7) echo "flock, O_APPEND and rename across the host-container bind mount" ;;
    B8L) echo "control: the claim gate refuses a second session on one side" ;;
    B8) echo "claim gate sees the other side's live episode across the mount, both ways" ;;
    B9) echo "send-text 2 + send-keys enter selects AskUserQuestion option 2" ;;
    D8a) echo "Claude Code: pane read --format ansi keeps the grey suggestion; draft is default" ;;
    D8b) echo "Codex: pane read --format ansi keeps the grey suggestion; draft is default" ;;
    C1) echo "Codex exec: final text, turn.completed usage, exit code, Stop hook" ;;
    C2) echo "Codex TUI: Stop final text, rollout token_count, task_complete, exit code" ;;
    C3) echo "wipnote plugin loads in both Codex modes (B4 and B5)" ;;
  esac
}

# needs_of ID: what a check needs, for preflight and --list.
needs_of() {
  case "$1" in
    H5) echo "herdr claude" ;;
    H*) echo herdr ;;
    B1) echo "model claude wipnote" ;;
    B2) echo "herdr model orch claude wipnote" ;;
    B3a) echo "herdr model claude wipnote" ;;
    B3b|B4) echo "herdr model codex wipnote" ;;
    B5|C1) echo "model codex" ;;
    B6) echo "container model wipnote" ;;
    B7L) echo "perl" ;;
    B7) echo "container perl" ;;
    B8L) echo "wipnote" ;;
    B8) echo "container wipnote" ;;
    B9|D8a) echo "herdr model interactive claude wipnote" ;;
    D8b) echo "herdr model interactive codex wipnote" ;;
    C2) echo "herdr model codex" ;;
    C3) echo "B4 B5" ;;
  esac
}

ONLY="" NO_MODEL=0 NO_INTERACTIVE=0 SELF_TEST=0 OUT="./phase-b-results.md"
parse_args() {
  while [ $# -gt 0 ]; do
    case "$1" in
      --only) [ $# -ge 2 ] || { echo "--only needs a value" >&2; exit 2; }; ONLY=$(echo "$2" | tr ',' ' '); shift ;;
      --only=*) ONLY=$(echo "${1#--only=}" | tr ',' ' ') ;;
      --no-model) NO_MODEL=1 ;;
      --no-interactive) NO_INTERACTIVE=1 ;;
      --out) [ $# -ge 2 ] || { echo "--out needs a value" >&2; exit 2; }; OUT="$2"; shift ;;
      --self-test) SELF_TEST=1 NO_MODEL=1 NO_INTERACTIVE=1 ;;
      --list) for i in $ALL_IDS; do printf '%-4s %-62s needs: %s\n' "$i" "$(title_of "$i")" "$(needs_of "$i")"; done; exit 0 ;;
      -h|--help) usage; exit 0 ;;
      *) echo "unknown flag: $1" >&2; usage >&2; exit 2 ;;
    esac
    shift
  done
  local t
  for t in $ONLY; do
    case " $ALL_IDS H B C D B3 D8 " in *" $t "*) ;; *) echo "unknown check id: $t" >&2; exit 2 ;; esac
  done
}

# want ID: ID is selected by --only (or no --only was given).
want() {
  [ -z "$ONLY" ] && return 0
  local t
  for t in $ONLY; do
    case "$1" in "$t"|"$t"[a-z]) return 0 ;; esac
    [ "${#t}" = 1 ] && case "$1" in "$t"*) return 0 ;; esac
  done
  return 1
}

# why_not NEEDS...: the first reason a check cannot run, or nothing. Missing
# tools come first, then herdr and the container, then the run flags.
why_not() {
  local r w
  for r in "$@"; do
    case "$r" in
      model|interactive|herdr|container) ;;
      *) have "$r" || { echo "$r not on PATH"; return 0; } ;;
    esac
  done
  for r in "$@"; do
    case "$r" in
      herdr) w=$(herdr_why); [ -n "$w" ] && { echo "$w"; return 0; } ;;
      container) w=$(ctr_why); [ -n "$w" ] && { echo "$w"; return 0; } ;;
    esac
  done
  for r in "$@"; do
    case "$r" in
      model) [ "$NO_MODEL" = 1 ] && { echo "--no-model: this check sends a prompt"; return 0; } ;;
      interactive)
        [ "$NO_INTERACTIVE" = 1 ] && { echo "--no-interactive: this check asks you y/n"; return 0; }
        [ -r /dev/tty ] || { echo "no terminal to ask on"; return 0; } ;;
    esac
  done
}

# preflight: name each missing tool and the selected checks that need it.
preflight() {
  local tool id users missing=0
  for tool in herdr claude codex wipnote git perl; do
    users=""
    for id in $ALL_IDS; do
      want "$id" || continue
      case " $(needs_of "$id") git perl " in *" $tool "*) users="$users $id" ;; esac
    done
    [ -z "$users" ] && continue
    if ! have "$tool"; then
      echo "missing tool: $tool (needed by$users)" >&2
      missing=1
    fi
  done
  if [ "$missing" = 1 ] && [ "$SELF_TEST" != 1 ]; then
    echo "install the missing tools, or narrow the run with --only" >&2
    exit 3
  fi
}

version_of() {
  have "$1" || { echo "not installed"; return 0; }
  oneline "$(with_timeout 20 "$@" 2>&1 | head -1)" 100
}

versions() {
  V_WIPNOTE=$(version_of wipnote version)
  V_CLAUDE=$(version_of claude --version)
  V_CODEX=$(version_of codex --version)
  V_HERDR=$(version_of herdr --version)
  printf 'wipnote: %s\nclaude: %s\ncodex: %s\nherdr: %s\n' "$V_WIPNOTE" "$V_CLAUDE" "$V_CODEX" "$V_HERDR"
}

run_checks() {
  local need_ctr=0 id
  for id in B6 B7 B8; do want "$id" && need_ctr=1; done
  [ "$need_ctr" = 1 ] && ctr_prepare
  for id in H1 H2 H3 H4 H5 H6 H7 H8; do want "$id" && { check_H; break; }; done
  want H9 && check_H9
  want B1 && check_B1
  want B7L && check_B7L
  want B8L && check_B8L
  want B5 && check_B5
  want C1 && check_C1
  want B4 && check_B4
  (want B3a || want B3b) && check_B3
  want C2 && check_C2
  want B2 && check_B2
  want B9 && check_B9
  (want D8a || want D8b) && check_D8
  want B6 && check_B6
  want B7 && check_B7
  want B8 && check_B8
  want C3 && check_C3
}

main() {
  parse_args "$@"
  if [ ! -f go.mod ] || [ ! -f scripts/delegate-phase-b.sh ]; then
    echo "run this from the wipnote repo root" >&2
    exit 2
  fi
  REPO=$(pwd -P)
  # shellcheck source=delegate-phase-b/lib.sh
  . "$REPO/scripts/delegate-phase-b/lib.sh"
  # shellcheck source=delegate-phase-b/herdr.sh
  . "$REPO/scripts/delegate-phase-b/herdr.sh"
  # shellcheck source=delegate-phase-b/checks-host.sh
  . "$REPO/scripts/delegate-phase-b/checks-host.sh"
  # shellcheck source=delegate-phase-b/checks-container.sh
  . "$REPO/scripts/delegate-phase-b/checks-container.sh"
  # shellcheck source=delegate-phase-b/checks-pane.sh
  . "$REPO/scripts/delegate-phase-b/checks-pane.sh"
  # shellcheck source=delegate-phase-b/report.sh
  . "$REPO/scripts/delegate-phase-b/report.sh"
  unset WIPNOTE_PROJECT_DIR CLAUDE_PROJECT_DIR
  [ "$SELF_TEST" = 1 ] && unset HERDR_ENV
  USER_NAME=$(id -un 2>/dev/null || echo "${USER:-}")
  HOST_NAME=$(hostname -s 2>/dev/null || hostname 2>/dev/null || echo "")
  RUN_TAG=$(date +%H%M%S)$$
  SCRATCH=$(cd "$(mktemp -d "${TMPDIR:-/tmp}/phase-b.XXXXXX")" && pwd -P) || { echo "mktemp failed" >&2; exit 2; }
  RAW_LOG="$SCRATCH/raw.log"
  : >"$RAW_LOG"
  CLEAN_HOOKS="herdr_cleanup orch_cleanup"
  CLEAN_DIRS="$SCRATCH"
  trap 'on_exit_cleanup' EXIT
  trap 'exit 130' INT TERM
  preflight
  versions
  echo "scratch: $SCRATCH (removed on exit)"
  [ -n "$(herdr_why)" ] || echo "background tabs are named phase-b-*; answer any dialog the script cannot"
  run_checks
  write_report
}

main "$@"
