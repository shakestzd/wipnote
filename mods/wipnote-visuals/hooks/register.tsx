// wipnote-visuals: draws wip:compare (and older wip:steps / wip:callout) fences
// in replies, puts a receipt line (with the writing score when /plainspeak is
// on) under each main-loop turn, and sends a one-line writing note with the
// next prompt when a reply broke a rule (turnhooks.ts).
// Same flow as wipnote-orchestra's wipnote-ref cards: a one-section rule asks
// the model for the fences (only while the mod is on), and a ui.render hook on
// AssistantMessage draws them. The stored message keeps the plain fence.
import { atom, read, update } from 'claude-code'
import type { EngineInterface, Register } from 'claude-code'

import { splitBlocks } from './blocks'
import { renderBlock } from './components'
import { registerTurn } from './turnhooks'

const enabled = atom({ plugin: 'wipnote-visuals', key: 'enabled' } as const, true)
// Same state as turnhooks' `receipt`: the scan needs the atom declared in the file that reads it.
const receipt = atom({ plugin: 'wipnote-visuals', key: 'receipt' } as const, true)

const USAGE = 'Usage: /visuals on|off|receipt on|receipt off (no argument reports the state).'

export type VisualsArgs = { kind: 'set'; which: 'blocks' | 'receipt'; wanted: boolean } | { kind: 'status'; which: 'blocks' | 'receipt' | 'both' } | { kind: 'usage' }

/** `/visuals [on|off|receipt [on|off]]`: anything else is a usage message and changes nothing. */
export function parseVisualsArgs(args: string): VisualsArgs {
  const words = String(args ?? '').trim().toLowerCase().split(/\s+/).filter(Boolean)
  if (words.length === 0) return { kind: 'status', which: 'both' }
  const which = words[0] === 'receipt' ? 'receipt' : 'blocks'
  const flag = which === 'receipt' ? words[1] : words[0]
  if (words.length > (which === 'receipt' ? 2 : 1)) return { kind: 'usage' }
  if (flag === undefined) return which === 'receipt' ? { kind: 'status', which } : { kind: 'usage' }
  if (flag !== 'on' && flag !== 'off') return { kind: 'usage' }
  return { kind: 'set', which, wanted: flag === 'on' }
}

async function registerCommands($: EngineInterface): Promise<void> {
  await $.command.register({
    name: 'visuals',
    description: 'Draw wip:* blocks as visuals (on|off); show the turn receipt under replies (receipt on|off); no argument reports the state',
    argumentHint: 'on|off|receipt on|receipt off',
  })
  await $.command.register({
    name: 'plainspeak',
    description: 'Writing check: strict or 80 (fewer rules) add a score to the receipt and a one-line note to the next prompt when a reply has AI tells or long sentences; off (default) does neither',
    argumentHint: 'strict|80|off',
  })
}

const onOff = (v: boolean) => (v ? 'on' : 'off')

/** The state scan needs each atom named literally at its read and update, so both branches are spelled out. */
async function runVisuals($: EngineInterface, args: string): Promise<string> {
  const a = parseVisualsArgs(args)
  if (a.kind === 'usage') return USAGE
  if (a.kind === 'status') {
    const [b, r] = await Promise.all([read($, enabled), read($, receipt)])
    return a.which === 'receipt' ? `Receipt: ${onOff(r)}.` : `Visuals: ${onOff(b)}. Receipt: ${onOff(r)}.`
  }
  if (a.which === 'receipt') {
    await update($, receipt, () => a.wanted)
    return `Receipt: ${onOff(a.wanted)}.`
  }
  await update($, enabled, () => a.wanted)
  return `Visuals: ${onOff(a.wanted)}.`
}

export const register: Register = on => {
  on('session.start', async ($, e, next) => {
    await registerCommands($)
    return next(e)
  })

  on('command.run', { command: 'visuals' }, async ($, e) => ({ text: await runVisuals($, e.args) }))

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
