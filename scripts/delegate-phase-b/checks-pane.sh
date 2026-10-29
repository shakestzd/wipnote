# shellcheck shell=bash
# Checks that drive a Claude Code or Codex TUI in a background herdr tab:
# B3, B4, B9, D8 and the Codex TUI parity probe C2. Each tab is labelled
# phase-b-<check> and closed on exit. Sourced, never run.

# has_new_sid DIR HARNESS BEFORE: DIR's ledger gained a HARNESS row.
has_new_sid() { [ -n "$(new_sids "$3" "$(ledger_sids "$1" "$2")")" ]; }

# has_sid DIR HARNESS SID: DIR's ledger has SID.
has_sid() { ledger_sids "$1" "$2" | grep -qx "$3"; }

# agent_line PANE: herdr's view of the pane's agent: kind and status.
agent_line() {
  local out
  out=$(with_timeout 10 herdr agent get "$1" 2>&1)
  printf 'agent=%s status=%s' "$(printf '%s' "$out" | jget result.agent.agent)" \
    "$(printf '%s' "$out" | jget result.agent.agent_status)"
}

# agent_settled PANE: herdr reports the agent idle or done.
agent_settled() { agent_line "$1" | grep -qE 'status=(idle|done)'; }

# herdr_hooks: where herdr's integration hooks are installed, if anywhere.
herdr_hooks() {
  local f found=""
  for f in "$HOME/.claude/settings.json" "$HOME/.codex/hooks.json" "$HOME/.codex/config.toml"; do
    [ -f "$f" ] && grep -q herdr "$f" && found="$found $f"
  done
  echo "${found# }"
}

# tui_turn ID TOOL DIR LAUNCH_ARGS: launch TOOL through wipnote in a tab and
# wait for a new wipnote session row. Sets TT_CMD and TT_NEW (new session
# ids). Returns 1 only when the tab could not be started.
tui_turn() {
  local before
  # Match on session id, any harness: run 5 found the right ids tagged
  # "human" (bug-5333ea49); the harness is reported, not filtered on.
  before=$(ledger_sids "$3" "")
  TT_CMD="wipnote $2 --in-place -- $4 (in tab phase-b-$1)"
  pane_launch "$1" "$3" "wipnote $2 --in-place -- $4" || return 1
  poll_pane "$L_PANE" 150 has_new_sid "$3" "" "$before"
  TT_NEW=$(new_sids "$before" "$(ledger_sids "$3" "")" | tr '\n' ' ')
  TT_HARNESS=""
  local s
  for s in $TT_NEW; do TT_HARNESS="$TT_HARNESS $s=$(ledger_harness "$3" "$s")"; done
  return 0
}

# B3: herdr's own claude/codex hooks and the wipnote plugin hooks both report.
check_B3() {
  local hooks why_a why_b
  hooks=$(herdr_hooks)
  why_a=$(why_not claude wipnote git herdr model)
  why_b=$(why_not codex wipnote git herdr model)
  if [ -z "$hooks" ]; then
    local none="no herdr hooks in ~/.claude/settings.json or ~/.codex/{hooks.json,config.toml}: install herdr's claude and codex integrations first"
    why_a=${why_a:-$none} why_b=${why_b:-$none}
  fi
  if [ -n "$why_a" ]; then skip B3a "$why_a"; else b3_one B3a claude "$hooks"; fi
  if [ -n "$why_b" ]; then skip B3b "$why_b"; else b3_one B3b codex "$hooks"; fi
}

