import type { RunRow } from '../types'
import { groupOf } from './model'

/**
 * Delegate runs (wipnote delegate, plan-28b4a7f6): the run folders under
 * .wipnote/delegate/runs/ (internal/delegate/runstore) joined to their rows in
 * .wipnote/delegations-ledger.html (core/delegationledger), which holds the
 * mode, harness and name a run folder does not. Read-only: this file only
 * lists, stats and reads files (and asks git where the checkout is); it never
 * writes, creates or removes anything. Pure: register.tsx makes the `$.fs`, `$.env` and `$.process`
 * calls and hands the results to these functions.
 */

export const RUNS_DIR = '.wipnote/delegate/runs'
export const LEDGER_FILE = '.wipnote/delegations-ledger.html'

/** An ended run stays in the band this long after it ended; a live one always shows. */
export const RUN_KEEP_MS = 60 * 60_000

const RUN_ID = /^dlg-[0-9a-f]{12,64}$/

/** What the ledger says of one run. */
export type LedgerRow = {
  mode: string
  harness: string
  name: string
  label: string
  closed: boolean
  endedAt?: number
  exit?: number
  endReason: string
}

const NAMED: Record<string, string> = { amp: '&', lt: '<', gt: '>', quot: '"', apos: "'" }

/** Undoes the HTML escaping the ledger writer applies to attribute values. */
export function decodeEntities(text: string): string {
  return text.replace(/&(#x[0-9a-f]+|#\d+|[a-z]+);/gi, (whole, ref: string) => {
    const named = NAMED[ref.toLowerCase()]
    if (named !== undefined) return named
    const isHex = /^#x/i.test(ref)
    const code = isHex ? parseInt(ref.slice(2), 16) : ref.startsWith('#') ? Number(ref.slice(1)) : NaN
    return Number.isFinite(code) && code > 0 && code <= 0x10ffff ? String.fromCodePoint(code) : whole
  })
}

function attrsOf(tag: string): Map<string, string> {
  const attrs = new Map<string, string>()
  for (const [, name = '', value = ''] of tag.matchAll(/\s([a-z][a-z0-9-]*)="([^"]*)"/g)) attrs.set(name, decodeEntities(value))
  return attrs
}

/**
 * The ledger's rows by run id. Each row is one `<tr data-run-id=…>` line whose
 * attributes carry every column; a row with no valid run id, mode or harness
 * (a torn or foreign line) is skipped.
 */
export function parseLedger(html: string): Map<string, LedgerRow> {
  const rows = new Map<string, LedgerRow>()
  for (const [tag] of html.matchAll(/<tr\s[^>]*data-run-id="[^"]*"[^>]*>/g)) {
    const a = attrsOf(tag)
    const get = (name: string) => (a.get(`data-${name}`) ?? '').trim()
    const [id, mode, harness, exit, endReason] = ['run-id', 'mode', 'harness', 'exit', 'end-reason'].map(get) as string[]
    if (!RUN_ID.test(id ?? '') || mode === '' || harness === '') continue
    const ended = Date.parse(get('ended'))
    rows.set(id ?? '', {
      mode: mode ?? '',
      harness: harness ?? '',
      name: get('name'),
      label: get('label'),
      closed: !Number.isNaN(ended) || endReason !== '',
      endedAt: Number.isNaN(ended) ? undefined : ended,
      exit: /^-?\d+$/.test(exit ?? '') ? Number(exit) : undefined,
      endReason: endReason ?? '',
    })
  }
  return rows
}

