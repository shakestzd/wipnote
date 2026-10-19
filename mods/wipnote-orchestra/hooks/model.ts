import type { AgentRow, Limit, PromptInfo } from '../types'

const ITEM = /\b(?:feat|bug|spk|trk)-[0-9a-f]{6,}\b/
const WIPNOTE = /\bwipnote\s+(?:feature|bug|spike|track)\s+(start|complete)\s+((?:feat|bug|spk|trk)-[0-9a-f]+)/g

/** The first wipnote work item id a subagent's brief names. */
export function workItemIn(text: string): string | undefined {
  return ITEM.exec(text)?.[0]
}

/** Applies every `wipnote <kind> start|complete <id>` in a shell command. */
export function applyWipnote(items: readonly string[], command: string): string[] {
  let next = [...items]
  for (const [, verb, id] of command.matchAll(WIPNOTE)) {
    if (id === undefined) continue
    next = next.filter(one => one !== id)
    if (verb === 'start') next.push(id)
  }
  return next
}

/** `## main...origin/main [ahead 1, behind 2]` plus one line per change. */
export function parseGitStatus(out: string): Omit<PromptInfo, 'dir'> {
  const [head = '', ...rest] = out.split('\n')
  const branch = /^## (?:No commits yet on )?([^.\s]+)/.exec(head)?.[1]
  return {
    branch: branch === 'HEAD' ? 'detached' : branch,
    dirty: rest.filter(line => line.trim() !== '').length,
    ahead: Number(/ahead (\d+)/.exec(head)?.[1] ?? 0),
    behind: Number(/behind (\d+)/.exec(head)?.[1] ?? 0),
  }
}

/** The repo or worktree's folder name, from `git rev-parse --show-toplevel`. */
export function baseName(path: string): string {
  return path.replace(/\/+$/, '').split('/').pop() || path
}

/** Outside git: p10k-style dir, home as ~, at most the last three parts. */
export function shortDir(cwd: string, home?: string): string {
  const path = home !== undefined && cwd.startsWith(home) ? `~${cwd.slice(home.length)}` : cwd
  const parts = path.split('/').filter(Boolean)
  if (parts.length <= 3) return path
  return `…/${parts.slice(-3).join('/')}`
}

/** One short phrase for what a tool call is doing. */
export function describeCall(tool: string, input: Record<string, unknown>): string {
  const str = (key: string) => (typeof input[key] === 'string' ? (input[key] as string) : '')
  const base = (path: string) => path.split('/').pop() ?? path
  switch (tool) {
    case 'Bash':
      return `$ ${str('command').split('\n')[0]}`
    case 'Read':
    case 'Edit':
    case 'Write':
    case 'NotebookEdit':
      return `${tool} ${base(str('file_path') || str('notebook_path'))}`
    case 'Grep':
    case 'Glob':
      return `${tool} ${str('pattern')}`
    case 'WebFetch':
      return `fetch ${str('url')}`
    case 'WebSearch':
      return `search ${str('query')}`
    case 'Agent':
      return `spawn ${str('subagent_type') || 'agent'}: ${str('description')}`
    default:
      return tool.startsWith('mcp__') ? tool.split('__').slice(1).join(':') : tool
  }
}

/** 9s, 4m05s, 1h12m. */
export function elapsed(ms: number): string {
  const s = Math.max(0, Math.floor(ms / 1000))
  if (s < 60) return `${s}s`
  const m = Math.floor(s / 60)
  if (m < 60) return `${m}m${String(s % 60).padStart(2, '0')}s`
  return `${Math.floor(m / 60)}h${String(m % 60).padStart(2, '0')}m`
}

export const isLive = (status: string) =>
  status === 'running' || status === 'pending' || status === 'waiting' || status === 'idle'

/** Live agents first (oldest first), then the most recently finished. */
export function visibleAgents(rows: readonly AgentRow[], now: number, keepMs: number): AgentRow[] {
  const live = rows.filter(row => isLive(row.status))
  const done = rows
    .filter(row => !isLive(row.status) && now - (row.endedAt ?? now) < keepMs)
    .sort((a, b) => (b.endedAt ?? 0) - (a.endedAt ?? 0))
  return [...live.sort((a, b) => a.startedAt - b.startedAt), ...done]
}

/** `claude-opus-5-5[1m]` → `Opus 5.5 1M`; anything unrecognized passes through. */
export function modelLabel(id: string): string {
  const wide = /\[1m\]$/i.test(id) ? ' 1M' : ''
  const bare = id.replace(/\[[^\]]*\]$/, '').replace(/^(?:[a-z]+\.)?anthropic\./, '')
  const m = /^claude-([a-z]+)-(\d+)(?:-(\d{1,2}))?(?:-\d{8}|@\d{8})?(?:-v\d.*)?$/.exec(bare)
  if (m === null) return id
  const [, family = '', major, minor] = m
  return `${family.charAt(0).toUpperCase()}${family.slice(1)} ${major}${minor !== undefined ? `.${minor}` : ''}${wide}`
}