# OK_PROMPT is set in checks-host.sh.
# shellcheck disable=SC2153
b3_one() {
  local d="$SCRATCH/$1" herdr_view
  new_project "$d" || { fail "$1" "new_project" "scratch project failed"; return 0; }
  tui_turn "$1" "$2" "$d" "'$OK_PROMPT'" || { fail "$1" "$CAP_CMD" "tab launch failed: $CAP_OUT"; return 0; }
  poll_pane "$L_PANE" 90 agent_settled "$L_PANE"
  herdr_view=$(agent_line "$L_PANE")
  pane_exit "$L_PANE"
  local obs
  obs="herdr hooks in: $3; herdr agent get: $herdr_view; wipnote session rows (id=harness):${TT_HARNESS:- none}"
  case "$herdr_view" in
    *"agent=$2 status="[a-z]*) [ -n "$TT_NEW" ] && { pass "$1" "$TT_CMD" "$obs"; return 0; } ;;
  esac
  dump_screen "$1" "$L_PANE"
  dump_rows "$1" "$d"
  fail "$1" "$TT_CMD" "$obs; screen: $(oneline "$(screen "$L_PANE")" 160)"
}

# B4: wipnote codex, interactive: SessionStart fires and carries session_id.
check_B4() {
  local why d s idx=""
  why=$(why_not herdr model wipnote codex git)
  [ -n "$why" ] && { skip B4 "$why"; return 0; }
  d="$SCRATCH/b4"
  new_project "$d" || { fail B4 "new_project" "scratch project failed"; return 0; }
  tui_turn b4 codex "$d" "'$OK_PROMPT'" || { fail B4 "$CAP_CMD" "tab launch failed: $CAP_OUT"; return 0; }
  pane_exit "$L_PANE"
  for s in $TT_NEW; do
    grep -q "$s" "$HOME/.codex/session_index.jsonl" 2>/dev/null && idx="$idx $s"
  done
  local obs
  obs="new codex session rows: ${TT_NEW:-none}; also in ~/.codex/session_index.jsonl:${idx:- none}"
  if [ -n "$TT_NEW" ]; then pass B4 "$TT_CMD" "$obs"; else
    fail B4 "$TT_CMD" "$obs; screen: $(oneline "$(screen "$L_PANE")" 200)"
  fi
}

# B9: a digit key then enter selects an AskUserQuestion option (slice 17).
B9_PROMPT="Use the AskUserQuestion tool now to ask me to pick one colour. Give exactly three options in this order: red, green, blue. After I answer, reply with only the colour I picked."

check_B9() {
  local why d u before after ans
  why=$(why_not herdr model interactive wipnote claude git)
  [ -n "$why" ] && { skip B9 "$why"; return 0; }
  d="$SCRATCH/b9"
  new_project "$d" || { fail B9 "new_project" "scratch project failed"; return 0; }
  u=$(new_uuid)
  pane_launch b9 "$d" "wipnote claude --in-place -- --session-id $u '$B9_PROMPT'" ||
    { fail B9 "$CAP_CMD" "tab launch failed: $CAP_OUT"; return 0; }
  if ! poll_pane "$L_PANE" 150 screen_has "$L_PANE" '2[.)] *[Gg]reen'; then
    fail B9 "wipnote claude (tab phase-b-b9)" "no option list seen in 150s: $(oneline "$(screen "$L_PANE")" 200)"
    return 0
  fi
  before=$(screen "$L_PANE" | strip_ansi | tail -12)
  cap 10 herdr pane send-text "$L_PANE" 2
  local cmd="$CAP_CMD; herdr pane send-keys $L_PANE enter"
  cap 10 herdr pane send-keys "$L_PANE" enter
  sleep 15
  after=$(screen "$L_PANE" | strip_ansi | tail -12)
  ask "B9: look at tab phase-b-b9. Was option 2 (green) selected, and did Claude reply green?"
  ans=$?
  local obs
  obs="owner: $(yn $ans); before: $(oneline "$before" 160); after: $(oneline "$after" 160)"
  case $ans in 0) pass B9 "$cmd" "$obs" ;; 1) fail B9 "$cmd" "$obs" ;; *) skip B9 "no terminal to ask on; $obs" ;; esac
}

yn() { case "$1" in 0) echo yes ;; 1) echo no ;; *) echo "not asked" ;; esac; }

