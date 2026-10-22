import { expect, test } from 'claude-code/testing'
import { classifyCommand, claimsTestsPass, emptyTurn, formatDuration, labelOf, noteCall, outcomeOf, receiptLine, segments } from '../hooks/receipt'
import { FIXTURE } from './receipt-fixture'

test('commands classify by anchored shape, per chained segment', () => {
  expect(segments('go build ./... && go vet ./... ; go test ./... | tail -3')).toEqual(['go build ./...', 'go vet ./...', 'go test ./...', 'tail -3'])
  expect(segments('(cd /x && npm test)')).toEqual(['npm test)'])
  expect(classifyCommand('go test ./...')).toBe('test')
  expect(classifyCommand('scripts/go-test-streaming.sh ./...')).toBe('test')
  expect(classifyCommand('wipnote check --gate --work-item feat-1')).toBe('test')
  expect(classifyCommand('go vet ./...')).toBe('build')
  expect(classifyCommand('git -C /repo commit -m x')).toBe('commit')
  expect(classifyCommand('echo go test')).toBe('other')
  expect(classifyCommand('git commit-tree abc')).toBe('other')
  expect(labelOf('go test ./... -run X')).toBe('go test')
  expect(labelOf('pytest -q')).toBe('pytest')
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

test('claims: passing-test phrases, not the word test alone', () => {
  for (const s of ['Tests pass.', 'All tests pass now.', 'the test suite passes', 'Checks are green.', 'all checks passed', 'the quality gate passes']) {
    expect(claimsTestsPass(s)).toBe(true)
  }
  for (const s of ['Run the tests before you push.', 'The test fails.', 'I added a test.', '```\ntests pass\n```', '']) {
    expect(claimsTestsPass(s)).toBe(false)
  }
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
  expect(FIXTURE.length).toBe(16)
})
