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

# --- issue 225: a chained call that writes a brief and names its own -o file ---
OWN="$T/work"
for spell in '-o own.md' '--output-last-message=own.md'; do
  rm -rf "$OWN"; mkdir -p "$OWN"
  run "cd '$OWN' && printf 'brief\n' > brief.md; codex exec -C /w $spell \"\$(cat brief.md)\"; echo chain-done"
  nothas "own -o ($spell): no raw JSONL in the pane log" '{"type":"item.' "$D/clean.log"
  nothas "own -o ($spell): no raw JSONL in clean.md" '{"type":"item.' "$D/clean.md"
  has "own -o ($spell): events rendered in the pane log" "> echo hi" "$D/clean.log"
  has "own -o ($spell): the caller's -o file has the answer" "hello back" "$OWN/own.md"
  has "own -o ($spell): the command's stdout is the answer" "hello back" "$D/orig.out"
  nothas "own -o ($spell): no JSONL on the command's stdout" '{"type":"' "$D/orig.out"
  has "own -o ($spell): the report returns the answer" "hello back" <(echo "$OUT")
  nothas "own -o ($spell): no JSONL returned to Claude" '{"type":"' <(echo "$OUT")
  has "own -o ($spell): the rest of the command still returned" "chain-done" <(echo "$OUT")
  has "own -o ($spell): the shim still asks for --json" "--json" "$T/log.codexargs"
  has "own -o ($spell): the caller's -o reaches codex" "own.md" "$T/log.codexargs"
done
rm -rf "$OWN"; mkdir -p "$OWN"
run "codex exec -o '$OWN/own.md' hello"
has "standalone own -o: the caller's -o file has the answer" "hello back" "$OWN/own.md"
has "standalone own -o: the report returns the answer, not the log tail" "# Answer" <(echo "$OUT")
has "standalone own -o: the answer file has the answer" "hello back" "$D/last.md"

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
# the run time can differ by a second between the two runs, so it is masked with the run directory
eq "short answer: returned byte for byte whether capped (60) or not (0), apart from the run directory name and run time" \
  "$(echo "$SHORT_CAPPED" | sed -E 's|/codex-herdr\.[A-Za-z0-9]*|/D|g; s|after [0-9]+s|after Ns|')" "$(cat "$T/out.bin" | sed -E 's|/codex-herdr\.[A-Za-z0-9]*|/D|g; s|after [0-9]+s|after Ns|')"

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
has "ccx concurrent: the second run waited for the first to claim its log" "waiting for another ccx run" "$DB/clean.log"
eq "ccx concurrent: no launch lock left" "$(ls -d "$T/tmp/codex-herdr-ccx-claims-$(id -u)"/launch.lock 2>/dev/null | wc -l | tr -d ' ')" 0

# --- a run that gives up waiting for its log: no run takes another's log, though the late log appears first ---
rm -rf "$T/tmp" "$T/runs" "$T/log"*; mkdir -p "$T/tmp"
env $ENVS CODEX_HERDR_CCX_WAIT=1 SIMLOGDELAY=4 bash "$T/wa.sh" > "$T/out.a" 2>&1 & PA=$!
sleep 2
env $ENVS SIMLOGDELAY=3 bash "$T/wb.sh" > "$T/out.b" 2>&1 & PB=$!
wait $PA $PB; sleep 1.5
for d in "$T"/tmp/codex-herdr.*; do
  if grep -q alpha "$d/orig.sh"; then DA=$d; else DB=$d; fi
done
has "ccx timeout: the slow run says it has no live log" "no live log for this run" "$DA/clean.log"
nothas "ccx timeout: the slow run does not take the later run's log" "thinking about bravo" "$DA/clean.log"
nothas "ccx timeout: the later run does not take the slow run's late log" "thinking about alpha" "$DB/clean.log"
has "ccx timeout: the later run says why it has no live log" "not yet bound to its pane" "$DB/clean.log"
has "ccx timeout: the slow run still returns ccx's own result" "ccx task ok" "$T/out.a"
has "ccx timeout: the later run still returns ccx's own result" "ccx task ok" "$T/out.b"
eq "ccx timeout: no unbound mark is left" "$(ls "$T/tmp/codex-herdr-ccx-claims-$(id -u)"/unbound | wc -l | tr -d ' ')" 0

# --- a lock left by a run that died is broken, and the run follows its own log ---
rm -rf "$T/tmp" "$T/runs" "$T/log"*; mkdir -p "$T/tmp/codex-herdr-ccx-claims-$(id -u)/launch.lock"
sh -c 'exit 0' & DEAD=$!; wait $DEAD; echo "$DEAD" > "$T/tmp/codex-herdr-ccx-claims-$(id -u)/launch.lock/pid"
env $ENVS bash "$T/wa.sh" > "$T/out.a" 2>&1
sleep 1.5; DA=$(ls -d "$T"/tmp/codex-herdr.*)
has "ccx stale lock: the run follows its own log" "thinking about alpha" "$DA/clean.log"
has "ccx stale lock: ccx returned its result" "ccx task ok" "$T/out.a"
eq "ccx stale lock: no lock is left" "$(ls -d "$T/tmp/codex-herdr-ccx-claims-$(id -u)"/launch.lock 2>/dev/null | wc -l | tr -d ' ')" 0

