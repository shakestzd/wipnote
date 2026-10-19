import { NL_FN, mdFunctions } from './mdpipe.ts'

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
