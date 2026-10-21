// wipnote-visuals: draws wip:steps / wip:compare / wip:callout fences in replies.
// Same flow as wipnote-orchestra's wipnote-ref cards: a one-section rule asks
// the model for the fences (only while the mod is on), and a ui.render hook on
// AssistantMessage draws them. The stored message keeps the plain fence.
import { atom, read, update } from 'claude-code'
import type { Register } from 'claude-code'

import { RULE, splitBlocks } from './blocks'
import { renderBlock } from './components'

const enabled = atom({ plugin: 'wipnote-visuals', key: 'enabled' } as const, true)

export const register: Register = on => {
  on('session.start', async ($, e, next) => {
    await $.command.register({
      name: 'visuals',
      description: 'Draw wip:steps, wip:compare and wip:callout blocks as visuals (on) or as plain text (off)',
      argumentHint: 'on|off',
    })
    return next(e)
  })

  on('command.run', { command: 'visuals' }, async ($, e) => {
    const arg = e.args.trim().toLowerCase()
    const wanted = arg === 'on' ? true : arg === 'off' ? false : !(await read($, enabled))
    await update($, enabled, () => wanted)
    return { text: `Visuals: ${wanted ? 'on' : 'off'}.` }
  })

  on('prompt.compose', async ($, e, next) => {
    const r = await next(e)
    if (!(await read($, enabled))) return r
    return { sections: [...r.sections, { id: 'wipnote-visuals:rule', text: RULE, scope: 'session' }] }
  })

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
