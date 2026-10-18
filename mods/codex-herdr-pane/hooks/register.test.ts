import { expect, test } from 'claude-code/testing'
import { SHIM } from './shim.ts'
import { isStandalone } from './tools.ts'
import { TEXTUAL_REQ } from './panescript.ts'
import { MARKER, buildWrapper, isCodexDelegation, paneLabel, scriptPath, stubFor, type WrapOptions } from './wrap.ts'

const answer = { result: { stdout: '', stderr: '', interrupted: false } }

const defaults: WrapOptions = {
  direction: 'right', layout: 'tile', maxPanes: 6, closePane: 'always', closeDelay: 3, tailLines: 50, viewer: 'plain', leanProfile: true, leanProfileName: 'lean', root: '/mod', id: 't1',
}

// Runs one Bash call through the mod and returns the command and timeout the
// rest of the chain saw.
async function seenBy($: any, on: any, call: Record<string, unknown>) {
  const seen: { command: string; timeout?: number } = { command: '' }
  on('tool.call', { tool: 'Bash' }, (_$: unknown, e: { command: string; timeout?: number }) => {
    seen.command = e.command
    seen.timeout = e.timeout
    return answer
  })
  await $.tool.call({ tool: 'Bash', ...call })
  return seen
}

test('a codex exec call is rewritten to run in a herdr pane', async ($, on) => {
  const original = 'codex exec -s workspace-write "implement feat-1"'
  const seen = await seenBy($, on, { command: original })
  expect(seen.command).toContain(MARKER)
  expect(seen.command).toContain('herdr pane split $__S --direction $__DIR') // tiled by default
  expect(seen.command).toContain('herdr pane run "$__P" bash')
  expect(seen.command).toContain(original)
  expect(seen.timeout).toBe(600000)
})

test('a call that is not a Codex delegation passes unchanged', async ($, on) => {
  const command = 'git log --grep="codex exec" --oneline'
  expect((await seenBy($, on, { command })).command).toBe(command)
})

test('codex exec --help is not a delegation', async ($, on) => {
  const command = 'codex exec --help | head -5'
  expect((await seenBy($, on, { command })).command).toBe(command)
})

test('a background call keeps its own timeout', async ($, on) => {
  const seen = await seenBy($, on, { command: 'codex review --uncommitted', run_in_background: true })
  expect(seen.timeout).toBeUndefined()
})

test('the options decide the split and when the pane closes', { options: { layout: 'down', closePane: 'never' } }, async ($, on) => {
  const seen = await seenBy($, on, { command: 'codex exec "x"' })
  expect(seen.command).toContain('herdr pane split --current --direction down')
  expect(seen.command).not.toContain('__lock')
  expect(seen.command).toContain('if false; then')
})

test('layout right is the old single split with no registry', { options: { layout: 'right' } }, async ($, on) => {
  const seen = await seenBy($, on, { command: 'codex exec "x"' })
  expect(seen.command).toContain('herdr pane split --current --direction right --cwd "$PWD" --no-focus')
  expect(seen.command).not.toContain('codex-herdr-panes')
})

test('the legacy direction option still works when no layout is configured', { options: { direction: 'down' } }, async ($, on) => {
  const seen = await seenBy($, on, { command: 'codex exec "x"' })
  expect(seen.command).toContain('herdr pane split --current --direction down')
})

test('maxPanes comes from the config', { options: { layout: 'tile', maxPanes: 3 } }, async ($, on) => {
  const seen = await seenBy($, on, { command: 'codex exec "x"' })
  expect(seen.command).toContain('[ "$__n" -ge 3 ]')
})

// The tile policy (layout.ts), pinned at text level; test/pane_sim.sh runs it against the stub herdr.
const tile = buildWrapper('codex exec "x"', defaults)
const arm = (n: number) => tile.split('\n').find((l) => l.startsWith(`      ${n}) `)) ?? ''

test('tile policy by live pane count: right column, then 2x2, then down', () => {
  expect(arm(0)).toContain('__S="--current"; __DIR=right')
  expect(arm(1)).toContain('__S=$(__nth 1); __DIR=down')
  expect(arm(2)).toContain('__S=$(__nth 1); __DIR=right')
  expect(arm(3)).toContain('__S=$(__nth 2); __DIR=right')
  expect(tile).toContain('*) __S=$(__nth "$__n"); __DIR=down;;')
})

