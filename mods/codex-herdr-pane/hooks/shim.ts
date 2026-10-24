import { MDWRITER_FN, NL_FN, mdFunctions, streamJq } from './mdpipe.ts'

// The shell function that stands in for `codex` inside the pane. Pure text.
//
// For exec/e it adds, each only when the call does not already say so:
//   --output-last-message  the answer as a clean file
//   -p <profile>           the lean profile, only when ~/.codex/<profile>.config.toml
//                          exists (codex review has no profile flag and is untouched)
//   --json                 so the usage event can be read (codex.jq renders it)
// A standalone review (it has no --json) writes its final message to stdout and its progress
// to stderr: stdout goes to the answer file, so the call returns the verdict as exec does and
// the log holds the progress once. A chained review keeps stdout for the caller, and so does a
// review whose stdout the caller redirected: the wrapper sets __CODEX_REVIEW_LAST only for a
// standalone call with no stdout redirect of its own (redirectsStdout, tools.ts).
// CHAINED calls (see isStandalone in tools.ts) set __CODEX_CHAIN: the command's stdout
// is the caller's, so for exec the shim sends codex's JSONL only to the pane
// (raw.log, then codex.jq and clean.awk into clean.log) and prints the answer file to
// its own stdout, which is what a plain `codex exec` prints. A call that names its own -o
// file takes the same route (issue 225: its stdout used to stay JSONL and reached the pane
// raw): codex writes the caller's file exactly as asked, and the shim copies it to the answer
// file, so the report, the Answer tab and the chained stdout all get the answer. Every other
// call with its own -o gets that copy too. Only a chained call that asks for --json itself
// keeps codex's stdout as it is.
// The same stream also feeds clean.md through mdcodex (mdpipe.ts).
// __CODEX_PROFILE is empty when the option is off. A function, so no quoting of the
// user's text.
export const SHIM = [
  ...mdFunctions('$__CODEX_ROOT'),
  NL_FN,
  // The caller's own -o file becomes the answer file too (a relative path is read from the
  // directory codex was started in, as codex wrote it)
  'own_to_last() { [ -n "$own" ] && [ "$own" != "$__CODEX_LAST" ] && [ -f "$own" ] && cp "$own" "$__CODEX_LAST" 2>/dev/null; return 0; }',
  'codex() {',
  '  local a sub="$1" pre=() has_o="" has_p="" has_j="" own="" want=""',
  '  if [ -n "${__CODEX_LAST:-}" ] && { [ "$1" = exec ] || [ "$1" = e ]; }; then',
  '    shift',
  // own: the caller's -o file, in each spelling clap accepts (-o F, -oF, -o=F, --output-last-message[=]F)
  '    for a in "$@"; do',
  '      if [ -n "$want" ]; then own="$a"; want=""; continue; fi',
  '      case "$a" in',
  '      --) break;;',
  '      -o|--output-last-message) has_o=1; want=1;;',
  '      --output-last-message=*) has_o=1; own="${a#*=}";;',
  '      -o?*) has_o=1; own="${a#-o}"; own="${own#=}";;',
  '      -p|--profile|--profile=*) has_p=1;;',
  '      --json) has_j=1;;',
  '    esac; done',
  '    [ -z "$has_o" ] && pre+=(--output-last-message "$__CODEX_LAST")',
  '    if [ -z "$has_p" ] && [ -n "${__CODEX_PROFILE:-}" ] && [ -f "$HOME/.codex/${__CODEX_PROFILE}.config.toml" ]; then pre+=(-p "$__CODEX_PROFILE"); fi',
  '    [ -z "$has_j" ] && pre+=(--json)',
  '    local rc',
  '    if [ -n "${__CODEX_CHAIN:-}" ] && [ -z "$has_j" ]; then',
  '      : > "$__CODEX_LAST"',
  '      command codex "$sub" "${pre[@]}" "$@" | tee -a "$__CODEX_D/raw.log" >(mdcodex >> "$__CODEX_D/clean.md") | jq --unbuffered -R -r -f "$__CODEX_ROOT/viewer/codex.jq" 2>/dev/null \\',
  '        | awk -v seen="${MCP_SEEN_LOG:-}" -f "$__CODEX_ROOT/viewer/clean.awk" >> "$__CODEX_D/clean.log"',
  '      rc="${PIPESTATUS[0]}"; own_to_last; cat "$__CODEX_LAST" 2>/dev/null; nl_if_open "$__CODEX_LAST"; return "$rc"',
  '    fi',
  '    command codex "$sub" "${pre[@]}" "$@"; rc=$?',
  '    own_to_last; return "$rc"',
  '  elif [ -n "${__CODEX_LAST:-}" ] && [ -n "${__CODEX_REVIEW_LAST:-}" ] && [ "$1" = review ]; then',
  '    command codex "$@" > "$__CODEX_LAST"',
  '  else',
  '    command codex "$@"',
  '  fi',
  '}',
]

