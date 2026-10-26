# shellcheck shell=bash
# Host-side checks for scripts/delegate-phase-b.sh: B1, B2, B5, the local
# controls B7L and B8L, and the Codex exec probe C1. Each works in its own
# scratch project under $SCRATCH. Sourced, never run.

OK_PROMPT="reply with the word ok"
# shellcheck disable=SC2034 # used by checks-container.sh and checks-pane.sh
PROBE="$REPO/scripts/delegate-phase-b/probe.pl"

# CAP_CMD, CAP_OUT and CAP_RC are set by cap/capd in lib.sh.
# shellcheck disable=SC2153
# B1: --settings hooks add to the wipnote plugin hooks (wipnote claude passes
# claude flags only after --).
check_B1() {
  local why d u marker
  why=$(why_not model wipnote claude git)
  [ -n "$why" ] && { skip B1 "$why"; return 0; }
  d="$SCRATCH/b1"
  marker="$SCRATCH/b1-marker.json"
  new_project "$d" || { fail B1 "new_project" "scratch project failed"; return 0; }
  printf '{"hooks":{"SessionStart":[{"hooks":[{"type":"command","command":"cat > %s"}]}]}}\n' \
    "$marker" >"$SCRATCH/b1-settings.json"
  u=$(new_uuid)
  capd "$d" 240 "${HARNESS_ENV[@]}" wipnote claude --in-place --allow-non-interactive -- \
    --settings "$SCRATCH/b1-settings.json" --session-id "$u" -p "$OK_PROMPT"
  local m=no row=no
  [ -s "$marker" ] && m=yes
  ledger_sids "$d" claude | grep -qx "$u" && row=yes
  local obs
  obs="exit $CAP_RC; marker hook fired: $m; wipnote session row for $u: $row; output tail: $(echo "$CAP_OUT" | tail -2)"
  if [ "$m" = yes ] && [ "$row" = yes ]; then pass B1 "$CAP_CMD" "$obs"; else fail B1 "$CAP_CMD" "$obs"; fi
}

# B5: codex exec under the wipnote plugin: does SessionStart write a session
# row (bug-3e4c6e02)?
check_B5() {
  local why d before after new dry
  why=$(why_not model wipnote codex git)
  [ -n "$why" ] && { skip B5 "$why"; return 0; }
  d="$SCRATCH/b5"
  new_project "$d" || { fail B5 "new_project" "scratch project failed"; return 0; }
  capd "$d" 60 "${HARNESS_ENV[@]}" wipnote codex --dry-run --in-place -- exec "$OK_PROMPT"
  dry=$(oneline "$CAP_OUT" 200)
  before=$(ledger_sids "$d" codex)
  capd "$d" 300 "${HARNESS_ENV[@]}" wipnote codex --in-place --allow-non-interactive -- exec "$OK_PROMPT"
  after=$(ledger_sids "$d" codex)
  new=$(new_sids "$before" "$after" | tr '\n' ' ')
  local obs
  obs="exit $CAP_RC; new codex session rows: ${new:-none}; output tail: $(echo "$CAP_OUT" | tail -2); dry-run: $dry"
  if [ -n "$new" ]; then pass B5 "$CAP_CMD" "$obs"; else fail B5 "$CAP_CMD" "$obs"; fi
}

# scratch_feature DIR: create a track (once) and a feature in DIR; print its id.
scratch_feature() {
  local trk
  trk=$(cd "$1" && "${HARNESS_ENV[@]}" wipnote track list 2>/dev/null | grep -o 'trk-[0-9a-f]\{8\}' | head -1)
  if [ -z "$trk" ]; then
    trk=$(cd "$1" && "${HARNESS_ENV[@]}" wipnote track create "phase b scratch" 2>&1 | grep -o 'trk-[0-9a-f]\{8\}' | head -1)
  fi
  (cd "$1" && "${HARNESS_ENV[@]}" wipnote feature create "phase b scratch item" --track "$trk" \
    --description "phase b claim probe" 2>&1) | grep -o 'feat-[0-9a-f]\{8\}' | head -1
}

