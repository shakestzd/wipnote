// wipnote-visuals: puts a receipt line (with the writing score when /plainspeak
// is on) under each main-loop turn, and sends a one-line writing note with the
// next prompt when a reply broke a rule (turnhooks.ts).
//
// The mod draws nothing inside a reply. Steps, options and callouts are
// lulawrite blocks, which the lulawrite plugin checks and draws. Two plugins
// that each draw a whole reply fight over it, so this one leaves replies alone.
import { atom, read, update } from 'claude-code'
import type { EngineInterface, Register } from 'claude-code'

import { registerTurn } from './turnhooks'

// Same state as turnhooks' `receipt`: the scan needs the atom declared in the file that reads it.
const receipt = atom({ plugin: 'wipnote-visuals', key: 'receipt' } as const, true)

const USAGE = 'Usage: /visuals receipt on|off (no argument reports the state).'

export type VisualsArgs = { kind: 'set'; wanted: boolean } | { kind: 'status' } | { kind: 'usage' }

/** `/visuals [receipt [on|off]]`: anything else is a usage message and changes nothing. */
export function parseVisualsArgs(args: string): VisualsArgs {
  const words = String(args ?? '').trim().toLowerCase().split(/\s+/).filter(Boolean)
  if (words.length === 0) return { kind: 'status' }
  if (words[0] !== 'receipt' || words.length > 2) return { kind: 'usage' }
  const flag = words[1]
  if (flag === undefined) return { kind: 'status' }
  if (flag !== 'on' && flag !== 'off') return { kind: 'usage' }
  return { kind: 'set', wanted: flag === 'on' }
}

async function registerCommands($: EngineInterface): Promise<void> {
  await $.command.register({
    name: 'visuals',
    description: 'Show the turn receipt under replies (receipt on|off); no argument reports the state',
    argumentHint: 'receipt on|receipt off',
  })
  await $.command.register({
    name: 'plainspeak',
    description: 'Writing check: strict or 80 (fewer rules) add a score to the receipt and a one-line note to the next prompt when a reply has AI tells or long sentences; off (default) does neither',
    argumentHint: 'strict|80|off',
  })
}

const onOff = (v: boolean) => (v ? 'on' : 'off')

async function runVisuals($: EngineInterface, args: string): Promise<string> {
  const a = parseVisualsArgs(args)
  if (a.kind === 'usage') return USAGE
  if (a.kind === 'status') return `Receipt: ${onOff(await read($, receipt))}.`
  await update($, receipt, () => a.wanted)
  return `Receipt: ${onOff(a.wanted)}.`
}

export const register: Register = on => {
  on('session.start', async ($, e, next) => {
    await registerCommands($)
    return next(e)
  })

  on('command.run', { command: 'visuals' }, async ($, e) => ({ text: await runVisuals($, e.args) }))

  registerTurn(on)
}
