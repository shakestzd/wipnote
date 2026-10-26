# shellcheck shell=bash
# herdr helpers and the H checks for scripts/delegate-phase-b.sh. Every herdr
# call uses the exact form internal/delegate/herdr makes (commands.go, agent.go,
# cli.go), so a PASS here confirms the adapter's form. Sourced, never run.

OPEN_TABS=""
OPEN_PANES=""
TRUSTED=""
HERDR_WS=""
HERDR_WHY=""

# herdr_why: print why herdr checks cannot run, or nothing when they can.
herdr_why() {
  if [ "$SELF_TEST" = 1 ]; then echo "self-test: herdr checks are skipped"
  elif [ "${HERDR_ENV:-}" != 1 ]; then echo "HERDR_ENV is not 1: run inside a herdr pane"
  elif ! have herdr; then echo "herdr not on PATH"
  elif [ -z "${HERDR_PANE_ID:-}" ]; then echo "HERDR_PANE_ID is not set"
  elif [ -n "$HERDR_WHY" ]; then echo "$HERDR_WHY"
  fi
}

# herdr_init: find the caller's workspace once (CallerWorkspace in cli.go).
herdr_init() {
  [ -n "$(herdr_why)" ] && return 1
  [ -n "$HERDR_WS" ] && return 0
  cap 15 herdr pane get "$HERDR_PANE_ID"
  HERDR_WS=$(printf '%s' "$CAP_OUT" | jget result.pane.workspace_id)
  if [ -z "$HERDR_WS" ]; then
    HERDR_WHY="herdr pane get \$HERDR_PANE_ID gave no workspace_id: $(oneline "$CAP_OUT" 120)"
    return 1
  fi
}

# tab_open LABEL CWD: a background tab (TabCreate). Sets T_TAB and T_PANE.
tab_open() {
  T_TAB="" T_PANE=""
  cap 15 herdr tab create --workspace "$HERDR_WS" --label "$1" --cwd "$2" --no-focus
  T_TAB=$(printf '%s' "$CAP_OUT" | jget result.tab.tab_id)
  T_PANE=$(printf '%s' "$CAP_OUT" | jget result.root_pane.pane_id)
  [ -n "$T_TAB" ] && OPEN_TABS="$OPEN_TABS $T_TAB"
  [ -n "$T_PANE" ] && OPEN_PANES="$OPEN_PANES $T_PANE"
  [ -n "$T_TAB" ] && [ -n "$T_PANE" ]
}

# herdr_cleanup: close every tab and pane this run opened (EXIT trap).
herdr_cleanup() {
  local t p
  [ "${HERDR_ENV:-}" = 1 ] && have herdr || return 0
  for t in $OPEN_TABS; do with_timeout 10 herdr tab close "$t" >/dev/null 2>&1; done
  for p in $OPEN_PANES; do with_timeout 10 herdr pane close "$p" >/dev/null 2>&1; done
}

# screen PANE [ansi]: the pane's recent text (PaneRead), raw stdout only.
screen() {
  if [ "${2:-}" = ansi ]; then
    with_timeout 15 herdr pane read "$1" --source recent-unwrapped --lines 60 --format ansi 2>/dev/null
  else
    with_timeout 15 herdr pane read "$1" --source recent-unwrapped --lines 60 2>/dev/null
  fi
}

# dump_screen ID PANE: copy the pane's full recent text into the raw log, so a
# FAIL can be diagnosed later; the results table only holds a cut line.
dump_screen() {
  log "--- $1: screen of $2 ---"
  screen "$2" | strip_ansi >>"$RAW_LOG"
  log "--- end $1 screen ---"
}

# maybe_trust PANE: accept a first-run folder-trust dialog once. The folder is
# a scratch dir this script made, so trusting it is safe.
maybe_trust() {
  case " $TRUSTED " in *" $1 "*) return 0 ;; esac
  if screen "$1" | strip_ansi | grep -qiE 'trust (this|the) (folder|files|directory|contents|project)|project you created or one you trust'; then
    log "trust dialog in $1: sending enter"
    with_timeout 10 herdr pane send-keys "$1" enter >/dev/null 2>&1
    TRUSTED="$TRUSTED $1"
  fi
}

# poll_pane PANE SECS CMD...: wait for CMD, accepting a trust dialog meanwhile.
poll_pane() {
  local pane="$1" secs="$2" i=0
  shift 2
  while [ "$i" -lt "$secs" ]; do
    maybe_trust "$pane"
    "$@" && return 0
    sleep 1
    i=$((i + 1))
  done
  return 1
}

