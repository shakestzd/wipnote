import type { Register } from 'claude-code'
import { buildWrapper, isDelegation, scriptPath, stubFor, type ClosePolicy } from './wrap.ts'

const FOREGROUND_MAX_MS = 600000

const closePolicy = (v: unknown): ClosePolicy => (v === 'success' || v === 'never' ? v : 'always')

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
  let n = 0

  on('tool.call', { tool: 'Bash' }, async ($, e, next) => {
    if (!isDelegation(e.command)) return next(e)
    n += 1
    const id = `${Date.now().toString(36)}${n}`
    $.ui.toast('Delegation: opening a herdr pane')
    const script = buildWrapper(e.command, {
      direction, layout: fixed ? 'fixed' : 'tile', maxPanes, closePane, closeDelay, tailLines, maxReturnLines, viewer, leanProfile, leanProfileName, runsDir, root: $.plugin.root, id,
    })
    // The script goes to a file written here, so the call the harness shows (and quotes in the
    // notice of a background call) is the two-line stub. If the write fails or the file is not
    // there, the call carries the whole script, as before 0.7.0.
    let command = script
    try {
      const path = scriptPath(await $.env.get('TMPDIR'), id)
      await $.fs.write(path, script)
      if (await $.fs.exists(path)) command = stubFor(path)
    } catch { /* keep the inline script */ }
    // A foreground call may wait on a long Codex run, so one with no timeout gets the
    // longest wait the Bash tool allows. A timeout the caller set is kept, so a call meant
    // to give up early still does; the pane keeps running either way.
    const timeout = e.run_in_background || typeof e.timeout === 'number' ? e.timeout : FOREGROUND_MAX_MS
    return next({ ...e, command, timeout })
  }).catch((_$, e, next) => next(e)) // a failure here must never block the delegation
}
