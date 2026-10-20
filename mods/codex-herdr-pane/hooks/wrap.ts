// Pure helpers for the codex-herdr-pane mod: build the shell text that runs a
// delegation (see tools.ts) in a herdr pane.

import { MARKER, TOOLS, isStandalone, redirectsStdout, toolFor, type DelegationTool } from './tools.ts'

export { MARKER, isCodexDelegation, isDelegation, paneLabel } from './tools.ts'
import { paneLabel } from './tools.ts'
import { SHIM } from './shim.ts'
import { NL_FN } from './mdpipe.ts'
import { paneScript } from './panescript.ts'
import { splitSection, unregisterCmd } from './layout.ts'

export type ClosePolicy = 'always' | 'success' | 'never'

export type WrapOptions = {
  direction: 'right' | 'down' // the fixed split used when layout is 'fixed'
  layout: 'tile' | 'fixed' // tile: a grid beside Claude (layout.ts); fixed: always split Claude's pane in `direction`
  maxPanes: number // tile only: live delegation panes allowed before a run goes without a pane
  closePane: ClosePolicy
  closeDelay: number
  tailLines: number
  maxReturnLines?: number // the most lines of each returned section (answer, log tail, rest of command); 0 or unset: no cap
  viewer: 'textual' | 'plain'
  root: string // the plugin's folder: holds viewer/codex_view.py, clean.awk and codex.jq
  leanProfile: boolean // add -p <leanProfileName> to codex exec when that profile file exists
  leanProfileName: string
  id: string
  runsDir?: string // ccx's run-log directory; the pane's CCX_RUNS_DIR overrides it
  // Outside herdr, run the delegation headless into runDir (runDirPath) instead of running it
  // bare, so the Claude Code pane (runpane.tsx) can follow it. runDir is where the hook looks.
  claudePane?: boolean
  runDir?: string
}

// The hook writes the whole wrapper to <TMPDIR>/<SCRIPT_PREFIX><id>.sh itself and the Bash call
// becomes this stub, so the harness's notice of a background call quotes two lines, not the script.
export const SCRIPT_PREFIX = 'codex-herdr-run.'
export const scriptPath = (tmpdir: string | undefined, id: string): string => {
  const dir = tmpdir && tmpdir.startsWith('/') ? tmpdir.replace(/\/+$/, '') : '/tmp'
  return `${dir || ''}/${SCRIPT_PREFIX}${id.replace(/[^A-Za-z0-9]/g, '')}.sh`
}
// The run directory of a run the Claude Code pane follows: named by the id, so the hook knows it
// before the script runs. The script makes it with mkdir -m 700, which fails rather than reuse a
// path that is already there (then the run gets a mktemp directory and no Claude Code pane).
export const runDirPath = (tmpdir: string | undefined, id: string): string =>
  scriptPath(tmpdir, id).replace(/\/[^/]*$/, `/codex-herdr.${id.replace(/[^A-Za-z0-9]/g, '')}`)
