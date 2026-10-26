# shellcheck shell=bash
# Container checks for scripts/delegate-phase-b.sh: B6, B7 and B8 run one side
# in the repo's devcontainer through `devcontainer exec --workspace-folder`.
# Their scratch project lives in a temporary folder inside the repo checkout
# (the only host folder the container mounts) and is removed on exit; the
# real .wipnote/ is never touched. Also b7_suite, which B7L reuses on the host.
# Sourced, never run.

CTR_WS=""
CTR_WHY=""
CSCRATCH=""

# ctr_why: why container checks cannot run, or nothing. Pure: ctr_prepare
# does the probing, in the main shell, before the checks run.
ctr_why() {
  if [ "$SELF_TEST" = 1 ]; then echo "self-test: container checks are skipped"
  elif ! have devcontainer; then echo "devcontainer CLI not on PATH (npm install -g @devcontainers/cli)"
  elif ! have docker; then echo "docker not on PATH"
  elif [ -n "$CTR_WHY" ]; then echo "$CTR_WHY"
  elif [ -z "$CTR_WS" ]; then echo "container not probed"
  fi
}

# ctr_prepare: probe the container once when the cheap preconditions hold.
ctr_prepare() {
  case "$(ctr_why)" in "container not probed") ctr_init ;; esac
}

# ctr_init: find the container's workspace path and make the shared scratch.
ctr_init() {
  cap 120 devcontainer exec --workspace-folder "$REPO" pwd
  if [ "$CAP_RC" != 0 ]; then
    CTR_WHY="devcontainer exec failed (start it with scripts/devcontainer.sh up): $(oneline "$CAP_OUT" 160)"
    return 1
  fi
  CTR_WS=$(echo "$CAP_OUT" | grep '^/' | tail -1)
  CSCRATCH=$(mktemp -d "$REPO/.phase-b-scratch.XXXXXX") || { CTR_WHY="mktemp in the repo failed"; CTR_WS=""; return 1; }
  CLEAN_DIRS="$CLEAN_DIRS $CSCRATCH"
  touch "$CSCRATCH/visible"
  ctr_sh "$CSCRATCH" 60 "test -e visible"
  if [ "$CAP_RC" != 0 ]; then
    CTR_WHY="the scratch folder is not visible in the container at $(ctr_path "$CSCRATCH")"
    CTR_WS=""
    return 1
  fi
}

# ctr_path HOSTPATH: the same path as the container sees it.
ctr_path() { printf '%s%s' "$CTR_WS" "${1#"$REPO"}"; }

# ctr_line DIR LINE: LINE wrapped to run in the container's copy of DIR.
ctr_line() { printf '%s; cd %s && %s' "$UNSET_LINE" "$(quote_cmd "$(ctr_path "$1")")" "$2"; }

# ctr_sh DIR SECS LINE: run LINE with bash -lc in the container, in DIR.
# CAP_CMD is shortened to the part that matters (the harness env unset is
# left out).
ctr_sh() {
  cap "$2" devcontainer exec --workspace-folder "$REPO" bash -lc "$(ctr_line "$1" "$3")"
  CAP_CMD="devcontainer exec --workspace-folder . bash -lc 'cd $(ctr_path "$1") && $3'"
}

# side_bg SIDE DIR OUT LINE: start LINE in the background on SIDE (host or
# ctr), output to OUT. Sets BG_PID.
side_bg() {
  log "bg [$1] (cd $2) $4"
  if [ "$1" = ctr ]; then
    devcontainer exec --workspace-folder "$REPO" bash -lc "$(ctr_line "$2" "$4")" >"$3" 2>&1 </dev/null &
  else
    (cd "$2" && bash -c "$4") >"$3" 2>&1 </dev/null &
  fi
  BG_PID=$!
  CLEAN_PIDS="$CLEAN_PIDS $BG_PID"
}

# probe_cmd SIDE ARGS: a probe.pl call line for SIDE.
probe_cmd() {
  local p="$PROBE"
  [ "$1" = ctr ] && p=$(ctr_path "$PROBE")
  shift
  printf 'perl %s %s' "$(quote_cmd "$p")" "$*"
}

# b7_lock A B DIR NAME: A holds LOCK_EX on DIR/NAME, B tries LOCK_NB. Prints
# B's answer (busy means the lock is exclusive across the two sides).
b7_lock() {
  local f="$3/$4" pid
  side_bg "$1" "$3" "$3/$4.hold.out" "$(probe_cmd "$1" hold "$4" 60)"
  pid=$BG_PID
  if ! wait_until 60 test -e "$f.held"; then echo "holder never took the lock: $(cat "$3/$4.hold.out")"; return 0; fi
  side_bg "$2" "$3" "$3/$4.try.out" "$(probe_cmd "$2" try "$4")"
  wait "$BG_PID"
  touch "$f.release"
  wait "$pid"
  tail -1 "$3/$4.try.out"
}