/** A run of styled text in a line the band draws as one Text. */
export type Span = {
  text: string
  color?: string
  bg?: string
  bold?: boolean
  dim?: boolean
  italic?: boolean
}

const cells = (text: string) => [...text].length

/** Cuts a line of spans to `width` cells, ending a cut span with `…`, so it never wraps. */
export function fit(spans: readonly Span[], width: number): Span[] {
  const out: Span[] = []
  let left = Math.max(0, width)
  for (const span of spans) {
    if (left <= 0) break
    const chars = [...span.text]
    if (chars.length <= left) {
      out.push(span)
      left -= chars.length
      continue
    }
    out.push({ ...span, text: left > 1 ? `${chars.slice(0, left - 1).join('')}…` : '…' })
    break
  }
  return out
}

export const padEnd = (text: string, width: number) => text + ' '.repeat(Math.max(0, width - cells(text)))
export const padStart = (text: string, width: number) => ' '.repeat(Math.max(0, width - cells(text))) + text

/** Widths for the agent table's fixed columns, so every row lines up. */
export function columnsOf(rows: readonly AgentRow[]): { type: number; model: number; item: number } {
  const widest = (pick: (row: AgentRow) => string | undefined, cap: number) =>
    Math.min(cap, Math.max(0, ...rows.map(row => cells(pick(row) ?? ''))))
  return {
    type: widest(row => row.type, 24),
    model: widest(row => row.model, 14),
    item: widest(row => row.workItem, 28),
  }
}

/** One agent row: tree mark, status, fixed columns, then the free-length tail. */
export function agentSpans(
  row: AgentRow,
  cols: { type: number; model: number; item: number },
  mark: string,
  glyph: [string, string],
  took: string,
): Span[] {
  const isDone = !isLive(row.status)
  const cut = (text: string, width: number) => (cells(text) > width ? `${[...text].slice(0, width - 1).join('')}…` : text)
  const spans: Span[] = [
    { text: ` ${mark} `, dim: true },
    { text: `${glyph[0]} `, color: glyph[1] },
    { text: padEnd(cut(row.type, cols.type), cols.type), bold: !isDone, dim: isDone, color: isDone ? undefined : '#5fd7ff' },
  ]
  if (cols.model > 0) spans.push({ text: ` ${padEnd(row.model ?? '', cols.model)}`, color: '#d75f00', dim: isDone })
  if (cols.item > 0) spans.push({ text: ` ${padEnd(row.workItem ?? '', cols.item)}`, color: '#af87ff', dim: isDone })
  spans.push({ text: ` ${padStart(took, 6)} ${padStart(String(row.tools), 3)}t `, dim: true })
  spans.push({ text: ` ${row.description}`, dim: isDone })
  if (!isDone && row.lastTool !== undefined) spans.push({ text: `  ↳ ${row.lastTool}`, dim: true, italic: true })
  return spans
}


const PILLS: Record<string, string> = {
  auto: '⏵⏵ auto mode on',
  acceptEdits: '⏵⏵ accept edits on',
  plan: '⏸ plan mode on',
  bypassPermissions: '⏵⏵ bypass permissions on',
  dontAsk: "⏵⏵ don't ask on",
}

/**
 * Cells the engine's mode pill and its ` · ` take left of the hint line, which is where a
 * tree drawn there starts. No pill when the hint does not open with its `(shift+tab …)` note.
 */
export function pillIndent(permissionMode: string | null, hint: string): number {
  if (!hint.startsWith('(')) return 0
  const pill = PILLS[permissionMode ?? 'auto'] ?? PILLS.auto ?? ''
  return [...pill].length + 3
}

/** One powerline segment: its text with a cell of padding each side, and its colors. */
export type Segment = { text: string; bg: string; fg: string }

export const CAP_LEFT = ''
export const CAP_RIGHT = ''
export const ARROW = ''

/**
 * Lays segments into as few lines as `width` allows, breaking only between segments. A line is
 * `CAP_LEFT seg ARROW seg … CAP_RIGHT`, so it costs its texts plus one cell per segment and one more
 * for the left cap. A segment wider than a line on its own is cut to fit with `…`.
 */