# --- a claims path that is not a usable folder: the run claims nothing and does not hang ---
rm -rf "$T/tmp" "$T/runs" "$T/log"*; mkdir -p "$T/tmp"; : > "$T/tmp/codex-herdr-ccx-claims-$(id -u)"
env $ENVS bash "$T/wa.sh" > "$T/out.a" 2>&1
sleep 1.5; DA=$(ls -d "$T"/tmp/codex-herdr.*)
has "ccx bad claims folder: the pane says why it has no live log" "ccx claims folder not usable" "$DA/clean.log"
nothas "ccx bad claims folder: no log was claimed" "thinking about" "$DA/clean.log"
has "ccx bad claims folder: ccx still ran and returned its result" "ccx task ok" "$T/out.a"

# --- 0.7.2 (issue 219): a standalone review is cleaned and returns its verdict ---
SIMREVIEW=1 run 'codex review'
eq "review: the verdict is returned as the answer" "$(echo "$OUT" | sed -n '2p')" "The verdict."
nothas "review: no banner in clean.md" "OpenAI Codex v" "$D/clean.md"
nothas "review: no MCP listing in clean.md" "list_mcp_resources" "$D/clean.md"
eq "review: one text fence in clean.md" "$(grep -c '^```text$' "$D/clean.md")" 1
has "review: the progress is in clean.md" "current changes" "$D/clean.md"
has "review: raw.log keeps the banner" "OpenAI Codex v" "$D/raw.log"
fencecheck "$D/clean.md" review
SIMREVIEW=1 run "codex review > '$T/verdict.txt'"
eq "review redirected: the caller's file gets the verdict" "$(cat "$T/verdict.txt" 2>/dev/null)" "The verdict."
mkfifo "$T/verdict.pipe"; ( cat "$T/verdict.pipe" > "$T/fifo.out" ) &
SIMREVIEW=1 run "codex review > '$T/verdict.pipe'"; sleep 0.5
eq "review redirected to a FIFO: the reader gets the verdict" "$(cat "$T/fifo.out" 2>/dev/null)" "The verdict."
SIMREVIEW=1 run 'codex review'
has "review: pane title names what is reviewed" "Codex review: current changes" "$D/label"
eq "review: the pane is renamed after it starts" "$(grep -n -e '^herdr pane run' -e '^herdr pane rename' "$T/log" | cut -d: -f2 | cut -c1-16 | tr '\n' ,)" "herdr pane run w,herdr pane renam,"

run 'echo "OpenAI Codex v9 is a phrase"; echo "after the phrase"; codex exec "hello"'
has "chained: a banner-like line in the rest of the command hides nothing" "after the phrase" <(echo "$OUT")
has "chained: the line after it is in clean.md" "after the phrase" "$D/clean.md"
printf '%s\n' '```bash' 'ls' '```' '' '```text' 'err' '```' '' '```text' 'more' '```' > "$T/fences.md"
eq "joinfence: a code block keeps its closing fence; two text blocks join" "$(awk -f "$ROOT/viewer/joinfence.awk" "$T/fences.md" | tr '\n' '|')" '```bash|ls|```||```text|err|more|```|'

# --- 0.7.2: old leftovers in TMPDIR are swept ---
rm -rf "$T/tmp" "$T/runs" "$T/log"*; mkdir -p "$T/tmp/codex-herdr.old1" "$T/tmp/codex-herdr.new1" "$T/tmp/codex-herdr-ccx-claims-$(id -u)/a.jsonl"
touch "$T/tmp/codex-herdr-run.stale.sh" "$T/tmp/codex-herdr-run.fresh.sh"
touch -d '8 days ago' "$T/tmp/codex-herdr-run.stale.sh"; touch -d '2 hours ago' "$T/tmp/codex-herdr-run.fresh.sh"; touch -d '8 days ago' "$T/tmp/codex-herdr.old1" "$T/tmp/codex-herdr-ccx-claims-$(id -u)/a.jsonl"
node --experimental-strip-types "$ROOT/test/gen.ts" 'codex exec "hello"' "$ROOT" > "$T/w.sh" 2>/dev/null
env $ENVS bash "$T/w.sh" > /dev/null 2>&1; sleep 1.5
eq "sweep: a script older than 7 days is removed" "$(ls "$T/tmp/codex-herdr-run.stale.sh" 2>/dev/null | wc -l | tr -d ' ')" 0
eq "sweep: a script 2 hours old (a call that may still wait for approval) is kept" "$(ls "$T/tmp/codex-herdr-run.fresh.sh" 2>/dev/null | wc -l | tr -d ' ')" 1
eq "sweep: a run directory older than 7 days is removed" "$(ls -d "$T/tmp/codex-herdr.old1" 2>/dev/null | wc -l | tr -d ' ')" 0
eq "sweep: a newer run directory is kept" "$(ls -d "$T/tmp/codex-herdr.new1" 2>/dev/null | wc -l | tr -d ' ')" 1
eq "sweep: an old ccx claim is removed" "$(ls -d "$T/tmp/codex-herdr-ccx-claims-$(id -u)/a.jsonl" 2>/dev/null | wc -l | tr -d ' ')" 0

rm -rf "$T"
[ "$fails" = 0 ] && echo "pane simulation passed" || { echo "$fails failed"; exit 1; }
