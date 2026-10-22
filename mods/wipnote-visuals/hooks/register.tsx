// wipnote-visuals: draws wip:compare (and older wip:steps / wip:callout) fences
// in replies, and puts a receipt line under each main-loop turn.
// Same flow as wipnote-orchestra's wipnote-ref cards: a one-section rule asks
// the model for the fences (only while the mod is on), and a ui.render hook on
// AssistantMessage draws them. The stored message keeps the plain fence.
// The receipt costs the model nothing: tool.call collects, turn.complete shows.
import { atom, read, update } from 'claude-code'
import type { Register } from 'claude-code'

import { RULE, splitBlocks } from './blocks'
import { renderBlock } from './components'
import { emptyTurn, noteCall, receiptLine } from './receipt'
import type { Turn } from './receipt'

const enabled = atom({ plugin: 'wipnote-visuals', key: 'enabled' } as const, true)
const receipt = atom({ plugin: 'wipnote-visuals', key: 'receipt' } as const, true)
const turn = atom({ plugin: 'wipnote-visuals', key: 'turn' } as const, emptyTurn())

/** `/visuals [on|off|receipt on|receipt off]`: no argument toggles the blocks. */
export function parseVisualsArgs(args: string): { which: 'blocks' | 'receipt'; wanted?: boolean } {
  const words = String(args ?? '').trim().toLowerCase().split(/\s+/).filter(Boolean)
  const which = words[0] === 'receipt' ? 'receipt' : 'blocks'
  const flag = which === 'receipt' ? words[1] : words[0]
  return { which, wanted: flag === 'on' ? true : flag === 'off' ? false : undefined }
}

export const register: Register = on => {
  on('session.start', async ($, e, next) => {
    await $.command.register({
      name: 'visuals',
      description: 'Draw wip:* blocks as visuals (on|off); show the turn receipt under replies (receipt on|off)',
      argumentHint: 'on|off|receipt on|receipt off',
    })
    return next(e)
  })

  // The state scan needs each atom named literally at its read and update.
  on('command.run', { command: 'visuals' }, async ($, e) => {
    const { which, wanted } = parseVisualsArgs(e.args)
    if (which === 'receipt') {
      const next = wanted ?? !(await read($, receipt))
      await update($, receipt, () => next)
      return { text: `Receipt: ${next ? 'on' : 'off'}.` }
    }
    const next = wanted ?? !(await read($, enabled))
    await update($, enabled, () => next)
    return { text: `Visuals: ${next ? 'on' : 'off'}.` }
  })

  on('prompt.compose', async ($, e, next) => {
    const r = await next(e)
    if (!(await read($, enabled))) return r
    return { sections: [...r.sections, { id: 'wipnote-visuals:rule', text: RULE, scope: 'session' }] }
  })

  // The receipt reads the main loop only; a subagent's calls are its own turn.
  on('tool.call', async ($, e, next) => {
    const r = await next(e)
    if (e.agentId === undefined && (await read($, receipt))) {
      await update($, turn, (t: Turn) => noteCall(t, e as never, r))
    }
    return r
  }).catch(($, e, next) => next(e))

  on('turn.complete', async ($, e, next) => {
    const r = await next(e)
    if (e.agentId !== undefined || e.isAborted) return r
    const t = await read($, turn)
    await update($, turn, () => emptyTurn())
    if (!(await read($, receipt))) return r
    const line = receiptLine(t, e.answer, e.durationMs)
    return line === null ? r : { ...r, text: line }
  }).catch(($, e, next) => next(e))

  on('ui.render', { component: 'AssistantMessage' }, async ($, e, next) => {
    if (!(await read($, enabled))) return next(e)
    const segments = splitBlocks(e.props.text)
    if (!segments.some(s => s.kind === 'block')) return next(e)
    const ui = $.ui.resolve(e)
    const { Box, Markdown } = ui
    return (
      <Box flexDirection="column">
        {segments.map((s, i) =>
          s.kind === 'text' ? <Markdown key={`t${i}`} text={s.text} /> : <Box key={`b${i}`}>{renderBlock(ui, s.block)}</Box>,
        )}
      </Box>
    )
  })
}
