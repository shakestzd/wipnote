#!/usr/bin/env bash
# Runs the real wrapper + pane script against stub herdr/codex/ccx (no real tools) and
# checks clean.md beside clean.log and the report the Bash caller gets.
# usage: bash test/pane_sim.sh
ROOT="$(cd "$(dirname "$0")/.." && pwd)"; T="$(mktemp -d)"; fails=0
chmod +x "$ROOT"/test/stubs/*
ok() { echo "ok   $1"; }; bad() { echo "FAIL $1"; fails=$((fails+1)); }
run() { # $1 command -> sets OUT (returned text), D (run dir)
  rm -rf "$T/tmp" "$T/runs" "$T/log"*; mkdir -p "$T/tmp"
  # the call is the stub the hook returns; the script it runs is written beside it, as the hook does
  GEN_CAP="${CAP-60}" GEN_STUB_DIR="$T/tmp" node --experimental-strip-types "$ROOT/test/gen.ts" "$1" "$ROOT" > "$T/w.sh" 2>/dev/null
  HERDR_ENV=1 TMPDIR="$T/tmp" SIMLOG="$T/log" CCX_RUNS_DIR="$T/runs" HOME="$T" PATH="$ROOT/test/stubs:$PATH" bash "$T/w.sh" > "$T/out.bin" 2>&1; RC=$?; OUT=$(cat "$T/out.bin")
  sleep 1.5; D=$(ls -d "$T"/tmp/codex-herdr.*)
}
has() { grep -qF -- "$2" "$3" && ok "$1" || { bad "$1"; echo "     missing: $2"; sed 's/^/     | /' "$3"; }; }
nothas() { grep -qF -- "$2" "$3" && { bad "$1"; sed 's/^/     | /' "$3"; } || ok "$1"; }
fencecheck() { # every fence opened is closed by the same line
  awk '/^`{3,}/ { if (open == "") { open = $0; sub(/text$/, "", open) } else if ($0 == open) open = ""; next } END { exit open != "" }' "$1" \
    && ok "$2: no dangling fence" || bad "$2: dangling fence"; }

run 'codex exec "hello"'
has "codex: report unchanged (answer returned)" "hello back" <(echo "$OUT")
has "codex: report header" "codex exited 0" <(echo "$OUT")
has "codex: clean.log text events" "  = exit 0: hi \`\`\` # x" "$D/clean.log"
has "codex: clean.md heading" "### Turn" "$D/clean.md"
has "codex: clean.md label" '**Bash** `echo hi`' "$D/clean.md"
has "codex: clean.md fence longer than inner run" '````' "$D/clean.md"
has "codex: clean.md footer" "*input 24,603 (cached 0), output 5*" "$D/clean.md"
has "codex: hook error event shown as an error quote" "clamping SessionEnd hook timeout" "$D/clean.md"
nothas "codex: clean.log has no markdown" "###" "$D/clean.log"
fencecheck "$D/clean.md" codex

run 'echo before; codex exec "hello"; echo "chain-out"; echo "chain-err" >&2'
has "chained: report still returns the command output" "chain-out" <(echo "$OUT")
has "chained: clean.md has the events" '**Bash** `echo hi`' "$D/clean.md"
has "chained: rest of command output in a text fence" "chain-out" "$D/clean.md"
has "chained: clean.log keeps the rest of the command output" "chain-out" "$D/clean.log"
fencecheck "$D/clean.md" chained

run 'ccx task "hello"'
has "ccx: report unchanged" "ccx task ok: 3s, 2 turns" <(echo "$OUT")
has "ccx: clean.log text" "> Bash: echo hi" "$D/clean.log"
has "ccx: clean.md heading and label" "### Assistant" "$D/clean.md"
has "ccx: clean.md label" '**Bash** `echo hi`' "$D/clean.md"
has "ccx: clean.md footer" "*success in 3s, 2 turns, 5 in / 7 out tokens, \$0.01*" "$D/clean.md"
awk '/^\*\*Bash\*\* `echo hi`$/ { l = NR } l && NR == l + 2 && $0 == "```" { f = NR } f && NR == f + 1 && $0 == "hi" { ok = 1 } END { exit !ok }' "$D/clean.md" \
  && ok "ccx: clean.md label directly followed by its result" || bad "ccx: clean.md label directly followed by its result"
fencecheck "$D/clean.md" ccx


# --- 0.5.2: noise in chained calls, the newline before the header, xcrun noise ---
count() { grep -cF -- "$1" "$2"; }
eq() { [ "$2" = "$3" ] && ok "$1" || { bad "$1"; echo "     want: $3"; echo "     got:  $2"; }; }
lastbyte() { tail -c1 "$1" | od -An -c | tr -d ' '; }
export SIMNOISE=1 SIMNONL=1

run 'codex exec "hello"'
eq "standalone with noise: report ends with a newline though the answer file has none" "$(lastbyte "$T/out.bin")" '\n'
has "standalone with noise: answer returned" "hello back" <(echo "$OUT")
has "standalone with noise: header unchanged" "codex exited 0" <(echo "$OUT")
nothas "standalone: no rest-of-command section" "output of the rest of the command" <(echo "$OUT")
has "standalone: clean.md has the xcrun note" "*macOS xcrun cache errors hidden (sandbox), see raw.log*" "$D/clean.md"
eq "standalone: exactly one xcrun note" "$(count 'macOS xcrun cache errors hidden' "$D/clean.md")" 1
nothas "standalone: no xcrun line in clean.md" "xcrun_db" "$D/clean.md"
has "standalone: the real output stays in the fence" "real output line" "$D/clean.md"
has "standalone: clean.log still carries the xcrun text (text mode unchanged)" "confstr() failed" "$D/clean.log"
has "standalone: raw.log keeps the xcrun lines" "xcrun_db-abc" "$D/raw.log"
fencecheck "$D/clean.md" "standalone with noise"

CH='codex exec "hello" 2>&1 | head -8; codex exec "again" 2>&1 | head -8; echo "chain-out"; echo "chain-err" >&2'
run "$CH"
has "chained noise: the command's own output is returned" "chain-out" <(echo "$OUT")
has "chained noise: the header is returned" "--- output of the rest of the command ---" <(echo "$OUT")
nothas "chained noise: no stdin line in clean.md" "Reading additional" "$D/clean.md"
nothas "chained noise: no raw rmcp line in clean.md" "rmcp::transport" "$D/clean.md"
nothas "chained noise: no hostname in clean.md" "example.invalid" "$D/clean.md"
eq "chained noise: one MCP note in clean.md across both codex runs" "$(count 'MCP server unreachable' "$D/clean.md")" 1
nothas "chained noise: no stdin line in the returned text" "Reading additional" <(echo "$OUT")
nothas "chained noise: no raw rmcp line in the returned text" "rmcp::transport" <(echo "$OUT")
eq "chained noise: one MCP line in the returned text" "$(echo "$OUT" | grep -c 'MCP server was unreachable')" 1
has "chained noise: the unfiltered stream is kept on disk (stdin line)" "Reading additional" "$D/orig.out"
has "chained noise: the unfiltered stream is kept on disk (rmcp)" "rmcp::transport::worker" "$D/orig.out"
eq "chained: the answer text is not repeated in clean.md (one per codex run, from the events)" "$(grep -c "^hello back" "$D/clean.md")" 2
eq "chained: the answer text appears once in the returned text" "$(echo "$OUT" | grep -c '^hello back')" 1
nothas "chained: answer not glued to the header" "hello back (profile=)--- output" <(echo "$OUT")
eq "chained: the header starts its own line" "$(echo "$OUT" | grep -c '^--- output of the rest of the command ---$')" 1
eq "chained: the report ends with a newline" "$(lastbyte "$T/out.bin")" '\n'
eq "chained: xcrun note once per result with noise (two codex runs)" "$(count 'macOS xcrun cache errors hidden' "$D/clean.md")" 2
fencecheck "$D/clean.md" "chained with noise"
eq "chained: chain-out still the last text of the rest section" "$(echo "$OUT" | grep -c '^chain-out$')" 1
has "chained: stderr of the rest of the command still returned" "chain-err" <(echo "$OUT")

run 'codex exec "hello" 2>&1 | head -8; echo after; exit 3'
eq "chained: exit status of the command is preserved (3)" "$RC" 3
has "chained: failing report still shows the rest of the command" "after" <(echo "$OUT")
nothas "chained: failing report has no raw rmcp line" "rmcp::transport" <(echo "$OUT")
eq "chained: failing report ends with a newline" "$(lastbyte "$T/out.bin")" '\n'
run 'codex exec "hello" 2>&1 | head -8'
eq "chained: exit status preserved (0)" "$RC" 0
eq "chained: with only the answer and MCP noise left, the rest section is just the one note" "$(echo "$OUT" | sed -n "/^--- output of the rest/,\$p" | tail -n +2)" "! an MCP server was unreachable (rmcp transport closed); raw.log has the detail"

run 'echo before; ccx task "hello"; echo after'
eq "ccx chained: report ends with a newline" "$(lastbyte "$T/out.bin")" '\n'
nothas "ccx chained: no stdin line returned" "Reading additional" <(echo "$OUT")
nothas "ccx chained: no stdin line in clean.md" "Reading additional" "$D/clean.md"
has "ccx chained: status line still returned" "ccx task ok: 3s, 2 turns" <(echo "$OUT")
run 'ccx task "hello"'
eq "ccx standalone: report ends with a newline" "$(lastbyte "$T/out.bin")" '\n'
has "ccx standalone: stderr passes through unchanged" "Reading additional input from stdin" <(echo "$OUT")
unset SIMNOISE SIMNONL
run 'codex exec "hello"'
eq "no noise, answer file with a newline: no blank line added" "$(tail -n 1 "$T/out.bin")" "hello back (profile=)"

# --- 0.6.0: tiled layout. Concurrent delegations share one registry guarded by a mkdir lock. ---
conc() { # $1 layout (tile|fixed), $2 count, $3 max panes -> runs $2 delegations at once, waits for them
  rm -rf "$T/tmp" "$T/runs" "$T/log"* "$T"/out.[0-9]; mkdir -p "$T/tmp"; local i pids=""
  node --experimental-strip-types "$ROOT/test/gen.ts" 'codex exec "hello"' "$ROOT" "$1" "$3" > "$T/w.sh" 2>/dev/null
  for i in $(seq 1 "$2"); do
    HERDR_ENV=1 HERDR_PANE_ID=w1:p1 SIMSLOWSPLIT=0.3 TMPDIR="$T/tmp" SIMLOG="$T/log" CCX_RUNS_DIR="$T/runs" HOME="$T" PATH="$ROOT/test/stubs:$PATH" \
      bash "$T/w.sh" > "$T/out.$i" 2>&1 & pids="$pids $!"
  done
  wait $pids; sleep 2.5
}
splits() { sed 's/ /:/' "$T/log.splits" | paste -sd' ' -; }
closed() { ls "$T"/log.closed.* 2>/dev/null | wc -l | tr -d ' '; }

conc tile 4 6
eq "tile x4 at once: split sequence right-of-Claude, down, right, right-of-2" "$(splits)" "current:right w1:p101:down w1:p101:right w1:p102:right"
eq "tile x4: every pane was closed" "$(closed)" 4
eq "tile x4: registry is empty afterwards" "$(wc -l < "$T/tmp/codex-herdr-panes.w1:p1" | tr -d ' ')" 0
eq "tile x4: no lock directory left" "$(ls -d "$T"/tmp/codex-herdr-panes*.lock 2>/dev/null | wc -l | tr -d ' ')" 0
eq "tile x4: all four reports returned the answer" "$(grep -l 'hello back' "$T"/out.[0-9] | wc -l | tr -d ' ')" 4

conc tile 6 6
eq "tile x6: n>=4 splits the newest pane down" "$(splits)" "current:right w1:p101:down w1:p101:right w1:p102:right w1:p104:down w1:p105:down"
eq "tile x6: all closed" "$(closed)" 6

conc tile 3 2
eq "tile cap 2, 3 at once: only two panes made" "$(splits)" "current:right w1:p101:down"
eq "tile cap: one report says it ran without a pane" "$(cat "$T"/out.[0-9] | grep -c 'herdr pane limit reached (2 delegation panes open, layout limit 2): ran without a pane')" 1

conc fixed 4 6
eq "layout=right: old behaviour, every split targets Claude's pane to the right" "$(splits)" "current:right current:right current:right current:right"
eq "layout=right: no registry file" "$(ls "$T"/tmp/codex-herdr-panes* 2>/dev/null | wc -l | tr -d ' ')" 0

# --- 0.7.0: the call is a short stub; what is returned is capped. Every run() above already goes
# through the stub (GEN_STUB_DIR) with the default cap of 60. ---
ORIGINAL='codex exec "hello"'
CAP=60 run "$ORIGINAL"
STUB=$(cat "$T/w.sh")
eq "stub: at most 3 lines" "$(( $(echo "$STUB" | wc -l) <= 3 ))" 1
eq "stub: under 300 characters" "$(( ${#STUB} < 300 ))" 1
eq "stub: first line is the marker" "$(echo "$STUB" | head -n 1)" "# codex-herdr-pane"
nothas "stub: the original command is not in the stub" "$ORIGINAL" "$T/w.sh"
nothas "stub: no herdr command in the stub" "herdr pane" "$T/w.sh"
has "wrapper: the script ends up in the run directory" "herdr pane run" "$D/wrapper.sh"
has "wrapper: the original command is in the script verbatim" "$ORIGINAL" "$D/wrapper.sh"
has "wrapper: the original is also kept as orig.sh" "$ORIGINAL" "$D/orig.sh"
eq "wrapper: the hook-written file was moved, not left in TMPDIR" "$(ls "$T"/tmp/codex-herdr-run.* 2>/dev/null | wc -l | tr -d ' ')" 0
eq "recognition: an already-wrapped stub is not a delegation" "$(node --experimental-strip-types -e "import('$ROOT/hooks/tools.ts').then((m) => console.log(m.isDelegation(require('fs').readFileSync('$T/w.sh', 'utf8'))))" 2>/dev/null)" false
eq "recognition: the original is" "$(node --experimental-strip-types -e "import('$ROOT/hooks/tools.ts').then((m) => console.log(m.isDelegation('$ORIGINAL')))" 2>/dev/null)" true
SHORT_CAPPED=$(cat "$T/out.bin")
CAP=0 run "$ORIGINAL"
eq "short answer: returned byte for byte whether capped (60) or not (0), apart from the run directory name" \
  "$(echo "$SHORT_CAPPED" | sed 's|/codex-herdr\.[A-Za-z0-9]*|/D|g')" "$(cat "$T/out.bin" | sed 's|/codex-herdr\.[A-Za-z0-9]*|/D|g')"

CAP=60 SIMLONG=300 run "$ORIGINAL"
eq "long answer: the status line is the first line and is not cut" "$(echo "$OUT" | head -n 1 | cut -c1-14)" "codex exited 0"
eq "long answer: 60 lines of the answer are returned" "$(echo "$OUT" | grep -c '^answer line ')" 60
eq "long answer: the first lines of the answer are kept, not the last" "$(echo "$OUT" | grep -c '^answer line 60$')" 1
eq "long answer: the 61st line is not returned" "$(echo "$OUT" | grep -c '^answer line 61$')" 0
eq "long answer: one pointer line names last.md and the count" "$(echo "$OUT" | grep -cxF "... 240 more lines in $D/last.md")" 1
eq "long answer: the pointer is the last line" "$(echo "$OUT" | tail -n 1)" "... 240 more lines in $D/last.md"
eq "long answer: last.md still holds all 300 lines" "$(wc -l < "$D/last.md" | tr -d ' ')" 300
eq "long answer: exit status preserved" "$RC" 0
[ -n "$SIMSHOW" ] && { echo "--- returned text, long answer ---"; cat "$T/out.bin"; echo "--- end ---"; }

CAP=0 SIMLONG=300 run "$ORIGINAL"
eq "cap 0: the whole answer is returned" "$(echo "$OUT" | grep -c '^answer line ')" 300
nothas "cap 0: no pointer line" "more lines in" "$T/out.bin"

CAP=300 SIMLONG=300 run "$ORIGINAL"
eq "answer of exactly the cap: not cut" "$(echo "$OUT" | grep -c '^answer line ')" 300
nothas "answer of exactly the cap: no pointer line" "more lines in" "$T/out.bin"

CAP=20 SIMLONG=300 SIMEXIT=1 run "$ORIGINAL"
eq "failure: the status line is returned first and not cut" "$(echo "$OUT" | head -n 1 | cut -c1-14)" "codex exited 1"
eq "failure: exit status preserved with the cap" "$RC" 1
eq "failure: the pointer comes first, after the status line, and counts the earlier lines of clean.log" "$(echo "$OUT" | sed -n 2p)" "... $(( $(wc -l < "$D/clean.log" | tr -d ' ') - 20 )) earlier lines in $D/clean.log"
eq "failure: the last 20 lines of the 50-line tail follow the pointer (lines 31..50)" "$(echo "$OUT" | tail -n +3)" "$(tail -n 20 "$D/tail.out")"
eq "failure: the last line of the log tail is returned (the error is at the end)" "$(echo "$OUT" | tail -n 1)" "$(tail -n 1 "$D/clean.log")"
eq "failure: 21 lines after the status line, the pointer and 20 of the log" "$(echo "$OUT" | tail -n +2 | wc -l | tr -d ' ')" 21
CAP=0 SIMLONG=300 SIMEXIT=1 run "$ORIGINAL"
eq "failure, cap 0: the whole 50-line tail" "$(echo "$OUT" | tail -n +2 | wc -l | tr -d ' ')" 50
CAP=0 SIMLONG=300 SIMEXIT=1 run "$ORIGINAL"; FULL=$(echo "$OUT" | sed -e "s|/codex-herdr\.[A-Za-z0-9]*|/D|g" -e "s| after [0-9]*s,| after Ns,|")
CAP=50 SIMLONG=300 SIMEXIT=1 run "$ORIGINAL"
eq "failure: a tail of exactly the cap is returned unchanged, no pointer" "$(echo "$OUT" | sed -e "s|/codex-herdr\.[A-Za-z0-9]*|/D|g" -e "s| after [0-9]*s,| after Ns,|")" "$FULL"
CAP=60 SIMLONG=300 SIMEXIT=1 run "$ORIGINAL"
eq "failure: a tail under the cap is returned unchanged, no pointer" "$(echo "$OUT" | sed -e "s|/codex-herdr\.[A-Za-z0-9]*|/D|g" -e "s| after [0-9]*s,| after Ns,|")" "$FULL"

CHAINED='codex exec "hello" >/dev/null 2>&1; seq 1 300'
CAP=60 run "$CHAINED"
eq "chained long: exit status preserved (0)" "$RC" 0
eq "chained long: the status line and the header are kept" "$(echo "$OUT" | grep -c -e '^command exited 0' -e '^--- output of the rest of the command ---$')" 2
eq "chained long: 60 numbered lines of the rest are returned" "$(echo "$OUT" | grep -c '^[0-9][0-9]*$')" 60
eq "chained long: the pointer names rest.out" "$(echo "$OUT" | tail -n 1)" "... 240 more lines in $D/rest.out"
eq "chained long: rest.out has all 300" "$(grep -c '^[0-9][0-9]*$' "$D/rest.out")" 300
CAP=60 run "$CHAINED; exit 3"
eq "chained long failing: exit status preserved (3)" "$RC" 3
eq "chained long failing: the tail of clean.log (50 lines) fits the cap, the status says 3" "$(echo "$OUT" | head -n 1 | cut -c1-16)" "command exited 3"
CAP=0 run "$CHAINED"
eq "chained cap 0: all 300 numbered lines" "$(echo "$OUT" | grep -c '^[0-9][0-9]*$')" 300
CAP=60 run 'echo before; codex exec "hello"; echo "chain-out"'
has "chained short: returned unchanged under the cap" "chain-out" <(echo "$OUT")
nothas "chained short: no pointer" "more lines in" <(echo "$OUT")

# --- 0.7.1: two ccx runs at once, the later one's log appears first; each pane follows its own ---
rm -rf "$T/tmp" "$T/runs" "$T/log"*; mkdir -p "$T/tmp"
node --experimental-strip-types "$ROOT/test/gen.ts" 'ccx task "alpha"' "$ROOT" > "$T/wa.sh" 2>/dev/null
node --experimental-strip-types "$ROOT/test/gen.ts" 'ccx task "bravo"' "$ROOT" > "$T/wb.sh" 2>/dev/null
ENVS="HERDR_ENV=1 TMPDIR=$T/tmp SIMLOG=$T/log CCX_RUNS_DIR=$T/runs HOME=$T PATH=$ROOT/test/stubs:$PATH"
env $ENVS SIMLOGDELAY=2 bash "$T/wa.sh" > "$T/out.a" 2>&1 & PA=$!
sleep 0.5
env $ENVS bash "$T/wb.sh" > "$T/out.b" 2>&1 & PB=$!
wait $PA $PB; sleep 1.5
for d in "$T"/tmp/codex-herdr.*; do
  if grep -q alpha "$d/orig.sh"; then DA=$d; else DB=$d; fi
done
has "ccx concurrent: the first run's pane shows its own log" "thinking about alpha" "$DA/clean.log"
nothas "ccx concurrent: the first run's pane does not show the other log" "thinking about bravo" "$DA/clean.log"
has "ccx concurrent: the second run's pane shows its own log" "thinking about bravo" "$DB/clean.log"
nothas "ccx concurrent: the second run's pane does not show the other log" "thinking about alpha" "$DB/clean.log"
eq "ccx concurrent: no launch lock left" "$(ls -d "$T"/tmp/codex-herdr-ccx-claims/launch.lock 2>/dev/null | wc -l | tr -d ' ')" 0

# --- a run that gives up waiting for its log never takes a later run's log ---
rm -rf "$T/tmp" "$T/runs" "$T/log"*; mkdir -p "$T/tmp"
env $ENVS CODEX_HERDR_CCX_WAIT=1 SIMLOGDELAY=4 bash "$T/wa.sh" > "$T/out.a" 2>&1 & PA=$!
sleep 2
env $ENVS bash "$T/wb.sh" > "$T/out.b" 2>&1 & PB=$!
wait $PA $PB; sleep 1.5
for d in "$T"/tmp/codex-herdr.*; do
  if grep -q alpha "$d/orig.sh"; then DA=$d; else DB=$d; fi
done
has "ccx timeout: the slow run says it has no live log" "no live log for this run" "$DA/clean.log"
nothas "ccx timeout: the slow run does not take the later run's log" "thinking about bravo" "$DA/clean.log"
has "ccx timeout: the later run follows its own log" "thinking about bravo" "$DB/clean.log"
has "ccx timeout: the slow run still returns ccx's own result" "ccx task ok" "$T/out.a"

# --- a run that cannot get the launch lock starts ccx but claims no log ---
rm -rf "$T/tmp" "$T/runs" "$T/log"*; mkdir -p "$T/tmp/codex-herdr-ccx-claims/launch.lock"
sleep 30 & HOLD=$!; echo "$HOLD" > "$T/tmp/codex-herdr-ccx-claims/launch.lock/pid"  # a live holder
env $ENVS CODEX_HERDR_CCX_LOCKWAIT=1 bash "$T/wa.sh" > "$T/out.a" 2>&1
sleep 1.5; kill "$HOLD" 2>/dev/null
DA=$(ls -d "$T"/tmp/codex-herdr.*)
has "ccx no lock: the pane says it has no live log" "held the launch lock too long" "$DA/clean.log"
nothas "ccx no lock: no log was claimed" "thinking about" "$DA/clean.log"
has "ccx no lock: ccx still ran and returned its result" "ccx task ok" "$T/out.a"
eq "ccx no lock: the other holder's lock is untouched" "$(ls -d "$T"/tmp/codex-herdr-ccx-claims/launch.lock 2>/dev/null | wc -l | tr -d ' ')" 1

rm -rf "$T"
[ "$fails" = 0 ] && echo "pane simulation passed" || { echo "$fails failed"; exit 1; }
