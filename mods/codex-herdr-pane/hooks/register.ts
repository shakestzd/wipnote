import type { EngineInterface as Api, Register } from 'claude-code'
import { buildWrapper, isDelegation, paneLabel, runDirPath, scriptPath, stubFor, type ClosePolicy } from './wrap.ts'
import { runPane } from './runpane.tsx'
import { callEnded, settleRun, text, trackRun } from './runstate.ts'

const FOREGROUND_MAX_MS = 600000

const closePolicy = (v: unknown): ClosePolicy => (v === 'success' || v === 'never' ? v : 'always')

const SEND = 'copy it to a folder they can open (your scratchpad or the working directory) and send it to them as a file to render.'

// For the model, where nothing in this session draws mod panes: how to show the run to the user.
// A foreground call has ended, so its page exists or never will; a background one names its page
// in its report (a "run page:" line) when it ends. Elsewhere the report's line is enough.
// A call cut off by its timeout has returned for good while its run goes on (in a pane):
// no report will name the page, so the note names where the poller writes it. A bare run (no
// host file: outside herdr, no pane to be had) ended with the call and gets no page.
type CallEnd = 'done' | 'background' | 'interrupted'
async function pageNote($: Api, id: string, end: CallEnd, dir: string): Promise<string | undefined> {
  const page = end === 'done' ? await settleRun($, id) : undefined // ends the run here even if it left no exit
  if ((await $.session.surfaces()).length > 0) return undefined
  if (end === 'background') return `codex-herdr-pane: nothing in this session draws mod panes. If this delegation's report ends with a "run page:" line, show the run to the user: ${SEND}`
  if (end === 'interrupted') {
    const host = (await text($, `${dir}/host`))?.trim()
    if (host !== 'herdr' && host !== 'claude') return undefined
    return `codex-herdr-pane: nothing in this session draws mod panes. This call timed out while the delegation runs on, so no report will follow. Once it ends, its page is ${dir}/run.html (the user can also type /codex-runs). When that file exists, show the run to the user: ${SEND}`
  }
  return page ? `codex-herdr-pane: nothing in this session draws mod panes, so show the run to the user through its page, ${page}: ${SEND}` : undefined
}

export const register: Register = (on, options) => {
  // layout right/down is a fixed split of Claude's pane; anything else tiles. `direction` is the
  // legacy option, honoured only when layout is auto or not configured.
  const layout = typeof options.layout === 'string' && options.layout !== 'auto' ? options.layout : undefined
  const fixedDown = layout === 'down' || (layout === undefined && options.direction === 'down')
  const fixed = layout === 'right' || fixedDown
  const direction = fixedDown ? 'down' : 'right'
  const maxPanes = typeof options.maxPanes === 'number' && options.maxPanes >= 1 ? Math.floor(options.maxPanes) : 6
  const closePane = closePolicy(options.closePane)
  const closeDelay = typeof options.closeDelay === 'number' ? options.closeDelay : 3
  const tailLines = typeof options.tailLines === 'number' ? options.tailLines : 200
  const maxReturnLines = typeof options.maxReturnLines === 'number' && options.maxReturnLines >= 0 ? Math.floor(options.maxReturnLines) : 60
  const viewer = options.viewer === 'plain' ? 'plain' : 'textual'
  const leanProfile = options.leanProfile !== false
  const leanProfileName = typeof options.leanProfileName === 'string' && options.leanProfileName.trim() ? options.leanProfileName.trim() : 'lean'
  const runsDir = typeof options.ccxRunsDir === 'string' && options.ccxRunsDir.trim() ? options.ccxRunsDir.trim() : undefined
  // auto: outside herdr, a run is followed in a Claude Code pane (runpane.tsx); off: run as asked.
  // Every run is followed to its end either way, for its page (run.html).
  const claudePane = options.claudePane === 'auto'
  runPane(on, { closePane, closeDelay })
  let n = 0

  on('tool.call', { tool: 'Bash' }, async ($, e, next) => {
    if (!isDelegation(e.command)) return next(e)
    n += 1
    const id = `${Date.now().toString(36)}${n}`
    $.ui.toast(claudePane ? 'Delegation: opening a pane' : 'Delegation: opening a herdr pane')
    let tmpdir: string | undefined, home: string | undefined
    try { tmpdir = await $.env.get('TMPDIR') } catch { /* /tmp */ }
    try { home = await $.env.get('HOME') } catch { /* not followed */ }
    const runDir = runDirPath(home, id)
    const script = buildWrapper(e.command, {
      direction, layout: fixed ? 'fixed' : 'tile', maxPanes, closePane, closeDelay, tailLines, maxReturnLines, viewer, leanProfile, leanProfileName, runsDir, root: $.plugin.root, id,
      claudePane, runDir,
    })
    // Followed by what the script writes in $dir/host: claude (a Claude Code pane), herdr, or none (bare)
    if (runDir) await trackRun($, { id, dir: runDir, label: paneLabel(e.command) }).catch(() => {})
    // The script goes to a file written here, so the call the harness shows (and quotes in the
    // notice of a background call) is the two-line stub. If the write fails or the file is not
    // there, the call carries the whole script, as before 0.7.0.
    let command = script
    try {
      const path = scriptPath(tmpdir, id)
      await $.fs.write(path, script)
      if (await $.fs.exists(path)) command = stubFor(path)
    } catch { /* keep the inline script */ }
    // A foreground call may wait on a long Codex run, so one with no timeout gets the
    // longest wait the Bash tool allows. A timeout the caller set is kept, so a call meant
    // to give up early still does; the pane keeps running either way.
    const timeout = e.run_in_background || typeof e.timeout === 'number' ? e.timeout : FOREGROUND_MAX_MS
    const ran = await next({ ...e, command, timeout })
    if (!runDir) return ran
    // Never throws: the .catch below would run the delegation a second time
    try {
      await callEnded($, id, ran.result === undefined)
      if (ran.result === undefined) return ran
      const interrupted = Boolean((ran.result as { interrupted?: boolean }).interrupted)
      const end: CallEnd = e.run_in_background ? 'background' : interrupted ? 'interrupted' : 'done'
      const note = await pageNote($, id, end, runDir)
      return note ? { ...ran, context: [...(ran.context ?? []), note] } : ran
    } catch {
      return ran
    }
  }).catch((_$, e, next) => next(e)) // a failure here must never block the delegation
}