# D8: herdr pane read --format ansi keeps the dim/grey text of an empty input
# box and shows a typed draft in the default colour. D8a Claude Code, D8b
# Codex. No model turn: Claude Code shows no next-prompt suggestion after a
# short reply (2026-10-29 run 10), but the empty-box placeholder (Claude Code
# 'Try "..."', Codex 'Ask Codex to do anything') always is, and it is the same
# grey text idle detection must tell from a draft.
check_D8() {
  want D8a && d8_one D8a claude '❯'
  want D8b && d8_one D8b codex '›'
  return 0
}

# input_grey PANE MARKER: the input line (it starts with MARKER) has dim or
# grey text. Dismisses Codex's start-up dialogs first, since they hide the box.
input_grey() {
  codex_dialogs "$1"
  grey_now "$1" "$2" || return 1
  # Codex draws its composer, then may open a dialog over it: run 12's poll
  # passed in that gap, and the draft and /exit keys then chose "Update now".
  sleep 3
  codex_dialogs "$1"
  grey_now "$1" "$2"
}

# grey_now PANE MARKER: one read has grey text on the input line, and no
# dialog or start-up splash on screen. Codex 0.165 draws its composer under a
# braille-art splash for several seconds before its dialogs (run 13).
grey_now() {
  screen "$1" ansi >"$SCRATCH/input-grey.ansi"
  strip_ansi <"$SCRATCH/input-grey.ansi" | grep -qE "$TRUST_RE|Update available|Hooks need review|⣿" && return 1
  ! perl "$PROBE" dim "$SCRATCH/input-grey.ansi" "$2" | grep -qx 'dim_or_grey_chars=0'
}

# codex_dialogs PANE: on Codex's "Update available" prompt pick option 2,
# "Skip" (nothing installed, nothing saved); on "Hooks need review" pick
# option 3, "Continue without trusting" (D8 needs no hooks). The folder trust
# dialog behind them is poll_pane's maybe_trust.
codex_dialogs() {
  local n=0
  if screen_has "$1" 'Update available'; then
    log "$1: Codex update prompt up; choosing skip"
    n=1
  elif screen_has "$1" 'Hooks need review'; then
    log "$1: Codex hook review up; choosing continue without trusting"
    n=2
  else
    return 0
  fi
  while [ "$n" -gt 0 ]; do
    with_timeout 10 herdr pane send-keys "$1" down >/dev/null 2>&1
    n=$((n - 1))
  done
  with_timeout 10 herdr pane send-keys "$1" enter >/dev/null 2>&1
  sleep 2
}

d8_one() {
  local id="$1" tool="$2" marker="$3" why d a1 a2 q1 q2 dimc style
  why=$(why_not herdr interactive wipnote "$tool" git)
  [ -n "$why" ] && { skip "$id" "$why"; return 0; }
  d="$SCRATCH/$id"
  new_project "$d" || { fail "$id" "new_project" "scratch project failed"; return 0; }
  pane_launch "$id" "$d" "wipnote $tool --in-place" || { fail "$id" "$CAP_CMD" "tab launch failed: $CAP_OUT"; return 0; }
  # No keys go to a pane whose input box never showed: they could pick a
  # dialog option. The tab closes with the run.
  if ! poll_pane "$L_PANE" 90 input_grey "$L_PANE" "$marker"; then
    dump_screen "$id" "$L_PANE"
    skip "$id" "no grey text in the input box after 90s, or a dialog still up; no keys sent (tab phase-b-$id)"
    return 0
  fi
  ask "$id: in tab phase-b-$id, is grey (dim) text shown in the empty input box?"
  q1=$?
  a1="$SCRATCH/$id-suggestion.ansi"
  cap 15 herdr pane read "$L_PANE" --source recent-unwrapped --lines 60 --format ansi
  local cmd="$CAP_CMD" rc1="$CAP_RC"
  printf '%s' "$CAP_OUT" >"$a1"
  dimc=$(perl "$PROBE" dim "$a1" "$marker")
  with_timeout 10 herdr pane send-text "$L_PANE" "phaseb typed draft" >/dev/null 2>&1
  sleep 2
  a2="$SCRATCH/$id-draft.ansi"
  with_timeout 15 herdr pane read "$L_PANE" --source recent-unwrapped --lines 60 --format ansi >"$a2" 2>&1
  style=$(perl "$PROBE" style "$a2" "phaseb typed draft")
  ask "$id: is the typed text 'phaseb typed draft' in the normal (not grey) colour?"
  q2=$?
  with_timeout 10 herdr pane send-keys "$L_PANE" ctrl+u >/dev/null 2>&1
  pane_exit "$L_PANE"
  d8_verdict "$id" "$cmd" "$rc1" "$q1" "$q2" "$dimc" "$style"
}

