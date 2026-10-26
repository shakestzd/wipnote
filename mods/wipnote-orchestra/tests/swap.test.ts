import type { On } from 'claude-code'
import { mock, test, expect } from 'claude-code/testing'
import type { Engine } from 'claude-code/testing'

import { swapSeen, swapStarted } from '../hooks/swap'

// A managed (prepend-tier) plugin can continue `classic.*` events past every user-tier plugin,
// so the mod hooks none: a /clear or /resume is seen as a changed session id alone.

const ID = 'feat-1a2b3c4d'
const SHOW = JSON.stringify({ id: ID, title: 'Swapped in', status: 'in-progress', steps: [{ completed: false }] })
const REF = `Done.\n\`\`\`wipnote-ref\n${ID}\n\`\`\``

const BAND = {
  plugin: 'wipnote-orchestra',
  component: 'PromptHint',
  props: { isDraft: false, isWorking: false, hint: '? for shortcuts' },
} as const

// The kit has timers but the mod's lib has no DOM or Node typings.
declare const setTimeout: (run: () => void, ms: number) => unknown
const settle = async () => {
  for (let i = 0; i < 20; i++) await new Promise(resolve => setTimeout(() => resolve(undefined), 5))
}

type World = { sid: string; claim: string; messages: unknown[]; whos: number; reads: number }

/** The engine beneath the mod: the session id, `wipnote who`/`show`, the transcript, a clock. */
function engine(on: On) {
  const w: World = { sid: 'session-1', claim: '', messages: [], whos: 0, reads: 0 }
  const clock = mock.clock(on, { now: 1000 })
  const ok = (stdout: string) => ({ value: { exitCode: 0, stdout, stderr: '', isStdoutTruncated: false, isStderrTruncated: false } })
  on('session.id', async () => ({ value: w.sid }))
  on('session.start', (_$, e) => ({ cwd: e.cwd }))
  on('command.register', (_$, e) => ({ value: { command: e.name } }))
  on('process.run', async (_$, e) => {
    if (e.argv[1] === 'who') {
      w.whos += 1
      return ok(JSON.stringify({ work_item: w.claim }))
    }
    if (e.argv[2] === 'show') return ok(SHOW)
    return { value: { exitCode: 1, stdout: '', stderr: '', isStdoutTruncated: false, isStderrTruncated: false } }
  })
  on('session.messages', async () => {
    w.reads += 1
    return { value: w.messages } as never
  })
  return { w, clock }
}

/** The conversation another one replaces: its claim and its reply's fence. */
function swapTo(w: World, sid: string) {
  w.sid = sid
  w.claim = ID
  w.messages = [{ role: 'assistant', text: REF }]
}

async function bandShows($: Engine, text: RegExp) {
  const ui = await $.ui.mount({ ...BAND, surface: 'terminal' })
  const found = await ui.find({ type: 'Text', text })
  await ui.unmount()
  return found !== undefined
}

test('a /clear rehydrates the band from session.end alone, with no classic event', async ($, on) => {
  const { w } = engine(on)
  // /clear ends the conversation; the process goes on under a new id and no session.start fires.
  on('session.end', (_$, e) => {
    if (e.reason === 'clear') swapTo(w, 'session-2')
    return { sessionId: e.sessionId }
  })
  await $.session.start({ cwd: '/tmp', surface: 'terminal', isInteractive: true })
  await settle()
  const [whos, reads] = [w.whos, w.reads]
  expect(await bandShows($, new RegExp(ID))).toBe(false)

  await $.session.end({ reason: 'clear', sessionId: 'session-1', resume: { id: 'session-1' } })
  await settle()
  expect(w.whos).toBe(whos + 1)
  expect(w.reads).toBe(reads + 1)
  expect(await bandShows($, new RegExp(ID))).toBe(true)
})

