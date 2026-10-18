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
// the log holds the progress once. A chained review keeps stdout for the caller.
// CHAINED calls (see isStandalone in tools.ts) set __CODEX_CHAIN: the command's stdout
// is the caller's, so for exec the shim sends codex's JSONL only to the pane
// (raw.log, then codex.jq and clean.awk into clean.log) and prints the answer file to
// its own stdout, which is what a plain `codex exec` prints. A call that asks for
// --json itself, or names its own -o file, keeps codex's stdout as it is.
// The same stream also feeds clean.md through mdcodex (mdpipe.ts).
// __CODEX_PROFILE is empty when the option is off. A function, so no quoting of the
// user's text.
export const SHIM = [
  ...mdFunctions('$__CODEX_ROOT'),
  NL_FN,
  'codex() {',
  '  local a sub="$1" pre=() has_o="" has_p="" has_j=""',
  '  if [ -n "${__CODEX_LAST:-}" ] && { [ "$1" = exec ] || [ "$1" = e ]; }; then',
  '    shift',
  '    for a in "$@"; do case "$a" in',
  '      -o|--output-last-message|--output-last-message=*) has_o=1;;',
  '      -p|--profile|--profile=*) has_p=1;;',
  '      --json) has_j=1;;',
  '    esac; done',
  '    [ -z "$has_o" ] && pre+=(--output-last-message "$__CODEX_LAST")',
  '    if [ -z "$has_p" ] && [ -n "${__CODEX_PROFILE:-}" ] && [ -f "$HOME/.codex/${__CODEX_PROFILE}.config.toml" ]; then pre+=(-p "$__CODEX_PROFILE"); fi',
  '    [ -z "$has_j" ] && pre+=(--json)',
  '    if [ -n "${__CODEX_CHAIN:-}" ] && [ -z "$has_o" ] && [ -z "$has_j" ]; then',
  '      : > "$__CODEX_LAST"; local rc',
  '      command codex "$sub" "${pre[@]}" "$@" | tee -a "$__CODEX_D/raw.log" >(mdcodex >> "$__CODEX_D/clean.md") | jq --unbuffered -R -r -f "$__CODEX_ROOT/viewer/codex.jq" 2>/dev/null \\',
  '        | awk -v seen="${MCP_SEEN_LOG:-}" -f "$__CODEX_ROOT/viewer/clean.awk" >> "$__CODEX_D/clean.log"',
  '      rc="${PIPESTATUS[0]}"; cat "$__CODEX_LAST" 2>/dev/null; nl_if_open "$__CODEX_LAST"; return "$rc"',
  '    fi',
  '    command codex "$sub" "${pre[@]}" "$@"',
  '  elif [ -n "${__CODEX_LAST:-}" ] && [ -z "${__CODEX_CHAIN:-}" ] && [ "$1" = review ]; then',
  '    command codex "$@" > "$__CODEX_LAST"',
  '  else',
  '    command codex "$@"',
  '  fi',
  '}',
]