# pane_launch LABEL DIR BODY: write BODY into a bash script and start it in a
# new tab labelled phase-b-LABEL. The shell in the pane may be zsh, so all
# quoting lives in the script file. Sets L_PANE and L_EXIT (a file that
# gets "exit=N" when BODY ends).
pane_launch() {
  local script="$SCRATCH/run-$1.sh"
  L_EXIT="$SCRATCH/run-$1.exit"
  rm -f "$L_EXIT"
  printf '%s\n' '#!/usr/bin/env bash' "$UNSET_LINE" "cd $(quote_cmd "$2") || exit 1" "$3" \
    "echo \"exit=\$?\" > $(quote_cmd "$L_EXIT")" >"$script"
  log "launch script $script:"
  log "$(cat "$script")"
  tab_open "phase-b-$1" "$2" || return 1
  L_PANE="$T_PANE"
  cap 15 herdr pane run "$L_PANE" "bash $script"
}

# pane_exit PANE: type /exit into a Claude or Codex TUI and wait for its
# launch script to finish.
pane_exit() {
  with_timeout 10 herdr pane send-text "$1" /exit >/dev/null 2>&1
  with_timeout 10 herdr pane send-keys "$1" enter >/dev/null 2>&1
  wait_until 20 test -s "$L_EXIT"
}

# screen_has PANE REGEX: the plain screen matches REGEX (grep -E).
screen_has() { screen "$1" | strip_ansi | grep -qE "$2"; }

# --- H checks: herdr calls slice 6 could not confirm -------------------------
H_IDS="H1 H2 H3 H4 H5 H6 H7 H8"

check_H() {
  local why id
  why=$(herdr_why)
  [ -z "$why" ] && ! herdr_init && why=$(herdr_why)
  if [ -n "$why" ]; then
    for id in $H_IDS; do want "$id" && skip "$id" "$why"; done
    return 0
  fi
  local dir="$SCRATCH/h"
  mkdir -p "$dir"
  if ! tab_open phase-b-H "$dir"; then
    for id in $H_IDS; do want "$id" && fail "$id" "$CAP_CMD" "setup: tab create failed: $CAP_OUT"; done
    return 0
  fi
  H_TAB="$T_TAB" H_PANE="$T_PANE"
  want H4 && h4_read
  want H2 && h2_split
  want H3 && h3_layout
  want H6 && h6_rename
  want H8 && h8_close
  want H7 && h7_gone_code
  want H5 && h5_agent_rename
  want H1 && h1_tab_close
}

# H4 PaneRead: pane read <pane> --source recent-unwrapped --lines N.
h4_read() {
  cap 15 herdr pane run "$H_PANE" "echo phaseb-h4-marker"
  local i=0
  while [ "$i" -lt 10 ]; do
    cap 15 herdr pane read "$H_PANE" --source recent-unwrapped --lines 20
    case "$CAP_OUT" in *phaseb-h4-marker*) break ;; esac
    sleep 1
    i=$((i + 1))
  done
  local shape=plain
  case "$CAP_OUT" in '{'*) shape=json ;; esac
  case "$CAP_OUT" in
    *phaseb-h4-marker*) pass H4 "$CAP_CMD" "exit $CAP_RC; reply is $shape text; marker found: $CAP_OUT" ;;
    *) fail H4 "$CAP_CMD" "exit $CAP_RC; marker not found after 10s: $CAP_OUT" ;;
  esac
}

# H2 Split: pane split <pane-id> (positional) --direction right.
h2_split() {
  cap 15 herdr pane split "$H_PANE" --direction right --cwd "$SCRATCH/h" --no-focus
  H_PANE2=$(printf '%s' "$CAP_OUT" | jget result.pane.pane_id)
  if [ -n "$H_PANE2" ]; then
    OPEN_PANES="$OPEN_PANES $H_PANE2"
    pass H2 "$CAP_CMD" "exit $CAP_RC; new pane $H_PANE2; reply: $CAP_OUT"
  else
    fail H2 "$CAP_CMD" "exit $CAP_RC; no result.pane.pane_id: $CAP_OUT"
  fi
}

# H3 Layout: pane layout --pane <id>, reply result.layout.panes[].pane_id/.rect.
h3_layout() {
  cap 15 herdr pane layout --pane "$H_PANE"
  local id w h
  id=$(printf '%s' "$CAP_OUT" | jget result.layout.panes.0.pane_id)
  w=$(printf '%s' "$CAP_OUT" | jget result.layout.panes.0.rect.width)
  h=$(printf '%s' "$CAP_OUT" | jget result.layout.panes.0.rect.height)
  if [ -n "$id" ] && [ -n "$w" ] && [ -n "$h" ]; then
    pass H3 "$CAP_CMD" "exit $CAP_RC; panes[0]=$id ${w}x$h; reply: $CAP_OUT"
  else
    fail H3 "$CAP_CMD" "exit $CAP_RC; reply lacks result.layout.panes[].pane_id/rect.width/rect.height: $CAP_OUT"
  fi
}