test('tile cap: at maxPanes the run goes without a pane and the output says so', () => {
  expect(tile).toContain('if [ "$__n" -ge 6 ]; then __NOTE="herdr pane limit reached')
  expect(tile).toContain('[ -n "$__NOTE" ] && echo "$__NOTE"')
  expect(tile).toContain('if [ -z "$__P" ]; then exec bash "$__D/orig.sh"; fi') // the plain run
})

test('the registry is guarded by an atomic mkdir lock and pruned with herdr pane get', () => {
  expect(tile).toContain('__REG="${TMPDIR:-/tmp}/codex-herdr-panes.${HERDR_PANE_ID:-cur}"')
  expect(tile).toContain('until mkdir "$__REG.lock"')
  expect(tile).toContain('kill -0 "$p"') // a dead owner's lock is broken
  expect(tile).toContain('herdr pane get "$q" </dev/null')
  // the lock spans prune, decide, split and append, then is released
  const at = (s: string) => tile.indexOf(s)
  expect(at('if __lock; then')).toBeLessThan(at('__prune; __n='))
  expect(at('__prune; __n=')).toBeLessThan(at('herdr pane split $__S'))
  expect(at('herdr pane split $__S')).toBeLessThan(at('echo "$__P" >> "$__REG"'))
  expect(at('echo "$__P" >> "$__REG"')).toBeLessThan(at('  __unlock\nfi'))
})

test('a pane leaves the registry when it closes or is found closed', () => {
  expect(tile).toContain('herdr pane close "$__P" >/dev/null 2>&1; __unreg "$__P" )')
  expect(tile).toContain('{ __unreg "$__P"; echo "the herdr pane $__P was closed before')
})

test('the pane closes by default after a delay', async ($, on) => {
  const seen = await seenBy($, on, { command: 'codex exec "x"' })
  expect(seen.command).toContain('if true; then ( sleep 3; herdr pane close "$__P"')
})

test('recognises delegations and ignores look-alikes', () => {
  const yes = [
    'codex exec "do it"',
    'cd repo && codex exec -s workspace-write "do it"',
    'FOO=1 codex e "do it"',
    'codex review --base main',
    'x=$(codex exec "q")',
    'a; codex exec "q"',
  ]
  const no = [
    'codex "interactive prompt"',
    'codex --help',
    'codex exec --help',
    'codex review -h',
    'codex --version',
    'wipnote codex --resume abc',
    'echo "run codex exec later"',
    "echo 'a; codex exec b'",
    'git log --grep="codex exec"',
    'cat <<EOF\ncodex exec hidden\nEOF',
    'ls codex-exec-notes.md',
    `${MARKER}\ncodex exec "already wrapped"`,
  ]
  for (const c of yes) expect(isCodexDelegation(c), c).toBe(true)
  for (const c of no) expect(isCodexDelegation(c), c).toBe(false)
})

test('the pane title comes from the prompt', () => {
  expect(paneLabel('codex exec -s read-only "Implement the sharded CI gate for feat-53536cda please"'))
    .toBe('Codex task: Implement the sharded CI gate for feat-53536cda')
  expect(paneLabel('codex review --base main')).toBe('Codex review: changes against main')
  expect(paneLabel('codex review --commit 0123456789abcdef')).toBe('Codex review: commit 0123456789ab')
  expect(paneLabel('codex review')).toBe('Codex review: current changes')
  expect(paneLabel('codex review "check the locking"')).toBe('Codex review: check the locking')
  expect(paneLabel('codex exec "$(cat prompt.md)"')).toBe('Codex task: $(cat prompt.md)')
})

test('the wrapper keeps the original whole, shims the answer file and falls back outside herdr', () => {
  const original = "codex exec \"it's quoted\" <<'EOF'\nbody\nEOF"
  const text = buildWrapper(original, { ...defaults, closePane: 'success', tailLines: 50 })
  expect(text).toContain(original)
  expect(text).toContain('[ "${HERDR_ENV:-}" != 1 ]')
  expect(text).toContain('exec bash "$__D/orig.sh"')
  expect(text).toContain('--output-last-message "$__CODEX_LAST"')
  expect(text).toContain('tail -n 50')
  expect(text).toContain('if [ "$__X" = 0 ]; then ( sleep 3;')
})

