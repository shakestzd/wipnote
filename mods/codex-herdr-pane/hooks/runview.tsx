// Draws one run's Claude Code pane (runpane.tsx's ui.render hook): a header, the Live / Answer /
// Open-all buttons, then Live (prose, and each tool result as a fold: a Button row that opens it)
// or Answer. The log is read from clean.md here, its tail only once it is long, so the state never
// holds it.

import type { EngineInterface as Api } from 'claude-code'
import type { RunView } from '../types'
import { parseRun, type RunPart } from './livedoc.ts'
import { patch, runStatus, text } from './runstate.ts'

type Fold = Extract<RunPart, { kind: 'fold' }>
type Ui = ReturnType<Api['ui']['resolve']>

export const MAX_CHARS = 80000 // what one drawing shows (the engine cuts a drawing at 100000)
const TAIL_CHARS = 400000 // of a longer log only this much is read into a drawing

// The log's parts, from a top-level boundary (a fold or a heading) when only its tail is read
export async function loadParts($: Api, r: RunView): Promise<{ parts: RunPart[]; cut: boolean }> {
  let md = (await text($, `${r.dir}/clean.md`)) ?? ''
  const cut = md.length > TAIL_CHARS
  if (cut) {
    const tail = md.slice(-TAIL_CHARS)
    const at = tail.search(/\n(<!-- fold (open|closed): |#{1,6} )/)
    md = at >= 0 ? tail.slice(at + 1) : tail.slice(tail.indexOf('\n') + 1)
  }
  return { parts: parseRun(md, r.exit !== undefined), cut }
}

// The newest parts that fit MAX_CHARS. The newest alone may not: then it is cut to its last
// MAX_CHARS characters (a fold's body; prose from a line start).
export function fitParts(parts: RunPart[], isOpen: (i: number, p: Fold) => boolean) {
  let room = MAX_CHARS, first = parts.length
  const shown = parts.slice()
  while (first > 0) {
    const i = first - 1, p = shown[i]!
    const cost = p.kind === 'prose' ? p.text.length : p.title.length + (isOpen(i, p) ? p.body.length : 0)
    if (cost > room && first < parts.length) break
    if (cost > room) shown[i] = clip(p, room)
    room -= Math.min(cost, room); first = i
  }
  return { first, shown: shown.slice(first) }
}

function clip(p: RunPart, room: number): RunPart {
  const keep = Math.max(0, room - 200)
  const cutText = (t: string) => { const s = t.slice(-keep); return `*... earlier text cut*\n\n${s.slice(s.indexOf('\n') + 1)}` }
  return p.kind === 'prose' ? { ...p, text: cutText(p.text) } : { ...p, body: cutText(p.body) }
}

function foldRow(ui: Ui, $: Api, r: RunView, i: number, p: Fold, open: boolean) {
  const { Box, Button, Markdown, Text } = ui
  const toggle = () => void patch($, r.id, (c) => ({ ...c, open: { ...c.open, [String(i)]: !open } }))
  return (
    <Box key={`f${i}`} flexDirection="column">
      <Button key={`fold-${i}`} plain onPress={toggle}>
        {open ? '▼ ' : '▶ '}<Text color={p.failed ? 'red' : undefined}>{p.title}</Text>
      </Button>
      {open && <Box paddingLeft={2}><Markdown key={`b${i}`} text={p.body} /></Box>}
    </Box>
  )
}

function buttons(ui: Ui, $: Api, r: RunView, folds: number[], allOpen: boolean) {
  const { Box, Button } = ui
  const setTab = (tab: RunView['tab']) => void patch($, r.id, (c) => ({ ...c, tab }))
  const setAll = () => void patch($, r.id, (c) => ({ ...c, open: Object.fromEntries(folds.map((i) => [String(i), !allOpen])) }))
  return (
    <Box flexDirection="row" gap={1}>
      <Button key="tab-live" hotkey="l" variant={r.tab === 'live' ? 'primary' : undefined} onPress={() => setTab('live')}>Live</Button>
      <Button key="tab-answer" hotkey="a" variant={r.tab === 'answer' ? 'primary' : undefined} onPress={() => setTab('answer')}>Answer</Button>
      {r.tab === 'live' && folds.length > 0 && <Button key="folds" hotkey="f" onPress={setAll}>{allOpen ? 'Close all' : 'Open all'}</Button>}
    </Box>
  )
}

function liveView(ui: Ui, $: Api, r: RunView, parts: RunPart[], cut: boolean) {
  const { Box, Markdown, Text } = ui
  const isOpen = (i: number, p: Fold) => r.open[String(i)] ?? p.failed
  const { first, shown } = fitParts(parts, isOpen)
  const rows = shown.map((p, k) => {
    const i = first + k
    return p.kind === 'prose' ? <Markdown key={`p${i}`} text={p.text} /> : foldRow(ui, $, r, i, p, isOpen(i, p))
  })
  return (
    <Box flexDirection="column" gap={1}>
      {(first > 0 || cut) && <Text dimColor>Earlier entries not shown: {r.dir}/clean.md</Text>}
      {parts.length === 0 && <Text dimColor>Waiting for the first event...</Text>}
      {rows}
    </Box>
  )
}

export async function renderPane(ui: Ui, $: Api, r: RunView, now: number) {
  const { Box, Markdown, Text } = ui
  const ok = r.exit === '0'
  const { parts, cut } = r.tab === 'live' ? await loadParts($, r) : { parts: [] as RunPart[], cut: false }
  const folds = parts.flatMap((p, i) => (p.kind === 'fold' ? [i] : []))
  const allOpen = folds.length > 0 && folds.every((i) => r.open[String(i)] ?? (parts[i] as Fold).failed)
  const answer = r.answer || (r.exit === undefined ? '*The answer appears here when the run finishes.*' : '*No answer was written; see Live.*')
  return (
    <Box flexDirection="column" gap={1}>
      <Box flexDirection="column">
        <Text bold>{r.label}</Text>
        <Text color={r.exit === undefined ? undefined : ok ? 'green' : 'red'} dimColor={r.exit === undefined}>{runStatus(r, now)}</Text>
      </Box>
      {buttons(ui, $, r, folds, allOpen)}
      {r.tab === 'answer' ? <Markdown key="answer" text={answer} /> : liveView(ui, $, r, parts, cut)}
    </Box>
  )
}