export function wrapSegments(segs: readonly Segment[], width: number): Segment[][] {
  const room = Math.max(4, width)
  const lines: Segment[][] = []
  let line: Segment[] = []
  let used = 1
  for (const seg of segs) {
    const most = room - 2
    const text = cells(seg.text) > most ? `${[...seg.text].slice(0, most - 2).join('')}… ` : seg.text
    const cost = cells(text) + 1
    if (line.length > 0 && used + cost > room) {
      lines.push(line)
      line = []
      used = 1
    }
    line.push({ ...seg, text })
    used += cost
  }
  if (line.length > 0) lines.push(line)
  return lines
}

/** The spans of one wrapped line: rounded caps at its ends, arrows between its segments. */
export function lineSpans(line: readonly Segment[]): Span[] {
  const first = line[0]
  if (first === undefined) return []
  const spans: Span[] = [{ text: CAP_LEFT, color: first.bg }]
  line.forEach((seg, i) => {
    const next = line[i + 1]
    spans.push({ text: seg.text, bg: seg.bg, color: seg.fg, bold: true })
    spans.push(next === undefined ? { text: CAP_RIGHT, color: seg.bg } : { text: ARROW, color: seg.bg, bg: next.bg })
  })
  return spans
}

/** The person's own zone: the container runs on UTC, the person on US Eastern (DST included). */
export const TIME_ZONE = 'America/New_York'

/** `6:28 PM` in `zone`, the 12-hour form the person's prompt uses. */
export function localTime(ms: number, zone: string = TIME_ZONE): string {
  return new Intl.DateTimeFormat('en-US', { timeZone: zone, hour: 'numeric', minute: '2-digit', hour12: true }).format(ms)
}

const KINDS: Record<string, string> = { feat: 'feature', bug: 'bug', spk: 'spike' }

/** The `wipnote <kind> show` argv for a work item id, or undefined for an id of no known kind. */
export function showArgv(id: string): string[] | undefined {
  const kind = KINDS[id.split('-')[0] ?? '']
  return kind === undefined ? undefined : ['wipnote', kind, 'show', id, '--format', 'json']
}

/** A work item's title, status and track from `wipnote <kind> show --format json`. */
export function parseItemJson(text: string): { title: string; status: string; trackId?: string } | undefined {
  try {
    const item = JSON.parse(text) as {
      title?: unknown
      status?: unknown
      edges?: { part_of?: { target_id?: unknown }[] }
    }
    if (typeof item.title !== 'string') return undefined
    const track = item.edges?.part_of?.map(edge => edge.target_id).find(id => typeof id === 'string' && id.startsWith('trk-'))
    return { title: item.title, status: String(item.status ?? ''), trackId: track as string | undefined }
  } catch {
    return undefined
  }
}

/** A track's title and `(1/46 done)` progress from `wipnote track show` (its text form; the JSON has no counts). */
export function parseTrackText(text: string): { title: string; done: number; total: number } | undefined {
  const lines = text.split('\n').map(line => line.trim())
  const title = lines.find(line => line !== '' && !/^[─-]+$/.test(line))
  const progress = /\((\d+)\/(\d+) done\)/.exec(text)
  if (title === undefined || progress === null) return undefined
  return { title, done: Number(progress[1]), total: Number(progress[2]) }
}

/** `text` cut to `max` cells with `…`. */
export function clip(text: string, max: number): string {
  return cells(text) > max ? `${[...text].slice(0, Math.max(1, max - 1)).join('')}…` : text
}

const LIMIT_LABELS: Record<string, string> = { five_hour: '5h', seven_day: '7d', spend_limit: 'spend' }

/** When a window resets, in the person's zone: the time today, else the weekday and time. */
export function resetLabel(iso: string, now: number, zone: string = TIME_ZONE): string {
  const at = Date.parse(iso)
  if (Number.isNaN(at)) return ''
  const time = localTime(at, zone)
  if (at - now < 20 * 3600_000) return time
  const day = new Intl.DateTimeFormat('en-US', { timeZone: zone, weekday: 'short' }).format(at)
  return `${day} ${time}`
}

/** The usage-limits row: each window's used share, colored by how close it is, and when it resets. */
export function limitSpans(limits: readonly Limit[], now: number): Span[] {
  if (limits.length === 0) return []
  const spans: Span[] = [{ text: ' \uF0E4 ', dim: true }]
  limits.forEach((limit, i) => {
    const pct = Math.round(limit.percentUsed)
    const color = pct >= 80 ? '#d70000' : pct >= 50 ? '#d7af00' : '#5faf00'
    const reset = limit.resetsAt === undefined ? '' : resetLabel(limit.resetsAt, now)
    if (i > 0) spans.push({ text: '   ' })
    spans.push({ text: `${LIMIT_LABELS[limit.kind] ?? limit.kind} `, dim: true })
    spans.push({ text: `${pct}%`, color, bold: true })
    if (reset !== '') spans.push({ text: ` resets ${reset}`, dim: true })
  })
  return spans
}