test('recognises ccx task and ignores its other subcommands', () => {
  const yes = [
    'ccx task "x"',
    '~/work/_shared/claude-container/ccx task "x"',
    'ccx task "x" --resume abc',
    'cd repo && /opt/bin/ccx task "x"',
  ]
  const no = [
    'ccx status', 'ccx up', 'ccx down', 'ccx login',
    'echo "ccx task"',
    'cat <<EOF\nccx task hidden\nEOF',
    'ccx task --help',
    'ls ccx-task-notes.md',
    `${MARKER}\nccx task "already wrapped"`,
  ]
  for (const c of yes) expect(isCodexDelegation(c), c).toBe(true)
  for (const c of no) expect(isCodexDelegation(c), c).toBe(false)
})

test('a ccx task call is rewritten to run in a herdr pane', async ($, on) => {
  const original = 'ccx task "summarise the repo"'
  const seen = await seenBy($, on, { command: original })
  expect(seen.command).toContain(MARKER)
  expect(seen.command).toContain(original)
  expect(seen.command).toContain('herdr pane split $__S --direction $__DIR')
  expect(seen.timeout).toBe(600000)
})

test('the ccx pane title is ccx task plus the prompt', () => {
  expect(paneLabel('ccx task "Review the sharded CI gate for feat-53536cda and report every flaky test"'))
    .toBe('ccx task: Review the sharded CI gate for feat-53536cda and')
  expect(paneLabel('~/work/_shared/claude-container/ccx task "x" --resume abc')).toBe('ccx task: x')
  expect(paneLabel('ccx task --resume abc')).toBe('ccx task: running')
})

test('the ccx wrapper has no answer shim, returns its own stdout and follows the run log', () => {
  const text = buildWrapper('ccx task "hello"', defaults)
  expect(text).not.toContain('--output-last-message')
  expect(text).not.toContain('shim.sh')
  expect(text).toContain('viewer/stream.jq')
  expect(text).toContain('cap_file "$__D/out.txt"')
  expect(text).not.toContain('codex exited')
  expect(text).toContain('if true; then ( sleep 3; herdr pane close')
})

// Hooks modules cannot spawn processes, so these pin the shim's text; the sim2
// harness runs the same text in bash against a stub codex (two-sided, with and
// without the profile file).
const shim = SHIM.join('\n')

test('the shim adds -p <profile> and --json to exec, only when the profile file exists', () => {
  expect(shim).toContain('[ -f "$HOME/.codex/${__CODEX_PROFILE}.config.toml" ]')
  expect(shim).toContain('pre+=(-p "$__CODEX_PROFILE")')
  expect(shim).toContain('[ -z "$has_j" ] && pre+=(--json)')
  expect(shim).toContain('[ "$1" = exec ] || [ "$1" = e ]') // review never reaches the injection
})

test('the shim treats -p, --profile and --profile= as the call setting a profile', () => {
  expect(shim).toContain('-p|--profile|--profile=*) has_p=1;;')
  expect(shim).toContain('[ -z "$has_p" ] && [ -n "${__CODEX_PROFILE:-}" ]')
})

test('the shim does not repeat --json or the answer file the call names', () => {
  expect(shim).toContain('--json) has_j=1;;')
  expect(shim).toContain('-o|--output-last-message|--output-last-message=*) has_o=1;;')
  expect(shim).toContain('[ -z "$has_o" ] && pre+=(--output-last-message "$__CODEX_LAST")')
})

test('the profile reaches the shim through the options', () => {
  const on = buildWrapper('codex exec "x"', defaults)
  expect(on).toContain('__CODEX_PROFILE="lean"')
  expect(buildWrapper('codex exec "x"', { ...defaults, leanProfile: false })).toContain('__CODEX_PROFILE=""')
  expect(buildWrapper('codex exec "x"', { ...defaults, leanProfileName: 'my prof;rm' })).toContain('__CODEX_PROFILE="myprofrm"')
  expect(on).toContain('[ -f "$HOME/.codex/${__CODEX_PROFILE}.config.toml" ]')
})

test('the options come from the config and default to the lean profile', { options: { leanProfile: false } }, async ($, on) => {
  const seen = await seenBy($, on, { command: 'codex exec "x"' })
  expect(seen.command).toContain('__CODEX_PROFILE=""')
})

test('the codex wrapper filters JSONL, writes a usage file and reports the split', () => {
  const text = buildWrapper('codex exec "x"', defaults)
  expect(text).toContain('viewer/codex.jq')
  expect(text).toContain('--arg mode usage')
  expect(text).toContain('> "$D/usage"')
  expect(text).toContain('echo "$k tokens" > "$D/usage"')
  expect(text).toContain('__K=$(cat "$__D/usage"')
})