# b7_pair A B DIR PROBE_A PROBE_B ARGS_A ARGS_B: start both probes, release them
# together with a go file, wait for both, print both outputs.
b7_pair() {
  local pa pb
  rm -f "$3/go"
  side_bg "$1" "$3" "$3/a-$4.out" "$(probe_cmd "$1" "$4" "$6")"
  pa=$BG_PID
  side_bg "$2" "$3" "$3/b-$5.out" "$(probe_cmd "$2" "$5" "$7")"
  pb=$BG_PID
  sleep 5
  touch "$3/go"
  wait "$pa"
  wait "$pb"
  printf '%s / %s' "$(tail -1 "$3/a-$4.out")" "$(tail -1 "$3/b-$5.out")"
}

# b7_suite ID A B DIR: flock both ways, O_APPEND from both sides, rename by
# B while A reads. PASS needs all three: exclusive, no lost or torn line, no
# torn or missing read.
b7_suite() {
  local id="$1" a="$2" b="$3" dir="$4" l1 l2 cnt ren
  l1=$(b7_lock "$a" "$b" "$dir" lock1)
  l2=$(b7_lock "$b" "$a" "$dir" lock2)
  b7_pair "$a" "$b" "$dir" append append "app.txt A 4 5000 go" "app.txt B 4 5000 go" >/dev/null
  cnt=$(perl "$PROBE" count "$dir/app.txt" 40000)
  perl -e 'print "A" x 4096' >"$dir/ren.txt"
  ren=$(b7_pair "$a" "$b" "$dir" reader renamer "ren.txt 5 go" "ren.txt 5 go")
  local obs
  obs="flock $a holds, $b tries: $l1; $b holds, $a tries: $l2; O_APPEND 2x4x5000 lines: $cnt; rename by $b while $a reads: $ren"
  local cmd="scripts/delegate-phase-b/probe.pl hold|try|append|count|reader|renamer ($a + $b)"
  log "$id: $obs"
  if [ "$l1" = busy ] && [ "$l2" = busy ] && echo "$cnt" | grep -q 'lines=40000 good=40000 bad=0' &&
    echo "$ren" | grep -q 'torn=0 missing=0'; then
    pass "$id" "$cmd" "$obs"
  else
    fail "$id" "$cmd" "$obs"
  fi
}

# B7: flock and rename across the host-container bind mount.
check_B7() {
  local why
  why=$(why_not perl container)
  [ -n "$why" ] && { skip B7 "$why"; return 0; }
  mkdir -p "$CSCRATCH/b7"
  b7_suite B7 host ctr "$CSCRATCH/b7"
}

# B6: a container worker's sessions-ledger row appears in the host checkout.
check_B6() {
  local why d u
  why=$(why_not model wipnote git container)
  [ -n "$why" ] && { skip B6 "$why"; return 0; }
  d="$CSCRATCH/b6"
  new_project "$d" || { fail B6 "new_project" "scratch project failed"; return 0; }
  u=$(new_uuid)
  ctr_sh "$d" 300 "wipnote claude --in-place --allow-non-interactive -- --session-id $u -p 'reply with the word ok' </dev/null"
  local row=no project obs
  ledger_sids "$d" claude | grep -qx "$u" && row=yes
  project=$(perl -ne 'print $1 and exit if /data-session-id="\Q'"$u"'\E"[^>]*data-project="([^"]*)"/' \
    "$d/.wipnote/sessions-ledger.html" 2>/dev/null)
  obs="container exit $CAP_RC; host ledger row for $u: $row; data-project: '${project:-}'; output tail: $(echo "$CAP_OUT" | tail -2)"
  if [ "$row" = yes ]; then pass B6 "$CAP_CMD" "$obs"; else fail B6 "$CAP_CMD" "$obs"; fi
}

# B8: the claim gate sees the other side's live episode, both directions.
check_B8() {
  local why ok1 obs1
  why=$(why_not wipnote git container)
  [ -n "$why" ] && { skip B8 "$why"; return 0; }
  SCRATCH_PROJ="$CSCRATCH/b8"
  new_project "$SCRATCH_PROJ" || { fail B8 "new_project" "scratch project failed"; return 0; }
  claim_pair B8m host_sh ctr_sh "$SCRATCH_PROJ" "$SCRATCH_PROJ"
  ok1=$CP_OK obs1="host holds, container starts: $CP_OBS"
  claim_pair B8c ctr_sh host_sh "$SCRATCH_PROJ" "$SCRATCH_PROJ"
  local obs claims="" c
  for c in "$SCRATCH_PROJ"/.wipnote/claims/*.html; do [ -e "$c" ] && claims="$claims ${c##*/}"; done
  obs="$obs1; container holds, host starts: $CP_OBS; claim files:${claims:- none}"
  local cmd="WIPNOTE_SESSION_ID=<a> wipnote feature start <f> (host) / WIPNOTE_SESSION_ID=<b> ... (container), then reversed"
  if [ "$ok1" = 1 ] && [ "$CP_OK" = 1 ]; then pass B8 "$cmd" "$obs"; else fail B8 "$cmd" "$obs"; fi
}
