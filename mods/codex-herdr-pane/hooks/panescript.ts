// The script that runs inside the herdr pane (see wrap.ts for the Bash-call side).

import type { DelegationTool } from './tools.ts'
import type { WrapOptions } from './wrap.ts'
import { mdFunctions } from './mdpipe.ts'

// The Textual versions the viewer was tested on, defined once. The `uv run --with` in the pane
// script uses it; codex_view.py's inline header repeats it because a script header cannot
// import, and register.test.ts fails if the two differ.
export const TEXTUAL_REQ = 'textual>=8.2,<9'

// Chained calls (isStandalone in tools.ts) capture the whole original command's
// stdout and stderr into orig.out and orig.err while the same streams still feed the
// pane's live log (clean.log), by two tees. The command's own status goes through
// orig.st, since a pipeline reports the last stage.
const RUN_ORIG = [
  'run_orig() {',
  '  { { "$@" 2>&1 1>&3 3>&-; echo $? > "$D/orig.st"; } | tee "$D/orig.err" >(mdrest >> "$D/clean.md") | restfilter "${MCP_SEEN_LOG:-}" >> "$D/clean.log"; } 3>&1 | tee "$D/orig.out" >(mdrest >> "$D/clean.md") | restfilter "${MCP_SEEN_LOG:-}" >> "$D/clean.log"',
  '  return "$(cat "$D/orig.st")"',
  '}',
]

// After a chained run: rest.out / rest.err are orig.out / orig.err put through the same
// noise rules as clean.md (restfilter, mdpipe.ts); the report returns them. The
// unfiltered streams stay in orig.out / orig.err.
const MKREST = 'mkrest() { for s in out err; do restfilter "$D/mcp.ret.seen" < "$D/orig.$s" > "$D/rest.$s"; done; }'

const USAGE = [
  '  jq -R -r --arg mode usage -f "$ROOT/viewer/codex.jq" "$D/raw.log" 2>/dev/null | tail -n 1 > "$D/usage"',
  '  [ -s "$D/usage" ] || { k=$(grep -A1 "^tokens used" "$D/clean.log" 2>/dev/null | tail -1); [ -n "$k" ] && echo "$k tokens" > "$D/usage"; }',
  '  echo "$X" > "$D/exit.tmp"; date +%s > "$D/end"; mv "$D/exit.tmp" "$D/exit"',
]

// The background half of the pane for codex. Codex's output (JSONL for exec, plain
// text for review, stderr merged) goes to raw.log untouched, then through codex.jq
// (events become text, other lines pass) and clean.awk (noise rules) into clean.log.
// A second consumer of the same tee, mdcodex (mdpipe.ts), writes clean.md live; once the
// run ends clean.md is rebuilt whole from raw.log, which makes it complete however the
// live writer was scheduled.
// After the run, `usage` gets the token split from the same codex.jq, else the
// "tokens used" figure; the report and the viewer both read that one file.
const CODEX_RUN = [
  '  __CODEX_PROFILE="__PROFILE__" __CODEX_LAST="$D/last.md" bash -c \'. "$1/shim.sh"; . "$1/orig.sh"\' _ "$D" 2>&1 \\',
  '    | tee "$D/raw.log" >(mdcodex >> "$D/clean.md") | jq --unbuffered -R -r -f "$ROOT/viewer/codex.jq" 2>/dev/null \\',
  '    | awk -f "$ROOT/viewer/clean.awk" > "$D/clean.log"',
  '  X="${PIPESTATUS[0]}"',
  '  mdcodex < "$D/raw.log" > "$D/clean.md.tmp" && mv "$D/clean.md.tmp" "$D/clean.md"',
  ...USAGE,
]

// Chained: the shim (shim.ts) feeds raw.log and clean.log itself and gives the
// command's stdout the answer text.
const CODEX_CHAIN_RUN = [
  '  : > "$D/raw.log"; : > "$D/clean.log"',
  '  export __CODEX_PROFILE="__PROFILE__" __CODEX_LAST="$D/last.md" __CODEX_CHAIN=1 __CODEX_D="$D" __CODEX_ROOT="$ROOT"',
  '  run_orig bash -c \'. "$1/shim.sh"; . "$1/orig.sh"\' _ "$D"; X=$?',
  '  mkrest',
  ...USAGE,
]