test('a resume is caught by the changed session id within a tick, with no classic event', async ($, on) => {
  const { w, clock } = engine(on)
  await $.session.start({ cwd: '/tmp', surface: 'terminal', isInteractive: true })
  await settle()
  const reads = w.reads
  await clock.advance(3000)
  expect(w.reads).toBe(reads) // the same conversation: nothing to rehydrate

  swapTo(w, 'session-9') // session.end was continued past as well
  await clock.advance(1000)
  await settle()
  expect(w.reads).toBe(reads + 1)
  expect(await bandShows($, new RegExp(ID))).toBe(true)

  await clock.advance(5000)
  await settle()
  expect(w.reads).toBe(reads + 1) // once per swap, not once per tick
})

test('one /clear seen by session.end and the tick rehydrates once; classic.SessionStart adds nothing', async ($, on) => {
  const { w, clock } = engine(on)
  on('session.end', (_$, e) => {
    if (e.reason === 'clear') swapTo(w, 'session-2')
    return { sessionId: e.sessionId }
  })
  on('classic.SessionStart', async () => ({}))
  await $.session.start({ cwd: '/tmp', surface: 'terminal', isInteractive: true })
  await settle()
  const [whos, reads] = [w.whos, w.reads]

  await $.session.end({ reason: 'clear', sessionId: 'session-1', resume: { id: 'session-1' } })
  await clock.advance(2000)
  await $.classic.SessionStart({ source: 'clear' })
  await settle()
  expect(w.reads).toBe(reads + 1)
  expect(w.whos).toBe(whos + 1)
  // The tick's card refresh has run by now: the band shows the item by its title.
  expect(await bandShows($, /Swapped in/)).toBe(true)
})

test('a classic.SessionStart alone rehydrates nothing; the changed id does, on the next tick', async ($, on) => {
  const { w, clock } = engine(on)
  on('classic.SessionStart', async () => ({}))
  await $.session.start({ cwd: '/tmp', surface: 'terminal', isInteractive: true })
  await settle()
  const reads = w.reads

  swapTo(w, 'session-3')
  await $.classic.SessionStart({ source: 'clear' })
  await settle()
  expect(w.reads).toBe(reads) // the mod hooks no classic.* event
  await clock.advance(1000)
  await settle()
  expect(w.reads).toBe(reads + 1)
  expect(await bandShows($, new RegExp(ID))).toBe(true)
  await clock.advance(3000)
  await settle()
  expect(w.reads).toBe(reads + 1)
})

test('a main-loop agent spawn sets the permission mode, and classic events never change it', async ($, on) => {
  mock.clock(on, { now: 1000 })
  on('agent.spawn', () => ({ model: 'claude-haiku-4-5-20251001', agentId: 'a1' }) as never)
  on('classic.UserPromptSubmit', async () => ({}))
  const hint = '1 shell · ← 1 agent'
  const props = { isDraft: false, isWorking: true, hint }
  await $.agent.spawn({ subagentType: 'Explore', description: 'Look', prompt: 'look', permissionMode: 'plan' } as never)
  await $.classic.UserPromptSubmit({ prompt: 'go', permission_mode: 'auto' } as never)
  const ui = await $.ui.mount({ ...BAND, props, surface: 'terminal' })
  const band = await ui.find({ type: 'Box', key: 'band' })
  await ui.unmount()
  expect(band?.props.marginLeft).toBe(-17) // the spawn's plan pill, not the classic event's auto
})

test('the swap tracker: the first id is no swap, each move is one, once', async () => {
  const s: Parameters<typeof swapSeen>[0] = {}
  expect(swapSeen(s, 'a')).toBe(false) // first sight (session.start never ran)
  swapStarted(s, 'a')
  expect(swapSeen(s, undefined)).toBe(false) // an unreadable id is no swap
  expect(swapSeen(s, 'b')).toBe(true)
  expect(swapSeen(s, 'b')).toBe(false)
  expect(swapSeen(s, 'c')).toBe(true)
  swapStarted(s, 'd')
  expect(swapSeen(s, 'd')).toBe(false)
})