# d8_verdict ID CMD RC Q1 Q2 DIM STYLE: PASS needs the read to keep a dim or
# grey run on the input line, the draft to be default, and the owner to agree
# on both. No grey text on screen or in the read leaves nothing to check.
d8_verdict() {
  local obs
  obs="read exit $3; input line read: $6; draft read: $7; owner: input grey $(yn "$4"), draft normal $(yn "$5")"
  if [ "$4" = 2 ] || [ "$5" = 2 ]; then skip "$1" "no terminal to ask on; $obs"; return 0; fi
  if [ "$4" = 1 ] && [ "$6" = dim_or_grey_chars=0 ]; then skip "$1" "no grey text in the input box to check; $obs"; return 0; fi
  if [ "$3" = 0 ] && [ "$4" = 0 ] && [ "$5" = 0 ] && [ "$6" != dim_or_grey_chars=0 ] && [ "$7" = style=default ]; then
    pass "$1" "$2" "$obs"
  else
    fail "$1" "$2" "$obs"
  fi
}

# C2: Codex TUI parity: Stop payload final text, rollout token_count,
# task_complete, TUI exit code. Same scratch CODEX_HOME and hooks as C1.
check_C2() {
  local why body stop tok task code
  why=$(why_not herdr model codex git)
  [ -z "$why" ] && why=$(cx_why)
  [ -n "$why" ] && { skip C2 "$why"; return 0; }
  setup_cx || { fail C2 "setup_cx" "scratch CODEX_HOME setup failed"; return 0; }
  body="CODEX_HOME=$(quote_cmd "$CX/codex-home") PROBE_TAG=tui codex $(quote_cmd "${CX_HOOKS[@]}") --dangerously-bypass-hook-trust '$OK_PROMPT'"
  pane_launch c2 "$CX/work" "$body" || { fail C2 "$CAP_CMD" "tab launch failed: $CAP_OUT"; return 0; }
  poll_pane "$L_PANE" 150 test -s "$CX/tui-Stop.json"
  pane_exit "$L_PANE"
  stop=$(jget last_assistant_message <"$CX/tui-Stop.json" 2>/dev/null)
  tok=$(find "$CX/codex-home/sessions" -name 'rollout-*.jsonl' -exec grep -h '"token_count"' {} + 2>/dev/null | tail -1)
  task=$(find "$CX/codex-home/sessions" -name 'rollout-*.jsonl' -exec grep -h '"task_complete"' {} + 2>/dev/null | tail -1)
  code=$(cat "$L_EXIT" 2>/dev/null)
  local nz=no
  echo "$tok" | grep -qE '"(total_tokens|input_tokens|output_tokens)": *[1-9]' && nz=yes
  local obs
  obs="tui ${code:-exit=unknown}; Stop last_assistant_message: '${stop:-not fired}'; token_count non-zero: $nz; task_complete: $(oneline "$task" 120); exec side: see C1"
  if echo "$stop" | grep -qi ok && [ "$nz" = yes ]; then
    pass C2 "codex <Stop/SessionEnd -c hooks> --dangerously-bypass-hook-trust '$OK_PROMPT' (scratch CODEX_HOME, tab phase-b-c2)" "$obs"
  else
    fail C2 "codex <Stop/SessionEnd -c hooks> --dangerously-bypass-hook-trust '$OK_PROMPT' (scratch CODEX_HOME, tab phase-b-c2)" "$obs"
  fi
}
