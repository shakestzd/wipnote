#!/usr/bin/env bash
# Checks the hc forms and reply shapes that internal/delegate/herdr/core.go
# assumes, against a real herdr-core (feat-4d4ca0a1). Rerun it after a herdr-core
# upgrade: its schema can change between releases.
#
# It runs a throwaway core with its own HC_SESSION and XDG folders, as hc's
# manual requires for tests, so it never touches your terminals. It starts no
# agent and costs no tokens.
#
#   scripts/delegate-core-probe.sh            # uses herdr-core or hc on PATH
#   HC_BIN=/path/to/herdr-core scripts/delegate-core-probe.sh
set -u

HC_BIN=${HC_BIN:-$(command -v herdr-core || command -v hc || true)}
if [ -z "$HC_BIN" ]; then
  echo "no herdr-core or hc on PATH: curl -fsSL https://releases.herdr.dev/core/install.sh | sh" >&2
  exit 2
fi
command -v python3 >/dev/null || { echo "needs python3" >&2; exit 2; }

work=$(mktemp -d)
export HC_SESSION="wipnote-probe-$$"
export XDG_CONFIG_HOME="$work/config" XDG_STATE_HOME="$work/state" XDG_DATA_HOME="$work/data"
export XDG_CACHE_HOME="$work/cache" XDG_RUNTIME_DIR="$work/run"
mkdir -p "$XDG_RUNTIME_DIR" && chmod 700 "$XDG_RUNTIME_DIR"
unset HC_TERMINAL_ID HC_SOCKET

cleanup() { "$HC_BIN" server stop >/dev/null 2>&1; rm -rf "$work"; }
trap cleanup EXIT

fails=0
pass() { echo "PASS  $1"; }
fail() { echo "FAIL  $1: $2"; fails=$((fails + 1)); }
# field FILE EXPR: prints a value from a JSON reply, with d as the reply.
field() { python3 -I -c 'import json,sys; d=json.load(open(sys.argv[1])); print(eval(sys.argv[2]))' "$1" "$2" 2>/dev/null; }
# until_true SECONDS CMD...: retries CMD until it succeeds.
until_true() {
  local n=$(( $1 * 5 )); shift
  while [ "$n" -gt 0 ]; do "$@" && return 0; sleep 0.2; n=$((n - 1)); done
  return 1
}

echo "herdr-core: $("$HC_BIN" --version 2>&1)"
hc() { "$HC_BIN" "$@"; }

# Start a parent as delegate's caller, then its child the way Core.startShell does.
hc run --bg --json --name probe-parent --cwd "$work" --label herdr.space=s1 --label herdr.tab=tb1 >"$work/parent.json" 2>"$work/err"
P=$(field "$work/parent.json" "d['address']")
[ -n "$P" ] && pass "run --bg --json prints a bare reply with .address ($P)" || { fail "run reply" "$(cat "$work/err" "$work/parent.json")"; exit 1; }

hc run --bg --outlive-parent --no-notify --parent "$P" --cwd "$work" --label herdr.space=s1 --label herdr.tab=wipnote-workers-"$P" --json >"$work/child.json" 2>"$work/err"
T=$(field "$work/child.json" "d['address']")
[ -n "$T" ] && pass "run takes --outlive-parent --no-notify --label ($T)" || { fail "child run" "$(cat "$work/err")"; exit 1; }

bare_shell() {
  hc get "$T" --json >"$work/get.json" 2>/dev/null || return 1
  [ "$(field "$work/get.json" "d['pid'] == d['foreground']['pid']")" = True ]
}
if until_true 10 bare_shell; then
  pass "a fresh shell has foreground.pid == pid, so bareShell sees it: $(field "$work/get.json" "d['foreground']['name']")"
else
  fail "bare shell" "pid and foreground.pid differ, so spawn would never type its boot line: $(cat "$work/get.json")"
fi

[ "$(field "$work/get.json" "d['state']")" = live ] && pass "get reports state live" || fail "get state" "$(cat "$work/get.json")"
[ "$(field "$work/get.json" "d['labels']['herdr.space']")" = s1 ] && pass "get reports labels" || fail "get labels" "$(cat "$work/get.json")"
[ "$(field "$work/get.json" "d['parent']")" = "$P" ] && pass "get reports the parent" || fail "get parent" "$(cat "$work/get.json")"

