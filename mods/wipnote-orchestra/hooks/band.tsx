import type { EngineInterface } from 'claude-code'

import type { AgentRow, PromptInfo, Tally, WorkCard } from '../types'
import type { Segment, Span } from './model'
import {
  attentionSpans,
  clip,
  clockText,
  fit,
  groupByItem,
  groupSpans,
  isLive,
  limitSpans,
  lineSpans,
  needsAttention,
  tallyText,
  wrapSegments,
} from './model'

type Elements = ReturnType<EngineInterface['ui']['resolve']>

export type BandData = {
  info: PromptInfo | null
  /** Every agent the session has followed (the last 100). */
  rows: AgentRow[]
  tally: Tally
  /** How long a finished agent stays counted in its work item's row. */
  keepMs: number
  work: string[]
  cards: WorkCard[]
  doing: string | null
  at: number
  zone: string
}

export type BandRoom = {
  width: number
  maxRows: number
  isWorking: boolean
  isCompact: boolean
}

/** A work item segment's colour by its status; an unknown or unloaded status keeps the in-progress colour. */
const ITEM_BG: Record<string, string> = { done: '#5f8700', blocked: '#af0000', todo: '#5f5f87' }

/**
 * The session's work items as their track's progress (` Raw-data RCA agents [1/46] `, once per track) and
 * their own title and steps (` → Routine setup script 2/5 `), coloured by status; an item whose details
 * have not loaded shows its id.
 */
export function workSegments(work: readonly string[], cards: readonly WorkCard[]): Segment[] {
  const out: Segment[] = []
  const tracks = new Set<string>()
  for (const id of work) {
    const card = cards.find(one => one.id === id)
    if (card?.trackId !== undefined && card.trackTitle !== undefined && !tracks.has(card.trackId)) {
      tracks.add(card.trackId)
      const progress = card.total !== undefined ? ` [${card.done ?? 0}/${card.total}]` : ''
      out.push({ text: ` ${clip(card.trackTitle, 32)}${progress} `, bg: '#5f5faf', fg: '#ffffff' })
    }
    const steps = card?.stepsTotal !== undefined ? ` ${card.stepsDone ?? 0}/${card.stepsTotal}` : ''
    out.push({ text: ` → ${clip(card?.title ?? id, 40)}${steps} `, bg: ITEM_BG[card?.status ?? ''] ?? '#875fd7', fg: '#ffffff' })
  }
  return out
}

function segmentsOf(
  info: PromptInfo | null,
  work: readonly string[],
  cards: readonly WorkCard[],
  live: number,
  tally: string,
  clock: string,
): Segment[] {
  const out: Segment[] = []
  if (info !== null) {
    out.push({ text: ` ${info.dir} `, bg: '#0087af', fg: '#ffffff' })
    if (info.branch !== undefined) {
      const marks = [
        info.dirty > 0 ? `!${info.dirty}` : '',
        info.ahead > 0 ? `⇡${info.ahead}` : '',
        info.behind > 0 ? `⇣${info.behind}` : '',
      ].filter(Boolean)
      out.push({
        text: ` \uE0A0 ${info.branch}${marks.length > 0 ? ` ${marks.join(' ')}` : ''} `,
        bg: info.dirty > 0 ? '#d7af00' : '#5faf00',
        fg: '#000000',
      })
    }
    if (info.model !== undefined) {
      out.push({ text: ` ✻ ${info.model} `, bg: '#d75f00', fg: '#ffffff' })
    }
    if (info.ctxPercent !== undefined) {
      const pct = Math.round(info.ctxPercent)
      out.push({ text: ` ctx ${pct}% `, bg: pct >= 80 ? '#d70000' : '#444444', fg: '#ffffff' })
    }
  }
  out.push(...workSegments(work, cards))
  const agents = [live > 0 ? `◉ ${live} agent${live === 1 ? '' : 's'}` : '', tally].filter(Boolean).join(' · ')
  if (agents !== '') out.push({ text: ` ${agents} `, bg: '#005f87', fg: '#ffffff' })
  if (clock !== '') out.push({ text: ` \uF017 ${clock} `, bg: '#303030', fg: '#d0d0d0' })
  return out
}

/**
 * The band's tree, or null with nothing to show; drawn above or below the prompt alike.
 * Claude Code's own agent panel lists every agent (and opens one with Enter), so the band adds
 * what it does not show: agents grouped by work item, and the ones that need a look.
 */
export function drawBand({ Box, Text }: Elements, data: BandData, room: BandRoom) {
  const live = data.rows.filter(row => isLive(row.status)).length
  // The clock ends the usage-limits row; without that row (compact, or no limits known) it is a segment.
  const limits = room.isCompact ? [] : limitSpans(data.info?.limits ?? [], data.at, data.zone)
  const clock = data.at > 0 ? clockText(data.at, data.zone) : ''
  const segs = segmentsOf(data.info, data.work, data.cards, live, tallyText(data.tally), limits.length > 0 ? '' : clock)
  if (segs.length === 0) return null
  if (limits.length > 0 && clock !== '') limits.push({ text: '   ' }, { text: `\uF017 ${clock}`, color: '#d0d0d0' })

  const draw = (key: string, spans: readonly Span[]) => (
    <Text key={key} wrap="truncate-end">
      {fit(spans, room.width).map((s, i) => (
        <Text key={`${key}-${i}`} color={s.color} backgroundColor={s.bg} bold={s.bold} dimColor={s.dim} italic={s.italic}>
          {s.text}
        </Text>
      ))}
    </Text>
  )
  const lines = wrapSegments(segs, room.width).map(lineSpans)
  const last = lines.at(-1)
  if (last !== undefined && room.isWorking && data.doing !== null) last.push({ text: ` ▶ ${data.doing}`, dim: true })
  const bar = [...(limits.length > 0 ? [limits] : []), ...lines].map((spans, i) => draw(`line-${i}`, spans))
  if (room.isCompact) return <Box flexDirection="column">{bar}</Box>

  const card = (id: string) => data.cards.find(one => one.id === id)
  const flags = needsAttention(data.rows, data.at).map(flag => ({
    key: `x-${flag.row.id}`,
    spans: attentionSpans(flag, data.at),
  }))
  const groups = groupByItem(data.rows, data.at, data.keepMs).map(group => {
    const it = card(group.item)
    const steps = it?.stepsTotal !== undefined ? `${it.stepsDone ?? 0}/${it.stepsTotal} steps` : ''
    return { key: `g-${group.item}`, spans: groupSpans(group, clip(it?.title ?? group.item, 40), steps) }
  })
  const rows = [...flags, ...groups]
  const fits = Math.max(0, room.maxRows - 1 - bar.length)
  const shown = rows.length > fits ? rows.slice(0, Math.max(0, fits - 1)) : rows
  return (
    <Box flexDirection="column">
      {bar}
      {shown.map(row => draw(row.key, row.spans))}
      {shown.length < rows.length && <Text dimColor>{`   … ${rows.length - shown.length} more`}</Text>}
    </Box>
  )
}
