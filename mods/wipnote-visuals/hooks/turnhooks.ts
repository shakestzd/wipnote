// The per-turn hooks: the receipt (tool.call collects, turn.complete shows) and
// the writing check (lintReply on the finished answer; the score beside the
// receipt, and a one-line note for the next prompt when the reply broke a rule).
//
// The note travels as `prompt.submit` context, not as a system-prompt section:
// a section that changes between turns would break the prompt cache on every
// request, while context rides on the user message, which is new anyway. A
// clean reply sends nothing.
import { atom, read, update } from 'claude-code'
import type { EngineInterface, Register } from 'claude-code'

import { feedbackLine, lintReply } from './lint'
import type { Mode } from './lint'
import { emptyTurn, noteCall, receiptLine } from './receipt'
import type { Turn } from './receipt'

type On = Parameters<Register>[0]

const receipt = atom({ plugin: 'wipnote-visuals', key: 'receipt' } as const, true)
const turn = atom({ plugin: 'wipnote-visuals', key: 'turn' } as const, emptyTurn())
const plainspeak = atom({ plugin: 'wipnote-visuals', key: 'plainspeak' } as const, 'off')
const lintNote = atom({ plugin: 'wipnote-visuals', key: 'lintNote' } as const, '')

const MODES: readonly Mode[] = ['strict', '80', 'off']

/** `/plainspeak strict|80|off`; no argument reports the mode. */
export function parseMode(args: string): Mode | undefined {
  const w = String(args ?? '').trim().toLowerCase()
  return (MODES as readonly string[]).includes(w) ? (w as Mode) : undefined
}

/**
 * The line under the answer: receipt, then the lint score. The score alone
 * shows only when the reply broke a rule, so a clean tool-free turn stays quiet.
 */
export function turnLine(t: Turn, answer: string, durationMs: number): string | null {
  const line = receiptLine(t, answer, durationMs)
  const report = lintReply(answer, { mode: 'strict' })
  if (line === null) return report.findings.length > 0 ? report.score : null
  const [first, ...rest] = line.split('\n')
  return [`${first} · ${report.score}`, ...rest].join('\n')
}

async function completeTurn($: EngineInterface, e: { answer: string; durationMs: number }, text: string): Promise<string> {
  const t = await read($, turn)
  await update($, turn, () => emptyTurn())
  const mode = (await read($, plainspeak)) as Mode
  await update($, lintNote, () => (mode === 'off' ? '' : feedbackLine(lintReply(e.answer, { mode }))))
  if (!(await read($, receipt))) return text
  return turnLine(t, e.answer, e.durationMs) ?? text
}

export function registerTurn(on: On): void {
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
    return { ...r, text: await completeTurn($, e, r.text) }
  }).catch(($, e, next) => next(e))

  on('prompt.submit', async ($, e, next) => {
    const note = await read($, lintNote)
    if (note === '' || e.origin !== undefined) return next(e)
    return next({ ...e, context: [...(e.context ?? []), note] })
  }).catch(($, e, next) => next(e))

  on('command.run', { command: 'plainspeak' }, async ($, e) => {
    const wanted = parseMode(e.args)
    if (wanted === undefined) return { text: `Plainspeak: ${await read($, plainspeak)} (strict|80|off).` }
    await update($, plainspeak, () => wanted)
    if (wanted === 'off') await update($, lintNote, () => '')
    return { text: `Plainspeak: ${wanted}.` }
  })
}