marker="wipnote-probe-$$"
hc send "$T" "printf '%s HC=%s\n' $marker \"\$HC_TERMINAL_ID\" > $work/line; echo $marker" --json >"$work/send.json" 2>"$work/err" \
  && pass "send pastes a shell line and presses enter" || fail "send" "$(cat "$work/err")"
line_written() { [ -s "$work/line" ]; }
if until_true 10 line_written; then
  [ "$(cat "$work/line")" = "$marker HC=$T" ] && pass "the terminal carries HC_TERMINAL_ID=$T and no typing was lost" || fail "line" "$(cat "$work/line")"
else
  fail "send line" "the line never ran"
fi

hc read "$T" --lines 40 >"$work/read.txt" 2>"$work/err"
grep -q "$marker" "$work/read.txt" && pass "read --lines prints plain text" || fail "read" "$(cat "$work/err" "$work/read.txt")"

# Core.PaneReadStyled: a viewer's first paint keeps the styling hc read drops.
hc send "$T" "printf '\\033[2mdim-$marker\\033[0m\\n'" --json >/dev/null 2>&1
sleep 1
"$HC_BIN" attach --observe "$T" </dev/null >"$work/paint.raw" 2>/dev/null &
paint=$!; sleep 1; kill "$paint" 2>/dev/null; wait "$paint" 2>/dev/null
grep -q "$(printf '\033')\[2mdim-$marker" "$work/paint.raw" && pass "attach --observe keeps SGR 2 in its first paint" || fail "styled read" "no dim text in the paint"

hc label "$T" "wipnote.title=feat-1: a title with spaces" --json >/dev/null 2>"$work/err"
hc get "$T" --json >"$work/get.json"
[ "$(field "$work/get.json" "d['labels']['wipnote.title']")" = "feat-1: a title with spaces" ] \
  && pass "label keeps a value with spaces" || fail "label" "$(cat "$work/err" "$work/get.json")"

hc keys "$T" / e x i t space shift+a --json >/dev/null 2>"$work/err" && pass "keys takes each name Core.SendText sends" || fail "keys" "$(cat "$work/err")"
hc keys "$T" ctrl+c --json >/dev/null 2>&1

hc ls --json >"$work/ls.json"
[ "$(field "$work/ls.json" "any(t['address'] == '$T' for t in d['terminals'])")" = True ] \
  && pass "ls --json lists .terminals with .address" || fail "ls" "$(cat "$work/ls.json")"

printf 'x' | hc get nosuchterminal --json >/dev/null 2>"$work/err"; code=$?
[ "$code" = 3 ] && pass "a missing terminal exits 3" || fail "missing exit" "exit $code: $(cat "$work/err")"
[ "$(field "$work/err" "d['error']['kind']")" = not_found ] && pass "the error is .error.kind=not_found" || fail "missing kind" "$(cat "$work/err")"

# The worker must survive its caller, as a worker does in the TUI.
hc kill "$P" --json >/dev/null 2>&1
still_live() { hc get "$T" --json >"$work/get.json" 2>/dev/null; }
sleep 2
if still_live && [ "$(field "$work/get.json" "d['state']")" = live ]; then
  pass "--outlive-parent keeps the worker when its caller ends"
else
  fail "outlive" "the child ended with its parent: $(cat "$work/get.json")"
fi

hc kill "$T" --json >/dev/null 2>"$work/err" && pass "kill ends a terminal" || fail "kill" "$(cat "$work/err")"
finished() { hc get "$T" --json >"$work/get.json" 2>/dev/null; [ "$(field "$work/get.json" "d['state']")" = finished ]; }
until_true 10 finished && pass "a killed terminal reads finished" || fail "finished" "$(cat "$work/get.json")"
hc rm "$T" --json >/dev/null 2>"$work/err" && pass "rm removes a finished terminal" || fail "rm" "$(cat "$work/err")"
hc get "$T" --json >/dev/null 2>&1; code=$?
[ "$code" = 3 ] && pass "a removed terminal exits 3" || fail "removed exit" "exit $code"

echo
if [ "$fails" -eq 0 ]; then
  echo "All checks passed."
else
  echo "$fails check(s) failed. Fix internal/delegate/herdr/core.go to match."
  exit 1
fi
