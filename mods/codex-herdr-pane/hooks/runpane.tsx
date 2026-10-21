// The Claude Code side of a delegation run: outside herdr (claudePane auto) a run is headless and
// is drawn here, in a pane of Claude Code's own (one per run; several are tabs). runstate.ts
// follows every run to its end and writes its page; runview.tsx draws the pane. /codex-runs shows
// the newest run: in its pane where a surface draws one, else as text in the chat.

import type { EngineInterface as Api, On } from 'claude-code'
import { summarizeRun } from './livedoc.ts'
import { loadParts, renderPane } from './runview.tsx'
import { PANE_PREFIX, allRuns, paneId, poll, runStatus, setPaneOptions, type PaneOptions } from './runstate.ts'

export { paneId, PANE_PREFIX } from './runstate.ts'

// /codex-runs: the newest run in its pane, opened as the person asked (placed at any width)
async function showLatest($: Api) {
  const latest = Object.values(await allRuns($)).filter((r) => r.host !== 'pending').sort((a, b) => b.registered - a.registered)[0]
  if (!latest) return { text: 'No delegation run is followed in this session yet.' }
  // a live herdr run is drawn by its herdr pane; this side reads it only once it ends
  if (latest.host === 'herdr' && latest.exit === undefined) return { text: `${latest.label} is running in its herdr pane.` }
  const opened = await $.ui.open({ id: paneId(latest.id), title: latest.label.slice(0, 60), focus: true })
  if (opened.isPlaced) return { text: `Showing ${latest.label}.` }
  // No surface draws the pane (a cloud session viewed in the app, a -p run): the run goes in the chat
  const { parts } = await loadParts($, latest)
  const head = `${latest.label}: ${runStatus(latest, await $.clock.now())}`
  const page = latest.page ? `\n\nAs a page with collapsible tool results: ${latest.page}` : ''
  return { text: summarizeRun(parts, head, latest.answer, `${latest.dir}/clean.md`) + page }
}

export function runPane(on: On, o: PaneOptions) {
  setPaneOptions(o)
  // A reload keeps $.state but not the timer: follow the runs still live
  on('session.start', async ($, e, next) => {
    await $.command.register({ name: 'codex-runs', description: 'Show the newest Codex, ccx or Claude delegation run in a pane' })
    if (Object.values(await allRuns($)).some((r) => r.exit === undefined)) poll($)
    return next(e)
  })

  on('command.run', { command: 'codex-runs' }, async ($) => showLatest($))

  on('ui.render', { component: 'Pane' }, async ($, e, next) => {
    if (!e.requestId.startsWith(PANE_PREFIX)) return next(e)
    const ui = $.ui.resolve(e)
    const r = (await allRuns($))[e.requestId.slice(PANE_PREFIX.length)]
    if (!r) return <ui.Text dimColor>This run is no longer followed.</ui.Text>
    return renderPane(ui, $, r, await $.clock.now())
  })
}