// Chained calls: behavioural runs are in the sim2 harness (run4.sh); these pin the rule and the text.
test('a bare single delegation is standalone, anything chained or piped is not', () => {
  const standalone = ['codex exec "x"', 'codex exec "a; b | c" 2>&1', 'FOO=1 codex e "x" > out.txt', 'ccx task "x" --resume abc', 'codex exec - <<EOF\nbody; x | y\nEOF']
  const chained = [
    'codex exec "x" 2>&1 | tail -6', 'codex exec "x"; echo done', 'a && codex exec "x"', 'codex exec "x" || true',
    'codex exec "x"\necho after', 'echo $(codex exec "x")', 'codex exec "x" &', 'ccx task "x" | head -1',
  ]
  for (const c of standalone) expect(isStandalone(c), c).toBe(true)
  for (const c of chained) expect(isStandalone(c), c).toBe(false)
  expect(isStandalone('echo hi'), 'not a delegation').toBe(false)
})

test('a standalone call keeps the old wrapper: no capture, no header, codex exited', () => {
  const text = buildWrapper('codex exec "x"', defaults)
  expect(text).toContain(`"$__D" '/mod' plain 0 >/dev/null`)
  expect(text).not.toContain('output of the rest of the command')
  expect(text).toContain('echo "codex exited $__X')
  expect(text).not.toContain('__CODEX_CHAIN=1 ') // the shim stays in its plain mode
})

test('a chained codex call captures the whole command and returns it after the report', () => {
  const original = 'codex exec "x" 2>&1 | tail -3; echo AFTER'
  const text = buildWrapper(original, defaults)
  expect(text).toContain(original) // the original still runs whole, unchanged
  expect(text).toContain('plain 1 >/dev/null')
  expect(text).toContain('tee "$D/orig.out"')
  expect(text).toContain('tee "$D/orig.err"')
  expect(text).toContain('echo "command exited $__X')
  expect(text).toContain('echo "--- output of the rest of the command ---"; cap_file "$__D/rest.out"; cap_file "$__D/rest.err" >&2')
  expect(text.indexOf('cap_file "$__D/last.md"')).toBeLessThan(text.indexOf('output of the rest'))
  expect(text).toContain('[ -s "$__D/rest.out" ] || [ -s "$__D/rest.err" ]') // the header only when there is output
  expect(text).toContain('exit "$__X"') // the status is the original command's, read from the exit file
  expect(text).toContain('run_orig bash -c')
  expect(text).toContain('__CODEX_CHAIN=1')
})

test('the shim keeps JSONL off the caller in a chained call and prints the answer instead', () => {
  expect(shim).toContain('[ -n "${__CODEX_CHAIN:-}" ] && [ -z "$has_o" ] && [ -z "$has_j" ]')
  expect(shim).toContain('tee -a "$__CODEX_D/raw.log"')
  expect(shim).toContain('rc="${PIPESTATUS[0]}"; cat "$__CODEX_LAST" 2>/dev/null; nl_if_open "$__CODEX_LAST"')
})

test('a chained ccx call returns its capture, which holds its own status line and result', () => {
  const text = buildWrapper('echo a; ccx task "x"; echo b', defaults)
  expect(text).toContain('if [ "$CHAIN" = 1 ]; then run_orig bash "$D/orig.sh"')
  expect(text).toContain('cp "$D/rest.out" "$D/out.txt"')
  expect(text).toContain('cap_file "$__D/out.txt"')
  expect(text).not.toContain('output of the rest of the command')
})

test('the Textual range is defined once and the pane script uses it', () => {
  expect(TEXTUAL_REQ).toBe('textual>=8.2,<9')
  const text = buildWrapper('codex exec "x"', { ...defaults, viewer: 'textual' })
  expect(text).toContain(`--with "${TEXTUAL_REQ}" python`)
  expect(text).not.toContain('--with textual python')
})

test('a codex pane writes clean.md beside clean.log from the same raw stream', () => {
  const text = buildWrapper('codex exec "x"', defaults)
  expect(text).toContain('mdcodex() { awk -v seen="${MCP_SEEN_MD:-}" -v review=1 -f "$ROOT/viewer/clean.awk" | jq --unbuffered -R -r --arg mode md -f "$ROOT/viewer/codex.jq"')
  expect(text).toContain('tee "$D/raw.log" >(mdcodex >> "$D/clean.md") | jq') // a second consumer of the one tee
  expect(text).toContain('mdcodex < "$D/raw.log" > "$D/clean.md.tmp" && mv "$D/clean.md.tmp" "$D/clean.md"') // rebuilt whole at the end
  expect(text).toContain('| awk -v review=1 -f "$ROOT/viewer/clean.awk" > "$D/clean.log"') // the text log is unchanged
  expect(text).toContain(': > "$D/clean.md"') // exists from the start, so the viewer does not fall back
})

