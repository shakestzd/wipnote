import type { EngineInterface } from 'claude-code'

import type { AgentRow, PromptInfo, WorkCard } from '../types'
import type { Segment, Span } from './model'
import { agentSpans, clip, columnsOf, elapsed, fit, isLive, limitSpans, lineSpans, localTime, wrapSegments } from './model'

type Elements = ReturnType<EngineInterface['ui']['resolve']>

export type BandData = {
  info: PromptInfo | null
  list: AgentRow[]
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

const GLYPH: Record<string, [string, string]> = {
  running: ['●', '#5fd7ff'],
  pending: ['◌', '#878787'],
  waiting: ['◐', '#ffaf00'],
  idle: ['○', '#878787'],
  completed: ['✔', '#5faf00'],
  failed: ['✘', '#d70000'],
  killed: ['✘', '#d75f00'],
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
  at: number,
  zone: string,
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
  if (live > 0) {
    out.push({ text: ` ◉ ${live} agent${live === 1 ? '' : 's'} `, bg: '#005f87', fg: '#ffffff' })
  }
  if (at > 0) out.push({ text: ` \uF017 ${localTime(at, zone)} `, bg: '#303030', fg: '#d0d0d0' })
  return out
}

/** The band's tree, or null with nothing to show; drawn above or below the prompt alike. */
export function drawBand({ Box, Text }: Elements, data: BandData, room: BandRoom) {
  const live = data.list.filter(row => isLive(row.status)).length
  const segs = segmentsOf(data.info, data.work, data.cards, live, data.at, data.zone)
  if (segs.length === 0) return null

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
  const limits = room.isCompact ? [] : limitSpans(data.info?.limits ?? [], data.at, data.zone)
  const bar = [...(limits.length > 0 ? [limits] : []), ...lines].map((spans, i) => draw(`line-${i}`, spans))
  if (room.isCompact || data.list.length === 0) return <Box flexDirection="column">{bar}</Box>

  const fits = Math.max(0, room.maxRows - 1 - bar.length)
  // An agent's row names its work item by title, not by id.
  const titleOf = (id: string | undefined) => (id === undefined ? undefined : (data.cards.find(card => card.id === id)?.title ?? id))
  const shown = data.list.slice(0, fits).map(row => ({ ...row, workItem: titleOf(row.workItem) }))
  const cols = columnsOf(shown)
  return (
    <Box flexDirection="column">
      {bar}
      {shown.map((row, i) => {
        const mark = i === shown.length - 1 && data.list.length <= fits ? '└' : '├'
        const took = elapsed((row.endedAt ?? data.at) - row.startedAt)
        return draw(`a-${row.id}`, agentSpans(row, cols, mark, GLYPH[row.status] ?? ['?', '#878787'], took))
      })}
      {data.list.length > fits && <Text dimColor>{`   … ${data.list.length - fits} more`}</Text>}
    </Box>
  )
}
