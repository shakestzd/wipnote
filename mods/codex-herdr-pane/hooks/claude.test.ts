// Headless claude (claude -p / --print), bare or behind dev exec. Behavioural runs against a stub
// claude and dev are in test/pane_sim.sh; these pin the recogniser, the label and the text.
import { expect, test } from 'claude-code/testing'
import { CLAUDE_SHIM } from './shim.ts'
import { isStandalone, toolFor } from './tools.ts'
import { MARKER, buildWrapper, isDelegation, paneLabel, type WrapOptions } from './wrap.ts'

const defaults: WrapOptions = {
  direction: 'right', layout: 'tile', maxPanes: 6, closePane: 'always', closeDelay: 3, tailLines: 50, viewer: 'plain', leanProfile: true, leanProfileName: 'lean', root: '/mod', id: 't1',
}

test('recognises headless claude, bare, behind dev exec, with -d and chained', () => {
  const yes = ['claude -p "say ok" --allowedTools Read', 'claude --print "x"', 'cd r && claude --model opus -p "x"',
    'dev exec claude -p "x"', 'dev exec -d svc/api claude -p "/code-review high a.ts" --allowedTools "Read,Grep,Glob"',
    'dev exec -d=app /usr/local/bin/claude -p "x"', 'claude -p "x" | tail -3', 'claude -p "list sizes" | sort -h', 'claude \\\n  -p "x"']
  for (const c of yes) expect(toolFor(c)?.name, c).toBe('claude')
})

test('ignores interactive claude, info flags, subcommands, quoted text, its own format and other lines', () => {
  const no = ['claude', 'claude "fix it"', 'claude --version', 'claude -p --version', 'claude -p "x" -h', 'claude --help',
    'claude mcp list -p', 'claude plugin install -p x', 'dev exec -d svc claude', 'echo "claude -p x"', "echo 'a; claude -p b'",
    'cat <<EOF\nclaude -p hidden\nEOF', 'wipnote claude -p x', 'claude-p x', `${MARKER}\nclaude -p "already wrapped"`,
    // the shim is a shell function, which a path would bypass
    '/usr/local/bin/claude -p "x"', '~/.local/bin/claude -p "x"',
    // its stdout (json, text) is the caller's: run as asked
    'claude -p "x" --output-format json', 'claude -p --output-format=text "x"', 'claude \\\n  -p "x" --output-format json',
    // a -p on a later line belongs to another command
    'cd x && claude\nmkdir -p out', 'claude --resume abc\nls -p']
  for (const c of no) expect(isDelegation(c), c).toBe(false)
  for (const c of ['codex exec "x"', 'ccx task "x"']) expect(toolFor(c)?.name, c).not.toBe('claude')
  expect(isDelegation('codex exec "x" | sort -h'), 'codex keeps its rule: an info flag anywhere').toBe(false)
})

test('a claude run is standalone bare or behind dev exec, chained otherwise', () => {
  for (const c of ['claude -p "x"', 'dev exec -d d claude -p "x"', 'claude -p "x" > out.md']) expect(isStandalone(c), c).toBe(true)
  for (const c of ['claude -p "x" | tail -1', 'a; dev exec claude -p "x"']) expect(isStandalone(c), c).toBe(false)
})

test('the claude pane title is Claude review for /code-review, else Claude task', () => {
  expect(paneLabel('dev exec -d d claude -p "/code-review high a.ts" --allowedTools "Read"')).toBe('Claude review: /code-review high a.ts')
  expect(paneLabel('claude -p "say ok" --allowedTools Read')).toBe('Claude task: say ok')
})

test('a claude call is wrapped with its shim and stream.jq, and returns its answer like codex', async ($, on) => {
  let seen = { command: '', timeout: undefined as number | undefined }
  on('tool.call', { tool: 'Bash' }, (_$: unknown, e: { command: string; timeout?: number }) => {
    seen = { command: e.command, timeout: e.timeout }
    return { result: { stdout: '', stderr: '', interrupted: false } }
  })
  await $.tool.call({ tool: 'Bash', command: 'claude -p "say ok"' })
  expect(seen.command).toContain(MARKER)
  expect(seen.timeout).toBe(600000)
  const text = buildWrapper('claude -p "say ok"', defaults)
  expect(text).toContain('claude() { __claude_go 2 command claude "$@"; }')
  expect(text).toContain('jqs pane > "$D/clean.log"')
  expect(text).toContain('echo "claude exited $__X')
  expect(text).toContain('then cap_file "$__D/last.md"; else')
  expect(text).not.toContain('--output-last-message') // no codex shim
})