test('a chained call feeds clean.md from the shim and from the rest of the command', () => {
  const text = buildWrapper('echo a; codex exec "x"', defaults)
  expect(text).toContain('tee -a "$__CODEX_D/raw.log" >(mdcodex >> "$__CODEX_D/clean.md")')
  expect(text).toContain('mdcodex() { awk -v seen="${MCP_SEEN_MD:-}" -v review=1 -f "$__CODEX_ROOT/viewer/clean.awk"')
  expect(text).toContain('tee "$D/orig.err" >(mdrest >> "$D/clean.md") | restfilter "${MCP_SEEN_LOG:-}" >> "$D/clean.log"')
  expect(text).toContain('tee "$D/orig.out" >(mdrest >> "$D/clean.md") | restfilter "${MCP_SEEN_LOG:-}" >> "$D/clean.log"')
})

test('a ccx pane renders the followed log as text and as paired Markdown', () => {
  const text = buildWrapper('ccx task "x"', defaults)
  expect(text).toContain('jqs view < "$D/chunk" >> "$D/clean.log"')
  expect(text).toContain('sed -n "1,$2p" "$f" | jqs "$3" > "$D/md.tmp"') // whole prefix, so calls pair with results in earlier lines
  expect(text).toContain('tail -c +"$((W+1))" "$D/md.tmp" >> "$D/clean.md"') // append-only: only the new bytes
  expect(text).toContain('render "$((n+1))" "$t" mdlive')
  expect(text).toContain("render \"$((n+1))\" '$' md") // the final pass flushes unanswered calls
})

test('one noise implementation: every path runs clean.awk, and the returned text uses the same filter as clean.md', () => {
  const text = buildWrapper('echo a; codex exec "x" 2>&1 | head -3', defaults)
  expect(text).toContain('mdrest() { awk -v seen="${MCP_SEEN_MD:-}" -f "$ROOT/viewer/clean.awk" | awk -v last="${__CODEX_LAST:-}" -f "$ROOT/viewer/rest.awk" | jq')
  expect(text).toContain('restfilter() { awk -v seen="$1" -f "$ROOT/viewer/clean.awk" | awk -v last="${__CODEX_LAST:-}" -f "$ROOT/viewer/rest.awk" | jq --unbuffered -R -r --arg mode rest')
  expect(text).toContain('mkrest() { for s in out err; do restfilter "$D/mcp.ret.seen" < "$D/orig.$s" > "$D/rest.$s"; done; }')
  expect(text).toContain('export MCP_SEEN_MD="$D/mcp.md.seen" MCP_SEEN_LOG="$D/mcp.log.seen"') // one MCP note per run directory
  expect(text).not.toContain('cat "$__D/orig.out"') // the raw capture is never what is returned
})

test('every report ends a line the answer left open, and only then', () => {
  for (const cmd of ['codex exec "x"', 'echo a; codex exec "x"', 'ccx task "x"']) {
    const text = buildWrapper(cmd, defaults)
    expect(text, cmd).toContain('nl_if_open() { [ -n "$(tail -c1 "$1" 2>/dev/null)" ] && echo; return 0; }')
  }
  expect(buildWrapper('codex exec "x"', defaults)).toContain('then cap_file "$__D/last.md"; else')
})

// --- 0.7.0: a short stub in the call, and a cap on what is returned ---
// The test environment has no fs, so the fs and env nouns are answered by hooks of the test's own.
function fakeFs($: any, on: any, tmp: string | undefined, fail = false) {
  const files: Record<string, string> = {}
  on('env.get', {}, () => ({ value: tmp }))
  on('fs.write', {}, (_$: unknown, e: { path: string; text: string }) => { if (fail) throw new Error('refused'); files[e.path] = e.text; return { value: undefined } })
  on('fs.exists', {}, (_$: unknown, e: { path: string }) => ({ value: e.path in files }))
  return files
}

