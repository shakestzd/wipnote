// Pure helpers for the turn receipt: the one line under a reply that says what
// the tools actually did (files edited, test/build/commit commands and how they
// ended, duration) and flags a reply that claims passing tests when no test
// command ran. No engine imports. Every helper is total.
//
// Exit codes: the Bash tool's record has no exit code (its fields are stdout,
// stderr, interrupted, gitOperation...). A non-zero exit makes core answer the
// call with `isError: true`, and the model-facing `text` usually names the
// code. So: no isError = ok, isError = failed (code parsed from text when
// present), anything else = unknown. How one exit spreads over a chained
// command is commands.ts's job.
import { commandsOf } from './commands'
import type { Cmd, End, Outcome } from './commands'

export { classifyCommand, commandsOf, labelOf, pieces, stripWrappers } from './commands'
export type { Cmd, CmdKind, Outcome } from './commands'
export type Turn = { files: string[]; commands: Cmd[]; commits: string[] }

export const emptyTurn = (): Turn => ({ files: [], commands: [], commits: [] })

const EDIT_TOOLS = new Set(['Edit', 'Write', 'MultiEdit', 'NotebookEdit'])
// A passing-test claim: subject, a short gap, then a passing verb.
const CLAIM = /\b(?:all )?(?:\d+ )?(?:tests?|test suite|checks?|quality gate|suite|go test)\b([^.;\n]{0,40}?)\b(pass(?:es|ed|ing)?|green|succeed(?:s|ed)?)\b|\b(?:passing|green) (?:tests|suite|build)\b/gi
// Any negation between or just before the subject and the verb undoes the claim.
const NEGATED = /\b(?:not|no|never|none|fail(?:s|ed|ing)?|without)\b|n['’]t\b/i
// "pass through", "pass to the proxy": another sense of the verb.
const OTHER_SENSE = /^\s+(?:through|to|into|along|around|up|down|back|off)\b/i

/** Reads how a tool call ended from the shape core gives `tool.call` results. */
export function outcomeOf(result: unknown): End {
  if (result === null || typeof result !== 'object') return { outcome: 'unknown' }
  const r = result as { deny?: unknown; isError?: unknown; text?: unknown; result?: { interrupted?: unknown } }
  if (typeof r.deny === 'string') return { outcome: 'failed' }
  if (r.isError === true) {
    const m = typeof r.text === 'string' ? /\bexit code[: ]+(\d+)/i.exec(r.text) : null
    return m ? { outcome: 'failed', exitCode: Number(m[1]) } : { outcome: 'failed' }
  }
  if (r.result && typeof r.result === 'object') {
    return r.result.interrupted === true ? { outcome: 'unknown' } : { outcome: 'ok', exitCode: 0 }
  }
  return { outcome: 'unknown' }
}

type Call = { tool?: unknown; command?: unknown; file_path?: unknown; notebook_path?: unknown }

/** Folds one finished tool call into the turn. Returns a new turn; never throws. */
export function noteCall(turn: Turn, call: Call, result: unknown): Turn {
  try {
    const tool = String(call?.tool ?? '')
    if (EDIT_TOOLS.has(tool)) {
      const path = String(call.file_path ?? call.notebook_path ?? '')
      if (path === '' || turn.files.includes(path)) return turn
      return { ...turn, files: [...turn.files, path] }
    }
    if (tool !== 'Bash') return turn
    const sha = commitSha(result)
    const commands = commandsOf(String(call.command ?? ''), outcomeOf(result))
    if (commands.length === 0 && sha === undefined) return turn
    return {
      ...turn,
      commands: [...turn.commands, ...commands],
      commits: sha !== undefined && !turn.commits.includes(sha) ? [...turn.commits, sha] : turn.commits,
    }
  } catch {
    return turn
  }
}

/** The short sha core attaches to a committing Bash call, when it does. */
function commitSha(result: unknown): string | undefined {
  const r = result as { result?: { gitOperation?: { commit?: { sha?: unknown } } } } | null
  const sha = r?.result?.gitOperation?.commit?.sha
  return typeof sha === 'string' && sha !== '' ? sha.slice(0, 7) : undefined
}

/** True when the reply says tests (or checks, the suite, the gate) pass, in that sense, and not negated. */
export function claimsTestsPass(answer: string): boolean {
  const text = String(answer ?? '').replace(/```[\s\S]*?```/g, '')
  for (const m of text.matchAll(CLAIM)) {
    const before = text.slice(Math.max(0, m.index - 24), m.index)
    const after = text.slice(m.index + m[0].length, m.index + m[0].length + 12)
    if (NEGATED.test(before) || NEGATED.test(m[1] ?? '')) continue
    if (m[2] !== undefined && OTHER_SENSE.test(after)) continue
    return true
  }
  return false
}

export function formatDuration(ms: number): string {
  const s = Math.max(0, Math.round(Number(ms) / 1000))
  if (s < 60) return `${s}s`
  const m = Math.floor(s / 60)
  return m < 60 ? `${m}m${String(s % 60).padStart(2, '0')}s` : `${Math.floor(m / 60)}h${String(m % 60).padStart(2, '0')}m`
}

const MARK: Record<Outcome, string> = { ok: '✔', failed: '✘', unknown: '?' }

/** Flags the mismatch between what the reply says and what ran, or ''. */
export function mismatch(turn: Turn, answer: string): string {
  if (!claimsTestsPass(answer)) return ''
  const tests = turn.commands.filter(c => c.kind === 'test')
  if (tests.length === 0) return 'reply says tests pass; no test command ran this turn'
  if (tests.some(c => c.outcome === 'ok')) return ''
  const t = tests[tests.length - 1] as Cmd
  return t.outcome === 'failed' ? `reply says tests pass; ${t.label} failed` : `reply says tests pass; ${t.label} ended with an unknown exit`
}

/** The receipt line, or null when the turn ran no tool worth a line. */
export function receiptLine(turn: Turn, answer: string, durationMs: number): string | null {
  const parts: string[] = []
  if (turn.files.length > 0) parts.push(`✎ ${turn.files.length} ${turn.files.length === 1 ? 'file' : 'files'}`)
  const seen = new Set<string>()
  for (const c of turn.commands) {
    if (c.kind === 'commit') continue
    const key = `${c.label}:${c.outcome}`
    if (seen.has(key)) continue
    seen.add(key)
    const code = c.outcome === 'failed' && c.exitCode !== undefined ? ` (exit ${c.exitCode})` : c.outcome === 'unknown' ? ' (exit unknown)' : ''
    parts.push(`${MARK[c.outcome]} ${c.label}${code}`)
  }
  if (turn.commits.length > 0) parts.push(`⎇ ${turn.commits.join(' ')}`)
  else if (turn.commands.some(c => c.kind === 'commit')) parts.push('⎇ commit')
  const warn = mismatch(turn, answer)
  if (parts.length === 0 && warn === '') return null
  if (parts.length > 0) parts.push(formatDuration(durationMs))
  const line = parts.join(' · ')
  return warn === '' ? line : line === '' ? `⚠ ${warn}` : `${line}\n⚠ ${warn}`
}