// For ccx: stdout (the status line and the result) goes to out.txt untouched and
// stderr to err.log. A follower finds the run's stream-json log: a *.jsonl that was not in
// the runs directory when the pane started and that no other pane has claimed (an atomic
// mkdir per log name), so two overlapping runs never follow the same log and no timestamp
// comparison is needed. A launch lock lets only one run at a time wait for its new log, so
// the log a run claims is the one it started. It renders each complete line through stream.jq (text
// into clean.log, Markdown into clean.md). It polls by line count, so a half-written line waits for its newline
// and the final pass after the command ends takes whatever is left. The Markdown is rendered
// from line 1 each time (a call and its result are paired across lines, so a chunk alone is
// not enough) and only the new bytes are appended; the final pass (mode md) also shows a
// call that never got a result.
const CCX_RUN = [
  '  RUNS="${CCX_RUNS_DIR:-}"; [ -n "$RUNS" ] || RUNS=__RUNS__; : > "$D/clean.log"',
  '  CLAIMS="${TMPDIR:-/tmp}/codex-herdr-ccx-claims"; mkdir -p "$CLAIMS" 2>/dev/null',
  // One ccx run at a time waits for its new log: the launch lock is held from the snapshot
  // until this run claims its log (or CODEX_HERDR_CCX_WAIT, 30 s, passes and it stops looking,
  // or the command ends), so a new log can only be this run's. Others wait their turn; a holder that died is broken by its pid.
  '  LOCK="$CLAIMS/launch.lock"; waited=0; HAVE=0',
  '  for i in $(seq 1 $(( ${CODEX_HERDR_CCX_LOCKWAIT:-120} * 10 ))); do',
  '    if mkdir "$LOCK" 2>/dev/null; then echo $$ > "$LOCK/pid"; HAVE=1; break; fi',
  '    p=$(cat "$LOCK/pid" 2>/dev/null); if [ -n "$p" ] && ! kill -0 "$p" 2>/dev/null; then rm -rf "$LOCK"; continue; fi',
  '    [ "$waited" = 0 ] && { echo "*waiting for another ccx run to start its log*" | tee -a "$D/clean.log" >> "$D/clean.md"; waited=1; }',
  '    sleep 0.1',
  '  done',
  // Without the lock a run still starts ccx, but it never claims a log: a log it found could be
  // another run's.
  '  [ "$HAVE" = 1 ] || echo "*another ccx run held the launch lock too long: no live log for this run*" | tee -a "$D/clean.log" >> "$D/clean.md"',
  '  release() { [ "$(cat "$LOCK/pid" 2>/dev/null)" = "$$" ] && rm -rf "$LOCK"; return 0; }',
  '  ls -1 "$RUNS" 2>/dev/null | grep "\\.jsonl$" > "$D/before"',
  '  if [ ! -d "$RUNS" ]; then printf "*ccx runs directory not found: %s (set ccxRunsDir or CCX_RUNS_DIR)*\\n" "$RUNS" | tee -a "$D/clean.log" >> "$D/clean.md"; fi',
  '  claim() { ls -1 "$RUNS" 2>/dev/null | grep "\\.jsonl$" | grep -vxFf "$D/before" | while IFS= read -r c; do mkdir "$CLAIMS/$c" 2>/dev/null && { printf "%s\\n" "$RUNS/$c"; break; }; done; }',
  '  jqs() { nf=; [ "$1" = view ] || nf=-n; jq --unbuffered $nf -R -r --arg mode "$1" -f "$ROOT/viewer/stream.jq" 2>/dev/null; }',
  '  W=0  # bytes of the Markdown render already in clean.md: it is only ever appended to',
  '  render() { sed -n "$1,$2p" "$f" > "$D/chunk"; jqs view < "$D/chunk" >> "$D/clean.log"',
  '    sed -n "1,$2p" "$f" | jqs "$3" > "$D/md.tmp"; tail -c +"$((W+1))" "$D/md.tmp" >> "$D/clean.md"; W=$(wc -c < "$D/md.tmp" | tr -d " "); }',
  '  (',
  '    f=""; n=0; T0=$(date +%s); gaveup=$((1 - HAVE)); WAIT="${CODEX_HERDR_CCX_WAIT:-30}"',
  '    while :; do',
  '      done=0; [ -f "$D/cmddone" ] && done=1',
  '      [ -z "$f" ] && [ "$gaveup" = 0 ] && f=$(claim)',
  '      [ -n "$f" ] && release',
  // A run whose log has not appeared in time gives the lock up AND stops claiming, so it can
  // never take the log of a run that starts after it; its answer still comes from out.txt.
  '      if [ -z "$f" ] && [ "$gaveup" = 0 ] && [ $(( $(date +%s) - T0 )) -ge "$WAIT" ]; then',
  '        release; gaveup=1; echo "*ccx did not start its log within ${WAIT} s: no live log for this run*" | tee -a "$D/clean.log" >> "$D/clean.md"',
  '      fi',
  '      if [ -n "$f" ]; then',
  '        t=$(wc -l < "$f" | tr -d " ")',
  '        [ "$done" = 1 ] && render "$((n+1))" \'$\' md',
  '        [ "$done" = 0 ] && [ "$t" -gt "$n" ] && { render "$((n+1))" "$t" mdlive; n=$t; }',
  '      fi',
  '      [ "$done" = 1 ] && break',
  '      sleep 0.3',
  '    done',
  '    release',
  '  ) &',
  '  FOLLOW=$!',
  '  if [ "$CHAIN" = 1 ]; then run_orig bash "$D/orig.sh"; X=$?; mkrest; cp "$D/rest.out" "$D/out.txt"; cp "$D/rest.err" "$D/err.log"',
  '  else bash "$D/orig.sh" > "$D/out.txt" 2> "$D/err.log"; X=$?; fi',
  '  cat "$D/err.log" > "$D/raw.log"',
  '  touch "$D/cmddone"; wait $FOLLOW; release',
  '  { printf "> %s\\n\\n" "$(head -n 1 "$D/out.txt")"; tail -n +2 "$D/out.txt"; } > "$D/last.md"',
  '  echo "$X" > "$D/exit.tmp"; date +%s > "$D/end"; mv "$D/exit.tmp" "$D/exit"',
]

