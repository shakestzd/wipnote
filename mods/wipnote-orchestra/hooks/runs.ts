import type { EngineInterface } from 'claude-code'

import type { RunRow } from '../types'

/**
 * Delegate runs (wipnote delegate, plan-28b4a7f6): the run folders under
 * .wipnote/delegate/runs/ (internal/delegate/runstore) joined to their rows in
 * .wipnote/delegations-ledger.html (core/delegationledger), which holds the
 * mode, harness and name a run folder does not. Read-only: this file only
 * lists, stats and reads files (and asks git where the checkout is); it never
 * writes, creates or removes anything.
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
}

const unixMs = (text: string) => (/^\d+$/.test(text.trim()) && Number(text) > 0 ? Number(text.trim()) * 1000 : undefined)

/** The band's row for one run: its folder's files, and its ledger row when there is one. */
export function rowOf(id: string, facts: FolderFacts, ledger?: LedgerRow): RunRow {
  const ended = facts.exit !== undefined || ledger?.closed === true
  const exit = facts.exit ?? ledger?.exit
  const reason = facts.exit === undefined && ledger?.endReason !== 'exit' ? ledger?.endReason : undefined
  const endedAt = facts.endedAt ?? ledger?.endedAt
  return {
    id,
    title: ledger?.name || ledger?.label || facts.label || id,
    harness: ledger?.harness ?? '',
    mode: ledger?.mode ?? '',
    state: ended ? 'ended' : 'live',
    ...(ended && exit !== undefined ? { exit } : {}),
    ...(ended && reason ? { reason } : {}),
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

/** Reads one run folder: label, start, end, exit and the first line of last.md, from plain files only. */
export async function readFolder($: EngineInterface, dir: string): Promise<FolderFacts> {
  const entries = await $.fs.list(dir)
  const files = new Map(entries.filter(one => one.kind === 'file' && !one.isLink).map(one => [one.name, one]))
  const read = (name: string) => (files.has(name) ? $.fs.read(`${dir}/${name}`).then(String).catch(() => '') : Promise.resolve(''))
  const [label = '', start = '', end = '', exit = '', last = ''] = await Promise.all(['label', 'start', 'end', 'exit', 'last.md'].map(read))
  const code = exit.trim()
  return {
    label: label.trim().split('\n')[0] ?? '',
    startedAt: unixMs(start),
    endedAt: unixMs(end) ?? (files.has('exit') ? files.get('exit')?.mtimeMs || undefined : undefined),
    exit: files.has('exit') ? (/^-?\d+$/.test(code) ? Number(code) : 1) : undefined,
    answer: firstLine(last),
  }
}

/**
 * The project whose .wipnote/ holds the runs, found as wipnote's CLI finds it:
 * WIPNOTE_PROJECT_DIR when it has one, else the main checkout of a git worktree,
 * else the nearest folder with one from the session's directory up.
 */
export async function findProject($: EngineInterface): Promise<string | null> {
  const has = (dir: string) => $.fs.exists(`${dir}/.wipnote`).catch(() => false)
  const env = await $.env.get('WIPNOTE_PROJECT_DIR').catch(() => undefined)
  if (env !== undefined && env !== '' && (await has(env))) return env
  const git = await $.process
    .run(['git', 'rev-parse', '--path-format=absolute', '--git-common-dir'], { timeoutMs: 5000 })
    .catch(() => undefined)
  const common = git?.exitCode === 0 ? git.stdout.trim() : ''
  if (common.endsWith('/.git') && (await has(common.slice(0, -5) || '/'))) return common.slice(0, -5) || '/'
  let dir = (await $.session.cwd()).replace(/\/+$/, '')
  while (dir !== '') {
    if (await has(dir)) return dir
    dir = dir.slice(0, dir.lastIndexOf('/'))
  }
  return (await has('/')) ? '/' : null
}

type LedgerCache = { key: string; rows: Map<string, LedgerRow> }

/** The ledger's rows, read again only when its size or mtime changed. */
async function readLedger($: EngineInterface, path: string, last: LedgerCache): Promise<LedgerCache> {
  const stat = await $.fs.stat(path).catch(() => undefined)
  if (stat === undefined || stat.kind !== 'file') return { key: '', rows: new Map() }
  const key = `${stat.size}:${stat.mtimeMs}`
  if (key === last.key) return last
  const html = await $.fs.read(path).then(String).catch(() => undefined)
  return html === undefined ? { key: '', rows: new Map() } : { key, rows: parseLedger(html) }
}

/** What readRuns keeps between calls: the settled runs' facts and the ledger as last read. */
export type RunCache = { settled: Map<string, FolderFacts>; ledger: LedgerCache }

export const newRunCache = (): RunCache => ({ settled: new Map(), ledger: { key: '', rows: new Map() } })

/**
 * A project's runs, one row per run folder. A folder is read again on every call while its
 * run is open; once it has an exit file or a closed ledger row its facts are kept in `cache`,
 * and dropped when the folder is gone (swept).
 */
export async function readRuns($: EngineInterface, project: string, cache: RunCache): Promise<RunRow[]> {
  cache.ledger = await readLedger($, `${project}/${LEDGER_FILE}`, cache.ledger)
  const dir = `${project}/${RUNS_DIR}`
  const entries = await $.fs.list(dir).catch(() => [])
  const ids = entries.filter(one => one.kind === 'dir' && !one.isLink && RUN_ID.test(one.name)).map(one => one.name)
  for (const id of cache.settled.keys()) if (!ids.includes(id)) cache.settled.delete(id)
  const rows: RunRow[] = []
  for (const id of ids) {
    const facts = cache.settled.get(id) ?? (await readFolder($, `${dir}/${id}`).catch(() => undefined))
    if (facts === undefined) continue
    const row = rowOf(id, facts, cache.ledger.rows.get(id))
    if (row.state === 'ended') cache.settled.set(id, facts)
    rows.push(row)
  }
  return rows
}