/** The first non-blank line of last.md, without a heading's `#`s or a quote's `>`. */
export function firstLine(md: string): string {
  const line = md.split('\n').find(one => one.trim() !== '') ?? ''
  return line.trim().replace(/^(?:#{1,6}\s+|>\s*)+/, '').trim()
}

/** What one run folder holds. */
export type FolderFacts = {
  label: string
  startedAt?: number
  endedAt?: number
  /** The exit file's code (an unreadable one reads as 1, as Go reads it); undefined while there is none. */
  exit?: number
  answer: string
  /** The worker's state from state.json, as `wipnote delegate list` last observed it. */
  observed?: string
}

const unixMs = (text: string) => (/^\d+$/.test(text.trim()) && Number(text) > 0 ? Number(text.trim()) * 1000 : undefined)

/**
 * A run's state as Go's list and tree name it (session.RunStateOf and treeState): an open
 * worker's observed state (starting when it has none Go knows), running for any other open run,
 * and once ended gone, failed for a non-zero exit, else ended.
 */
export function statusOf(ended: boolean, exit: number | undefined, facts: FolderFacts, ledger?: LedgerRow): string {
  if (!ended && ledger?.mode === 'interactive') {
    const observed = facts.observed ?? ''
    return groupOf(observed) !== undefined ? observed : 'starting'
  }
  if (!ended) return 'running'
  if (ledger?.endReason === 'gone') return 'gone'
  return exit !== undefined && exit !== 0 ? 'failed' : 'ended'
}

/** The band's row for one run: its folder's files, and its ledger row when there is one. */
export function rowOf(id: string, facts: FolderFacts, ledger?: LedgerRow): RunRow {
  const ended = facts.exit !== undefined || ledger?.closed === true
  const exit = facts.exit ?? ledger?.exit
  const reason = facts.exit === undefined && ledger?.endReason !== 'exit' ? ledger?.endReason : undefined
  const endedAt = facts.endedAt ?? ledger?.endedAt
  const status = statusOf(ended, exit, facts, ledger)
  return {
    id,
    title: ledger?.name || ledger?.label || facts.label || id,
    harness: ledger?.harness ?? '',
    mode: ledger?.mode ?? '',
    state: ended ? 'ended' : 'live',
    ...(ended && exit !== undefined ? { exit } : {}),
    ...(ended && reason ? { reason } : {}),
    status,
    group: groupOf(status) ?? '',
    ...(facts.startedAt !== undefined ? { startedAt: facts.startedAt } : {}),
    ...(ended && endedAt !== undefined ? { endedAt } : {}),
    answer: facts.answer,
  }
}

/** The runs to draw: live ones oldest first, then the ones that ended within RUN_KEEP_MS, newest first. */
export function visibleRuns(rows: readonly RunRow[], now: number): RunRow[] {
  const live = rows.filter(one => one.state === 'live').sort((a, b) => (a.startedAt ?? 0) - (b.startedAt ?? 0))
  const endOf = (one: RunRow) => one.endedAt ?? one.startedAt ?? 0
  const ended = rows
    .filter(one => one.state === 'ended' && now - endOf(one) < RUN_KEEP_MS)
    .sort((a, b) => endOf(b) - endOf(a))
  return [...live, ...ended]
}

/** The state named in a worker's state.json, or undefined when it has none or cannot be read. */
export function observedState(json: string): string | undefined {
  try {
    const state = (JSON.parse(json) as { state?: unknown }).state
    return typeof state === 'string' && state !== '' ? state : undefined
  } catch {
    return undefined
  }
}

/** The files of a run folder worth reading: plain files, not links. */
export const FOLDER_FILES = ['label', 'start', 'end', 'exit', 'last.md', 'state.json']

/** An entry of a folder listing (`$.fs.list`). */
export type Entry = { name: string; kind: string; isLink?: boolean; mtimeMs?: number }

/** The plain files of a listing, by name. */
export const plainFiles = (entries: readonly Entry[]): Map<string, Entry> =>
  new Map(entries.filter(one => one.kind === 'file' && !one.isLink).map(one => [one.name, one]))

/** One run folder's facts: label, start, end, exit, the first line of last.md and the observed state. `texts` follows FOLDER_FILES. */
export function folderFacts(files: ReadonlyMap<string, Entry>, texts: readonly string[]): FolderFacts {
  const [label = '', start = '', end = '', exit = '', last = '', state = ''] = texts
  const code = exit.trim()
  const observed = observedState(state)
  return {
    label: label.trim().split('\n')[0] ?? '',
    startedAt: unixMs(start),
    endedAt: unixMs(end) ?? (files.has('exit') ? files.get('exit')?.mtimeMs || undefined : undefined),
    exit: files.has('exit') ? (/^-?\d+$/.test(code) ? Number(code) : 1) : undefined,
    answer: firstLine(last),
    ...(observed !== undefined ? { observed } : {}),
  }
}

/** The main checkout a `git rev-parse --git-common-dir` answer names, if it names one. */
export const commonCheckout = (exitCode: number | undefined, stdout: string | undefined): string | undefined => {
  const common = exitCode === 0 ? (stdout ?? '').trim() : ''
  return common.endsWith('/.git') ? common.slice(0, -5) || '/' : undefined
}

/** The folder above `dir`, as the walk up from the session's directory takes it. */
export const parentDir = (dir: string): string => dir.slice(0, dir.lastIndexOf('/'))

/** The args of the git call that finds the main checkout. */
export const GIT_COMMON_ARGV = ['git', 'rev-parse', '--path-format=absolute', '--git-common-dir']

export type LedgerCache = { key: string; rows: Map<string, LedgerRow> }

export const emptyLedger = (): LedgerCache => ({ key: '', rows: new Map() })

/** The ledger's cache key (size and mtime), or undefined when it is not a file. */
export const ledgerKey = (stat: { kind: string; size: number; mtimeMs: number } | undefined): string | undefined =>
  stat === undefined || stat.kind !== 'file' ? undefined : `${stat.size}:${stat.mtimeMs}`

/** The ledger as read: its rows, or none when it could not be read. */
export const ledgerRead = (key: string, html: string | undefined): LedgerCache =>
  html === undefined ? emptyLedger() : { key, rows: parseLedger(html) }

/** What readRuns keeps between calls: the settled runs' facts and the ledger as last read. */
export type RunCache = { settled: Map<string, FolderFacts>; ledger: LedgerCache }

export const newRunCache = (): RunCache => ({ settled: new Map(), ledger: emptyLedger() })

/** The run folders in a listing; the settled ones whose folder is gone (swept) are dropped from `cache`. */
export function runIds(entries: readonly Entry[], cache: RunCache): string[] {
  const ids = entries.filter(one => one.kind === 'dir' && !one.isLink && RUN_ID.test(one.name)).map(one => one.name)
  for (const id of cache.settled.keys()) if (!ids.includes(id)) cache.settled.delete(id)
  return ids
}

/**
 * One run's row. A folder is read again on every call while its run is open; once it has an
 * exit file or a closed ledger row its facts are kept in `cache`.
 */
export function runRow(id: string, facts: FolderFacts, cache: RunCache): RunRow {
  const row = rowOf(id, facts, cache.ledger.rows.get(id))
  if (row.state === 'ended') cache.settled.set(id, facts)
  return row
}
