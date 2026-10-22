import { expect, test } from 'claude-code/testing'
import { classifyCommand, claimsTestsPass, commandsOf, emptyTurn, formatDuration, labelOf, noteCall, outcomeOf, pieces, receiptLine, stripWrappers } from '../hooks/receipt'
import { FIXTURE } from './receipt-fixture'

test('a command splits into pieces that keep the operator before each one', () => {
  expect(pieces('go build ./... && go vet ./... ; go test ./... | tail -3')).toEqual([
    { text: 'go build ./...', op: null },
    { text: 'go vet ./...', op: '&&' },
    { text: 'go test ./...', op: ';' },
    { text: 'tail -3', op: '|' },
  ])
  expect(pieces('(cd /x && npm test)')).toEqual([{ text: 'cd /x', op: null }, { text: 'npm test', op: '&&' }])
  expect(pieces('a || b')).toEqual([{ text: 'a', op: null }, { text: 'b', op: '||' }])
  expect(pieces('')).toEqual([])
})

test('env assignments and wrappers are stripped before a command is classified (finding 3)', () => {
  expect(stripWrappers('CGO_ENABLED=0 GOFLAGS=-mod=mod go test ./...')).toBe('go test ./...')
  expect(stripWrappers('time go test ./...')).toBe('go test ./...')
  expect(stripWrappers('sudo -E env X=1 go test ./...')).toBe('go test ./...')
  expect(stripWrappers('nohup nice -n 5 timeout 300 go test ./...')).toBe('go test ./...')
  expect(stripWrappers('npx tsc --noEmit')).toBe('tsc --noEmit')
  for (const c of ['CGO_ENABLED=0 go test ./...', 'time go test ./...', 'sudo go test ./...', 'env X=1 go test ./...', 'nohup go test ./...', 'npx vitest run', 'bunx jest']) {
    expect(classifyCommand(c)).toBe('test')
  }
  expect(classifyCommand('npx tsc --noEmit')).toBe('build')
  expect(labelOf('CGO_ENABLED=0 go test ./... -run X')).toBe('go test')
  expect(labelOf('time pytest -q')).toBe('pytest')
})

test('commands classify by anchored shape', () => {
  expect(classifyCommand('go test ./...')).toBe('test')
  expect(classifyCommand('scripts/go-test-streaming.sh ./...')).toBe('test')
  expect(classifyCommand('wipnote check --gate --work-item feat-1')).toBe('test')
  expect(classifyCommand('go vet ./...')).toBe('build')
  expect(classifyCommand('git -C /repo commit -m x')).toBe('commit')
  expect(classifyCommand('echo go test')).toBe('other')
  expect(classifyCommand('git commit-tree abc')).toBe('other')
  expect(labelOf('go test ./... -run X')).toBe('go test')
})

test('one result for a chain: never claim an outcome the data cannot support (finding 2)', () => {
  const ok = { outcome: 'ok' as const, exitCode: 0 }
  const fail = { outcome: 'failed' as const, exitCode: 1 }
  // && chain, success: every piece ran and succeeded
  expect(commandsOf('go build ./... && go vet ./... && go test ./...', ok).map(c => `${c.label}:${c.outcome}`)).toEqual([
    'go build:ok', 'go vet:ok', 'go test:ok',
  ])
  // && chain, failure: which piece failed is unknown, so the chain is one failed item that counts as a test run
  expect(commandsOf('go build ./... && go vet ./... && go test ./...', fail)).toEqual([
    { label: 'go build && go vet && go test', kind: 'test', outcome: 'failed', exitCode: 1 },
  ])
  // a pipe reports the last command's exit, so the test before it is unknown
  expect(commandsOf('go test ./... 2>&1 | tail -5', ok)).toEqual([{ label: 'go test', kind: 'test', outcome: 'unknown' }])
  // `;` reports the last command's exit; `|| true` hides the exit
  expect(commandsOf('go test ./...; echo done', ok)[0]).toMatchObject({ label: 'go test', outcome: 'unknown' })
  expect(commandsOf('go test ./... || true', ok)[0]).toMatchObject({ label: 'go test', outcome: 'unknown' })
  // `a || b` fails only when both fail
  expect(commandsOf('go test ./... || echo failed', fail)[0]).toMatchObject({ label: 'go test', outcome: 'failed', exitCode: 1 })
  // the piece after `;` or `|` is the one the exit describes
  expect(commandsOf('echo start; go test ./...', fail)).toEqual([{ label: 'go test', kind: 'test', outcome: 'failed', exitCode: 1 }])
  // interrupted: nothing is known
  expect(commandsOf('go build ./... && go test ./...', { outcome: 'unknown' }).map(c => c.outcome)).toEqual(['unknown', 'unknown'])
  // a cd prefix in a subshell is not a command worth a line
  expect(commandsOf('(cd /repo && go test ./...)', ok)).toEqual([{ label: 'go test', kind: 'test', outcome: 'ok', exitCode: 0 }])
})