# claim_pair ID RUN_A RUN_B DIR_A DIR_B: side A starts a feature, then side B
# starts the same feature as another session and must be refused naming A's
# session. Sets CP_OK (1 when refused correctly), CP_CMD and CP_OBS.
claim_pair() {
  local f sa sb rc_a
  CP_OK=0 CP_CMD="" CP_OBS=""
  f=$(scratch_feature "$SCRATCH_PROJ")
  [ -z "$f" ] && { CP_OBS="could not create a scratch feature"; return 0; }
  sa="phaseb-$1-a-$RUN_TAG" sb="phaseb-$1-b-$RUN_TAG"
  "$2" "$4" 60 "WIPNOTE_SESSION_ID=$sa wipnote feature start $f"
  rc_a=$CAP_RC
  "$3" "$5" 60 "WIPNOTE_SESSION_ID=$sb wipnote feature start $f"
  CP_CMD="$CAP_CMD"
  CP_OBS="A ($sa) start exit $rc_a; B ($sb) start exit $CAP_RC: $(oneline "$CAP_OUT" 200)"
  if [ "$rc_a" = 0 ] && [ "$CAP_RC" != 0 ]; then
    case "$CAP_OUT" in *"$sa"*) CP_OK=1 ;; esac
  fi
}

# host_sh DIR SECS LINE: run LINE with bash in DIR on the host, harness env cleared.
host_sh() { capd "$1" "$2" "${HARNESS_ENV[@]}" bash -c "$3"; }

# B8L: control for B8 on one side: the claim gate refuses a second session.
check_B8L() {
  local why
  why=$(why_not wipnote git)
  [ -n "$why" ] && { skip B8L "$why"; return 0; }
  SCRATCH_PROJ="$SCRATCH/b8l"
  new_project "$SCRATCH_PROJ" || { fail B8L "new_project" "scratch project failed"; return 0; }
  claim_pair B8L host_sh host_sh "$SCRATCH_PROJ" "$SCRATCH_PROJ"
  if [ "$CP_OK" = 1 ]; then pass B8L "$CP_CMD" "$CP_OBS"; else fail B8L "$CP_CMD" "$CP_OBS"; fi
}

# B7L: control for B7 on the local disk, both sides on the host.
check_B7L() {
  local why
  why=$(why_not perl)
  [ -n "$why" ] && { skip B7L "$why"; return 0; }
  mkdir -p "$SCRATCH/b7l"
  b7_suite B7L host host "$SCRATCH/b7l"
}

# --- B2: orch-spawned worker sid equals its wipnote session id ---------------
ORCH_NAMES=""

orch_cleanup() {
  local n
  for n in $ORCH_NAMES; do with_timeout 60 orch stop "$n" >/dev/null 2>&1; done
}

# b2_settled PANE: the orch worker shows Claude's input prompt and no dialog.
b2_settled() {
  screen "$1" | strip_ansi | grep -q '❯' &&
    ! screen "$1" | strip_ansi | grep -qiE 'trust|do you want|permission'
}

check_B2() {
  local why d name reg mailbox sids hit s b2_pane
  why=$(why_not herdr model orch wipnote git)
  [ -n "$why" ] && { skip B2 "$why"; return 0; }
  d="$SCRATCH/b2"
  name="phaseb-b2-$RUN_TAG"
  new_project "$d" || { fail B2 "new_project" "scratch project failed"; return 0; }
  capd "$d" 120 orch spawn "$name" --kind claude
  [ "$CAP_RC" != 0 ] && { fail B2 "$CAP_CMD" "exit $CAP_RC: $CAP_OUT"; return 0; }
  ORCH_NAMES="$ORCH_NAMES $name"
  b2_pane=$(printf '%s' "$CAP_OUT" | jget p)
  # orch's worker pane shows the same first-run folder-trust dialog as ours
  # (Phase B run 1: send answered "blocked"); accept it before sending.
  [ -n "$b2_pane" ] && poll_pane "$b2_pane" 30 b2_settled "$b2_pane"
  if [ -n "$b2_pane" ] && trust_up "$b2_pane"; then
    dump_screen B2 "$b2_pane"
    fail B2 "orch spawn $name --kind claude" "worker pane $b2_pane still shows the folder-trust dialog; not sending (screen in the raw log)"
    return 0
  fi
  capd "$d" 300 orch send "$name" "$OK_PROMPT" --wait --timeout 240000
  local send_cmd="$CAP_CMD" send_rc="$CAP_RC"
  [ "$send_rc" != 0 ] && [ -n "$b2_pane" ] && dump_screen B2 "$b2_pane"
  reg="${ORCH_REGISTRY:-$HOME/.cache/orch/registry.json}"
  mailbox=$(jget "sessions.$name.mailbox" <"$reg")
  sids=$(perl -MJSON::PP -ne 'my $e = eval { decode_json($_) } or next; print "$e->{sid}\n" if $e->{sid}' \
    "$mailbox/events.ndjson" 2>/dev/null | sort -u)
  hit=""
  for s in $sids; do ledger_sids "$d" "" | grep -qx "$s" && hit="$hit $s=$(ledger_harness "$d" "$s")"; done
  local obs
  obs="send exit $send_rc; events.ndjson sids: $(echo "$sids" | tr '\n' ' '); in wipnote ledger (id=harness):${hit:- none}"
  if [ -n "$hit" ]; then pass B2 "$send_cmd" "$obs"; else
    dump_rows B2 "$d"
    [ -n "$b2_pane" ] && cap 15 herdr pane get "$b2_pane" && log "B2 worker pane: $CAP_OUT"
    fail B2 "$send_cmd" "$obs"
  fi
}

