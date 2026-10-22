// Shell command reading for the receipt: split one Bash call into its pieces,
// strip wrappers, classify each piece, and spread the call's single outcome
// over the pieces without claiming more than the exit status can tell.
//
// What one exit status says (bash without pipefail):
//   a && b && c   ok: every piece ran and succeeded. failed: some piece failed and
//                 the rest never ran, which one is unknown: the chain is one item.
//   a ; b         the status is b's; a is unknown either way.
//   a | b         the status is b's; a is unknown either way.
//   a || b        ok: a or b succeeded, unknown which. failed: both failed.
//   interrupted   nothing is known.
// No engine imports. Every helper is total.

export type CmdKind = 'test' | 'build' | 'commit' | 'other'
export type Outcome = 'ok' | 'failed' | 'unknown'
export type End = { outcome: Outcome; exitCode?: number }
export type Cmd = { label: string; kind: CmdKind; outcome: Outcome; exitCode?: number }
export type Op = '&&' | '||' | ';' | '|' | null
export type Piece = { text: string; op: Op }

const TEST = /^(?:go test|gotestsum|scripts\/go-test-streaming\.sh|(?:npm|pnpm|yarn|bun) (?:run )?test|pytest|vitest|jest|mocha|cargo test|claude plugin test|wipnote check)\b/
const BUILD = /^(?:go build|go vet|wipnote build|(?:npm|pnpm|yarn|bun) run build|cargo build|tsc|claude plugin validate)\b/
const COMMIT = /^git(?: -C \S+)? commit(?=\s|$)/
// Leading `NAME=value` assignments and the wrappers that run another command.
const ENV_ASSIGN = /^[A-Za-z_][A-Za-z0-9_]*=\S*\s+/
const WRAPPER = /^(?:time|sudo(?:\s+-[A-Za-z]+)*|env|nohup|nice(?:\s+-n\s+\d+)?|timeout\s+\d+[smh]?|npx|bunx|pnpm exec|npm exec)\s+/
const KIND_RANK: Record<CmdKind, number> = { test: 3, build: 2, commit: 1, other: 0 }

/** Splits on `&&`, `||`, `;`, `|` and line ends; each piece carries the operator before it. */
export function pieces(command: string): Piece[] {
  const out: Piece[] = []
  const re = /\s*(&&|\|\||;|\||\r?\n)\s*/g
  const src = String(command ?? '')
  let last = 0
  let op: Op = null
  const push = (text: string, nextOp: Op) => {
    const t = text.replace(/^[\s(]+|[\s)]+$/g, '')
    if (t !== '') out.push({ text: t, op })
    op = nextOp
  }
  for (const m of src.matchAll(re)) {
    push(src.slice(last, m.index), /\n/.test(m[1] as string) ? ';' : (m[1] as Op))
    last = m.index + m[0].length
  }
  push(src.slice(last), null)
  return out
}

/** `CGO_ENABLED=0 time go test` reads as `go test`. */
export function stripWrappers(segment: string): string {
  let s = String(segment ?? '').trim()
  for (;;) {
    const next = s.replace(ENV_ASSIGN, '').replace(WRAPPER, '')
    if (next === s) return s
    s = next
  }
}

export function classifyCommand(segment: string): CmdKind {
  const s = stripWrappers(segment)
  if (TEST.test(s)) return 'test'
  if (BUILD.test(s)) return 'build'
  if (COMMIT.test(s)) return 'commit'
  return 'other'
}

/** `go test ./...` reads as `go test`; `pytest -q` keeps one word. */
export function labelOf(segment: string): string {
  const words = stripWrappers(segment).split(/\s+/)
  const first = words[0] ?? ''
  return /^(?:go|npm|pnpm|yarn|bun|cargo|git|claude|wipnote)$/.test(first) ? words.slice(0, 2).join(' ') : first
}

type Group = { cmds: Cmd[]; op: Op }

/** Pieces grouped into `&&` chains; a group's `op` is the operator before its first piece. */
function groups(command: string): Group[] {
  const out: Group[] = []
  for (const p of pieces(command)) {
    const kind = classifyCommand(p.text)
    const cmd: Cmd = { label: labelOf(p.text), kind, outcome: 'unknown' }
    if (p.op === '&&' && out.length > 0) (out[out.length - 1] as Group).cmds.push(cmd)
    else out.push({ cmds: [cmd], op: p.op })
  }
  // `other` pieces (cd, echo, tail) still count for position: `go test | tail` leaves go test before the last group.
  return out.map(g => ({ ...g, cmds: g.cmds.filter(c => c.kind !== 'other') }))
}

const withEnd = (c: Cmd, end: End): Cmd => (end.exitCode === undefined ? { ...c, outcome: end.outcome } : { ...c, outcome: end.outcome, exitCode: end.exitCode })

/** A failed `&&` chain of several commands is one item named for all of them, ranked by its strongest kind. */
function merged(g: Group, end: End): Cmd[] {
  if (g.cmds.length <= 1) return g.cmds.map(c => withEnd(c, end))
  const kind = g.cmds.reduce<CmdKind>((best, c) => (KIND_RANK[c.kind] > KIND_RANK[best] ? c.kind : best), 'other')
  return [withEnd({ label: g.cmds.map(c => c.label).join(' && '), kind, outcome: 'unknown' }, end)]
}

/** The commands of one Bash call with the outcome each can honestly be given. */
export function commandsOf(command: string, end: End): Cmd[] {
  const gs = groups(command)
  const hasOr = gs.some(g => g.op === '||')
  return gs.flatMap((g, i) => {
    const isLast = i === gs.length - 1
    if (end.outcome === 'ok') return isLast && !hasOr ? g.cmds.map(c => withEnd(c, end)) : g.cmds
    if (end.outcome !== 'failed') return g.cmds
    // Failed: the last group failed, and so did every group joined to it by `||`.
    const joinedToLast = gs.slice(i + 1).every(h => h.op === '||')
    return isLast || joinedToLast ? merged(g, end) : g.cmds
  })
}