test('outcome reads core result shapes; the Bash record has no exit code', () => {
  expect(outcomeOf({ result: { stdout: '', stderr: '', interrupted: false } })).toEqual({ outcome: 'ok', exitCode: 0 })
  expect(outcomeOf({ isError: true, result: 'x', text: 'Exit code 2\nerr' })).toEqual({ outcome: 'failed', exitCode: 2 })
  expect(outcomeOf({ isError: true, result: 'x' })).toEqual({ outcome: 'failed' })
  expect(outcomeOf({ deny: 'no' })).toEqual({ outcome: 'failed' })
  expect(outcomeOf({ result: { interrupted: true } })).toEqual({ outcome: 'unknown' })
  expect(outcomeOf(undefined)).toEqual({ outcome: 'unknown' })
  expect(outcomeOf({ result: 'a string record' })).toEqual({ outcome: 'unknown' })
})

test('claims: passing-test phrases, with negation and context guards (finding 4)', () => {
  for (const s of ['Tests pass.', 'All tests pass now.', 'the test suite passes', 'Checks are green.', 'all checks passed', 'the quality gate passes', 'All 39 tests pass.', 'go test passes on every package.']) {
    expect(claimsTestsPass(s)).toBe(true)
  }
  for (const s of ['Run the tests before you push.', 'The test fails.', 'I added a test.', '```\ntests pass\n```', '']) {
    expect(claimsTestsPass(s)).toBe(false)
  }
  // The three review sentences: a negated claim, a negated count, and a different sense of "pass"
  expect(claimsTestsPass('The tests did not pass yet.')).toBe(false)
  expect(claimsTestsPass('No checks passed on the first run.')).toBe(false)
  expect(claimsTestsPass('Health checks pass through the proxy unchanged.')).toBe(false)
  expect(claimsTestsPass("The tests don't pass on Windows.")).toBe(false)
  expect(claimsTestsPass('Two tests failed; the rest never passed either.')).toBe(false)
})

test('duration formats', () => {
  expect(formatDuration(4000)).toBe('4s')
  expect(formatDuration(65_000)).toBe('1m05s')
  expect(formatDuration(3_700_000)).toBe('1h01m')
  expect(formatDuration(-5)).toBe('0s')
})

test('helpers never throw on odd input', () => {
  for (const bad of [undefined, null, 42, {}, { tool: 'Bash' }, { tool: 'Edit' }] as never[]) {
    expect(() => noteCall(emptyTurn(), bad, bad)).not.toThrow()
    expect(() => receiptLine(emptyTurn(), bad, bad)).not.toThrow()
    expect(() => commandsOf(bad, bad)).not.toThrow()
  }
  expect(noteCall(emptyTurn(), { tool: 'Edit', file_path: '' } as never, {})).toEqual(emptyTurn())
})

test('fixture: every fake turn gets the expected line (precision)', () => {
  const misses: string[] = []
  for (const t of FIXTURE) {
    let turn = emptyTurn()
    for (const c of t.calls) turn = noteCall(turn, c.call as never, c.result)
    const got = receiptLine(turn, t.answer, t.durationMs)
    if (got !== t.expect) misses.push(`${t.name}: got ${JSON.stringify(got)}, want ${JSON.stringify(t.expect)}`)
  }
  expect(misses).toEqual([])
  expect(FIXTURE.length).toBe(20)
})