test('stdout the caller owns (chained, or redirected) takes the answer-on-stdout route', () => {
  for (const c of ['claude -p "x" | tail -1', 'claude -p "x" > out.md']) {
    expect(buildWrapper(c, defaults), c).toContain('__CLAUDE_CHAIN=1 __CODEX_LAST="$D/last.md"')
  }
  expect(buildWrapper('claude -p "x" > out.md', defaults)).toContain('echo "claude exited $__X') // still standalone in the report
  expect(buildWrapper('claude -p "x"', defaults)).not.toContain('__CLAUDE_CHAIN=1')
})

test('the claude shim adds stream-json and --verbose only when absent, and each call keeps its own stream', () => {
  const cs = CLAUDE_SHIM.join('\n')
  expect(cs).toContain('if [ -n "$p" ] && [ -z "$fmt" ]; then __PRE=(--output-format stream-json); [ -z "$v" ] && __PRE+=(--verbose); fi')
  expect(cs).toContain('--output-format) fmt=set; want=1;; --output-format=*) fmt="${a#*=}";;')
  expect(cs).toContain('cl=$(mktemp "$__CLAUDE_D/call.XXXXXX")')
  expect(cs).toContain('jqs answer < "$cl" > "$cl.md"; cat "$cl.md"') // the answer on stdout
  expect(cs).toContain('mv -f "$cl.md" "$__CLAUDE_LAST"')
  expect(cs).toContain('-d) i=$((i+2));; -d?*) i=$((i+1));;') // dev exec -d <dir>
})

// The pane script and the shim (sourced in a bash of its own) each define it, from the one source
test('one stream.jq helper serves ccx and claude', () => {
  const shared = 'jqs() { local nf=; case "$1" in view|pane) ;; *) nf=-n;; esac;'
  for (const c of ['ccx task "x"', 'claude -p "x"']) {
    const text = buildWrapper(c, defaults)
    expect(text.match(/jqs\(\) \{/g)?.length, c).toBe(text.split(shared).length - 1)
    expect(text, c).toContain(shared)
  }
  expect(buildWrapper('codex exec "x"', defaults)).not.toContain('jqs()')
})

test('the Markdown writer keeps its subshell (so the pipeline waits), records its status and drains stdin', () => {
  const cs = CLAUDE_SHIM.join('\n')
  const text = buildWrapper('claude -p "x"', defaults)
  for (const t of [cs, text]) expect(t).toContain('mdwriter() { jqs md >> "$1"; echo "$?" > "$2"; cat > /dev/null; }')
  expect(cs).toContain('>(mdwriter "$__CLAUDE_D/clean.md" "$cl.st")')
  expect(text).toContain('>(mdwriter "$D/clean.md" "$D/md.st")')
  expect(cs + text).not.toContain('await_md') // no polling: the pipeline itself waits
})

test('a failed writer is repaired: standalone re-renders raw.log, chained adds the call after a note', () => {
  const cs = CLAUDE_SHIM.join('\n')
  expect(cs).toContain('[ "$(cat "$cl.st" 2>/dev/null)" = 0 ] || { printf')
  expect(cs).toContain('the live log of this call was cut short')
  expect(cs.indexOf('jqs md < "$cl"')).toBeLessThan(cs.indexOf('jqs answer < "$cl"'))
  const text = buildWrapper('claude -p "x"', defaults)
  expect(text).toContain('[ "$(cat "$D/md.st" 2>/dev/null)" = 0 ] || { jqs md < "$D/raw.log" > "$D/clean.md.tmp"')
})

test('the end reads raw.log once; if that pass fails the answer is read alone and the log says so', () => {
  const text = buildWrapper('claude -p "x"', defaults)
  expect(text).toContain('if jqs end < "$D/raw.log" > "$D/end.out"; then')
  expect(text).toContain('else\n    : > "$D/usage"; [ -s "$D/last.md" ] || jqs answer < "$D/raw.log" > "$D/last.md"')
  expect(text).toContain('the usage could not be read from raw.log')
})