const shq = (s: string) => `'${s.replace(/'/g, `'\\''`)}'`
export const stubFor = (path: string): string => `${MARKER}\nexec bash ${shq(path)}`

const word =(s: string) => s.replace(/[^A-Za-z0-9 _.:\/=+-]/g, '')

function heredoc(path: string, delim: string, body: readonly string[]): string[] {
  return [`cat > "${path}" <<'${delim}'`, ...body, delim]
}

// What the Bash call returns to Claude. Codex: the status, then the answer when
// there is one, else the tail of the cleaned log. ccx: what an unwrapped run
// returns, its own stdout (status line and result) and its stderr.
// A chained call (isStandalone in tools.ts) adds, for codex, a header and the whole
// original command's captured stdout, then its stderr; for ccx that capture already
// is the status line and result, so it is returned as is.
//
// Every section but the status line goes through cap_file: the first __M lines, then one
// "... N more lines in <file>" line naming the file that holds them. __M=0 returns it whole,
// and a section of at most __M lines is returned byte for byte as `cat` and nl_if_open did.
const CAP_FN = 'cap_file() { local n; n=$(awk \'END{print NR}\' "$1"); if [ "$__M" -gt 0 ] && [ "$n" -gt "$__M" ]; then head -n "$__M" "$1"; echo "... $((n - __M)) more lines in ${2:-$1}"; else cat "$1"; nl_if_open "$1"; fi; }'
// cap_tail is cap_file for a log's end, where the error is: the LAST __M lines, led by a line naming how many earlier ones $2 (the whole log) holds.
const CAP_TAIL_FN = 'cap_tail() { local n t; n=$(awk \'END{print NR}\' "$1"); if [ "$__M" -gt 0 ] && [ "$n" -gt "$__M" ]; then t=$(awk \'END{print NR}\' "${2:-$1}"); echo "... $((t - __M)) earlier lines in ${2:-$1}"; tail -n "$__M" "$1"; else cat "$1"; fi; nl_if_open "$1"; }'

function report(o: WrapOptions, tool: DelegationTool, chained: boolean): string[] {
  const cap = Math.max(0, Math.floor(o.maxReturnLines ?? 0)) || 0
  const head = [NL_FN, CAP_FN, CAP_TAIL_FN, `__M=${cap}`]
  if (tool.filter === 'ccx') {
    return [...head, '__X=$(cat "$__D/exit")', 'cap_file "$__D/out.txt"', 'cat "$__D/err.log" >&2; nl_if_open "$__D/err.log" >&2']
  }
  const tail = Math.max(1, Math.floor(o.tailLines))
  const who = chained ? 'command' : 'codex'
  return [
    ...head,
    '__X=$(cat "$__D/exit")',
    '__T=$(( $(cat "$__D/end" 2>/dev/null || date +%s) - $(cat "$__D/start" 2>/dev/null || date +%s) ))',
    '__K=$(cat "$__D/usage" 2>/dev/null)',
    `echo "${who} exited $__X after \${__T}s\${__K:+, $__K}; raw log $__D/raw.log, answer file $__D/last.md"`,
    // on failure the end of clean.log: its last __M lines, led by a pointer to the earlier ones in clean.log
    'if [ "$__X" = 0 ] && [ -s "$__D/last.md" ]; then cap_file "$__D/last.md"; else tail -n ' + tail + ' "$__D/clean.log" > "$__D/tail.out"; cap_tail "$__D/tail.out" "$__D/clean.log"; fi',
    ...(chained ? [
      // rest.out / rest.err: orig.out / orig.err with the pane's noise rules applied (panescript.ts)
      'if [ -s "$__D/rest.out" ] || [ -s "$__D/rest.err" ]; then echo "--- output of the rest of the command ---"; cap_file "$__D/rest.out"; cap_file "$__D/rest.err" >&2; fi',
    ] : []),
  ]
}

function closeSection(o: WrapOptions): string[] {
  const delay = Math.max(0, Math.floor(o.closeDelay))
  const when = o.closePane === 'always' ? 'true' : o.closePane === 'success' ? '[ "$__X" = 0 ]' : 'false'
  return [
    `if ${when}; then ( sleep ${delay}; herdr pane close "$__P" >/dev/null 2>&1; ${unregisterCmd(o)} ) >/dev/null 2>&1 & fi`,
    'exit "$__X"',
  ]
}

// Old leftovers in TMPDIR, removed in the background at each run after CODEX_HERDR_KEEP_DAYS days
// (7): a script the hook wrote for a call that never ran (a denied call), run directories and
// ccx claims. Days, not hours, for the scripts too: another session's call can wait a long time
// at its approval prompt, and its script must still be there when it is approved.
const SWEEP = '( T="${TMPDIR:-/tmp}"; K="${CODEX_HERDR_KEEP_DAYS:-7}"; '
  + 'find "$T" -maxdepth 1 -type f -name \'' + SCRIPT_PREFIX + '*.sh\' -mtime +"$K" -delete; '
  + 'find "$T" -maxdepth 1 -type d -name \'codex-herdr.*\' -mtime +"$K" -exec rm -rf {} +; '
  + 'find "$T/codex-herdr-ccx-claims-$(id -u)" -mindepth 1 -maxdepth 1 -type d -name \'*.jsonl\' -mtime +"$K" -exec rm -rf {} +; '
  + ') >/dev/null 2>&1 &'

export function buildWrapper(command: string, o: WrapOptions): string {
  const tool = toolFor(command) ?? TOOLS[0]!
  const chained = !isStandalone(command)
  const orig = `__CODEX_HERDR_ORIG_${o.id}__`
  const shim = `__CODEX_HERDR_SHIM_${o.id}__`
  const pane = `__CODEX_HERDR_PANE_${o.id}__`
  const claude = Boolean(o.claudePane && o.runDir)
  const mktemp = '__D=$(mktemp -d "${TMPDIR:-/tmp}/codex-herdr.XXXXXX") || exit 1'
  return [
    MARKER,
    claude ? `__D=${shq(o.runDir!)}; mkdir -m 700 "$__D" 2>/dev/null || { ${mktemp}; }` : mktemp,
    // A script the hook wrote (see stubFor) moves into the run directory, so it is kept with the logs
    'case "$0" in */' + SCRIPT_PREFIX + '*.sh) mv "$0" "$__D/wrapper.sh" 2>/dev/null;; esac',
    SWEEP,
    ...heredoc('$__D/orig.sh', orig, [command]),
    ...(claude ? [
      ...(tool.shim ? heredoc('$__D/shim.sh', shim, SHIM) : []),
      ...heredoc('$__D/pane.sh', pane, paneScript(o, tool, chained, !chained && !redirectsStdout(command))),
      `printf '%s' ${JSON.stringify(word(paneLabel(command)))} > "$__D/label"`,
      // Outside herdr, or without the binary: the same run with no viewer, which the Claude Code
      // pane follows (host tells it which runs are its own), then the same report.
      'if [ "${HERDR_ENV:-}" != 1 ] || ! command -v herdr >/dev/null 2>&1; then',
      '  echo claude > "$__D/host"',
      `  bash "$__D/pane.sh" "$__D" ${shq(o.root)} none ${chained ? 1 : 0} </dev/null >/dev/null 2>&1`,
      ...report(o, tool, chained).map((l) => '  ' + l),
      '  exit "$__X"',
      'fi',
      'echo herdr > "$__D/host"',
    ] : [
      // Outside herdr, or without the binary, run the command exactly as asked.
      'if [ "${HERDR_ENV:-}" != 1 ] || ! command -v herdr >/dev/null 2>&1; then exec bash "$__D/orig.sh"; fi',
      ...(tool.shim ? heredoc('$__D/shim.sh', shim, SHIM) : []),
      ...heredoc('$__D/pane.sh', pane, paneScript(o, tool, chained, !chained && !redirectsStdout(command))),
      `printf '%s' ${JSON.stringify(word(paneLabel(command)))} > "$__D/label"`,
    ]),
    ...splitSection(o),
    'if [ -z "$__P" ]; then exec bash "$__D/orig.sh"; fi',
    `herdr pane run "$__P" bash "$__D/pane.sh" "$__D" ${shq(o.root)} ${o.viewer} ${chained ? 1 : 0} >/dev/null`,
    // After the run, so the shell's own title for the typed command does not replace it
    'herdr pane rename "$__P" "$(cat "$__D/label")" >/dev/null 2>&1 || true',
    'while [ ! -f "$__D/exit" ]; do',
    `  herdr pane get "$__P" >/dev/null 2>&1 || { ${unregisterCmd(o)}; echo "the herdr pane $__P was closed before ${tool.name} finished (log: $__D/raw.log)"; exit 130; }`,
    '  sleep 1',
    'done',
    ...report(o, tool, chained),
    ...closeSection(o),
  ].join('\n')
}