# --- C1: Codex exec parity probe (docs/design/delegate-interactive-parity.md) -
CX=""

# setup_cx: a scratch CODEX_HOME holding a copy of auth.json, a git work dir and
# a hook dump script. Sets CX and CX_HOOKS (the -c hook arguments).
setup_cx() {
  [ -n "$CX" ] && return 0
  CX="$SCRATCH/cx"
  mkdir -p "$CX/work" "$CX/codex-home" && chmod 700 "$CX/codex-home" || return 1
  cp "$HOME/.codex/auth.json" "$CX/codex-home/auth.json" && chmod 600 "$CX/codex-home/auth.json" || return 1
  (cd "$CX/work" && git init -q) || return 1
  # shellcheck disable=SC2016 # the dump script expands these, not this one
  printf '#!/bin/bash\ncat > "%s/${PROBE_TAG}-$1.json"\n' "$CX" >"$CX/dump.sh"
  chmod +x "$CX/dump.sh"
  CX_HOOKS=(-c "hooks.Stop=[{hooks=[{type=\"command\",command=\"$CX/dump.sh Stop\"}]}]"
    -c "hooks.SessionEnd=[{hooks=[{type=\"command\",command=\"$CX/dump.sh SessionEnd\"}]}]")
}

cx_why() {
  [ -f "$HOME/.codex/auth.json" ] || echo "no ~/.codex/auth.json to copy into a scratch CODEX_HOME"
}

check_C1() {
  local why last usage stop
  why=$(why_not model codex git)
  [ -z "$why" ] && why=$(cx_why)
  [ -n "$why" ] && { skip C1 "$why"; return 0; }
  setup_cx || { fail C1 "setup_cx" "scratch CODEX_HOME setup failed"; return 0; }
  capd "$CX/work" 300 "${HARNESS_ENV[@]}" CODEX_HOME="$CX/codex-home" PROBE_TAG=exec codex exec --json \
    -o "$CX/exec-last.txt" "${CX_HOOKS[@]}" --dangerously-bypass-hook-trust "$OK_PROMPT"
  last=$(cat "$CX/exec-last.txt" 2>/dev/null)
  usage=$(echo "$CAP_OUT" | grep '"turn.completed"' | tail -1 | jget usage)
  stop=$(jget last_assistant_message <"$CX/exec-Stop.json" 2>/dev/null)
  local obs
  obs="exit $CAP_RC; last message: '$(oneline "$last" 60)'; turn.completed usage: ${usage:-none}; Stop hook last_assistant_message: '${stop:-not fired}'"
  if [ "$CAP_RC" = 0 ] && [ -n "$usage" ] && echo "$last" | grep -qi ok; then
    pass C1 "$CAP_CMD" "$obs"
  else
    fail C1 "$CAP_CMD" "$obs"
  fi
}

# C3: the wipnote plugin loads in both Codex modes: B4 (TUI) and B5 (exec).
check_C3() {
  local b4 b5
  b4=$(result_of B4) b5=$(result_of B5)
  if [ "$b4" = PASS ] && [ "$b5" = PASS ]; then
    pass C3 "derived from B4 and B5" "TUI: PASS (B4); exec: PASS (B5)"
  elif [ "$b4" = FAIL ] || [ "$b5" = FAIL ]; then
    fail C3 "derived from B4 and B5" "TUI: ${b4:-not selected} (B4); exec: ${b5:-not selected} (B5)"
  else
    skip C3 "needs B4 and B5 to run (TUI: ${b4:-not selected}; exec: ${b5:-not selected})"
  fi
}
