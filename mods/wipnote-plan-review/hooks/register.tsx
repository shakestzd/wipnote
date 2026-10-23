import { atom, read, update } from 'claude-code'
import type { EngineInterface, Register } from 'claude-code'

import type { Review } from '../types'
import { MARK, detailLine, failReason, parseSlices, planCallIn, tallyLine } from './model'

const PANE = 'wipnote-plan-review'
const planId = atom({ plugin: 'wipnote-plan-review', key: 'planId' } as const, null)
// Change the tag when Review's fields change: it declines what an earlier
// version stored, which outlives a reload.
const review = atom({ plugin: 'wipnote-plan-review', key: 'review' } as const, null, { shape: 'review-1' })

const POLL_MS = 5000

async function refresh($: EngineInterface, id: string) {
  const ran = await $.process.run(['wipnote', 'plan', 'slices', id], { timeoutMs: 5000 }).catch(() => undefined)
  const at = await $.clock.now()
  let next: Review
  if (ran === undefined) {
    next = { state: 'failed', planId: id, reason: 'wipnote unavailable', at }
  } else if (ran.exitCode !== 0) {
    next = { state: 'failed', planId: id, reason: failReason(ran.stderr, ran.exitCode), at }
  } else {
    const parsed = parseSlices(ran.stdout)
    next = parsed === undefined
      ? { state: 'failed', planId: id, reason: 'unreadable output from wipnote plan slices', at }
      : { state: 'loaded', planId: id, ...parsed, at }
  }
  // A plan switched while this one loaded keeps the newer one.
  if ((await read($, planId)) === id) await update($, review, () => next)
}

async function show($: EngineInterface, id: string) {
  await update($, planId, () => id)
  await refresh($, id)
  await $.ui.open({ id: PANE, title: 'Plan review' })
}

export const register: Register = on => {
  on('session.start', async ($, e, next) => {
    await $.command.register({
      name: 'plan-review-pane',
      description: 'Show a wipnote plan\'s slices and their approval state in a side pane',
      argumentHint: '<plan-id>',
    })
    $.clock.every(POLL_MS, () => {
      void (async () => {
        const id = await read($, planId)
        if (id !== null) await refresh($, id)
      })().catch(() => undefined)
    })
    return next(e)
  })

  on('command.run', { command: 'plan-review-pane' }, async ($, e) => {
    const arg = e.args.trim()
    const id = arg !== '' ? arg : await read($, planId)
    if (id === null) return { text: 'Usage: /plan-review-pane <plan-id> (wipnote plan list shows the ids).' }
    await show($, id)
    return { text: `Plan review pane: ${id}.` }
  })

  on('tool.call', { tool: 'Bash' }, async ($, e, next) => {
    const ran = await next(e)
    const call = planCallIn(e.command)
    if (call !== undefined) {
      void (async () => {
        if (call.opens) await show($, call.planId)
        else if ((await read($, planId)) === call.planId) await refresh($, call.planId)
      })().catch(() => undefined)
    }
    return ran
  }).catch(($, e, next) => next(e))

  on('ui.render', { component: 'Pane', requestId: PANE }, async ($, e) => {
    const { Box, Text } = $.ui.resolve(e)
    const r = await read($, review)
    if (r === null) {
      return (
        <Box flexDirection="column">
          <Text dimColor>No plan in view.</Text>
          <Text dimColor>Run /plan-review-pane &lt;plan-id&gt;.</Text>
        </Box>
      )
    }
    if (r.state === 'failed') {
      return (
        <Box flexDirection="column">
          <Text bold>{r.planId}</Text>
          <Text color="red" wrap="truncate-end">{r.reason}</Text>
        </Box>
      )
    }
    // Every slice goes in the tree: the pane's body scrolls a taller one.
    return (
      <Box flexDirection="column">
        <Text bold wrap="truncate-end">{r.title !== '' ? r.title : r.planId}</Text>
        <Text dimColor wrap="truncate-end">{`${r.planId} · ${r.status} · ${tallyLine(r.slices)}`}</Text>
        {r.slices.map(s => (
          <Box key={`s${s.num}`} flexDirection="column">
            <Text wrap="truncate-end">
              <Text color={MARK[s.approval].color}>{MARK[s.approval].glyph}</Text>
              <Text>{` ${s.num}. ${s.title}`}</Text>
            </Text>
            <Text dimColor wrap="truncate-end">{`   ${MARK[s.approval].label} · ${detailLine(s)}`}</Text>
          </Box>
        ))}
      </Box>
    )
  })
}