test('the call is a stub of two short lines and the script is a file the hook wrote', async ($, on) => {
  const files = fakeFs($, on, '/var/folders/x/T/')
  const original = 'codex exec -s workspace-write "implement feat-1"'
  const seen = await seenBy($, on, { command: original })
  const lines = seen.command.split('\n')
  expect(lines.length).toBeLessThanOrEqual(3)
  expect(lines[0]).toBe(MARKER)
  expect(seen.command.length).toBeLessThan(300)
  expect(seen.command).not.toContain(original) // the original is in the file, not in the stub
  expect(seen.command).not.toContain('herdr pane')
  const paths = Object.keys(files)
  expect(paths.length).toBe(1)
  expect(paths[0]).toMatch(/^\/var\/folders\/x\/T\/codex-herdr-run\.[a-z0-9]+\.sh$/) // one slash, whatever TMPDIR ends in
  expect(seen.command).toBe(`${MARKER}\nexec bash '${paths[0]}'`)
  expect(files[paths[0]!]).toContain(original) // verbatim, in the script
  expect(files[paths[0]!]!.startsWith(MARKER)).toBe(true)
  expect(files[paths[0]!]).toContain('herdr pane run "$__P" bash')
})

test('an already-wrapped stub is not wrapped again', async ($, on) => {
  const files = fakeFs($, on, '/tmp')
  const stub = `${MARKER}\nexec bash '/tmp/codex-herdr-run.a1.sh'`
  expect(isCodexDelegation(stub)).toBe(false)
  expect((await seenBy($, on, { command: stub })).command).toBe(stub)
  expect(Object.keys(files).length).toBe(0)
})

test('without a usable TMPDIR the script goes to /tmp; a refused write keeps the whole script inline', async ($, on) => {
  const files = fakeFs($, on, undefined)
  await seenBy($, on, { command: 'codex exec "x"' })
  expect(Object.keys(files)[0]).toMatch(/^\/tmp\/codex-herdr-run\./)
})

test('a failed write falls back to the inline script, never blocks the delegation', async ($, on) => {
  fakeFs($, on, '/tmp', true)
  const seen = await seenBy($, on, { command: 'codex exec "x"' })
  expect(seen.command.startsWith(MARKER)).toBe(true)
  expect(seen.command).toContain('herdr pane run "$__P" bash')
  expect(seen.command).toContain('codex exec "x"')
})

test('a path with a quote is single-quoted safely in the stub', () => {
  expect(stubFor("/tmp/it's/x.sh")).toBe(`${MARKER}\nexec bash '/tmp/it'\\''s/x.sh'`)
  expect(scriptPath('relative/dir', 'ab-1')).toBe('/tmp/codex-herdr-run.ab1.sh')
})

test('the wrapper moves a hook-written script into the run directory', () => {
  const text = buildWrapper('codex exec "x"', defaults)
  expect(text).toContain('case "$0" in */codex-herdr-run.*.sh) mv "$0" "$__D/wrapper.sh"')
  expect(text.indexOf('mktemp')).toBeLessThan(text.indexOf('mv "$0"'))
})

test('maxReturnLines caps the answer, the log tail and the rest of the command, naming the file', () => {
  const text = buildWrapper('echo a; codex exec "x"', { ...defaults, maxReturnLines: 25 })
  expect(text).toContain('__M=25')
  expect(text).toContain('cap_file "$__D/last.md"')
  expect(text).toContain('cap_tail "$__D/tail.out" "$__D/clean.log"')
  expect(text).toContain('tail -n "$__M" "$1"') // the log tail keeps its last lines
  expect(text).toContain('echo "... $((t - __M)) earlier lines in ${2:-$1}"')
  expect(text).toContain('cap_file "$__D/rest.out"')
  expect(text).toContain('echo "... $((n - __M)) more lines in ${2:-$1}"')
  expect(buildWrapper('codex exec "x"', { ...defaults, maxReturnLines: 0 })).toContain('__M=0')
  expect(buildWrapper('codex exec "x"', defaults)).toContain('__M=0') // unset: no cap
  expect(buildWrapper('ccx task "x"', { ...defaults, maxReturnLines: 7 })).toContain('__M=7')
})

test('maxReturnLines defaults to 60 and 0 turns the cap off', { options: { maxReturnLines: 0 } }, async ($, on) => {
  expect((await seenBy($, on, { command: 'codex exec "x"' })).command).toContain('__M=0')
})

test('maxReturnLines is 60 when the config does not set it', async ($, on) => {
  expect((await seenBy($, on, { command: 'codex exec "x"' })).command).toContain('__M=60')
})
