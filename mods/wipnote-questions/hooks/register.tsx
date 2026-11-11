import { atom, read, update } from 'claude-code'
import type { EngineInterface, Register, Timer } from 'claude-code'

import type { Answer, Listing } from '../types'
import { answerArgv, countLine, failReason, fullOutput, listArgv, parseAnswerArgs, parseQuestions, rowLine, shortID, toastLine } from './model'

const PANE = 'wipnote-questions'
// Change the tag when a stored shape changes: it declines what an earlier version stored.
const listing = atom({ plugin: 'wipnote-questions', key: 'listing' } as const, null, { shape: 'listing-2' })
const answered = atom({ plugin: 'wipnote-questions', key: 'answered' } as const, null, { shape: 'answered-2' })

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

/**
 * Runs `question answer`, toasts the overturn or reopen line when there is one (else the first line),
 * shows the whole output in the pane, and drops the row at once; the next poll confirms.
 */
async function answer($: EngineInterface, questionId: string, a: Answer) {
  const ran = await $.process.run(answerArgv(questionId, a), { timeoutMs: 15000 }).catch(() => undefined)
  const out = ran === undefined ? 'wipnote unavailable' : ran.exitCode === 0 ? ran.stdout : ran.stderr || ran.stdout
  const fallback = `${questionId}: ${'key' in a ? a.key : a.text}`
  const line = toastLine(out)
  const lines = fullOutput(out)
  $.ui.toast(line !== '' ? line : fallback)
  await update($, answered, () => (lines.length > 0 ? lines : [fallback]))
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

type On = Parameters<Register>[0]

/** session.start registers the two commands; closing the pane or the session stops the poll. */
function onLifecycle(on: On) {
  on('session.start', async ($, e, next) => {
    await $.command.register({ name: 'questions-pane', description: 'Show the open wipnote questions in a side pane' })
    await $.command.register({
      name: 'answer-question',
      description: 'Answer an open wipnote question with an option key, or --text for a free-text question',
      argumentHint: '<question-id> <option-key> | <question-id> --text <reply>',
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
}

/** /questions-pane and /answer-question. */
function onCommands(on: On) {
  on('command.run', { command: 'questions-pane' }, async $ => {
    await show($)
    return { text: 'Questions pane.' }
  })

  on('command.run', { command: 'answer-question' }, async ($, e) => {
    const parsed = parseAnswerArgs(e.args)
    if (parsed === undefined) return { text: 'Usage: /answer-question <question-id> <option-key>, or <question-id> --text <reply>.' }
    await answer($, parsed.id, parsed.answer)
    return { text: `Answered ${parsed.id}.` }
  })
}

/** The pane: the count, the last answer's output, and each open question with its options. */
function onRender(on: On) {
  on('ui.render', { component: 'Pane', requestId: PANE }, async ($, e) => {
    startPoll($)
    const { Box, Text } = $.ui.resolve(e)
    const [l, last] = await Promise.all([read($, listing), read($, answered)])
    if (l === null) return <Text dimColor>Loading questions…</Text>
    if (l.state === 'failed') return <Text color="red" wrap="truncate-end">{l.reason}</Text>
    // Every line names a theme key: the pane is painted in Claude Code's
    // theme, and a Text with no colour takes the terminal's own foreground,
    // which is unreadable when the two differ (light theme, dark terminal).
    return (
      <Box flexDirection="column">
        <Text bold color="text">{countLine(l.rows)}</Text>
        {last?.map((line, i) => (
          <Text key={i} color="subtle" wrap="wrap">{line}</Text>
        ))}
        {l.rows.map(r => (
          <Box key={r.id} flexDirection="column" marginTop={1}>
            <Text bold color={r.blocking ? 'warning' : 'text'} wrap="wrap">{r.text}</Text>
            <Text color="subtle" wrap="truncate-end">{`${r.id} · ${r.itemTitle !== '' ? `${r.itemTitle} · ` : ''}${rowLine(r)}`}</Text>
            {r.freeText && <Text color="suggestion" wrap="truncate-end">{`answer: /answer-question ${shortID(r.id)} --text <reply>`}</Text>}
            {r.options.map(o => (
              <Text key={o.key} color={o.key === r.recommended ? 'success' : 'text'} wrap="wrap">{`${o.key === r.recommended ? '★' : '○'} ${o.key}. ${o.label}${o.tradeoff !== undefined ? ` — ${o.tradeoff}` : ''}`}</Text>
            ))}
            {!r.freeText && <Text color="suggestion" wrap="truncate-end">{`answer: /answer-question ${shortID(r.id)} <key>`}</Text>}
          </Box>
        ))}
      </Box>
    )
  })
}

export const register: Register = on => {
  onLifecycle(on)
  onCommands(on)
  onRender(on)
}
