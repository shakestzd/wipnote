// Fake turns for the receipt: the tool calls one turn made, the reply's final
// text, and the line the receipt should show. `null` means no line.
// The precision test counts how many of these the helpers get right.

type Call = { call: Record<string, unknown>; result: unknown }

export type FakeTurn = { name: string; calls: Call[]; answer: string; durationMs: number; expect: string | null }

const bash = (command: string, result: unknown): Call => ({ call: { tool: 'Bash', command }, result })
const edit = (file_path: string): Call => ({ call: { tool: 'Edit', file_path }, result: { result: { filePath: file_path } } })
const ok = { result: { stdout: 'ok', stderr: '', interrupted: false } }
const fail = (code: number) => ({ isError: true, result: `Exit code ${code}\nFAIL`, text: `Exit code ${code}\nFAIL` })
const committed = (sha: string) => ({ result: { stdout: `[main ${sha}] msg`, stderr: '', interrupted: false, gitOperation: { commit: { sha: `${sha}0000000`, kind: 'committed' } } } })

export const FIXTURE: FakeTurn[] = [
  { name: 'pure answer, no tools', calls: [], answer: 'A mutex serialises access.', durationMs: 4000, expect: null },
  { name: 'reads only', calls: [{ call: { tool: 'Read', file_path: 'a.go' }, result: ok }], answer: 'The file defines Add.', durationMs: 9000, expect: null },
  {
    name: 'edit and passing tests',
    calls: [edit('a.go'), edit('a_test.go'), bash('go test ./...', ok)],
    answer: 'Added Add and its test. Tests pass.',
    durationMs: 130_000,
    expect: '✎ 2 files · ✔ go test · 2m10s',
  },
  {
    name: 'edit, failing tests, honest reply',
    calls: [edit('a.go'), bash('go test ./...', fail(1))],
    answer: 'The test still fails on the nil case.',
    durationMs: 65_000,
    expect: '✎ 1 file · ✘ go test (exit 1) · 1m05s',
  },
  {
    name: 'edit, failing tests, reply claims pass',
    calls: [edit('a.go'), bash('go test ./...', fail(1))],
    answer: 'Done. All tests pass now.',
    durationMs: 65_000,
    expect: '✎ 1 file · ✘ go test (exit 1) · 1m05s\n⚠ reply says tests pass; go test failed',
  },
  {
    name: 'edit, no tests, reply claims pass',
    calls: [edit('a.go')],
    answer: 'Fixed the bug; the test suite passes.',
    durationMs: 20_000,
    expect: '✎ 1 file · 20s\n⚠ reply says tests pass; no test command ran this turn',
  },
  {
    name: 'claim with no tools at all',
    calls: [],
    answer: 'Tests are green.',
    durationMs: 3000,
    expect: '⚠ reply says tests pass; no test command ran this turn',
  },
  {
    name: 'chained gate and a commit with sha',
    calls: [edit('x.ts'), bash('go build ./... && go vet ./... && go test ./...', ok), bash('git add x.ts && git commit -m "fix (feat-1)"', committed('a5f39e1'))],
    answer: 'Committed the fix; all checks pass.',
    durationMs: 200_000,
    expect: '✎ 1 file · ✔ go build · ✔ go vet · ✔ go test · ⎇ a5f39e1 · 3m20s',
  },
  {
    name: 'commit without sha in the record',
    calls: [bash('git commit -am wip', ok)],
    answer: 'Committed.',
    durationMs: 2000,
    expect: '⎇ commit · 2s',
  },
  {
    name: 'interrupted test is unknown',
    calls: [bash('npm test', { result: { stdout: '', stderr: '', interrupted: true } })],
    answer: 'The run was interrupted.',
    durationMs: 10_000,
    expect: '? npm test (exit unknown) · 10s',
  },
  {
    name: 'interrupted test but reply claims pass',
    calls: [bash('bun test', { result: { stdout: '', stderr: '', interrupted: true } })],
    answer: 'Tests pass.',
    durationMs: 10_000,
    expect: '? bun test (exit unknown) · 10s\n⚠ reply says tests pass; bun test ended with an unknown exit',
  },
  {
    name: 'denied command',
    calls: [bash('go test ./...', { deny: 'blocked by a guard' })],
    answer: 'The guard refused the run.',
    durationMs: 1000,
    expect: '✘ go test · 1s',
  },
  {
    name: 'subshell cd prefix and a pipe: the pipe hides the test exit',
    calls: [bash('(cd /repo && go test ./... 2>&1 | tail -5)', ok)],
    answer: 'Tests pass.',
    durationMs: 50_000,
    expect: '? go test (exit unknown) · 50s\n⚠ reply says tests pass; go test ended with an unknown exit',
  },
  {
    name: 'failed && chain is one item; the claim is flagged against it',
    calls: [edit('a.go'), bash('go build ./... && go vet ./... && go test ./...', fail(1))],
    answer: 'All tests pass.',
    durationMs: 65_000,
    expect: '✎ 1 file · ✘ go build && go vet && go test (exit 1) · 1m05s\n⚠ reply says tests pass; go build && go vet && go test failed',
  },
  {
    name: 'env assignment and time wrapper still count as the test run',
    calls: [bash('CGO_ENABLED=0 time go test ./...', ok)],
    answer: 'Tests pass.',
    durationMs: 20_000,
    expect: '✔ go test · 20s',
  },
  {
    name: 'a negated claim is not a claim',
    calls: [edit('a.go')],
    answer: 'The tests did not pass yet; two cases still fail.',
    durationMs: 5000,
    expect: '✎ 1 file · 5s',
  },
  {
    name: 'pass in another sense is not a claim',
    calls: [edit('proxy.go')],
    answer: 'Health checks pass through the proxy unchanged.',
    durationMs: 5000,
    expect: '✎ 1 file · 5s',
  },
  {
    name: 'same test twice reads once',
    calls: [bash('go test ./...', ok), bash('go test ./...', ok)],
    answer: 'Both runs pass.',
    durationMs: 70_000,
    expect: '✔ go test · 1m10s',
  },
  {
    name: 'other commands only',
    calls: [bash('ls -la', ok), bash('git status', ok)],
    answer: 'The tree is clean.',
    durationMs: 3000,
    expect: null,
  },
  {
    name: 'the word test in prose is not a claim',
    calls: [edit('README.md')],
    answer: 'Run the tests before you push.',
    durationMs: 5000,
    expect: '✎ 1 file · 5s',
  },
]
