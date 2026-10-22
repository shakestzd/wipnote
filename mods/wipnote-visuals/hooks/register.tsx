// wipnote-visuals: draws wip:compare (and older wip:steps / wip:callout) fences
// in replies, puts a receipt line with the writing score under each main-loop
// turn, and sends a one-line writing note with the next prompt when a reply
// broke a rule (turnhooks.ts).
// Same flow as wipnote-orchestra's wipnote-ref cards: a one-section rule asks
// the model for the fences (only while the mod is on), and a ui.render hook on
// AssistantMessage draws them. The stored message keeps the plain fence.
import { atom, read, update } from 'claude-code'
import type { EngineInterface, Register } from 'claude-code'

import { RULE, splitBlocks } from './blocks'
import { renderBlock } from './components'
import { registerTurn } from './turnhooks'

const enabled = atom({ plugin: 'wipnote-visuals', key: 'enabled' } as const, true)
// Same state as turnhooks' `receipt`: the scan needs the atom declared in the file that reads it.
const receipt = atom({ plugin: 'wipnote-visuals', key: 'receipt' } as const, true)

/** `/visuals [on|off|receipt on|receipt off]`: no argument toggles the blocks. */
export function parseVisualsArgs(args: string): { which: 'blocks' | 'receipt'; wanted?: boolean } {
  const words = String(args ?? '').trim().toLowerCase().split(/\s+/).filter(Boolean)
  const which = words[0] === 'receipt' ? 'receipt' : 'blocks'
  const flag = which === 'receipt' ? words[1] : words[0]
  return { which, wanted: flag === 'on' ? true : flag === 'off' ? false : undefined }
}

async function registerCommands($: EngineInterface): Promise<void> {
  await $.command.register({
    name: 'visuals',
    description: 'Draw wip:* blocks as visuals (on|off); show the turn receipt and writing score under replies (receipt on|off)',
    argumentHint: 'on|off|receipt on|receipt off',
  })
  await $.command.register({
    name: 'plainspeak',
    description: 'Writing check feedback for the next prompt when a reply has AI tells or long sentences: strict, 80 (fewer rules), off',
    argumentHint: 'strict|80|off',
  })
}

export const register: Register = on => {
  on('session.start', async ($, e, next) => {
    await registerCommands($)
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

  registerTurn(on)

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