# H6 Rename: pane rename <pane> <title>.
h6_rename() {
  local target="${H_PANE2:-$H_PANE}"
  cap 15 herdr pane rename "$target" phaseb-h6
  local c="$CAP_CMD" rc="$CAP_RC" out="$CAP_OUT"
  cap 15 herdr pane get "$target"
  if [ "$rc" = 0 ]; then
    pass H6 "$c" "exit 0: $out; pane get after: $CAP_OUT"
  else
    fail H6 "$c" "exit $rc: $out"
  fi
}

# H8 Close: pane close <pane> on the split pane.
h8_close() {
  if [ -z "${H_PANE2:-}" ]; then
    skip H8 "needs the pane H2 makes"
    return 0
  fi
  cap 15 herdr pane close "$H_PANE2"
  if [ "$CAP_RC" = 0 ]; then
    pass H8 "$CAP_CMD" "exit 0: $CAP_OUT"
  else
    fail H8 "$CAP_CMD" "exit $CAP_RC: $CAP_OUT"
  fi
}

# H7 gone-pane code: pane get on a closed pane: exit 1 with pane_not_found or
# not_found in {"error":{"code"}} on stderr (IsAbsent in errors.go).
h7_gone_code() {
  local gone="${H_PANE2:-}"
  if [ -z "$gone" ] || [ "$(result_of H8)" != PASS ]; then
    skip H7 "needs a pane closed by H8"
    return 0
  fi
  cap 15 herdr pane get "$gone"
  local code
  code=$(printf '%s' "$CAP_OUT" | perl -ne 'print $1 and exit if /"code"\s*:\s*"([^"]+)"/')
  case "$CAP_RC:$code" in
    1:pane_not_found|1:not_found) pass H7 "$CAP_CMD" "exit 1, code $code: $CAP_OUT" ;;
    *) fail H7 "$CAP_CMD" "exit $CAP_RC, code '${code:-none}' (adapter expects exit 1 + pane_not_found|not_found): $CAP_OUT" ;;
  esac
}

# H5 AgentRename: agent rename <target> <name> on a pane running claude (no
# prompt, so no model call).
h5_agent_rename() {
  have claude || { skip H5 "claude not on PATH"; return 0; }
  new_project "$SCRATCH/h5" || { fail H5 "new_project" "scratch project failed"; return 0; }
  cap 15 herdr pane run "$H_PANE" "cd $(quote_cmd "$SCRATCH/h5") && claude"
  if ! poll_pane "$H_PANE" 30 agent_present "$H_PANE"; then
    fail H5 "herdr agent get $H_PANE" "no agent detected in 30s: $(oneline "$(screen "$H_PANE")" 200)"
    return 0
  fi
  cap 15 herdr agent rename "$H_PANE" phaseb-h5
  local c="$CAP_CMD" rc="$CAP_RC" out="$CAP_OUT"
  cap 15 herdr agent get phaseb-h5
  local name
  name=$(printf '%s' "$CAP_OUT" | jget result.agent.name)
  if [ "$rc" = 0 ] && [ "$name" = phaseb-h5 ]; then
    pass H5 "$c" "exit 0: $out; agent get phaseb-h5 -> name $name"
  else
    fail H5 "$c" "exit $rc: $out; agent get phaseb-h5 (exit $CAP_RC): $CAP_OUT"
  fi
  with_timeout 10 herdr pane send-keys "$H_PANE" ctrl+c >/dev/null 2>&1
}

agent_present() {
  [ -n "$(with_timeout 10 herdr agent get "$1" 2>/dev/null | jget result.agent.agent)" ]
}

# H1 TabClose: tab close <tab>; the root pane must then be absent.
h1_tab_close() {
  cap 15 herdr tab close "$H_TAB"
  local c="$CAP_CMD" rc="$CAP_RC" out="$CAP_OUT"
  cap 15 herdr pane get "$H_PANE"
  if [ "$rc" = 0 ] && [ "$CAP_RC" != 0 ]; then
    OPEN_TABS=${OPEN_TABS// $H_TAB/}
    pass H1 "$c" "exit 0: $out; root pane after: exit $CAP_RC $CAP_OUT"
  else
    fail H1 "$c" "exit $rc: $out; root pane after: exit $CAP_RC $CAP_OUT"
  fi
}

# H9: the adapter's own live test (internal/delegate/herdr/live_test.go).
check_H9() {
  local why
  why=$(herdr_why)
  [ -z "$why" ] && ! have go && why="go not on PATH"
  [ -n "$why" ] && { skip H9 "$why"; return 0; }
  capd "$REPO" 300 go test ./internal/delegate/herdr/ -run '^TestHerdr_LiveTabRoundTrip$' -count=1 -v
  case "$CAP_RC:$CAP_OUT" in
    0:*"--- PASS: TestHerdr_LiveTabRoundTrip"*) pass H9 "HERDR_ENV=1 $CAP_CMD" "$(echo "$CAP_OUT" | grep -- '---' | head -3)" ;;
    *) fail H9 "HERDR_ENV=1 $CAP_CMD" "exit $CAP_RC: $(echo "$CAP_OUT" | tail -8)" ;;
  esac
}