// What runs inside the pane: the tool in the background writing the logs, the
// viewer in the foreground, a plain fallback if it cannot run.
// Empty turns the profile off; the name is held to filename characters.
const profileName = (o: WrapOptions) => (o.leanProfile ? o.leanProfileName.replace(/[^A-Za-z0-9_.-]/g, '') : '')

// ccx's run-log directory when neither the ccxRunsDir setting nor CCX_RUNS_DIR names one: the
// author's container layout, kept so existing setups keep working. A missing directory is
// reported in the pane instead of leaving it empty.
const DEFAULT_RUNS = '"$HOME/work/_shared/claude-container/runs"'
const shq = (s: string) => `'${s.replace(/'/g, `'\\''`)}'`

export function paneScript(o: WrapOptions, tool: DelegationTool, chained: boolean): string[] {
  const codex = (chained ? CODEX_CHAIN_RUN : CODEX_RUN).map((l) => l.replace('__PROFILE__', profileName(o)))
  const run = tool.filter === 'ccx'
    ? CCX_RUN.map((l) => l.replace('__RUNS__', () => (o.runsDir ? shq(o.runsDir) : DEFAULT_RUNS)))
    : codex
  return [
    '#!/usr/bin/env bash',
    'D="$1"; ROOT="$2"; VIEW="$3"; CHAIN="$4"',
    ...RUN_ORIG,
    ...mdFunctions('$ROOT'),
    MKREST,
    'if [ "$CHAIN" = 1 ]; then export MCP_SEEN_MD="$D/mcp.md.seen" MCP_SEEN_LOG="$D/mcp.log.seen"; fi',
    'date +%s > "$D/start"; : > "$D/clean.md"',
    '(',
    ...run,
    ') &',
    'RUN=$!',
    'if [ "$VIEW" = textual ] && command -v uv >/dev/null 2>&1 \\',
    '   && uv run --quiet --no-project --with "' + TEXTUAL_REQ + '" python "$ROOT/viewer/codex_view.py" "$D"; then :',
    'else',
    '  clear; touch "$D/clean.log"; tail -f "$D/clean.log" & T=$!',
    '  while [ ! -f "$D/exit" ]; do sleep 0.5; done; sleep 0.5; kill $T 2>/dev/null',
    '  if [ -s "$D/last.md" ]; then',
    '    printf "\\n--- answer ---\\n"',
    '    if command -v glow >/dev/null 2>&1; then glow "$D/last.md"; else cat "$D/last.md"; fi',
    '  fi',
    'fi',
    'wait $RUN',
  ]
}