// The shell functions that stand in for `claude` and `dev` inside the pane. Pure text.
// A claude call in print mode (-p or --print among its words) that names no --output-format
// gets `--output-format stream-json --verbose` (print mode refuses stream-json without
// --verbose), so the pane shows each message, tool call and result as it happens (stream.jq).
// A call that names its own format, or is not in print mode, runs exactly as asked. A
// `dev exec [-d <dir>] claude ...` call gets the same flags after its claude word.
// STANDALONE: the stream is the pane's stdout (raw.log, then stream.jq into clean.log) and the
// pane script takes the answer from its last result event. When the command's stdout is the
// caller's (__CLAUDE_CHAIN: a chained call, or one that redirects stdout itself) the shim sends
// the stream to the pane only and prints the answer (the result event's text), which is what a
// plain `claude -p` prints. Each call streams into a file of its own, so calls running at once
// in one command never read each other's answer; the newest to end is the run's last.md. The
// call returns only once its Markdown is in clean.md (mdwriter, mdpipe.ts), so what the command
// prints next lands after it. If the writer failed, the call's whole render is added after a note
// saying the live one was cut: clean.md holds the rest of the command too, so it is never redone.
export const CLAUDE_SHIM = [
  streamJq('$__CLAUDE_ROOT'),
  MDWRITER_FN,
  NL_FN,
  // __PRE: the flags to add, none when the call is not print mode or names its own format
  '__claude_pre() {',
  '  local a want="" p="" fmt="" v=""; __PRE=()',
  '  for a in "$@"; do',
  '    if [ -n "$want" ]; then fmt="$a"; want=""; continue; fi',
  '    case "$a" in --) break;; -p|--print) p=1;; --verbose) v=1;; --output-format) fmt=set; want=1;; --output-format=*) fmt="${a#*=}";; esac',
  '  done',
  '  if [ -n "$p" ] && [ -z "$fmt" ]; then __PRE=(--output-format stream-json); [ -z "$v" ] && __PRE+=(--verbose); fi',
  '  return 0',
  '}',
  // $1: how many words lead up to claude (command claude, or command dev exec ... claude); the rest are claude's own
  '__claude_go() {',
  '  local n="$1" rc; shift; local head=("${@:1:$n}"); shift "$n"',
  '  __claude_pre "$@"',
  '  if [ "${#__PRE[@]}" = 0 ] || [ -z "${__CLAUDE_LAST:-}" ]; then "${head[@]}" "$@"; return; fi',
  '  if [ -z "${__CLAUDE_CHAIN:-}" ]; then "${head[@]}" "${__PRE[@]}" "$@"; return; fi',
  '  local cl; cl=$(mktemp "$__CLAUDE_D/call.XXXXXX") || { "${head[@]}" "$@"; return; }',
  '  "${head[@]}" "${__PRE[@]}" "$@" | tee -a "$__CLAUDE_D/raw.log" "$cl" >(mdwriter "$__CLAUDE_D/clean.md" "$cl.st") | jqs pane >> "$__CLAUDE_D/clean.log"',
  '  rc="${PIPESTATUS[0]}"',
  '  [ "$(cat "$cl.st" 2>/dev/null)" = 0 ] || { printf "\\n*the live log of this call was cut short; its whole log follows*\\n\\n"; jqs md < "$cl"; } >> "$__CLAUDE_D/clean.md"',
  '  jqs answer < "$cl" > "$cl.md"; cat "$cl.md"; nl_if_open "$cl.md"',
  '  mv -f "$cl.md" "$__CLAUDE_LAST"; rm -f "$cl" "$cl.st"; return "$rc"',
  '}',
  'claude() { __claude_go 2 command claude "$@"; }',
  // dev exec: skip -d <dir> (or -d=<dir>, -d<dir>); a claude word next is the run, anything else runs untouched
  'dev() {',
  '  local i=2',
  '  if [ "$1" = exec ]; then while [ "$i" -le "$#" ]; do case "${!i}" in -d) i=$((i+2));; -d?*) i=$((i+1));; *) break;; esac; done; fi',
  '  if [ "$1" = exec ] && [ "$i" -le "$#" ] && case "${!i}" in claude|*/claude) true;; *) false;; esac; then',
  '    __claude_go $((i+2)) command dev "$@"',
  '  else command dev "$@"; fi',
  '}',
]
