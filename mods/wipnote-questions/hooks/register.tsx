import { atom, read, update } from 'claude-code'
import type { EngineInterface, Register, Timer } from 'claude-code'

import type { Listing } from '../types'
import { answerArgv, countLine, failReason, firstLine, listArgv, parseQuestions, rowLine } from './model'

const PANE = 'wipnote-questions'
// Change the tag when Listing's fields change: it declines what an earlier version stored.
const listing = atom({ plugin: 'wipnote-questions', key: 'listing' } as const, null, { shape: 'listing-1' })
const answered = atom({ plugin: 'wipnote-questions', key: 'answered' } as const, null)

const POLL_MS = 5000
let timer: Timer | undefined

function startPoll($: EngineInterface) {
  timer ??= $.clock.every(POLL_MS, () => void refresh($).catch(() => undefined))
}

function stopPoll() {
  timer?.cancel()
  timer = undefined
}

async function refresh($: EngineInterface) {
  const ran = await $.process.run(listArgv(), { timeoutMs: 5000 }).catch(() => undefined)
  const at = await $.clock.now()
  let next: Listing
  if (ran === undefined) next = { state: 'failed', reason: 'wipnote unavailable', at }
  else if (ran.exitCode !== 0) next = { state: 'failed', reason: failReason(ran.stderr, ran.exitCode), at }
  else {
    const rows = parseQuestions(ran.stdout)
    next = rows === undefined
      ? { state: 'failed', reason: 'unreadable output from wipnote question list', at }
      : { state: 'loaded', rows, at }
  }
  await update($, listing, () => next)
}

/** Runs `question answer`, toasts its output, and drops the row at once; the next poll confirms. */
async function answer($: EngineInterface, questionId: string, key: string) {
  const ran = await $.process.run(answerArgv(questionId, key), { timeoutMs: 15000 }).catch(() => undefined)
  const out = ran === undefined ? 'wipnote unavailable' : firstLine(ran.exitCode === 0 ? ran.stdout : ran.stderr || ran.stdout)
  const line = out !== '' ? out : `${questionId}: ${key}`
  $.ui.toast(line)
  await update($, answered, () => line)
  if (ran?.exitCode === 0) {
    await update($, listing, l => (l?.state === 'loaded' ? { ...l, rows: l.rows.filter(r => r.id !== questionId) } : l))
  }
  await refresh($)
}

async function show($: EngineInterface) {
  await refresh($)
  const r = await $.ui.open({ id: PANE, title: 'Questions' })
  if (r.isPlaced) startPoll($)
  else $.ui.toast(`Questions pane is waiting (${r.reason}). Type /questions-pane to show it.`)
}

export const register: Register = on => {
  on('session.start', async ($, e, next) => {
    await $.command.register({ name: 'questions-pane', description: 'Show the open wipnote questions in a side pane' })
    await $.command.register({
      name: 'answer-question',
      description: 'Answer an open wipnote question with one of its option keys',
      argumentHint: '<question-id> <option-key>',
    })
    return next(e)
  })

  on('ui.close', {}, async ($, e, next) => {
    if (e.id === PANE) stopPoll()
    return next(e)
  })

  on('session.end', async ($, e, next) => {
    stopPoll()
    return next(e)
  })

  on('command.run', { command: 'questions-pane' }, async $ => {
    await show($)
    return { text: 'Questions pane.' }
  })

  on('command.run', { command: 'answer-question' }, async ($, e) => {
    const [id, key] = e.args.trim().split(/\s+/)
    if (id === undefined || id === '' || key === undefined) return { text: 'Usage: /answer-question <question-id> <option-key>.' }
    await answer($, id, key)
    return { text: `Answered ${id}: ${key}.` }
  })

  on('ui.render', { component: 'Pane', requestId: PANE }, async ($, e) => {
    startPoll($)
    const { Box, Text } = $.ui.resolve(e)
    const [l, last] = await Promise.all([read($, listing), read($, answered)])
    if (l === null) return <Text dimColor>Loading questions…</Text>
    if (l.state === 'failed') return <Text color="red" wrap="truncate-end">{l.reason}</Text>
    return (
      <Box flexDirection="column">
        <Text bold>{countLine(l.rows)}</Text>
        {last !== null && <Text dimColor wrap="truncate-end">{last}</Text>}
        {l.rows.map(r => (
          <Box key={r.id} flexDirection="column" marginTop={1}>
            <Text wrap="truncate-end">{r.text}</Text>
            <Text dimColor wrap="truncate-end">{`${r.id} · ${r.itemTitle !== '' ? `${r.itemTitle} · ` : ''}${rowLine(r)}`}</Text>
            {r.options.map(o => (
              <Text key={o.key} wrap="truncate-end">{`${o.key === r.recommended ? '★' : '○'} ${o.key}. ${o.label}${o.tradeoff !== undefined ? ` — ${o.tradeoff}` : ''}`}</Text>
            ))}
          </Box>
        ))}
      </Box>
    )
  })
}
