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

# --- B2: orch workers' sids are their wipnote session ids (read-only) --------
# Reads orch's registry and each live session's mailbox events.ndjson, and
# looks every sid up in the sessions ledger of the project the worker ran in.
# It spawns, sends and stops nothing (owner 2026-10-26, B2 option 1), so orch's
# session cap does not matter. Archived mailboxes are not read: the registry
# no longer holds their working directory.

# b2_project CWD: the checkout whose .wipnote/ holds CWD's sessions, or nothing.
# A worktree's sessions land in the main checkout, so that is tried first.
b2_project() {
  local common top
  common=$(git -C "$1" rev-parse --path-format=absolute --git-common-dir 2>/dev/null)
  [ -n "$common" ] && [ -d "$(dirname "$common")/.wipnote" ] && { dirname "$common"; return 0; }
  top=$(git -C "$1" rev-parse --show-toplevel 2>/dev/null)
  [ -n "$top" ] && [ -d "$top/.wipnote" ] && echo "$top"
}

# b2_sessions REG: one "name<TAB>kind<TAB>cwd<TAB>mailbox" line per session.
b2_sessions() {
  perl -MJSON::PP -e '
    my $d = eval { local $/; decode_json(<STDIN>) } or exit 1;
    my $s = $d->{sessions} || {};
    for my $n (sort keys %$s) {
      my $r = $s->{$n};
      print join("\t", $n, map { $r->{$_} // "" } qw(kind cwd mailbox)), "\n";
    }' <"$1"
}

# b2_sids MAILBOX: the distinct sids in MAILBOX's events.ndjson.
b2_sids() {
  perl -MJSON::PP -ne 'my $e = eval { decode_json($_) } or next; print "$e->{sid}\n" if $e->{sid}' \
    "$1/events.ndjson" 2>/dev/null | sort -u
}

# b2_one NAME KIND CWD MAILBOX: check one session; adds to B2_OBS, B2_SIDS,
# B2_HITS and B2_PROJ.
b2_one() {
  local sids proj s hits="" miss=""
  case "$2" in claude|codex|claude_p|codex_exec) ;; *) return 0 ;; esac
  sids=$(b2_sids "$4")
  [ -z "$sids" ] && return 0
  proj=$(b2_project "$3")
  [ -z "$proj" ] && { B2_OBS="$B2_OBS $1: no wipnote project;"; return 0; }
  B2_PROJ=$((B2_PROJ + 1))
  for s in $sids; do
    B2_SIDS=$((B2_SIDS + 1))
    if ledger_sids "$proj" "" | grep -qx "$s"; then
      B2_HITS=$((B2_HITS + 1)); hits="$hits $s=$(ledger_harness "$proj" "$s")"
    else miss="$miss $s"; fi
  done
  B2_OBS="$B2_OBS $1($2):${hits:- none}${miss:+; not in ledger:$miss};"
}

check_B2() {
  local why reg cmd name kind cwd mbox tab
  why=$(why_not perl git)
  [ -n "$why" ] && { skip B2 "$why"; return 0; }
  reg="${ORCH_REGISTRY:-$HOME/.cache/orch/registry.json}"
  [ -r "$reg" ] || { skip B2 "no orch registry at $reg"; return 0; }
  cmd="read $reg and each session's events.ndjson (read-only)"
  B2_OBS="" B2_SIDS=0 B2_HITS=0 B2_PROJ=0
  tab=$(printf '\t')
  while IFS="$tab" read -r name kind cwd mbox; do
    [ -n "$name" ] && b2_one "$name" "$kind" "$cwd" "$mbox"
  done <<EOS
$(b2_sessions "$reg")
EOS
  log "B2 per session:$B2_OBS"
  [ "$B2_PROJ" = 0 ] && { skip B2 "no orch session with events in a wipnote project:$B2_OBS"; return 0; }
  # One sid found in the ledger proves the property; misses are listed (a
  # worker that ran before its project had wipnote, or bug-5333ea49's era).
  if [ "$B2_HITS" -gt 0 ]; then pass B2 "$cmd" "$B2_HITS of $B2_SIDS sids in a wipnote ledger (id=harness):$B2_OBS"
  else fail B2 "$cmd" "none of $B2_SIDS sids in a wipnote ledger:$B2_OBS"; fi
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
