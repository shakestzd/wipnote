// Tests for the fixes on top of 0.7.0: quoting, the caller's timeout, the ccx follower
// binding, the ccxRunsDir setting and layout auto.
import { expect, test } from 'claude-code/testing'
import { buildWrapper, type WrapOptions } from './wrap.ts'

const answer = { result: { stdout: '', stderr: '', interrupted: false } }

const defaults: WrapOptions = {
  direction: 'right', layout: 'tile', maxPanes: 6, closePane: 'always', closeDelay: 3, tailLines: 50, viewer: 'plain', leanProfile: true, leanProfileName: 'lean', root: '/mod', id: 't1',
}

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

test('a foreground call keeps a timeout it set', async ($, on) => {
  expect((await seenBy($, on, { command: 'codex exec "x"', timeout: 30000 })).timeout).toBe(30000)
})

test('a foreground call with no timeout gets the longest wait', async ($, on) => {
  expect((await seenBy($, on, { command: 'codex exec "x"' })).timeout).toBe(600000)
})

test('the plugin root reaches the pane script shell-quoted, so $ and backticks stay literal', () => {
  const root = "/opt/my $HOME `id` it's"
  const text = buildWrapper('codex exec "x"', { ...defaults, root })
  expect(text).toContain(`"$__D" '/opt/my $HOME \`id\` it'\\''s' plain 0 >/dev/null`)
  expect(text).not.toContain('"/opt/my $HOME')
})

test('the pane title is passed to herdr as one argument', () => {
  expect(buildWrapper('codex exec "two words"', defaults)).toContain('herdr pane rename "$__P" "$(cat "$__D/label")"')
})

test('the ccx follower claims a new log instead of taking the newest by mtime', () => {
  const text = buildWrapper('ccx task "x"', defaults)
  expect(text).toContain('> "$D/before"') // the logs already there when the pane started
  expect(text).toContain('grep -vxFf "$D/before"')
  expect(text).toContain('mkdir "$CLAIMS/$c"') // one pane per log
  expect(text).toContain('f=$(claim)')
  expect(text).not.toContain('-newer')
  expect(text).toContain('RUNS="$HOME/work/_shared/claude-container/runs"') // default unchanged
  expect(text).toContain('ccx runs directory not found')
})

test('ccxRunsDir comes from the config, shell-quoted, and CCX_RUNS_DIR still wins', { options: { ccxRunsDir: '/data/ccx $x' } }, async ($, on) => {
  const text = (await seenBy($, on, { command: 'ccx task "x"' })).command
  expect(text).toContain(`RUNS="\${CCX_RUNS_DIR:-}"; [ -n "$RUNS" ] || RUNS='/data/ccx $x'`)
})

test('a runs dir with replacement patterns is kept as written', () => {
  const text = buildWrapper('ccx task "x"', { ...defaults, runsDir: "/r/$&/$'x" })
  expect(text).toContain(`RUNS='/r/$&/$'\\''x'`)
})

test('layout auto keeps the legacy direction behaviour', { options: { layout: 'auto', direction: 'down' } }, async ($, on) => {
  const text = (await seenBy($, on, { command: 'codex exec "x"' })).command
  expect(text).toContain('--direction down')
  expect(text).not.toContain('__REG=')
})

test('ccx runs take a launch lock until their own log is claimed', () => {
  const text = buildWrapper('ccx task "x"', defaults)
  expect(text).toContain('LOCK="$CLAIMS/launch.lock"')
  expect(text.indexOf('mkdir "$LOCK"')).toBeLessThan(text.indexOf('> "$D/before"')) // lock before the snapshot
  expect(text).toContain('[ -z "$f" ] && [ "$gaveup" = 0 ] && f=$(claim)') // no claim after giving up
  expect(text).toContain('release; gaveup=1;')
  expect(text).toContain('touch "$D/cmddone"; wait $FOLLOW; release')
})

test('a ccx run waits for the launch lock and claims only when no other log is unbound', () => {
  const text = buildWrapper('ccx task "x"', defaults)
  expect(text).toContain('while [ "$OK" = 1 ] && ! mkdir "$LOCK" 2>/dev/null; do') // no time limit: a claimer's hold is bounded by its own wait
  expect(text).not.toContain('LOCKWAIT')
  expect(text).toContain('CAN=0; : > "$UNB/$$"; release;') // another run is unbound: mark, release, claim nothing
  expect(text).toContain('gaveup=$((1 - CAN))')
  expect(text).toContain(': > "$UNB/$$"; release; gaveup=1;') // a timed-out run is marked before it releases
  expect(text).toContain('release; rm -f "$UNB/$$"')
})

// 0.7.2, issue 219
test('a standalone review sends its final message to the answer file; a chained one keeps stdout', () => {
  const text = buildWrapper('codex review', defaults)
  expect(text).toContain('[ -z "${__CODEX_CHAIN:-}" ] && [ "$1" = review ]; then')
  expect(text).toContain('command codex "$@" > "$__CODEX_LAST"')
})

test('the Markdown log joins neighbouring text fences', () => {
  expect(buildWrapper('codex review', defaults)).toContain('| awk -f "$ROOT/viewer/joinfence.awk"; }')
})

test('the pane sets its own title and clears the typed command before the log', () => {
  expect(buildWrapper('codex exec "x"', defaults)).toContain('printf "\\033]0;%s\\007\\033[H\\033[2J" "$(cat "$D/label" 2>/dev/null)"')
})

test('the pane is renamed after herdr runs the pane script', () => {
  const text = buildWrapper('codex exec "x"', defaults)
  expect(text.indexOf('herdr pane rename')).toBeGreaterThan(text.indexOf('herdr pane run'))
})

test('old scripts, run directories and ccx claims are swept in the background', () => {
  const text = buildWrapper('codex exec "x"', defaults)
  expect(text).toContain("-name 'codex-herdr-run.*.sh' -mmin +60 -delete")
  expect(text).toContain('K="${CODEX_HERDR_KEEP_DAYS:-7}"')
  expect(text).toContain("-name 'codex-herdr.*' -mtime +\"$K\"")
  expect(text).toContain(') >/dev/null 2>&1 &')
})

test('the ccx claims folder is per OS user, and an unusable one stops the wait', () => {
  const text = buildWrapper('ccx task "x"', defaults)
  expect(text).toContain('CLAIMS="${TMPDIR:-/tmp}/codex-herdr-ccx-claims-$(id -u)"; mkdir -p -m 700 "$CLAIMS"')
  expect(text).toContain('[ -d "$LOCK" ] || { OK=0; break; }') // a failed mkdir with no lock there is not contention
  expect(text).toContain('CAN=$OK;')
  expect(text).toContain('find "$T/codex-herdr-ccx-claims-$(id -u)"') // the sweep looks in the same folder
})

test('only one run at a time breaks a dead holder\'s lock, after checking its pid again', () => {
  const text = buildWrapper('ccx task "x"', defaults)
  expect(text).toContain('mkdir "$LOCK.break" 2>/dev/null; then')
  expect(text).toContain('[ "$(cat "$LOCK/pid" 2>/dev/null)" = "$p" ] && rm -rf "$LOCK"; rmdir "$LOCK.break"; continue')
  expect(text).not.toContain('kill -0 "$p" 2>/dev/null; then rm -rf "$LOCK"')
})
